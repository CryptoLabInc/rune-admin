package updater

import (
	"archive/tar"
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"
)

const (
	backupArchiveName  = "state.tar"
	backupManifestName = "manifest.json"
	// ManagedRootMarker must exist with ManagedRootMarkerContent in every
	// recursively snapshotted config data/key root. It proves that rollback is
	// operating on an installer-owned directory rather than a broad path.
	ManagedRootMarker        = ".runeconsole-managed"
	ManagedRootMarkerContent = "runeconsole-managed-v1\n"
	stateKindMissing         = "missing"
	stateKindFile            = "file"
	stateKindDirectory       = "directory"
)

type manifest struct {
	CreatedAt     time.Time       `json:"created_at"`
	ArchiveSHA256 string          `json:"archive_sha256"`
	Entries       []manifestEntry `json:"entries"`
}

type manifestEntry struct {
	Target string `json:"target"`
	Exists bool   `json:"exists"`
	Kind   string `json:"kind"`
}

type Snapshot struct {
	directory     string
	entries       []manifestEntry
	archiveSHA256 string
}

func (s *Snapshot) Directory() string { return s.directory }

// CanonicalizePaths resolves symlinked roots, including paths whose final
// component does not exist (SQLite -wal/-shm files are commonly absent). This
// ensures that a configured symlink backs up the durable target, not merely
// the link inode.
func CanonicalizePaths(paths []string) ([]string, error) {
	result, err := canonicalizeDistinctPaths(paths)
	if err != nil {
		return nil, err
	}
	// If one selected state root contains another, archive it once. Besides
	// shrinking backups this prevents overlapping removal during restore.
	sort.Slice(result, func(i, j int) bool {
		if len(result[i]) == len(result[j]) {
			return result[i] < result[j]
		}
		return len(result[i]) < len(result[j])
	})
	filtered := result[:0]
	for _, path := range result {
		covered := false
		for _, parent := range filtered {
			if isWithin(parent, path) {
				covered = true
				break
			}
		}
		if !covered {
			filtered = append(filtered, path)
		}
	}
	return filtered, nil
}

func canonicalizeDistinctPaths(paths []string) ([]string, error) {
	result := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, input := range paths {
		if input == "" {
			continue
		}
		path, err := canonicalizePath(input)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		result = append(result, path)
	}
	return result, nil
}

func canonicalizePath(input string) (string, error) {
	path, err := cleanAbsolute(input)
	if err != nil {
		return "", err
	}
	probe := path
	var suffix []string
	for {
		_, statErr := os.Lstat(probe)
		if statErr == nil {
			resolved, evalErr := filepath.EvalSymlinks(probe)
			if evalErr != nil {
				return "", errors.New("resolve configured state path failed")
			}
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return filepath.Clean(resolved), nil
		}
		if !errors.Is(statErr, fs.ErrNotExist) {
			return "", errors.New("inspect configured state path failed")
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return "", errors.New("configured state path has no existing ancestor")
		}
		suffix = append(suffix, filepath.Base(probe))
		probe = parent
	}
}

// ValidateUpdatePaths rejects broad or overlapping mutation targets before
// the service is stopped. In particular, a malformed data_dir or keys.path of
// "/" must never turn rollback into a system-wide removal.
func ValidateUpdatePaths(binaryPath, backupDir string, statePaths, managedRoots []string) error {
	canonicalBinary, err := canonicalizePath(binaryPath)
	if err != nil {
		return errors.New("binary path must be absolute")
	}
	canonicalPrevious, err := canonicalizePath(binaryPath + ".previous")
	if err != nil {
		return errors.New("previous binary path is unsafe")
	}
	canonicalBackup, err := canonicalizePath(backupDir)
	if err != nil {
		return errors.New("backup directory is unsafe")
	}
	if err := validateManagedPath(canonicalBackup); err != nil {
		return fmt.Errorf("backup directory is unsafe: %w", err)
	}
	all := append(append([]string(nil), statePaths...), managedRoots...)
	for _, path := range all {
		canonical, canonicalErr := canonicalizePath(path)
		if canonicalErr != nil || canonical != path {
			return errors.New("configured durable-state path changed during update preflight")
		}
	}
	canonical, err := CanonicalizePaths(all)
	if err != nil {
		return errors.New("configured durable-state path is unsafe")
	}
	if len(statePaths) == 0 {
		return errors.New("no durable-state paths were configured")
	}
	for _, root := range managedRoots {
		if err := validateManagedRoot(root); err != nil {
			return err
		}
	}
	for _, path := range canonical {
		if err := validateManagedPath(path); err != nil {
			return fmt.Errorf("configured durable-state path is unsafe: %w", err)
		}
		if isWithin(path, canonicalBackup) || isWithin(canonicalBackup, path) {
			return errors.New("backup directory must be outside every durable-state path")
		}
		if isWithin(path, canonicalBinary) || isWithin(path, canonicalPrevious) {
			return errors.New("service binary and .previous must be outside durable-state roots")
		}
	}
	if isWithin(canonicalBackup, canonicalBinary) || isWithin(canonicalBackup, canonicalPrevious) {
		return errors.New("service binary and .previous must be outside the backup directory")
	}
	return nil
}

