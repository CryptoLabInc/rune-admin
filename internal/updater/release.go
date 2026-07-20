package updater

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultRepository = "CryptoLabInc/rune-console"
	maxArchiveBytes   = 512 << 20
	maxMetadataBytes  = 2 << 20
	metadataTimeout   = 15 * time.Second
)

// LocalSource is used for air-gapped updates. The command layer requires all
// three offline flags together; this type repeats the validation for callers
// outside Cobra.
type LocalSource struct {
	ArchivePath   string
	ChecksumsPath string
	Version       string
}

func (s LocalSource) Resolve(_ context.Context, requested, _, _ string) (Release, error) {
	if s.ArchivePath == "" || s.ChecksumsPath == "" || s.Version == "" {
		return Release{}, errors.New("offline update requires archive, checksums, and version")
	}
	if requested != "" {
		a, err := ParseVersion(requested)
		if err != nil {
			return Release{}, fmt.Errorf("invalid requested version: %w", err)
		}
		b, err := ParseVersion(s.Version)
		if err != nil {
			return Release{}, fmt.Errorf("invalid offline version: %w", err)
		}
		if a.Compare(b) != 0 {
			return Release{}, errors.New("offline version does not match requested version")
		}
	}
	return Release{Version: s.Version, ArchivePath: s.ArchivePath, ChecksumsPath: s.ChecksumsPath}, nil
}

type GitHubSource struct {
	Client     *http.Client
	Repository string
	APIBaseURL string
	WebBaseURL string
}

// LatestVersion resolves only the tag of GitHub's latest stable release. It
// deliberately does not resolve or download any release assets, so callers can
// use it for a cheap UI availability check. GitHub's /releases/latest endpoint
// excludes drafts and prereleases.
func (s GitHubSource) LatestVersion(ctx context.Context) (string, error) {
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: metadataTimeout}
	}
	repository := s.Repository
	if repository == "" {
		repository = defaultRepository
	}
	apiBase := strings.TrimRight(s.APIBaseURL, "/")
	if apiBase == "" {
		apiBase = "https://api.github.com"
	}
	endpoint := fmt.Sprintf("%s/repos/%s/releases/latest", apiBase, repository)
	var metadata struct {
		TagName string `json:"tag_name"`
	}
	if err := getJSON(ctx, client, endpoint, &metadata); err != nil {
		return "", fmt.Errorf("resolve latest GitHub release: %w", err)
	}
	raw := strings.TrimSpace(metadata.TagName)
	version, err := ParseVersion(raw)
	if err != nil || version.String() != raw {
		return "", errors.New("latest GitHub release has a non-canonical version tag")
	}
	return raw, nil
}

func (s GitHubSource) Resolve(ctx context.Context, requested, goos, goarch string) (Release, error) {
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Minute}
	}
	repository := s.Repository
	if repository == "" {
		repository = defaultRepository
	}
	apiBase := strings.TrimRight(s.APIBaseURL, "/")
	if apiBase == "" {
		apiBase = "https://api.github.com"
	}
	webBase := strings.TrimRight(s.WebBaseURL, "/")
	if webBase == "" {
		webBase = "https://github.com"
	}

	version := requested
	if version == "" {
		var err error
		version, err = (GitHubSource{
			Client:     client,
			Repository: repository,
			APIBaseURL: apiBase,
		}).LatestVersion(ctx)
		if err != nil {
			return Release{}, err
		}
	}
	version = strings.TrimSpace(version)
	_, err := ParseVersion(version)
	if err != nil {
		return Release{}, fmt.Errorf("invalid GitHub release version: %w", err)
	}
	if err := ValidatePlatform(goos, goarch); err != nil {
		return Release{}, err
	}

	dir, err := os.MkdirTemp("", "runeconsole-release-")
	if err != nil {
		return Release{}, errors.New("create private release workspace failed")
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		removePrivateTree(dir)
		return Release{}, errors.New("secure release workspace failed")
	}
	cleanup := func() { removePrivateTree(dir) }
	archiveName := fmt.Sprintf("runeconsole_%s_%s_%s.tar.gz", version, goos, goarch)
	archivePath := filepath.Join(dir, archiveName)
	checksumsPath := filepath.Join(dir, "SHA256SUMS")
	base := fmt.Sprintf("%s/%s/releases/download/%s", webBase, repository, url.PathEscape(version))
	if err := downloadFile(ctx, client, base+"/"+url.PathEscape(archiveName), archivePath, maxArchiveBytes); err != nil {
		cleanup()
		return Release{}, fmt.Errorf("download release archive: %w", err)
	}
	if err := downloadFile(ctx, client, base+"/SHA256SUMS", checksumsPath, maxMetadataBytes); err != nil {
		cleanup()
		return Release{}, fmt.Errorf("download release checksums: %w", err)
	}
	return Release{
		Version:       version,
		ArchivePath:   archivePath,
		ChecksumsPath: checksumsPath,
		Cleanup:       cleanup,
	}, nil
}

