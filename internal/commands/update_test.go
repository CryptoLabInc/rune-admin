package commands

import (
	"context"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/CryptoLabInc/rune-console/internal/server"
)

func TestUpdateCommandRequiresCompleteOfflineInputs(t *testing.T) {
	t.Parallel()
	called := false
	cmd := newUpdateCmdWithRunner(func(context.Context, io.Writer, updateOptions) error {
		called = true
		return nil
	})
	cmd.SetArgs([]string{"--archive", "release.tar.gz", "--version", "v1.1.0"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "--archive, --checksums, and --version") {
		t.Fatalf("error = %v", err)
	}
	if called {
		t.Fatal("runner called with incomplete offline inputs")
	}
}

func TestUpdateCommandPassesSafeFlags(t *testing.T) {
	t.Parallel()
	var got updateOptions
	cmd := newUpdateCmdWithRunner(func(_ context.Context, _ io.Writer, opts updateOptions) error {
		got = opts
		return nil
	})
	cmd.SetArgs([]string{
		"--archive", "/media/release.tar.gz",
		"--checksums", "/media/SHA256SUMS",
		"--version", "v1.1.0",
		"--dry-run",
		"--backup-dir", "/safe/backups",
		"--binary-path", "/custom/bin/runeconsole",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := updateOptions{
		archive: "/media/release.tar.gz", checksums: "/media/SHA256SUMS", version: "v1.1.0",
		dryRun: true, backupDir: "/safe/backups", binaryPath: "/custom/bin/runeconsole",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("options = %+v, want %+v", got, want)
	}
	if cmd.Flags().Lookup("force") != nil {
		t.Fatal("update command unexpectedly exposes --force")
	}
}

func TestDurableStatePathsIncludeDataDirDatabasesConfigKeysAndTLS(t *testing.T) {
	t.Parallel()
	cfg := &server.Config{
		Source:  "/opt/runeconsole/configs/runeconsole.conf",
		Storage: server.StorageConfig{DataDir: "/opt/runeconsole/data"},
		Keys:    server.KeysConfig{Path: "/opt/runeconsole/keys"},
		Server: server.ServerConfig{GRPC: server.GRPCConfig{TLS: server.TLSConfig{
			Cert: "/opt/runeconsole/certs/server.pem",
			Key:  "/opt/runeconsole/certs/server.key",
			CA:   "/opt/runeconsole/certs/ca.pem",
		}}},
	}
	got := durableStatePaths(cfg)
	for _, want := range []string{
		cfg.Source, cfg.Storage.DataDir,
		cfg.StoreDBPath(), cfg.StoreDBPath() + "-wal", cfg.StoreDBPath() + "-shm",
		cfg.ConsoleDBPath(), cfg.ConsoleDBPath() + "-wal", cfg.ConsoleDBPath() + "-shm",
		cfg.Keys.Path, cfg.Server.GRPC.TLS.Cert, cfg.Server.GRPC.TLS.Key, cfg.Server.GRPC.TLS.CA,
	} {
		if !containsString(got, want) {
			t.Errorf("durableStatePaths omitted required entry")
		}
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
