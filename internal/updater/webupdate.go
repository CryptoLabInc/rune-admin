package updater

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	DefaultUpdateControlDir  = "/var/lib/runeconsole-updater"
	DefaultUpdateEnabledPath = DefaultUpdateControlDir + "/enabled"
	DefaultUpdateStagingDir  = DefaultUpdateControlDir + "/staging"
	DefaultUpdateRequestPath = DefaultUpdateControlDir + "/inbox/request"
	DefaultUpdateStatusPath  = DefaultUpdateControlDir + "/status.json"

	DefaultWebUpdateCacheTTL       = time.Hour
	DefaultUpdateHeartbeatInterval = 15 * time.Second
	DefaultUpdateStaleAfter        = 5 * time.Minute
	maxUpdateControlBytes          = 16 << 10
)

var (
	ErrWebUpdateNotCapable  = errors.New("web update is not capable on this installation")
	ErrUpdateCheckFailed    = errors.New("check latest runeconsole release failed")
	ErrUpdateCheckRequired  = errors.New("a fresh update check is required")
	ErrUpdateTargetMismatch = errors.New(
		"requested update target does not match the latest checked release",
	)
	ErrNoUpdateAvailable       = errors.New("no newer runeconsole release is available")
	ErrUpdateInProgress        = errors.New("a runeconsole update is already queued or running")
	ErrNoUpdateRequest         = errors.New("no queued runeconsole update request")
	ErrNoUpdateStatus          = errors.New("no runeconsole update status")
	ErrInvalidUpdateRequest    = errors.New("invalid runeconsole update request")
	ErrInvalidUpdateStatus     = errors.New("invalid runeconsole update status")
	ErrUnsafeUpdateControlPath = errors.New("unsafe runeconsole update control path")
)

// UpdateState is the durable state shared by the console UI and the privileged
// update worker. Idle is implicit when no status file exists.
type UpdateState string

const (
	UpdateStateIdle      UpdateState = "idle"
	UpdateStateQueued    UpdateState = "queued"
	UpdateStateRunning   UpdateState = "running"
	UpdateStateFailed    UpdateState = "failed"
	UpdateStateSucceeded UpdateState = "succeeded"
)

// UpdateControlPaths are the only filesystem hand-off points between the
// unprivileged console daemon and the separately supervised root worker.
type UpdateControlPaths struct {
	Enabled string
	Staging string
	Request string
	Status  string
}

func DefaultUpdateControlPaths() UpdateControlPaths {
	return UpdateControlPaths{
		Enabled: DefaultUpdateEnabledPath,
		Staging: DefaultUpdateStagingDir,
		Request: DefaultUpdateRequestPath,
		Status:  DefaultUpdateStatusPath,
	}
}

// LatestVersionSource is intentionally narrower than ReleaseSource: checking
// the web UI must fetch metadata only, never a release archive.
type LatestVersionSource interface {
	LatestVersion(context.Context) (string, error)
}

// WebUpdateStatus is the console-facing update contract. UpdateAvailable is
// true only when TargetVersion is a strictly newer stable release and the
// privileged worker capability marker is present.
type WebUpdateStatus struct {
	CurrentVersion  string      `json:"currentVersion"`
	TargetVersion   string      `json:"targetVersion,omitempty"`
	UpdateAvailable bool        `json:"updateAvailable"`
	Capable         bool        `json:"capable"`
	State           UpdateState `json:"state"`
}

// UpdateJobRequest is the bounded, path-free message consumed by the root
// worker. Filesystem paths and offline archive inputs are never accepted from
// the web surface.
type UpdateJobRequest struct {
	JobID         string    `json:"jobId"`
	TargetVersion string    `json:"targetVersion"`
	RequestedAt   time.Time `json:"requestedAt"`
}

// UpdateJobStatus is atomically written by the root worker. Error is a short,
// operator-safe summary; detailed failures remain in the service journal.
type UpdateJobStatus struct {
	JobID          string      `json:"jobId"`
	CurrentVersion string      `json:"currentVersion,omitempty"`
	TargetVersion  string      `json:"targetVersion"`
	State          UpdateState `json:"state"`
	Error          string      `json:"error,omitempty"`
	UpdatedAt      time.Time   `json:"updatedAt"`
}