type pathIdentity struct {
	path   string
	exists bool
	dev    uint64
	ino    uint64
}

func capturePathIdentities(paths []string) ([]pathIdentity, error) {
	paths, err := canonicalizeDistinctPaths(paths)
	if err != nil {
		return nil, errors.New("resolve durable-state identity paths failed")
	}
	identities := make([]pathIdentity, 0, len(paths))
	for _, path := range paths {
		identity := pathIdentity{path: path}
		info, err := os.Lstat(path)
		if errors.Is(err, fs.ErrNotExist) {
			identities = append(identities, identity)
			continue
		}
		if err != nil {
			return nil, errors.New("inspect durable-state identity failed")
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return nil, errors.New("durable-state identity is unavailable")
		}
		identity.exists = true
		identity.dev = uint64(stat.Dev)
		identity.ino = uint64(stat.Ino)
		identities = append(identities, identity)
	}
	return identities, nil
}

func verifyPathIdentities(want []pathIdentity) error {
	paths := make([]string, len(want))
	for i := range want {
		paths[i] = want[i].path
	}
	got, err := capturePathIdentities(paths)
	if err != nil || len(got) != len(want) {
		return errors.New("durable-state paths changed while the release was prepared")
	}
	for i := range want {
		if got[i] != want[i] {
			return errors.New("durable-state paths changed while the release was prepared")
		}
	}
	return nil
}

type fileDigest struct {
	path string
	sum  [sha256.Size]byte
}

func captureFileDigests(paths []string) ([]fileDigest, error) {
	digests := make([]fileDigest, 0, len(paths))
	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			return nil, errors.New("open immutable config failed")
		}
		h := sha256.New()
		_, copyErr := io.Copy(h, f)
		closeErr := f.Close()
		if copyErr != nil || closeErr != nil {
			return nil, errors.New("hash immutable config failed")
		}
		var sum [sha256.Size]byte
		copy(sum[:], h.Sum(nil))
		digests = append(digests, fileDigest{path: path, sum: sum})
	}
	return digests, nil
}

func verifyFileDigests(want []fileDigest) error {
	paths := make([]string, len(want))
	for i := range want {
		paths[i] = want[i].path
	}
	got, err := captureFileDigests(paths)
	if err != nil || len(got) != len(want) {
		return errors.New("configuration changed while the release was prepared")
	}
	for i := range want {
		if got[i] != want[i] {
			return errors.New("configuration changed while the release was prepared")
		}
	}
	return nil
}

func validateManagedRoot(root string) error {
	if err := validateManagedPath(root); err != nil {
		return errors.New("configured managed root is unsafe")
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return errors.New("configured managed root must be an existing directory")
	}
	markerPath := filepath.Join(root, ManagedRootMarker)
	markerInfo, err := os.Lstat(markerPath)
	if err != nil || !markerInfo.Mode().IsRegular() || markerInfo.Mode()&os.ModeSymlink != 0 || markerInfo.Mode().Perm()&0o022 != 0 {
		return errors.New("configured managed root is missing its ownership marker")
	}
	rootStat, rootOK := info.Sys().(*syscall.Stat_t)
	markerStat, markerOK := markerInfo.Sys().(*syscall.Stat_t)
	if !rootOK || !markerOK || rootStat.Uid != markerStat.Uid || rootStat.Gid != markerStat.Gid {
		return errors.New("configured managed root ownership does not match its marker")
	}
	content, err := os.ReadFile(markerPath)
	if err != nil || string(content) != ManagedRootMarkerContent {
		return errors.New("configured managed root has an invalid ownership marker")
	}
	if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path != root && entry.Type()&os.ModeSymlink != 0 {
			return errors.New("managed root contains a nested symlink")
		}
		return nil
	}); err != nil {
		return errors.New("configured managed root contains an unsafe entry")
	}
	if err := validateNoMountBoundaries(root); err != nil {
		return err
	}
	return nil
}

