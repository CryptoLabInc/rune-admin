package commands

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/CryptoLabInc/rune-console/internal/console"
	"github.com/CryptoLabInc/rune-console/internal/server"
	"github.com/CryptoLabInc/rune-console/internal/updater"
)

// consoleUpdateManager adapts the updater package's filesystem hand-off to the
// deliberately small interface exposed by the loopback console BFF.
type consoleUpdateManager struct {
	controller *updater.WebController
}

func newConsoleUpdateManager(currentVersion string, cfg *server.Config) console.UpdateManager {
	// The systemd helper's writable sandbox is generated for the official
	// installation prefix. Hide web updates for custom layouts rather than risk
	// discovering during rollback that an external state path is read-only.
	if !webUpdateStateWithinInstallRoot(cfg) {
		return nil
	}
	return &consoleUpdateManager{controller: updater.NewWebController(updater.WebControllerOptions{
		CurrentVersion: currentVersion,
	})}
}

func webUpdateStateWithinInstallRoot(cfg *server.Config) bool {
	if cfg == nil || cfg.Source == "" || filepath.Base(filepath.Dir(cfg.Source)) != "configs" {
		return false
	}
	root, err := canonicalPathAllowMissing(filepath.Dir(filepath.Dir(cfg.Source)))
	if err != nil {
		return false
	}
	for _, path := range durableStatePaths(cfg) {
		resolved, err := canonicalPathAllowMissing(path)
		if err != nil || !pathWithin(root, resolved) {
			return false
		}
	}
	return true
}

func canonicalPathAllowMissing(path string) (string, error) {
	if path == "" || !filepath.IsAbs(path) {
		return "", errors.New("path must be absolute")
	}
	probe := filepath.Clean(path)
	var suffix []string
	for {
		if _, err := os.Lstat(probe); err == nil {
			resolved, err := filepath.EvalSymlinks(probe)
			if err != nil {
				return "", err
			}
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return filepath.Clean(resolved), nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return "", errors.New("path has no existing ancestor")
		}
		suffix = append(suffix, filepath.Base(probe))
		probe = parent
	}
}

func pathWithin(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (m *consoleUpdateManager) Check(ctx context.Context) (console.UpdateStatus, error) {
	status, err := m.controller.Check(ctx)
	if err != nil {
		return console.UpdateStatus{}, mapConsoleUpdateError(err)
	}
	return console.UpdateStatus{
		CurrentVersion:  status.CurrentVersion,
		TargetVersion:   status.TargetVersion,
		UpdateAvailable: status.UpdateAvailable,
		Capable:         status.Capable,
		State:           console.UpdateState(status.State),
	}, nil
}

func (m *consoleUpdateManager) Request(ctx context.Context, target string) error {
	_, err := m.controller.Request(ctx, target)
	return mapConsoleUpdateError(err)
}

func mapConsoleUpdateError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, updater.ErrUpdateInProgress):
		return console.ErrUpdatePending
	case errors.Is(err, updater.ErrUpdateCheckRequired), errors.Is(err, updater.ErrUpdateTargetMismatch):
		return console.ErrUpdateStale
	case errors.Is(err, updater.ErrWebUpdateNotCapable), errors.Is(err, updater.ErrNoUpdateAvailable):
		return console.ErrUpdateUnavailable
	case errors.Is(err, updater.ErrInvalidUpdateRequest):
		return console.ErrUpdateInvalid
	default:
		return err
	}
}
