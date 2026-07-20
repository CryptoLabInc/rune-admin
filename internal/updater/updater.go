// Package updater implements a verified, transactional in-place update of the
// runeconsole daemon.  It deliberately knows nothing about Cobra or the
// process' global configuration so its side effects can be exercised with
// fakes in unit tests.
package updater

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"time"
)

var (
	ErrDowngrade = errors.New("refusing to downgrade runeconsole")
	ErrLocked    = errors.New("another runeconsole update is already running")
)

type UpdateFailure struct {
	Cause              error
	BackupPath         string
	RecoveryIncomplete bool
}

func (e *UpdateFailure) Error() string {
	if e.RecoveryIncomplete {
		return fmt.Sprintf("%v; automatic recovery halted (service may be stopped); protected backup: %s", e.Cause, e.BackupPath)
	}
	return fmt.Sprintf("%v; protected backup: %s", e.Cause, e.BackupPath)
}

func (e *UpdateFailure) Unwrap() error { return e.Cause }

// Release is a local, verified-later representation of a release. Cleanup is
// called by Engine.Run after the archive has been consumed.
type Release struct {
	Version       string
	ArchivePath   string
	ChecksumsPath string
	Cleanup       func()
}

// ReleaseSource resolves a requested version (empty means latest) and makes
// its archive and SHA256SUMS file available locally.
type ReleaseSource interface {
	Resolve(context.Context, string, string, string) (Release, error)
}

// Service controls the operating-system service manager.
type Service interface {
	Stop(context.Context) error
	Start(context.Context) error
}

// HealthChecker waits until the service is ready to accept requests.
type HealthChecker interface {
	WaitHealthy(context.Context) error
}

// Locker prevents concurrent real updates. Check-only runs intentionally do
// not acquire it because they must not mutate any local state.
type Locker interface {
	Lock() (func() error, error)
}

// CandidateVerifier inspects an extracted binary without executing it and
// confirms its release version and target platform.
type CandidateVerifier interface {
	Verify(context.Context, string, string, string, string) error
}

type Request struct {
	CurrentVersion   string
	RequestedVersion string
	GOOS             string
	GOARCH           string
	BinaryPath       string
	PreviousPath     string
	BackupDir        string
	StatePaths       []string
	// ManagedRoots contains the configured data and key roots. They are
	// validated even when only selected files below them are backed up.
	ManagedRoots []string
	// ImmutablePaths are configuration inputs that must not change between
	// initial resolution and the stopped-service snapshot.
	ImmutablePaths []string
	CheckOnly      bool
}

type Result struct {
	CurrentVersion string
	TargetVersion  string
	BackupPath     string
	Checked        bool
	Updated        bool
	RolledBack     bool
	AlreadyCurrent bool
}

type Engine struct {
	Source   ReleaseSource
	Service  Service
	Health   HealthChecker
	Locker   Locker
	Verifier CandidateVerifier
}