type WebControllerOptions struct {
	CurrentVersion string
	GOOS           string
	GOARCH         string
	Source         LatestVersionSource
	Paths          UpdateControlPaths
	CacheTTL       time.Duration
	StaleAfter     time.Duration
	Now            func() time.Time
}

// WebController owns the metadata-only availability cache and the one-file
// enqueue handoff. Its zero value is not usable; construct it with
// NewWebController so platform, source, paths, and TTL receive safe defaults.
type WebController struct {
	currentVersion string
	goos           string
	goarch         string
	source         LatestVersionSource
	paths          UpdateControlPaths
	cacheTTL       time.Duration
	staleAfter     time.Duration
	now            func() time.Time

	mu       sync.Mutex
	cached   latestCache
	inFlight *latestCall
}

type latestCache struct {
	version   string
	expiresAt time.Time
}

type latestCall struct {
	done    chan struct{}
	version string
	err     error
}

func NewWebController(opts WebControllerOptions) *WebController {
	goos := opts.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	goarch := opts.GOARCH
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	source := opts.Source
	if source == nil {
		source = GitHubSource{}
	}
	paths := opts.Paths
	defaults := DefaultUpdateControlPaths()
	if paths.Enabled == "" {
		paths.Enabled = defaults.Enabled
	}
	if paths.Request == "" {
		paths.Request = defaults.Request
	}
	if paths.Staging == "" {
		paths.Staging = defaults.Staging
	}
	if paths.Status == "" {
		paths.Status = defaults.Status
	}
	ttl := opts.CacheTTL
	if ttl <= 0 {
		ttl = DefaultWebUpdateCacheTTL
	}
	staleAfter := opts.StaleAfter
	if staleAfter <= 0 {
		staleAfter = DefaultUpdateStaleAfter
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &WebController{
		currentVersion: strings.TrimSpace(opts.CurrentVersion),
		goos:           goos,
		goarch:         goarch,
		source:         source,
		paths:          paths,
		cacheTTL:       ttl,
		staleAfter:     staleAfter,
		now:            now,
	}
}

// Check returns a fail-closed UI status. Missing capability, development
// builds, and unsupported platforms are normal non-capable results rather than
// errors, allowing the frontend to omit the floating update prompt.
func (c *WebController) Check(ctx context.Context) (WebUpdateStatus, error) {
	status, current, capable, err := c.baseStatus()
	if err != nil {
		return status, err
	}
	if !capable {
		return status, nil
	}
	if status.State == UpdateStateQueued || status.State == UpdateStateRunning {
		if target, targetErr := canonicalReleaseVersion(status.TargetVersion); targetErr == nil {
			status.UpdateAvailable = target.Compare(current) > 0
		}
		return status, nil
	}
	if err := ctx.Err(); err != nil {
		return status, err
	}

	latestRaw, err := c.latestVersion(ctx)
	if err != nil {
		return status, fmt.Errorf("%w: %v", ErrUpdateCheckFailed, err)
	}
	latest, err := canonicalReleaseVersion(latestRaw)
	if err != nil {
		return status, fmt.Errorf("%w: %v", ErrUpdateCheckFailed, err)
	}
	jobTarget := status.TargetVersion
	status.TargetVersion = latest.String()
	status.UpdateAvailable = latest.Compare(current) > 0
	status.State = normalizeTerminalState(status.State, status.TargetVersion, jobTarget)
	return status, nil
}

// Request enqueues target only when it exactly matches the last successful,
// still-fresh metadata check. It never performs a network request or accepts
// caller-controlled filesystem paths.
func (c *WebController) Request(ctx context.Context, target string) (WebUpdateStatus, error) {
	status, current, capable, err := c.baseStatus()
	if err != nil {
		return status, err
	}
	if !capable {
		return status, ErrWebUpdateNotCapable
	}
	if err := ctx.Err(); err != nil {
		return status, err
	}
	if status.State == UpdateStateQueued || status.State == UpdateStateRunning {
		return status, ErrUpdateInProgress
	}

	latestRaw, ok := c.freshCachedVersion()
	if !ok {
		return status, ErrUpdateCheckRequired
	}
	latest, err := canonicalReleaseVersion(latestRaw)
	if err != nil {
		return status, fmt.Errorf("%w: %v", ErrUpdateCheckRequired, err)
	}
	status.TargetVersion = latest.String()
	status.UpdateAvailable = latest.Compare(current) > 0
	if target != status.TargetVersion {
		return status, ErrUpdateTargetMismatch
	}
	if !status.UpdateAvailable {
		return status, ErrNoUpdateAvailable
	}
	jobID, err := newUpdateJobID()
	if err != nil {
		return status, errors.New("generate update job id failed")
	}
	request := UpdateJobRequest{
		JobID:         jobID,
		TargetVersion: status.TargetVersion,
		RequestedAt:   c.now().UTC(),
	}
	if err := enqueueUpdateRequest(c.paths.Staging, c.paths.Request, request); err != nil {
		return status, err
	}
	status.State = UpdateStateQueued
	return status, nil
}

func (c *WebController) baseStatus() (WebUpdateStatus, Version, bool, error) {
	status := WebUpdateStatus{
		CurrentVersion: c.currentVersion,
		State:          UpdateStateIdle,
	}
	current, err := canonicalReleaseVersion(c.currentVersion)
	if err != nil || ValidatePlatform(c.goos, c.goarch) != nil || !capabilityEnabled(c.paths.Enabled) {
		return status, Version{}, false, nil
	}
	status.CurrentVersion = current.String()
	status.Capable = true
	if persisted, err := ReadUpdateStatus(c.paths.Status); err == nil {
		status.State = reconcilePersistedState(persisted, current, c.now(), c.staleAfter)
		status.TargetVersion = persisted.TargetVersion
	} else if !errors.Is(err, ErrNoUpdateStatus) {
		return status, Version{}, false, err
	}
	// The unprivileged daemon cannot write status.json. During the short gap
	// between atomic request publication and the root worker's first status
	// write, the inbox itself is the durable queued state.
	if status.State != UpdateStateQueued && status.State != UpdateStateRunning {
		if request, peekErr := peekUpdateRequest(c.paths.Request); peekErr == nil {
			status.State = UpdateStateQueued
			status.TargetVersion = request.TargetVersion
		} else if !errors.Is(peekErr, ErrNoUpdateRequest) {
			return status, Version{}, false, peekErr
		}
	}
	return status, current, true, nil
}

func reconcilePersistedState(persisted UpdateJobStatus, current Version, now time.Time, staleAfter time.Duration) UpdateState {
	if persisted.State != UpdateStateRunning {
		return persisted.State
	}
	target, err := canonicalReleaseVersion(persisted.TargetVersion)
	if err == nil && current.Compare(target) >= 0 {
		// The new binary can begin serving before the old helper completes its
		// final status write. Treat that observable installed version as success
		// so a status-file failure cannot strand every browser in "running".
		return UpdateStateSucceeded
	}
	if now.After(persisted.UpdatedAt) && now.Sub(persisted.UpdatedAt) > staleAfter {
		// A live helper refreshes UpdatedAt. Once its lease expires, surface a
		// retryable failure instead of keeping the owner UI busy forever after a
		// crash, host reboot, or abrupt supervisor termination.
		return UpdateStateFailed
	}
	return persisted.State
}

func (c *WebController) latestVersion(ctx context.Context) (string, error) {
	now := c.now()
	c.mu.Lock()
	if c.cached.version != "" && now.Before(c.cached.expiresAt) {
		version := c.cached.version
		c.mu.Unlock()
		return version, nil
	}
	if call := c.inFlight; call != nil {
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-call.done:
			return call.version, call.err
		}
	}
	call := &latestCall{done: make(chan struct{})}
	c.inFlight = call
	c.mu.Unlock()

	version, err := c.source.LatestVersion(ctx)
	if err == nil {
		parsed, parseErr := canonicalReleaseVersion(version)
		if parseErr != nil {
			err = parseErr
		} else {
			version = parsed.String()
		}
	}

	c.mu.Lock()
	call.version, call.err = version, err
	if err == nil {
		c.cached = latestCache{version: version, expiresAt: c.now().Add(c.cacheTTL)}
	}
	c.inFlight = nil
	close(call.done)
	c.mu.Unlock()
	return version, err
}