func validateNoMountBoundaries(root string) error {
	rootInfo, err := os.Stat(root)
	if err != nil || !rootInfo.IsDir() {
		return errors.New("managed root is not an inspectable directory")
	}
	rootStat, ok := rootInfo.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New("managed root filesystem identity is unavailable")
	}
	parent := filepath.Dir(root)
	if parent == root {
		return errors.New("managed root must not be a filesystem mount point")
	}
	parentInfo, err := os.Stat(parent)
	if err != nil || !parentInfo.IsDir() {
		return errors.New("managed root parent is not inspectable")
	}
	parentStat, ok := parentInfo.Sys().(*syscall.Stat_t)
	if !ok || uint64(parentStat.Dev) != uint64(rootStat.Dev) {
		return errors.New("managed root must not be a filesystem mount point")
	}
	if err := filepath.WalkDir(root, func(_ string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || uint64(stat.Dev) != uint64(rootStat.Dev) {
			return errors.New("managed root contains a nested filesystem boundary")
		}
		return nil
	}); err != nil {
		return errors.New("managed root contains a nested filesystem boundary")
	}
	if runtime.GOOS != "linux" {
		return nil
	}
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return errors.New("cannot inspect Linux mount boundaries")
	}
	mounts, parseErr := parseLinuxMountInfo(f)
	closeErr := f.Close()
	if parseErr != nil || closeErr != nil {
		return errors.New("cannot parse Linux mount boundaries")
	}
	for _, mount := range mounts {
		if mount == root || isWithin(root, mount) {
			return errors.New("managed root is or contains a Linux mount point")
		}
	}
	return nil
}

func parseLinuxMountInfo(r io.Reader) ([]string, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64<<10), 2<<20)
	var mounts []string
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 6 {
			return nil, errors.New("invalid mountinfo record")
		}
		mount, err := unescapeMountInfoPath(fields[4])
		if err != nil || !filepath.IsAbs(mount) {
			return nil, errors.New("invalid mountinfo mount point")
		}
		mounts = append(mounts, filepath.Clean(mount))
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return mounts, nil
}

func unescapeMountInfoPath(input string) (string, error) {
	var output strings.Builder
	for i := 0; i < len(input); i++ {
		if input[i] != '\\' {
			output.WriteByte(input[i])
			continue
		}
		if i+3 >= len(input) {
			return "", errors.New("truncated mountinfo escape")
		}
		var value byte
		for j := 1; j <= 3; j++ {
			digit := input[i+j]
			if digit < '0' || digit > '7' {
				return "", errors.New("invalid mountinfo escape")
			}
			value = value*8 + (digit - '0')
		}
		output.WriteByte(value)
		i += 3
	}
	return output.String(), nil
}

func validateManagedPath(path string) error {
	clean, err := cleanAbsolute(path)
	if err != nil {
		return err
	}
	broad := map[string]struct{}{
		"/": {}, "/bin": {}, "/boot": {}, "/dev": {}, "/etc": {},
		"/home": {}, "/Library": {}, "/Applications": {}, "/media": {},
		"/mnt": {}, "/opt": {}, "/private": {}, "/private/etc": {},
		"/private/tmp": {}, "/private/var": {}, "/proc": {}, "/root": {},
		"/run": {}, "/sbin": {}, "/srv": {}, "/sys": {}, "/System": {},
		"/System/Volumes": {}, "/System/Volumes/Data": {}, "/tmp": {},
		"/Users": {}, "/usr": {}, "/usr/local": {}, "/usr/local/bin": {},
		"/var": {}, "/var/backups": {}, "/Volumes": {},
	}
	if _, exists := broad[clean]; exists {
		return errors.New("path is broader than a runeconsole-owned location")
	}
	if home, homeErr := os.UserHomeDir(); homeErr == nil {
		if resolvedHome, resolveErr := canonicalizePath(home); resolveErr == nil && clean == resolvedHome {
			return errors.New("path must not be a user's home directory")
		}
	}
	return nil
}