func (e *Engine) Run(ctx context.Context, req Request) (Result, error) {
	result := Result{CurrentVersion: req.CurrentVersion}
	var initialIdentities []pathIdentity
	var immutableDigests []fileDigest
	if req.GOOS == "" {
		req.GOOS = runtime.GOOS
	}
	if req.GOARCH == "" {
		req.GOARCH = runtime.GOARCH
	}
	if err := ValidatePlatform(req.GOOS, req.GOARCH); err != nil {
		return result, err
	}
	if e.Source == nil || e.Verifier == nil {
		return result, errors.New("updater is missing a release source or candidate verifier")
	}
	if !req.CheckOnly && (e.Service == nil || e.Health == nil || e.Locker == nil) {
		return result, errors.New("updater is missing a service, health checker, or lock")
	}

	current, err := ParseVersion(req.CurrentVersion)
	if err != nil {
		return result, fmt.Errorf("current runeconsole version is not a release version: %w", err)
	}

	if !req.CheckOnly {
		canonicalState, err := CanonicalizePaths(req.StatePaths)
		if err != nil {
			return result, errors.New("resolve configured durable-state paths failed")
		}
		canonicalRoots, err := canonicalizeDistinctPaths(req.ManagedRoots)
		if err != nil {
			return result, errors.New("resolve configured durable-state roots failed")
		}
		req.StatePaths = canonicalState
		req.ManagedRoots = canonicalRoots
		canonicalImmutable, err := canonicalizeDistinctPaths(req.ImmutablePaths)
		if err != nil {
			return result, errors.New("resolve immutable config paths failed")
		}
		req.ImmutablePaths = canonicalImmutable
		if req.PreviousPath != "" && req.PreviousPath != req.BinaryPath+".previous" {
			return result, errors.New("previous binary path must be the service binary plus .previous")
		}
		if err := ValidateUpdatePaths(req.BinaryPath, req.BackupDir, req.StatePaths, req.ManagedRoots); err != nil {
			return result, err
		}
		initialIdentities, err = capturePathIdentities(append(append(append([]string(nil), req.StatePaths...), req.ManagedRoots...), req.ImmutablePaths...))
		if err != nil {
			return result, err
		}
		immutableDigests, err = captureFileDigests(req.ImmutablePaths)
		if err != nil {
			return result, err
		}
		unlock, err := e.Locker.Lock()
		if err != nil {
			return result, err
		}
		defer unlock() // best effort; the process is exiting on an unlock failure anyway
	}

	release, err := e.Source.Resolve(ctx, req.RequestedVersion, req.GOOS, req.GOARCH)
	if err != nil {
		return result, fmt.Errorf("resolve release: %w", err)
	}
	if release.Cleanup != nil {
		defer release.Cleanup()
	}
	target, err := ParseVersion(release.Version)
	if err != nil {
		return result, fmt.Errorf("release has an invalid version: %w", err)
	}
	result.TargetVersion = target.String()
	comparison := target.Compare(current)
	if comparison < 0 {
		return result, fmt.Errorf("%w (%s is older than %s)", ErrDowngrade, target, current)
	}

	verifiedDir, verifiedArchive, err := StageVerifiedArchive(release.ArchivePath, release.ChecksumsPath)
	if err != nil {
		return result, err
	}
	defer removePrivateTree(verifiedDir)
	candidateDir, candidatePath, err := ExtractCandidate(verifiedArchive)
	if err != nil {
		return result, err
	}
	defer removePrivateTree(candidateDir)
	if err := e.Verifier.Verify(ctx, candidatePath, target.String(), req.GOOS, req.GOARCH); err != nil {
		return result, err
	}
	result.Checked = true
	if req.CheckOnly {
		return result, nil
	}
	if comparison == 0 {
		result.AlreadyCurrent = true
		return result, nil
	}

	if err := ctx.Err(); err != nil {
		return result, err
	}
	criticalParent := context.WithoutCancel(ctx)
	stopCtx, cancelStop := recoveryContext(criticalParent)
	stopErr := e.Service.Stop(stopCtx)
	cancelStop()
	if stopErr != nil {
		return result, e.recoverAfterStopError(criticalParent, stopErr)
	}
	if err := verifyPathIdentities(initialIdentities); err != nil {
		return result, e.recoverStoppedOriginal(criticalParent, err)
	}
	if err := verifyFileDigests(immutableDigests); err != nil {
		return result, e.recoverStoppedOriginal(criticalParent, err)
	}
	// Recheck marker ownership and nested symlinks after the daemon is stopped;
	// the initial preflight happened while that process could still write its
	// managed roots.
	if err := ValidateUpdatePaths(req.BinaryPath, req.BackupDir, req.StatePaths, req.ManagedRoots); err != nil {
		return result, e.recoverStoppedOriginal(criticalParent, err)
	}

	snapshot, err := BackupState(req.BackupDir, req.StatePaths)
	if err != nil {
		return result, e.recoverStoppedOriginal(criticalParent, fmt.Errorf("backup state: %w", err))
	}
	result.BackupPath = snapshot.Directory()

	previousPath := req.PreviousPath
	if previousPath == "" {
		previousPath = req.BinaryPath + ".previous"
	}
	previousReady, installErr := InstallCandidate(candidatePath, req.BinaryPath, previousPath)
	if installErr != nil {
		result.RolledBack = true
		rollbackErr := e.recoverInstallFailure(criticalParent, req.BinaryPath, previousPath, previousReady)
		if rollbackErr != nil {
			return result, &UpdateFailure{Cause: fmt.Errorf("install candidate failed and rollback was incomplete: %w", errors.Join(installErr, rollbackErr)), BackupPath: result.BackupPath, RecoveryIncomplete: true}
		}
		return result, &UpdateFailure{Cause: fmt.Errorf("install candidate failed; the previous binary was restored and durable state remained unchanged: %w", installErr), BackupPath: result.BackupPath}
	}

	startCtx, cancelStart := recoveryContext(criticalParent)
	startErr := e.Service.Start(startCtx)
	if startErr == nil {
		startErr = e.Health.WaitHealthy(startCtx)
	}
	cancelStart()
	if startErr == nil {
		result.Updated = true
		return result, nil
	}

	result.RolledBack = true
	rollbackErr := e.rollback(criticalParent, snapshot, req.BinaryPath, previousPath, true, true)
	if rollbackErr != nil {
		return result, &UpdateFailure{Cause: fmt.Errorf("update to %s failed and rollback was incomplete: %w", target, errors.Join(startErr, rollbackErr)), BackupPath: result.BackupPath, RecoveryIncomplete: true}
	}
	return result, &UpdateFailure{Cause: fmt.Errorf("update to %s failed; previous binary and state were restored: %w", target, startErr), BackupPath: result.BackupPath}
}