func (c *WebController) freshCachedVersion() (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cached.version == "" || !c.now().Before(c.cached.expiresAt) {
		return "", false
	}
	return c.cached.version, true
}

func normalizeTerminalState(state UpdateState, latest, jobTarget string) UpdateState {
	if (state == UpdateStateFailed || state == UpdateStateSucceeded) && jobTarget != latest {
		return UpdateStateIdle
	}
	return state
}

func canonicalReleaseVersion(raw string) (Version, error) {
	trimmed := strings.TrimSpace(raw)
	version, err := ParseVersion(trimmed)
	if err != nil || version.String() != trimmed {
		return Version{}, errors.New("release version must be canonical vMAJOR.MINOR.PATCH[-PRERELEASE]")
	}
	return version, nil
}

func capabilityEnabled(path string) bool {
	resolved, err := resolvedControlPath(path, false)
	if err != nil {
		return false
	}
	info, err := os.Lstat(resolved)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && (int(stat.Uid) == 0 || int(stat.Uid) == os.Geteuid())
}

func newUpdateJobID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func enqueueUpdateRequest(stagingDir, requestPath string, request UpdateJobRequest) error {
	return enqueueUpdateRequestWithLink(stagingDir, requestPath, request, os.Link)
}

func enqueueUpdateRequestWithLink(
	stagingDir, requestPath string,
	request UpdateJobRequest,
	link func(string, string) error,
) error {
	if err := validateUpdateJobRequest(request); err != nil {
		return err
	}
	body, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("%w: encode request", ErrInvalidUpdateRequest)
	}
	body = append(body, '\n')
	resolvedStaging, err := resolvedControlDirectory(stagingDir, true)
	if err != nil {
		return err
	}
	resolvedRequest, err := resolvedControlPath(requestPath, true)
	if err != nil {
		return err
	}
	tempPath, err := stageUpdateRequest(resolvedStaging, request.JobID, body)
	if err != nil {
		return err
	}
	defer removeStagedUpdateRequest(tempPath)

	// A hard link is the publish point: the watcher sees either no request or
	// the complete, fsynced JSON inode. Unlike Rename, Link cannot overwrite a
	// request another browser/session already queued.
	linkErr := link(tempPath, resolvedRequest)
	if errors.Is(linkErr, syscall.EXDEV) {
		// Older systemd units exposed staging and inbox as separate
		// ReadWritePaths. systemd turns each path into its own bind mount, so
		// link(2) reports EXDEV even when both directories are on the same
		// filesystem. Restage inside the already-validated inbox and retain the
		// same atomic, no-overwrite hard-link publication semantics.
		inboxTempPath, fallbackErr := stageUpdateRequest(filepath.Dir(resolvedRequest), request.JobID, body)
		if fallbackErr != nil {
			return fallbackErr
		}
		defer removeStagedUpdateRequest(inboxTempPath)
		linkErr = link(inboxTempPath, resolvedRequest)
	}
	if linkErr != nil {
		if errors.Is(linkErr, fs.ErrExist) || errors.Is(linkErr, syscall.EEXIST) {
			return ErrUpdateInProgress
		}
		return fmt.Errorf("publish update request: %w", linkErr)
	}
	if err := syncDirectory(filepath.Dir(resolvedRequest)); err != nil {
		_ = os.Remove(resolvedRequest)
		return errors.New("persist update request directory failed")
	}
	return nil
}