func validateSecureAncestors(path string) error {
	current := filepath.Clean(path)
	for {
		info, err := os.Lstat(current)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errors.New("private path has an unsafe ancestor")
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || (int(stat.Uid) != 0 && int(stat.Uid) != os.Geteuid()) {
			return errors.New("private path ancestor has an unsafe owner")
		}
		if info.Mode().Perm()&0o022 != 0 {
			stickyRootDirectory := info.Mode()&os.ModeSticky != 0 && stat.Uid == 0
			if !stickyRootDirectory {
				return errors.New("private path ancestor is group/world writable")
			}
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
		current = parent
	}
}

func isWithin(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func BackupState(backupDir string, statePaths []string) (*Snapshot, error) {
	canonicalBackup, err := canonicalizePath(backupDir)
	if err != nil {
		return nil, errors.New("resolve backup directory failed")
	}
	paths, err := CanonicalizePaths(statePaths)
	if err != nil {
		return nil, errors.New("resolve durable-state paths failed")
	}
	if err := validateManagedPath(canonicalBackup); err != nil {
		return nil, errors.New("backup directory is unsafe")
	}
	for _, path := range paths {
		if err := validateManagedPath(path); err != nil {
			return nil, errors.New("durable-state path is unsafe")
		}
		if isWithin(path, canonicalBackup) || isWithin(canonicalBackup, path) {
			return nil, errors.New("backup directory overlaps durable state")
		}
	}
	if err := os.MkdirAll(canonicalBackup, 0o700); err != nil {
		return nil, errors.New("create backup root failed")
	}
	rootInfo, err := os.Lstat(canonicalBackup)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("backup root is not a private directory")
	}
	if stat, ok := rootInfo.Sys().(*syscall.Stat_t); !ok || int(stat.Uid) != os.Geteuid() || rootInfo.Mode().Perm()&0o022 != 0 {
		return nil, errors.New("backup root must be owned by the updater and not group/world writable")
	}
	if err := validateSecureAncestors(canonicalBackup); err != nil {
		return nil, err
	}
	if err := os.Chmod(canonicalBackup, 0o700); err != nil {
		return nil, errors.New("secure backup root failed")
	}
	dir, err := os.MkdirTemp(canonicalBackup, time.Now().UTC().Format("20060102T150405Z")+"-")
	if err != nil {
		return nil, errors.New("create backup directory failed")
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		removePrivateTree(dir)
		return nil, errors.New("secure backup directory failed")
	}

	entries := make([]manifestEntry, len(paths))
	for i, path := range paths {
		entries[i].Target = path
		info, statErr := os.Lstat(path)
		switch {
		case statErr == nil:
			entries[i].Exists = true
			switch {
			case info.IsDir():
				entries[i].Kind = stateKindDirectory
			case info.Mode().IsRegular():
				entries[i].Kind = stateKindFile
			default:
				removePrivateTree(dir)
				return nil, fmt.Errorf("durable-state entry %d has an unsupported type", i)
			}
		case errors.Is(statErr, fs.ErrNotExist):
			entries[i].Exists = false
			entries[i].Kind = stateKindMissing
		default:
			removePrivateTree(dir)
			return nil, fmt.Errorf("inspect durable-state entry %d failed", i)
		}
	}

	archivePath := filepath.Join(dir, backupArchiveName)
	archive, err := os.OpenFile(archivePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		removePrivateTree(dir)
		return nil, errors.New("create backup archive failed")
	}
	archiveHash := sha256.New()
	tw := tar.NewWriter(io.MultiWriter(archive, archiveHash))
	for i, entry := range entries {
		if !entry.Exists {
			continue
		}
		if err := archiveStatePath(tw, i, entry.Target); err != nil {
			_ = tw.Close()
			_ = archive.Close()
			removePrivateTree(dir)
			return nil, fmt.Errorf("archive durable-state entry %d: %w", i, err)
		}
	}
	if err := errors.Join(tw.Close(), archive.Sync(), archive.Close()); err != nil {
		removePrivateTree(dir)
		return nil, errors.New("finish backup archive failed")
	}
	if err := os.Chmod(archivePath, 0o600); err != nil {
		removePrivateTree(dir)
		return nil, errors.New("secure backup archive failed")
	}

	manifestPath := filepath.Join(dir, backupManifestName)
	mf, err := os.OpenFile(manifestPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		removePrivateTree(dir)
		return nil, errors.New("create backup manifest failed")
	}
	archiveSHA256 := hex.EncodeToString(archiveHash.Sum(nil))
	encErr := json.NewEncoder(mf).Encode(manifest{CreatedAt: time.Now().UTC(), ArchiveSHA256: archiveSHA256, Entries: entries})
	if err := errors.Join(encErr, mf.Sync(), mf.Close()); err != nil {
		removePrivateTree(dir)
		return nil, errors.New("write backup manifest failed")
	}
	if err := os.Chmod(manifestPath, 0o600); err != nil {
		removePrivateTree(dir)
		return nil, errors.New("secure backup manifest failed")
	}
	if err := errors.Join(syncDirectory(dir), syncDirectory(canonicalBackup)); err != nil {
		removePrivateTree(dir)
		return nil, errors.New("sync backup directory failed")
	}
	return &Snapshot{directory: dir, entries: entries, archiveSHA256: archiveSHA256}, nil
}

func archiveStatePath(tw *tar.Writer, index int, root string) error {
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return sanitizePathError(err)
	}
	rootStat, ok := rootInfo.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New("durable-state filesystem identity is unavailable")
	}
	return filepath.Walk(root, func(path string, info fs.FileInfo, walkErr error) error {
		if walkErr != nil {
			return sanitizePathError(walkErr)
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || uint64(stat.Dev) != uint64(rootStat.Dev) {
			return errors.New("durable state crosses a filesystem boundary")
		}
		link := ""
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("nested symlink in durable state is not supported")
		}
		if !(info.Mode().IsRegular() || info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
			return errors.New("unsupported special file in durable state")
		}
		hdr, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		name := fmt.Sprintf("state/%06d", index)
		if rel != "." {
			name += "/" + filepath.ToSlash(rel)
		}
		hdr.Name = name
		hdr.Format = tar.FormatPAX
		hdr.Uid = int(stat.Uid)
		hdr.Gid = int(stat.Gid)
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		if err != nil {
			return sanitizePathError(err)
		}
		f := os.NewFile(uintptr(fd), "durable-state-entry")
		openedInfo, statErr := f.Stat()
		if statErr != nil || !os.SameFile(info, openedInfo) || !openedInfo.Mode().IsRegular() {
			f.Close()
			return errors.New("durable-state file changed while being archived")
		}
		_, copyErr := io.Copy(tw, f)
		closeErr := f.Close()
		return errors.Join(copyErr, sanitizePathError(closeErr))
	})
}

