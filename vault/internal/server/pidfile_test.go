package server

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestAcquireWritesOwnPID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.pid")
	pf, err := AcquirePIDFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer pf.Release()

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(string(body))
	want := strconv.Itoa(os.Getpid())
	if got != want {
		t.Errorf("pid file = %q, want %q", got, want)
	}
}

func TestSecondAcquireFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.pid")
	pf1, err := AcquirePIDFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer pf1.Release()

	_, err = AcquirePIDFile(path)
	if err == nil {
		t.Fatal("second acquire returned nil error")
	}
	var locked *ErrPIDFileLocked
	if !errors.As(err, &locked) {
		t.Fatalf("err = %v, want ErrPIDFileLocked", err)
	}
	if locked.HolderPID != os.Getpid() {
		t.Errorf("holder pid = %d, want %d", locked.HolderPID, os.Getpid())
	}
}

func TestReleaseAllowsReacquire(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.pid")
	pf, err := AcquirePIDFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pf.Release()

	pf2, err := AcquirePIDFile(path)
	if err != nil {
		t.Fatalf("re-acquire after release: %v", err)
	}
	pf2.Release()
}

func TestReleaseRemovesPIDFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.pid")
	pf, _ := AcquirePIDFile(path)
	pf.Release()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("pid file should be removed, stat err = %v", err)
	}
}

func TestReadPIDFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.pid")
	if err := os.WriteFile(path, []byte("12345\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pid, err := ReadPIDFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if pid != 12345 {
		t.Errorf("pid = %d, want 12345", pid)
	}
}

func TestReadPIDFileMissing(t *testing.T) {
	_, err := ReadPIDFile("/tmp/no-such.pid")
	if err == nil {
		t.Error("expected error for missing pid file")
	}
}

func TestPIDLiveSelf(t *testing.T) {
	if !PIDLive(os.Getpid()) {
		t.Error("PIDLive(self) = false")
	}
}

func TestPIDLiveImpossible(t *testing.T) {
	if PIDLive(0) {
		t.Error("PIDLive(0) = true")
	}
	if PIDLive(-1) {
		t.Error("PIDLive(-1) = true")
	}
	// PID 99999999 is well above any reasonable kernel max
	if PIDLive(99999999) {
		t.Error("PIDLive(99999999) = true (process should not exist)")
	}
}