func stageUpdateRequest(directory, jobID string, body []byte) (string, error) {
	tempPath := filepath.Join(directory, ".request-"+jobID)
	fd, err := syscall.Open(tempPath, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return "", fmt.Errorf("stage update request: %w", err)
	}
	f := os.NewFile(uintptr(fd), "runeconsole-update-request")
	_, writeErr := f.Write(body)
	syncErr := f.Sync()
	closeErr := f.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		_ = os.Remove(tempPath)
		return "", errors.New("persist update request failed")
	}
	return tempPath, nil
}

func removeStagedUpdateRequest(path string) {
	_ = os.Remove(path)
	_ = syncDirectory(filepath.Dir(path))
}

func peekUpdateRequest(path string) (UpdateJobRequest, error) {
	clean, err := cleanAbsolute(path)
	if err != nil {
		return UpdateJobRequest{}, fmt.Errorf("%w: request path must be absolute", ErrUnsafeUpdateControlPath)
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(clean))
	if err != nil {
		return UpdateJobRequest{}, fmt.Errorf("%w: resolve inbox", ErrUnsafeUpdateControlPath)
	}
	parentInfo, err := validateSharedWritableDirectory(parent)
	if err != nil {
		return UpdateJobRequest{}, err
	}
	resolved := filepath.Join(parent, filepath.Base(clean))
	f, err := openBoundedRequestFile(resolved, parentInfo, os.Geteuid())
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return UpdateJobRequest{}, ErrNoUpdateRequest
		}
		return UpdateJobRequest{}, err
	}
	body, readErr := io.ReadAll(io.LimitReader(f, maxUpdateControlBytes+1))
	closeErr := f.Close()
	if readErr != nil || closeErr != nil || len(body) > maxUpdateControlBytes {
		return UpdateJobRequest{}, fmt.Errorf("%w: read queued request", ErrInvalidUpdateRequest)
	}
	var request UpdateJobRequest
	if err := decodeStrictJSON(body, &request); err != nil {
		return UpdateJobRequest{}, fmt.Errorf("%w: %v", ErrInvalidUpdateRequest, err)
	}
	if err := validateUpdateJobRequest(request); err != nil {
		return UpdateJobRequest{}, err
	}
	return request, nil
}

