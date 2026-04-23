package tests

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestCLISmokeEndToEnd builds the runevault binary, boots it as a daemon
// against a tmp config (TLS disabled, no FHE key generation needed because
// we never call DecryptScores), exercises issue/list/revoke through the
// admin UDS, then verifies daemon stop.
//
// Skipped under -short.
func TestCLISmokeEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping CLI smoke under -short")
	}

	repoRoot := RepoRoot()
	tmp, err := os.MkdirTemp("", "vts-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(tmp) })

	binary := filepath.Join(tmp, "runevault")
	build := exec.Command("go", "build", "-o", binary, "./cmd/runevault")
	build.Dir = filepath.Join(repoRoot, "vault")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	confPath := filepath.Join(tmp, "runevault.conf")
	conf := fmt.Sprintf(`daemon:
  pid_file: %[1]s/runevault.pid
server:
  grpc:
    host: 127.0.0.1
    port: 53052
    tls:
      disable: true
  admin:
    socket: %[1]s/x.sock
keys:
  path: %[1]s/keys
  embedding_dim: 1024
envector:
  endpoint: ""
  api_key: ""
tokens:
  team_secret: smoke-secret
  roles_file: %[1]s/roles.yml
  tokens_file: %[1]s/tokens.yml
audit:
  mode: stdout
`, tmp)
	if err := os.WriteFile(confPath, []byte(conf), 0o600); err != nil {
		t.Fatal(err)
	}

	// Spawn daemon in background.
	daemon := exec.Command(binary, "--config", confPath, "daemon", "start")
	logFile, err := os.Create(filepath.Join(tmp, "daemon.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	daemon.Stdout = logFile
	daemon.Stderr = logFile
	if err := daemon.Start(); err != nil {
		t.Fatalf("daemon start: %v", err)
	}
	daemonPID := daemon.Process.Pid
	// Reap the daemon promptly when it exits — without this, the kernel
	// keeps it as a zombie and PIDLive (kill -0) returns true even though
	// the daemon has already finished its shutdown sequence. That defeats
	// `daemon stop`'s liveness poll.
	waitDone := make(chan error, 1)
	go func() { waitDone <- daemon.Wait() }()
	t.Cleanup(func() {
		// Send SIGKILL via syscall so we don't race with cmd.Wait's fd close.
		_ = syscall.Kill(daemonPID, syscall.SIGKILL)
		select {
		case <-waitDone:
		case <-time.After(2 * time.Second):
		}
	})

	// Wait for the admin socket to appear (FHE key generation can take a
	// few seconds on cold starts).
	socket := filepath.Join(tmp, "x.sock")
	if !waitFor(socket, 30*time.Second) {
		body, _ := os.ReadFile(filepath.Join(tmp, "daemon.log"))
		t.Fatalf("admin socket never appeared\ndaemon log:\n%s", body)
	}

	run := func(args ...string) (string, error) {
		cmd := exec.Command(binary, append([]string{"--config", confPath}, args...)...)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	// ── status ──
	out, err := run("status")
	if err != nil {
		t.Fatalf("status: %v\n%s", err, out)
	}
	if !strings.Contains(out, "PID:") || !strings.Contains(out, "ok)") {
		t.Errorf("status output unexpected:\n%s", out)
	}

	// ── token issue ──
	out, err = run("token", "issue", "--user", "alice", "--role", "member", "--expires", "30d")
	if err != nil {
		t.Fatalf("issue: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Token issued for 'alice'") {
		t.Errorf("issue output: %s", out)
	}
	if !strings.Contains(out, "evt_") {
		t.Errorf("token not in output: %s", out)
	}

	// ── token list ──
	out, err = run("token", "list")
	if err != nil {
		t.Fatalf("list: %v\n%s", err, out)
	}
	if !strings.Contains(out, "alice") || !strings.Contains(out, "member") {
		t.Errorf("list output missing alice: %s", out)
	}

	// ── role list (defaults present) ──
	out, err = run("role", "list")
	if err != nil {
		t.Fatalf("role list: %v\n%s", err, out)
	}
	if !strings.Contains(out, "admin") || !strings.Contains(out, "member") {
		t.Errorf("role list output: %s", out)
	}

	// ── token revoke ──
	out, err = run("token", "revoke", "--user", "alice")
	if err != nil {
		t.Fatalf("revoke: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Revoked") {
		t.Errorf("revoke output: %s", out)
	}

	// ── daemon stop ──
	out, err = run("--timeout", "30s", "daemon", "stop")
	if err != nil {
		body, _ := os.ReadFile(filepath.Join(tmp, "daemon.log"))
		t.Fatalf("stop: %v\n%s\n--- daemon log ---\n%s", err, out, body)
	}
	if !strings.Contains(out, "stopped") {
		t.Errorf("stop output: %s", out)
	}

	// Daemon process should exit on its own after SIGTERM.
	select {
	case <-waitDone:
	case <-time.After(5 * time.Second):
		t.Errorf("daemon did not exit after stop")
	}
}

func waitFor(path string, dur time.Duration) bool {
	deadline := time.Now().Add(dur)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}