func (s *Snapshot) Restore() error {
	if s == nil || s.directory == "" || s.archiveSHA256 == "" {
		return errors.New("backup snapshot is unavailable")
	}
	if err := s.validateLiveTargets(); err != nil {
		return err
	}
	if err := s.verifyArchiveHash(); err != nil {
		return err
	}
	staged, err := s.stageArchive()
	if err != nil {
		return err
	}
	// Revalidate immediately before the first rename so a path swap during the
	// potentially long staging phase cannot broaden a mutation target.
	if err := s.validateLiveTargets(); err != nil {
		cleanupStagedEntries(staged)
		return err
	}
	cleanupSafe, err := swapStagedEntries(staged)
	if err == nil || cleanupSafe {
		cleanupErr := cleanupStagedEntries(staged)
		if err == nil && cleanupErr != nil {
			return errors.New("restored state but could not remove restore quarantine")
		}
		if err != nil && cleanupErr != nil {
			err = errors.Join(err, errors.New("could not remove restore quarantine"))
		}
	}
	if err != nil && !cleanupSafe {
		return fmt.Errorf("restore swap failed and quarantine was retained for manual recovery: %w", err)
	}
	return err
}

type stagedEntry struct {
	manifest  manifestEntry
	container string
	staged    string
	rootSeen  bool
}

func (s *Snapshot) validateLiveTargets() error {
	for i, entry := range s.entries {
		canonical, err := canonicalizePath(entry.Target)
		if err != nil || canonical != entry.Target || validateManagedPath(entry.Target) != nil {
			return fmt.Errorf("backup manifest entry %d is unsafe", i)
		}
		kind, err := liveStateKind(entry.Target)
		if err != nil {
			return fmt.Errorf("inspect live durable-state entry %d failed", i)
		}
		switch entry.Kind {
		case stateKindDirectory:
			if kind != stateKindDirectory && kind != stateKindMissing {
				return fmt.Errorf("live durable-state entry %d changed from directory to a non-directory", i)
			}
			if kind == stateKindDirectory {
				if err := validateNoMountBoundaries(entry.Target); err != nil {
					return fmt.Errorf("live durable-state directory %d has an unsafe mount boundary", i)
				}
			}
		case stateKindFile, stateKindMissing:
			if kind == stateKindDirectory {
				return fmt.Errorf("live durable-state entry %d unexpectedly became a directory", i)
			}
		default:
			return fmt.Errorf("backup manifest entry %d has an invalid kind", i)
		}
	}
	return nil
}

func (s *Snapshot) verifyArchiveHash() error {
	f, err := os.Open(filepath.Join(s.directory, backupArchiveName))
	if err != nil {
		return errors.New("open backup archive failed")
	}
	h := sha256.New()
	_, copyErr := io.Copy(h, f)
	closeErr := f.Close()
	if copyErr != nil || closeErr != nil {
		return errors.New("hash backup archive failed")
	}
	if !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), s.archiveSHA256) {
		return errors.New("backup archive checksum does not match its manifest")
	}
	return nil
}