// ConsumeUpdateRequest atomically claims and removes one queued request. The
// caller must be the separately supervised root worker; the inbox itself is
// treated as untrusted input owned by the daemon service account.
func ConsumeUpdateRequest(path string) (UpdateJobRequest, error) {
	return ConsumeUpdateRequestForUID(path, os.Geteuid())
}

// ConsumeUpdateRequestForUID consumes a request only when the published inode
// belongs to the expected unprivileged producer. The root update agent passes
// the installed runeconsole service UID so other members of the shared group
// cannot bypass the console-owner gate by writing directly to the inbox.
func ConsumeUpdateRequestForUID(path string, expectedUID int) (UpdateJobRequest, error) {
	if expectedUID < 0 {
		return UpdateJobRequest{}, fmt.Errorf("%w: invalid producer uid", ErrUnsafeUpdateControlPath)
	}
	resolved, parentInfo, err := resolvedConsumerRequestPath(path)
	if err != nil {
		return UpdateJobRequest{}, err
	}
	claimID, err := newUpdateJobID()
	if err != nil {
		return UpdateJobRequest{}, errors.New("generate request claim id failed")
	}
	// Move the published inode out of the group-writable inbox before parsing.
	// The control root is root-owned in production, so the daemon cannot replace
	// the claimed pathname while the privileged worker validates it.
	claimRoot := filepath.Dir(filepath.Dir(resolved))
	if err := validateSecureAncestors(claimRoot); err != nil {
		return UpdateJobRequest{}, fmt.Errorf("%w: insecure claim directory", ErrUnsafeUpdateControlPath)
	}
	claim := filepath.Join(claimRoot, ".request-claim-"+claimID)
	if err := os.Rename(resolved, claim); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return UpdateJobRequest{}, ErrNoUpdateRequest
		}
		return UpdateJobRequest{}, fmt.Errorf("claim update request: %w", err)
	}
	if err := errors.Join(syncDirectory(filepath.Dir(resolved)), syncDirectory(claimRoot)); err != nil {
		_ = os.Remove(claim)
		return UpdateJobRequest{}, errors.New("persist update request claim failed")
	}
	defer func() {
		_ = os.Remove(claim)
		_ = syncDirectory(filepath.Dir(claim))
	}()

	f, err := openBoundedRequestFile(claim, parentInfo, expectedUID)
	if err != nil {
		return UpdateJobRequest{}, err
	}
	if os.Geteuid() == 0 {
		// The daemon owns the staged inode. Take ownership before reading so any
		// still-open staging hard link can no longer be used to modify it.
		if err := f.Chown(0, 0); err != nil {
			f.Close()
			return UpdateJobRequest{}, fmt.Errorf("%w: claim request ownership", ErrUnsafeUpdateControlPath)
		}
		if err := f.Chmod(0o600); err != nil {
			f.Close()
			return UpdateJobRequest{}, fmt.Errorf("%w: secure claimed request", ErrUnsafeUpdateControlPath)
		}
	}
	body, readErr := io.ReadAll(io.LimitReader(f, maxUpdateControlBytes+1))
	closeErr := f.Close()
	if readErr != nil || closeErr != nil {
		return UpdateJobRequest{}, fmt.Errorf("%w: read request", ErrInvalidUpdateRequest)
	}
	if len(body) > maxUpdateControlBytes {
		return UpdateJobRequest{}, fmt.Errorf("%w: request exceeds size limit", ErrInvalidUpdateRequest)
	}
	var request UpdateJobRequest
	if err := decodeStrictJSON(body, &request); err != nil {
		return UpdateJobRequest{}, fmt.Errorf("%w: %v", ErrInvalidUpdateRequest, err)
	}
	if err := validateUpdateJobRequest(request); err != nil {
		return UpdateJobRequest{}, err
	}
	return request, nil
}

