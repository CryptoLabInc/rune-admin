package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/CryptoLabInc/rune-console/internal/server"
)

func TestWebUpdateStateMustStayInsideOfficialInstallRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, dir := range []string{"configs", "keys", "certs"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &server.Config{
		Source:  filepath.Join(root, "configs", "runeconsole.conf"),
		Storage: server.StorageConfig{DataDir: filepath.Join(root, "configs")},
		Keys:    server.KeysConfig{Path: filepath.Join(root, "keys")},
		Server: server.ServerConfig{GRPC: server.GRPCConfig{TLS: server.TLSConfig{
			Cert: filepath.Join(root, "certs", "server.pem"),
			Key:  filepath.Join(root, "certs", "server.key"),
			CA:   filepath.Join(root, "certs", "ca.pem"),
		}}},
	}
	if !webUpdateStateWithinInstallRoot(cfg) {
		t.Fatal("official install-root state was rejected")
	}

	external := t.TempDir()
	cfg.Server.GRPC.TLS.Key = filepath.Join(external, "server.key")
	if webUpdateStateWithinInstallRoot(cfg) {
		t.Fatal("external TLS state was accepted by the sandboxed web updater")
	}
}

func TestWebUpdateStateRejectsSymlinkEscape(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	configs := filepath.Join(root, "configs")
	if err := os.Mkdir(configs, 0o700); err != nil {
		t.Fatal(err)
	}
	external := t.TempDir()
	linkedKeys := filepath.Join(root, "keys")
	if err := os.Symlink(external, linkedKeys); err != nil {
		t.Fatal(err)
	}
	cfg := &server.Config{
		Source:  filepath.Join(configs, "runeconsole.conf"),
		Storage: server.StorageConfig{DataDir: configs},
		Keys:    server.KeysConfig{Path: linkedKeys},
	}
	if webUpdateStateWithinInstallRoot(cfg) {
		t.Fatal("symlinked state outside the install root was accepted")
	}
}
