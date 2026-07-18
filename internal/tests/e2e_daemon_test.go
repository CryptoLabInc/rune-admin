//go:build e2e

package tests

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// freePort reserves an ephemeral loopback TCP port and returns it. The
// listener is closed before the daemon starts, so a parallel process could in
// principle steal the port — acceptable for a smoke test.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port
}

// TestDaemonBootsAndShutsDownCleanly is the E2E harness bootstrap: it boots
// the pre-built runeconsole binary (RUNECONSOLE_TEST_BINARY, exported by
// `mise run go:test:e2e`) against a minimal temp-dir config, waits for the
// console listener's /healthz, and SIGTERMs it expecting a clean exit. The
// YAML→SQLite importer E2E extends this harness once import-at-boot lands.
func TestDaemonBootsAndShutsDownCleanly(t *testing.T) {
	binary := os.Getenv("RUNECONSOLE_TEST_BINARY")
	if binary == "" {
		t.Skip("RUNECONSOLE_TEST_BINARY not set — run `mise run go:build` then `mise run go:test:e2e`")
	}

	dir := t.TempDir()
	grpcPort := freePort(t)
	consolePort := freePort(t)

	// Minimal boot config (shape mirrors livetest/dev-fullstack.sh):
	// TLS disabled (dev mode), no static runespace endpoint, console enabled
	// so /healthz is served. cloud.* is required-when-console-enabled but only
	// dialed on login, and the explicit members.console_endpoint skips the
	// boot-time public-IP probe (no network dependency in CI). First boot
	// generates FHE keys under keys.path, which dominates startup time.
	conf := fmt.Sprintf(`server:
  grpc: { host: 127.0.0.1, port: %d, tls: { disable: true } }
  console: { enabled: true, port: %d }
cloud: { api_base_url: https://cloud.invalid, web_base_url: https://cloud.invalid }
keys: { path: %s, embedding_dim: 1024 }
runespace: { endpoint: "" }
tokens: { team_secret: e2e-team-secret, tokens_file: %s }
members: { console_endpoint: "127.0.0.1:%d" }
audit: { mode: stdout }
`,
		grpcPort, consolePort,
		filepath.Join(dir, "keys"),
		filepath.Join(dir, "tokens.yml"),
		grpcPort)
	confPath := filepath.Join(dir, "runeconsole.conf")
	if err := os.WriteFile(confPath, []byte(conf), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(binary, "--config", confPath, "daemon", "start")
	logPath := filepath.Join(dir, "daemon.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = logFile.Close() }()
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		t.Fatalf("start daemon: %v", err)
	}
	started := time.Now()
	defer func() { _ = cmd.Process.Kill() }() // safety net; no-op after a clean Wait

	dumpLog := func() {
		data, rerr := os.ReadFile(logPath)
		if rerr == nil {
			t.Logf("daemon log:\n%s", data)
		}
	}

	// Readiness: the console listener answers /healthz once Serve is up.
	// First boot includes FHE key generation, so the budget is generous.
	healthURL := fmt.Sprintf("http://127.0.0.1:%d/healthz", consolePort)
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(120 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		if resp, herr := client.Get(healthURL); herr == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				ready = true
				break
			}
		}
		// A crashed daemon will never become ready; fail fast.
		if cmd.ProcessState != nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !ready {
		dumpLog()
		t.Fatalf("daemon not ready after %s (%s)", time.Since(started).Round(time.Second), healthURL)
	}

	// The daemon opens the unified store database unconditionally at boot: it
	// must exist beside tokens_file with owner-only permissions.
	dbInfo, serr := os.Stat(filepath.Join(dir, "runeconsole.db"))
	if serr != nil {
		dumpLog()
		t.Fatalf("store database missing after boot: %v", serr)
	}
	if perm := dbInfo.Mode().Perm(); perm != 0o600 {
		t.Errorf("store database mode = %04o, want 0600", perm)
	}

	// Graceful shutdown: SIGTERM must return a zero exit (Serve returns nil).
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signal daemon: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case werr := <-done:
		if werr != nil {
			dumpLog()
			t.Fatalf("daemon exited uncleanly after SIGTERM: %v", werr)
		}
	case <-time.After(30 * time.Second):
		dumpLog()
		_ = cmd.Process.Kill()
		t.Fatal("daemon did not exit within 30s of SIGTERM")
	}
}