// WriteUpdateStatus atomically replaces the root-worker status file. The
// parent directory's group is preserved so the unprivileged console can read a
// 0640 status without gaining write access.
func WriteUpdateStatus(path string, status UpdateJobStatus) error {
	if status.UpdatedAt.IsZero() {
		status.UpdatedAt = time.Now().UTC()
	} else {
		status.UpdatedAt = status.UpdatedAt.UTC()
	}
	if err := validateUpdateJobStatus(status); err != nil {
		return err
	}
	body, err := json.Marshal(status)
	if err != nil {
		return fmt.Errorf("%w: encode status", ErrInvalidUpdateStatus)
	}
	body = append(body, '\n')
	resolved, err := resolvedControlPath(path, true)
	if err != nil {
		return err
	}
	parent := filepath.Dir(resolved)
	parentInfo, err := os.Stat(parent)
	if err != nil {
		return fmt.Errorf("%w: inspect status directory", ErrUnsafeUpdateControlPath)
	}
	temp, err := os.CreateTemp(parent, ".status-")
	if err != nil {
		return errors.New("create update status staging file failed")
	}
	tempPath := temp.Name()
	keep := false
	defer func() {
		_ = temp.Close()
		if !keep {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(0o640); err != nil {
		return errors.New("secure update status staging file failed")
	}
	if os.Geteuid() == 0 {
		if stat, ok := parentInfo.Sys().(*syscall.Stat_t); ok {
			if err := temp.Chown(0, int(stat.Gid)); err != nil {
				return errors.New("set update status ownership failed")
			}
		}
	}
	if _, err := temp.Write(body); err != nil {
		return errors.New("write update status failed")
	}
	if err := temp.Sync(); err != nil {
		return errors.New("sync update status failed")
	}
	if err := temp.Close(); err != nil {
		return errors.New("close update status failed")
	}
	if err := os.Rename(tempPath, resolved); err != nil {
		return errors.New("activate update status failed")
	}
	keep = true
	if err := syncDirectory(parent); err != nil {
		return errors.New("sync update status directory failed")
	}
	return nil
}

func ReadUpdateStatus(path string) (UpdateJobStatus, error) {
	resolved, err := resolvedControlPath(path, false)
	if err != nil {
		return UpdateJobStatus{}, err
	}
	parentInfo, err := os.Stat(filepath.Dir(resolved))
	if err != nil {
		return UpdateJobStatus{}, fmt.Errorf("%w: inspect status directory", ErrUnsafeUpdateControlPath)
	}
	f, err := openBoundedStatusFile(resolved, parentInfo)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return UpdateJobStatus{}, ErrNoUpdateStatus
		}
		return UpdateJobStatus{}, err
	}
	body, readErr := io.ReadAll(io.LimitReader(f, maxUpdateControlBytes+1))
	closeErr := f.Close()
	if readErr != nil || closeErr != nil || len(body) > maxUpdateControlBytes {
		return UpdateJobStatus{}, fmt.Errorf("%w: read status", ErrInvalidUpdateStatus)
	}
	var status UpdateJobStatus
	if err := decodeStrictJSON(body, &status); err != nil {
		return UpdateJobStatus{}, fmt.Errorf("%w: %v", ErrInvalidUpdateStatus, err)
	}
	if err := validateUpdateJobStatus(status); err != nil {
		return UpdateJobStatus{}, err
	}
	return status, nil
}