func (s *Snapshot) stageArchive() ([]stagedEntry, error) {
	staged := make([]stagedEntry, len(s.entries))
	for i, entry := range s.entries {
		staged[i].manifest = entry
		parent := filepath.Dir(entry.Target)
		parentInfo, err := os.Lstat(parent)
		if err != nil || !parentInfo.IsDir() || parentInfo.Mode()&os.ModeSymlink != 0 {
			cleanupStagedEntries(staged)
			return nil, fmt.Errorf("durable-state entry %d has an unsafe parent", i)
		}
		container, err := os.MkdirTemp(parent, ".runeconsole-restore-")
		if err != nil {
			cleanupStagedEntries(staged)
			return nil, fmt.Errorf("create restore staging for entry %d failed", i)
		}
		if err := os.Chmod(container, 0o700); err != nil {
			removePrivateTree(container)
			cleanupStagedEntries(staged)
			return nil, fmt.Errorf("secure restore staging for entry %d failed", i)
		}
		staged[i].container = container
		staged[i].staged = filepath.Join(container, "restored")
	}

	f, err := os.Open(filepath.Join(s.directory, backupArchiveName))
	if err != nil {
		cleanupStagedEntries(staged)
		return nil, errors.New("open backup archive failed")
	}
	stagedHash := sha256.New()
	tr := tar.NewReader(io.TeeReader(f, stagedHash))
	type directoryMetadata struct {
		path string
		hdr  tar.Header
	}
	var directories []directoryMetadata
	seenNames := make(map[string]struct{})
	for {
		hdr, nextErr := tr.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			f.Close()
			cleanupStagedEntries(staged)
			return nil, errors.New("read backup archive failed")
		}
		index, rel, err := parseStateArchiveName(hdr.Name)
		if err != nil || index < 0 || index >= len(staged) || !staged[index].manifest.Exists || hdr.Size < 0 {
			f.Close()
			cleanupStagedEntries(staged)
			return nil, errors.New("backup archive contains an unsafe entry")
		}
		if _, duplicate := seenNames[hdr.Name]; duplicate {
			f.Close()
			cleanupStagedEntries(staged)
			return nil, errors.New("backup archive contains a duplicate entry")
		}
		seenNames[hdr.Name] = struct{}{}
		entry := &staged[index]
		if rel == "" {
			if entry.rootSeen {
				f.Close()
				cleanupStagedEntries(staged)
				return nil, errors.New("backup archive contains a duplicate state root")
			}
			entry.rootSeen = true
			if (entry.manifest.Kind == stateKindDirectory && hdr.Typeflag != tar.TypeDir) ||
				(entry.manifest.Kind == stateKindFile && hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA) {
				f.Close()
				cleanupStagedEntries(staged)
				return nil, errors.New("backup archive root type does not match its manifest")
			}
		} else if !entry.rootSeen || entry.manifest.Kind != stateKindDirectory {
			f.Close()
			cleanupStagedEntries(staged)
			return nil, errors.New("backup archive child appears outside a directory root")
		}
		target := entry.staged
		if rel != "" {
			target = filepath.Join(target, filepath.FromSlash(rel))
		}
		if !isWithin(entry.staged, target) {
			f.Close()
			cleanupStagedEntries(staged)
			return nil, errors.New("backup archive entry escapes restore staging")
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if rel != "" {
				if err := secureParentDirectories(entry.staged, filepath.Dir(target)); err != nil {
					f.Close()
					cleanupStagedEntries(staged)
					return nil, err
				}
			}
			if err := os.Mkdir(target, 0o700); err != nil {
				f.Close()
				cleanupStagedEntries(staged)
				return nil, errors.New("stage durable-state directory failed")
			}
			directories = append(directories, directoryMetadata{path: target, hdr: *hdr})
		case tar.TypeReg, tar.TypeRegA:
			if rel != "" {
				if err := secureParentDirectories(entry.staged, filepath.Dir(target)); err != nil {
					f.Close()
					cleanupStagedEntries(staged)
					return nil, err
				}
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			if err != nil {
				f.Close()
				cleanupStagedEntries(staged)
				return nil, errors.New("create staged durable-state file failed")
			}
			_, copyErr := io.CopyN(out, tr, hdr.Size)
			syncErr := out.Sync()
			closeErr := out.Close()
			if err := errors.Join(copyErr, syncErr, closeErr); err != nil {
				f.Close()
				cleanupStagedEntries(staged)
				return nil, errors.New("write staged durable-state file failed")
			}
			if err := restoreMetadata(target, hdr, false); err != nil {
				f.Close()
				cleanupStagedEntries(staged)
				return nil, err
			}
		default:
			f.Close()
			cleanupStagedEntries(staged)
			return nil, errors.New("backup archive contains an unsupported file type")
		}
	}
	if _, err := io.Copy(stagedHash, f); err != nil {
		f.Close()
		cleanupStagedEntries(staged)
		return nil, errors.New("finish hashing staged backup archive failed")
	}
	if err := f.Close(); err != nil {
		cleanupStagedEntries(staged)
		return nil, errors.New("close backup archive failed")
	}
	if !strings.EqualFold(hex.EncodeToString(stagedHash.Sum(nil)), s.archiveSHA256) {
		cleanupStagedEntries(staged)
		return nil, errors.New("backup archive changed while it was staged")
	}
	for i := range staged {
		if staged[i].manifest.Exists && !staged[i].rootSeen {
			cleanupStagedEntries(staged)
			return nil, fmt.Errorf("backup archive is missing durable-state root %d", i)
		}
	}
	for i := len(directories) - 1; i >= 0; i-- {
		if err := restoreMetadata(directories[i].path, &directories[i].hdr, false); err != nil {
			cleanupStagedEntries(staged)
			return nil, err
		}
		if err := syncDirectory(directories[i].path); err != nil {
			cleanupStagedEntries(staged)
			return nil, errors.New("sync staged durable-state directory failed")
		}
	}
	for i := range staged {
		if staged[i].container != "" {
			if err := syncDirectory(staged[i].container); err != nil {
				cleanupStagedEntries(staged)
				return nil, fmt.Errorf("sync restore staging for entry %d failed", i)
			}
		}
	}
	return staged, nil
}

