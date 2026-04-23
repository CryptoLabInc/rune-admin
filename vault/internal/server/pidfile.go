package server

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// PIDFile encapsulates the daemon's single-instance lock and recorded PID.
// The lock is held via flock(LOCK_EX|LOCK_NB) on the PID file itself for
// the lifetime of the daemon process; release happens on Close.
type PIDFile struct {
	path string
	f    *os.File
}

// AcquirePIDFile opens (creating if needed) the PID file at path, takes
// an exclusive non-blocking flock, then writes the current process PID
// atomically. Returns ErrPIDFileLocked if another process holds the lock.
func AcquirePIDFile(path string) (*PIDFile, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("pidfile: mkdir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("pidfile: open %s: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		// Read holder PID for a friendly error message.
		holder, _ := readPIDFromFile(f)
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, &ErrPIDFileLocked{Path: path, HolderPID: holder}
		}
		return nil, fmt.Errorf("pidfile: flock: %w", err)
	}
	// Truncate + write own PID atomically (no race because we hold the lock).
	if err := f.Truncate(0); err != nil {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
		return nil, fmt.Errorf("pidfile: truncate: %w", err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
		return nil, fmt.Errorf("pidfile: seek: %w", err)
	}
	if _, err := fmt.Fprintf(f, "%d\n", os.Getpid()); err != nil {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
		return nil, fmt.Errorf("pidfile: write: %w", err)
	}
	if err := f.Sync(); err != nil {
		// Sync best-effort; not fatal.
		_ = err
	}
	return &PIDFile{path: path, f: f}, nil
}

// Release unlocks and removes the PID file. Safe to call multiple times.
func (p *PIDFile) Release() {
	if p == nil || p.f == nil {
		return
	}
	_ = syscall.Flock(int(p.f.Fd()), syscall.LOCK_UN)
	_ = p.f.Close()
	_ = os.Remove(p.path)
	p.f = nil
}

// Path returns the pidfile path.
func (p *PIDFile) Path() string { return p.path }

// ReadPIDFile returns the PID recorded at path. Does NOT check liveness.
func ReadPIDFile(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return readPIDFromFile(f)
}

func readPIDFromFile(f *os.File) (int, error) {
	if _, err := f.Seek(0, 0); err != nil {
		return 0, err
	}
	body, err := io.ReadAll(f)
	if err != nil {
		return 0, err
	}
	s := strings.TrimSpace(string(body))
	if s == "" {
		return 0, errors.New("pidfile: empty")
	}
	pid, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("pidfile: parse pid: %w", err)
	}
	return pid, nil
}

// PIDLive checks whether the given PID is a live process by sending
// signal 0 (kernel-level existence check, no actual signal delivered).
func PIDLive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	// EPERM means the process exists but we can't signal it (different uid).
	return errors.Is(err, syscall.EPERM)
}

// ErrPIDFileLocked indicates another daemon already holds the PID file.
type ErrPIDFileLocked struct {
	Path      string
	HolderPID int
}

func (e *ErrPIDFileLocked) Error() string {
	if e.HolderPID > 0 {
		return fmt.Sprintf("another runevault daemon is already running (pid %d, pidfile %s)", e.HolderPID, e.Path)
	}
	return fmt.Sprintf("another runevault daemon is already running (pidfile %s)", e.Path)
}