func validateUpdateJobRequest(request UpdateJobRequest) error {
	if !validJobID(request.JobID) {
		return fmt.Errorf("%w: invalid job id", ErrInvalidUpdateRequest)
	}
	if _, err := canonicalReleaseVersion(request.TargetVersion); err != nil {
		return fmt.Errorf("%w: invalid target version", ErrInvalidUpdateRequest)
	}
	if request.RequestedAt.IsZero() {
		return fmt.Errorf("%w: missing request time", ErrInvalidUpdateRequest)
	}
	return nil
}

func validateUpdateJobStatus(status UpdateJobStatus) error {
	if !validJobID(status.JobID) {
		return fmt.Errorf("%w: invalid job id", ErrInvalidUpdateStatus)
	}
	if _, err := canonicalReleaseVersion(status.TargetVersion); err != nil {
		return fmt.Errorf("%w: invalid target version", ErrInvalidUpdateStatus)
	}
	if status.CurrentVersion != "" {
		if _, err := canonicalReleaseVersion(status.CurrentVersion); err != nil {
			return fmt.Errorf("%w: invalid current version", ErrInvalidUpdateStatus)
		}
	}
	switch status.State {
	case UpdateStateQueued, UpdateStateRunning, UpdateStateFailed, UpdateStateSucceeded:
	default:
		return fmt.Errorf("%w: invalid state", ErrInvalidUpdateStatus)
	}
	if len(status.Error) > 1024 {
		return fmt.Errorf("%w: error summary is too long", ErrInvalidUpdateStatus)
	}
	if status.UpdatedAt.IsZero() {
		return fmt.Errorf("%w: missing update time", ErrInvalidUpdateStatus)
	}
	return nil
}

func validJobID(id string) bool {
	if len(id) < 16 || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func decodeStrictJSON(body []byte, destination any) error {
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func resolvedControlPath(path string, requireWritableParent bool) (string, error) {
	clean, err := cleanAbsolute(path)
	if err != nil {
		return "", fmt.Errorf("%w: path must be absolute", ErrUnsafeUpdateControlPath)
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(clean))
	if err != nil {
		return "", fmt.Errorf("%w: resolve parent", ErrUnsafeUpdateControlPath)
	}
	if requireWritableParent {
		if _, err := validateSharedWritableDirectory(parent); err != nil {
			return "", err
		}
	} else if err := validateSecureAncestors(parent); err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnsafeUpdateControlPath, err)
	}
	return filepath.Join(parent, filepath.Base(clean)), nil
}

func resolvedControlDirectory(path string, requireWritable bool) (string, error) {
	clean, err := cleanAbsolute(path)
	if err != nil {
		return "", fmt.Errorf("%w: directory must be absolute", ErrUnsafeUpdateControlPath)
	}
	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return "", fmt.Errorf("%w: resolve directory", ErrUnsafeUpdateControlPath)
	}
	if requireWritable {
		if _, err := validateSharedWritableDirectory(resolved); err != nil {
			return "", err
		}
	} else if err := validateSecureAncestors(resolved); err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnsafeUpdateControlPath, err)
	}
	return resolved, nil
}