func swapStagedEntries(staged []stagedEntry) (bool, error) {
	type swapRecord struct {
		entry      stagedEntry
		displaced  string
		hadCurrent bool
		installed  bool
	}
	var records []swapRecord
	rollbackSwaps := func() error {
		var rollbackErrs []error
		for i := len(records) - 1; i >= 0; i-- {
			record := records[i]
			if record.installed {
				if err := os.Rename(record.entry.manifest.Target, record.entry.staged); err != nil {
					rollbackErrs = append(rollbackErrs, err)
					continue
				}
			}
			if record.hadCurrent {
				if err := os.Rename(record.displaced, record.entry.manifest.Target); err != nil {
					rollbackErrs = append(rollbackErrs, err)
				}
			}
			if err := syncDirectory(filepath.Dir(record.entry.manifest.Target)); err != nil {
				rollbackErrs = append(rollbackErrs, err)
			}
		}
		return errors.Join(rollbackErrs...)
	}

	for i := range staged {
		entry := staged[i]
		kind, err := liveStateKind(entry.manifest.Target)
		if err != nil {
			rollbackErr := rollbackSwaps()
			return rollbackErr == nil, fmt.Errorf("inspect live durable-state entry %d before swap failed: %w", i, errors.Join(err, rollbackErr))
		}
		if entry.manifest.Kind != stateKindDirectory && kind == stateKindDirectory {
			rollbackErr := rollbackSwaps()
			return rollbackErr == nil, fmt.Errorf("live durable-state entry %d became an unsafe directory: %w", i, errors.Join(errors.New("unsafe live state kind"), rollbackErr))
		}
		if entry.manifest.Kind == stateKindDirectory && kind != stateKindDirectory && kind != stateKindMissing {
			rollbackErr := rollbackSwaps()
			return rollbackErr == nil, fmt.Errorf("live durable-state entry %d became an unsafe non-directory: %w", i, errors.Join(errors.New("unsafe live state kind"), rollbackErr))
		}
		if entry.manifest.Kind == stateKindFile && kind == stateKindFile && sameRegularFile(entry.manifest.Target, entry.staged) {
			continue
		}
		if entry.manifest.Kind == stateKindMissing && kind == stateKindMissing {
			continue
		}
		record := swapRecord{entry: entry, displaced: filepath.Join(entry.container, "displaced"), hadCurrent: kind != stateKindMissing}
		if record.hadCurrent {
			if err := os.Rename(entry.manifest.Target, record.displaced); err != nil {
				rollbackErr := rollbackSwaps()
				return rollbackErr == nil, fmt.Errorf("quarantine live durable-state entry %d failed: %w", i, errors.Join(err, rollbackErr))
			}
		}
		if entry.manifest.Exists {
			if err := os.Rename(entry.staged, entry.manifest.Target); err != nil {
				var currentRestoreErr error
				if record.hadCurrent {
					currentRestoreErr = os.Rename(record.displaced, entry.manifest.Target)
				}
				rollbackErr := rollbackSwaps()
				cleanupSafe := currentRestoreErr == nil && rollbackErr == nil
				return cleanupSafe, fmt.Errorf("activate restored durable-state entry %d failed: %w", i, errors.Join(err, currentRestoreErr, rollbackErr))
			}
			record.installed = true
		}
		record.entry = entry
		records = append(records, record)
		if err := syncDirectory(filepath.Dir(entry.manifest.Target)); err != nil {
			rollbackErr := rollbackSwaps()
			return rollbackErr == nil, fmt.Errorf("sync restored durable-state entry %d failed: %w", i, errors.Join(err, rollbackErr))
		}
	}
	return true, nil
}