func ValidatePlatform(goos, goarch string) error {
	if (goos == "linux" && (goarch == "amd64" || goarch == "arm64")) || (goos == "darwin" && goarch == "arm64") {
		return nil
	}
	return fmt.Errorf("release artifacts are unavailable for %s/%s; supported targets are linux/amd64, linux/arm64, and darwin/arm64", goos, goarch)
}

func getJSON(ctx context.Context, client *http.Client, endpoint string, destination any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "runeconsole-updater")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("server returned HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > maxMetadataBytes {
		return errors.New("metadata exceeds size limit")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxMetadataBytes+1))
	if err != nil {
		return err
	}
	if len(body) > maxMetadataBytes {
		return errors.New("metadata exceeds size limit")
	}
	if err := json.Unmarshal(body, destination); err != nil {
		return err
	}
	return nil
}

func downloadFile(ctx context.Context, client *http.Client, endpoint, destination string, limit int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/octet-stream")
	req.Header.Set("User-Agent", "runeconsole-updater")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("server returned HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > limit {
		return errors.New("download exceeds size limit")
	}
	f, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return errors.New("create downloaded release file failed")
	}
	written, copyErr := io.Copy(f, io.LimitReader(resp.Body, limit+1))
	syncErr := f.Sync()
	closeErr := f.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil {
		return fmt.Errorf("write downloaded release file: %w", errors.Join(copyErr, syncErr, closeErr))
	}
	if written > limit {
		return errors.New("download exceeds size limit")
	}
	return nil
}

func VerifyArchiveChecksum(archivePath, checksumsPath string) error {
	dir, _, err := StageVerifiedArchive(archivePath, checksumsPath)
	removePrivateTree(dir)
	return err
}

// StageVerifiedArchive hashes and copies from one already-open archive file
// descriptor into a private directory. Extraction uses that immutable copy,
// so an offline/removable source cannot be swapped after checksum validation.
func StageVerifiedArchive(archivePath, checksumsPath string) (string, string, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return "", "", errors.New("open release archive failed")
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return "", "", errors.New("release archive is not a regular file")
	}
	if info.Size() > maxArchiveBytes {
		f.Close()
		return "", "", errors.New("release archive exceeds size limit")
	}
	want, err := checksumForFile(checksumsPath, filepath.Base(archivePath))
	if err != nil {
		f.Close()
		return "", "", fmt.Errorf("read release checksum: %w", err)
	}
	dir, err := os.MkdirTemp("", "runeconsole-verified-")
	if err != nil {
		f.Close()
		return "", "", errors.New("create private verification workspace failed")
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		f.Close()
		removePrivateTree(dir)
		return "", "", errors.New("secure verification workspace failed")
	}
	destination := filepath.Join(dir, filepath.Base(archivePath))
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		f.Close()
		removePrivateTree(dir)
		return "", "", errors.New("create staged release archive failed")
	}
	h := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(out, h), io.LimitReader(f, maxArchiveBytes+1))
	syncErr := out.Sync()
	outCloseErr := out.Close()
	closeErr := f.Close()
	if copyErr != nil || syncErr != nil || outCloseErr != nil || closeErr != nil {
		removePrivateTree(dir)
		return "", "", fmt.Errorf("stage release archive: %w", errors.Join(copyErr, syncErr, outCloseErr, closeErr))
	}
	if written > maxArchiveBytes || written != info.Size() {
		removePrivateTree(dir)
		return "", "", errors.New("release archive changed or exceeds size limit")
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, want) {
		removePrivateTree(dir)
		return "", "", errors.New("release archive SHA-256 checksum does not match SHA256SUMS")
	}
	return dir, destination, nil
}