func resolvedConsumerRequestPath(path string) (string, os.FileInfo, error) {
	clean, err := cleanAbsolute(path)
	if err != nil {
		return "", nil, fmt.Errorf("%w: path must be absolute", ErrUnsafeUpdateControlPath)
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(clean))
	if err != nil {
		return "", nil, fmt.Errorf("%w: resolve inbox", ErrUnsafeUpdateControlPath)
	}
	info, err := os.Lstat(parent)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o002 != 0 {
		return "", nil, fmt.Errorf("%w: insecure inbox", ErrUnsafeUpdateControlPath)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || (int(stat.Uid) != 0 && int(stat.Uid) != os.Geteuid()) {
		return "", nil, fmt.Errorf("%w: unsafe inbox owner", ErrUnsafeUpdateControlPath)
	}
	if info.Mode().Perm()&0o020 != 0 && info.Mode()&os.ModeSetgid == 0 {
		return "", nil, fmt.Errorf("%w: shared inbox must be setgid", ErrUnsafeUpdateControlPath)
	}
	if err := validateSecureAncestors(filepath.Dir(parent)); err != nil {
		return "", nil, fmt.Errorf("%w: insecure inbox ancestor", ErrUnsafeUpdateControlPath)
	}
	return filepath.Join(parent, filepath.Base(clean)), info, nil
}

func validateSharedWritableDirectory(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o002 != 0 {
		return nil, fmt.Errorf("%w: invalid shared directory", ErrUnsafeUpdateControlPath)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !sharedDirectoryAccessAllowed(info.Mode(), int(stat.Uid), int(stat.Gid), os.Geteuid(), os.Getegid()) {
		return nil, fmt.Errorf("%w: unsafe shared directory permissions", ErrUnsafeUpdateControlPath)
	}
	if err := validateSecureAncestors(filepath.Dir(path)); err != nil {
		return nil, fmt.Errorf("%w: insecure shared directory ancestor", ErrUnsafeUpdateControlPath)
	}
	return info, nil
}

func sharedDirectoryAccessAllowed(mode os.FileMode, uid, gid, euid, egid int) bool {
	if mode.Perm()&0o002 != 0 || (uid != 0 && uid != euid) {
		return false
	}
	if euid == 0 {
		return true
	}
	if uid == euid {
		return mode.Perm()&0o300 == 0o300
	}
	return gid == egid && mode.Perm()&0o030 == 0o030 && mode&os.ModeSetgid != 0
}

func openRawControlFile(path string) (*os.File, os.FileInfo, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil, err
		}
		return nil, nil, fmt.Errorf("%w: open control file", ErrUnsafeUpdateControlPath)
	}
	f := os.NewFile(uintptr(fd), filepath.Base(path))
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 0 {
		f.Close()
		return nil, nil, fmt.Errorf("%w: unsafe control file", ErrUnsafeUpdateControlPath)
	}
	return f, info, nil
}

func openBoundedRequestFile(path string, parentInfo os.FileInfo, expectedUID int) (*os.File, error) {
	f, info, err := openRawControlFile(path)
	if err != nil {
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	parentStat, parentOK := parentInfo.Sys().(*syscall.Stat_t)
	if !ok || !parentOK || stat.Nlink < 1 || stat.Nlink > 2 || info.Mode().Perm() != 0o600 || stat.Gid != parentStat.Gid {
		f.Close()
		return nil, fmt.Errorf("%w: unsafe request identity", ErrUnsafeUpdateControlPath)
	}
	if int(stat.Uid) != expectedUID {
		f.Close()
		return nil, fmt.Errorf("%w: request is not owned by the expected daemon uid", ErrUnsafeUpdateControlPath)
	}
	return f, nil
}

func openBoundedStatusFile(path string, parentInfo os.FileInfo) (*os.File, error) {
	f, info, err := openRawControlFile(path)
	if err != nil {
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	_, parentOK := parentInfo.Sys().(*syscall.Stat_t)
	if !ok || !parentOK || stat.Nlink != 1 || info.Mode().Perm()&0o022 != 0 || (int(stat.Uid) != 0 && int(stat.Uid) != os.Geteuid()) {
		f.Close()
		return nil, fmt.Errorf("%w: unsafe status identity", ErrUnsafeUpdateControlPath)
	}
	return f, nil
}