func liveStateKind(path string) (string, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return stateKindMissing, nil
	}
	if err != nil {
		return "", err
	}
	if info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		return stateKindDirectory, nil
	}
	if info.Mode().IsRegular() {
		return stateKindFile, nil
	}
	return "", errors.New("unsupported live state type")
}

func sameRegularFile(a, b string) bool {
	aInfo, aErr := os.Stat(a)
	bInfo, bErr := os.Stat(b)
	if aErr != nil || bErr != nil || !aInfo.Mode().IsRegular() || !bInfo.Mode().IsRegular() ||
		aInfo.Size() != bInfo.Size() || aInfo.Mode().Perm() != bInfo.Mode().Perm() || !aInfo.ModTime().Equal(bInfo.ModTime()) {
		return false
	}
	aStat, aOK := aInfo.Sys().(*syscall.Stat_t)
	bStat, bOK := bInfo.Sys().(*syscall.Stat_t)
	if !aOK || !bOK || aStat.Uid != bStat.Uid || aStat.Gid != bStat.Gid {
		return false
	}
	aFile, err := os.Open(a)
	if err != nil {
		return false
	}
	defer aFile.Close()
	bFile, err := os.Open(b)
	if err != nil {
		return false
	}
	defer bFile.Close()
	aHash, bHash := sha256.New(), sha256.New()
	if _, err := io.Copy(aHash, aFile); err != nil {
		return false
	}
	if _, err := io.Copy(bHash, bFile); err != nil {
		return false
	}
	return string(aHash.Sum(nil)) == string(bHash.Sum(nil))
}

func cleanupStagedEntries(staged []stagedEntry) error {
	var cleanupErrs []error
	for _, entry := range staged {
		if entry.container != "" {
			if err := os.RemoveAll(entry.container); err != nil {
				cleanupErrs = append(cleanupErrs, sanitizePathError(err))
			}
		}
	}
	return errors.Join(cleanupErrs...)
}

func parseStateArchiveName(name string) (int, string, error) {
	clean := filepath.ToSlash(filepath.Clean(name))
	if clean != name || strings.HasPrefix(clean, "/") || strings.Contains(clean, "../") {
		return 0, "", errors.New("unsafe archive name")
	}
	parts := strings.SplitN(clean, "/", 3)
	if len(parts) < 2 || parts[0] != "state" || len(parts[1]) != 6 {
		return 0, "", errors.New("invalid archive name")
	}
	for _, digit := range parts[1] {
		if digit < '0' || digit > '9' {
			return 0, "", errors.New("invalid archive index")
		}
	}
	var index int
	if _, err := fmt.Sscanf(parts[1], "%06d", &index); err != nil {
		return 0, "", errors.New("invalid archive index")
	}
	if len(parts) == 3 {
		return index, parts[2], nil
	}
	return index, "", nil
}

func secureParentDirectories(root, parent string) error {
	if root == parent {
		return nil
	}
	// The source walker never emits children of a symlink. Refusing symlinked
	// parents here makes a corrupted archive unable to redirect extraction.
	rel, err := filepath.Rel(root, parent)
	if err != nil || strings.HasPrefix(rel, "..") {
		return errors.New("restored file parent escapes durable-state root")
	}
	current := root
	for _, component := range strings.Split(rel, string(filepath.Separator)) {
		if component == "." || component == "" {
			continue
		}
		current = filepath.Join(current, component)
		info, statErr := os.Lstat(current)
		if errors.Is(statErr, fs.ErrNotExist) {
			if err := os.Mkdir(current, 0o700); err != nil {
				return errors.New("create restored durable-state parent failed")
			}
			continue
		}
		if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("restored durable-state parent is unsafe")
		}
	}
	return nil
}

func restoreMetadata(path string, hdr *tar.Header, symlink bool) error {
	if err := os.Lchown(path, hdr.Uid, hdr.Gid); err != nil {
		return errors.New("restore durable-state ownership failed")
	}
	if symlink {
		return nil
	}
	if err := os.Chmod(path, fs.FileMode(hdr.Mode)&os.ModePerm); err != nil {
		return errors.New("restore durable-state mode failed")
	}
	if !hdr.ModTime.IsZero() {
		accessTime := hdr.AccessTime
		if accessTime.IsZero() {
			accessTime = hdr.ModTime
		}
		if err := os.Chtimes(path, accessTime, hdr.ModTime); err != nil {
			return errors.New("restore durable-state timestamps failed")
		}
	}
	return nil
}

func sanitizePathError(err error) error {
	if err == nil {
		return nil
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return fmt.Errorf("%s: %w", pathErr.Op, pathErr.Err)
	}
	return err
}