func checksumForFile(checksumsPath, name string) (string, error) {
	f, err := os.Open(checksumsPath)
	if err != nil {
		return "", errors.New("open SHA256SUMS failed")
	}
	defer f.Close()
	scanner := bufio.NewScanner(io.LimitReader(f, maxMetadataBytes+1))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 {
			continue
		}
		listed := strings.TrimPrefix(fields[1], "*")
		if listed != name && filepath.Base(listed) != name {
			continue
		}
		if len(fields[0]) != sha256.Size*2 {
			return "", errors.New("invalid SHA-256 checksum")
		}
		if _, err := hex.DecodeString(fields[0]); err != nil {
			return "", errors.New("invalid SHA-256 checksum")
		}
		return strings.ToLower(fields[0]), nil
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", errors.New("release archive is not listed in SHA256SUMS")
}

func ExtractCandidate(archivePath string) (string, string, error) {
	dir, err := os.MkdirTemp("", "runeconsole-candidate-")
	if err != nil {
		return "", "", errors.New("create private candidate workspace failed")
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		removePrivateTree(dir)
		return "", "", errors.New("secure candidate workspace failed")
	}
	f, err := os.Open(archivePath)
	if err != nil {
		removePrivateTree(dir)
		return "", "", errors.New("open release archive failed")
	}
	gz, err := gzip.NewReader(f)
	if err != nil {
		f.Close()
		removePrivateTree(dir)
		return "", "", errors.New("release archive is not valid gzip")
	}
	t := tar.NewReader(io.LimitReader(gz, maxArchiveBytes+1))
	destination := filepath.Join(dir, "runeconsole")
	found := false
	for {
		hdr, nextErr := t.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			err = errors.New("read release archive failed")
			break
		}
		archiveName := filepath.ToSlash(hdr.Name)
		if archiveName != "runeconsole" && archiveName != "./runeconsole" {
			continue
		}
		if found {
			err = errors.New("release archive contains multiple runeconsole binaries")
			break
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA {
			err = errors.New("release archive runeconsole entry is not a regular file")
			break
		}
		if hdr.Size <= 0 || hdr.Size > maxArchiveBytes {
			err = errors.New("release binary has an invalid size")
			break
		}
		out, createErr := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
		if createErr != nil {
			err = errors.New("create candidate binary failed")
			break
		}
		written, copyErr := io.CopyN(out, t, hdr.Size)
		syncErr := out.Sync()
		closeErr := out.Close()
		if copyErr != nil || syncErr != nil || closeErr != nil || written != hdr.Size {
			err = fmt.Errorf("extract candidate binary: %w", errors.Join(copyErr, syncErr, closeErr))
			break
		}
		found = true
	}
	closeErr := errors.Join(gz.Close(), f.Close())
	if err == nil && closeErr != nil {
		err = errors.New("close release archive failed")
	}
	if err == nil && !found {
		err = errors.New("release archive does not contain runeconsole")
	}
	if err != nil {
		removePrivateTree(dir)
		return "", "", err
	}
	return dir, destination, nil
}

type BuildInfoVerifier struct {
	VersionVariable string
}

func (v BuildInfoVerifier) Verify(_ context.Context, candidatePath, expectedVersion, expectedOS, expectedArch string) error {
	info, err := buildinfo.ReadFile(candidatePath)
	if err != nil {
		return errors.New("candidate is not a readable Go executable")
	}
	variable := v.VersionVariable
	if variable == "" {
		variable = "github.com/CryptoLabInc/rune-console/internal/commands.buildVersion"
	}
	settings := make(map[string]string, len(info.Settings))
	for _, setting := range info.Settings {
		settings[setting.Key] = setting.Value
	}
	if settings["GOOS"] != expectedOS || settings["GOARCH"] != expectedArch {
		return errors.New("candidate build platform does not match this host")
	}
	reported := ""
	words := splitCommandWords(settings["-ldflags"])
	for i := 0; i < len(words); i++ {
		assignment := ""
		switch {
		case words[i] == "-X" && i+1 < len(words):
			i++
			assignment = words[i]
		case strings.HasPrefix(words[i], "-X="):
			assignment = strings.TrimPrefix(words[i], "-X=")
		}
		if strings.HasPrefix(assignment, variable+"=") {
			reported = strings.TrimPrefix(assignment, variable+"=")
		}
	}
	if reported == "" {
		return errors.New("candidate build info does not contain the runeconsole release version")
	}
	reportedVersion, err := ParseVersion(reported)
	if err != nil {
		return errors.New("candidate build info contains an invalid release version")
	}
	expected, _ := ParseVersion(expectedVersion)
	if reportedVersion.Compare(expected) != 0 {
		return fmt.Errorf("candidate binary version %s does not match release %s", reportedVersion, expected)
	}
	return nil
}

func removePrivateTree(path string) {
	if path != "" {
		_ = os.RemoveAll(path)
	}
}