func (e *Engine) recoverAfterStopError(ctx context.Context, stopErr error) error {
	recoveryCtx, cancel := recoveryContext(ctx)
	defer cancel()
	startErr := e.Service.Start(recoveryCtx)
	healthErr := e.Health.WaitHealthy(recoveryCtx)
	if healthErr == nil {
		// systemd start is idempotent; launchd bootstrap can report already
		// loaded, in which case a successful health probe still proves the old
		// service remained available.
		return fmt.Errorf("stop runeconsole service: %w", stopErr)
	}
	return fmt.Errorf("stop runeconsole service failed and original availability could not be recovered: %w", errors.Join(stopErr, startErr, healthErr))
}

func (e *Engine) recoverInstallFailure(ctx context.Context, binaryPath, previousPath string, restoreBinary bool) error {
	recoveryCtx, cancel := recoveryContext(ctx)
	defer cancel()
	if restoreBinary {
		if err := RestorePrevious(binaryPath, previousPath); err != nil {
			return fmt.Errorf("restore previous binary: %w", err)
		}
	}
	if err := e.Service.Start(recoveryCtx); err != nil {
		return fmt.Errorf("restart previous service: %w", err)
	}
	if err := e.Health.WaitHealthy(recoveryCtx); err != nil {
		return fmt.Errorf("previous service did not become healthy: %w", err)
	}
	return nil
}

func (e *Engine) recoverStoppedOriginal(ctx context.Context, cause error) error {
	recoveryCtx, cancel := recoveryContext(ctx)
	defer cancel()
	startErr := e.Service.Start(recoveryCtx)
	if startErr == nil {
		startErr = e.Health.WaitHealthy(recoveryCtx)
	}
	if startErr != nil {
		return fmt.Errorf("update preparation failed and the original service could not be recovered: %w", errors.Join(cause, startErr))
	}
	return cause
}

func (e *Engine) rollback(ctx context.Context, snapshot *Snapshot, binaryPath, previousPath string, restoreBinary, candidateMayBeRunning bool) error {
	// The ordering is a safety invariant: no process may write state while it is
	// being restored, and the old daemon must not start until both state and
	// binary are back in place.
	recoveryCtx, cancel := recoveryContext(ctx)
	defer cancel()
	if candidateMayBeRunning {
		if err := e.Service.Stop(recoveryCtx); err != nil {
			// Never restore files while a candidate may still be writing them.
			return fmt.Errorf("stop failed candidate before rollback: %w", err)
		}
	}
	if err := snapshot.Restore(); err != nil {
		// Keep the service stopped when durable state could be only partially
		// restored. An operator can recover from the retained private backup.
		return fmt.Errorf("restore state: %w", err)
	}
	if restoreBinary {
		if err := RestorePrevious(binaryPath, previousPath); err != nil {
			return fmt.Errorf("restore previous binary: %w", err)
		}
	}
	if err := e.Service.Start(recoveryCtx); err != nil {
		return fmt.Errorf("restart previous service: %w", err)
	}
	if err := e.Health.WaitHealthy(recoveryCtx); err != nil {
		return fmt.Errorf("previous service did not become healthy: %w", err)
	}
	return nil
}

func recoveryContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(parent), 2*time.Minute)
}

func cleanAbsolute(path string) (string, error) {
	if path == "" || !filepath.IsAbs(path) {
		return "", errors.New("update paths must be absolute")
	}
	return filepath.Clean(path), nil
}
