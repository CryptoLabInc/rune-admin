package commands

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CryptoLabInc/rune-admin/vault/internal/server"
	"github.com/CryptoLabInc/rune-admin/vault/internal/tokens"
)

// adminUDSFixture spins up a real UDS-backed admin server with a demo
// store. Exposes the socket path so AdminClient can dial it.
func adminUDSFixture(t *testing.T) (socket string, store *tokens.Store, shutdown func()) {
	t.Helper()
	// Darwin sun_path caps at ~104 bytes; t.TempDir() can overflow with
	// long test names. Use a short MkdirTemp.
	dir, err := os.MkdirTemp("", "vt-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket = filepath.Join(dir, "x.sock")

	store = tokens.NewStore()
	store.LoadDefaultsWithDemoToken()

	cfg := &server.Config{
		Server: server.ServerConfig{Admin: server.AdminConfig{Socket: socket}},
		Tokens: server.TokensConfig{TeamSecret: "test-secret"},
		Keys:   server.KeysConfig{Path: t.TempDir(), EmbeddingDim: 1024},
	}
	audit, _ := server.NewAuditLogger(server.AuditConfig{Mode: ""})
	v := server.NewVault(cfg, store, nil, audit)

	stop, err := server.AdminFromConfig(context.Background(), v)
	if err != nil {
		t.Fatal(err)
	}
	shutdown = func() { _ = stop(context.Background()) }
	t.Cleanup(shutdown)
	return socket, store, shutdown
}

func TestAdminClientHealth(t *testing.T) {
	socket, _, _ := adminUDSFixture(t)
	c, err := NewAdminClient(socket)
	if err != nil {
		t.Fatal(err)
	}
	var status struct {
		Status string `json:"status"`
	}
	if err := c.Do("GET", "/health", nil, &status); err != nil {
		t.Fatal(err)
	}
	if status.Status != "ok" {
		t.Errorf("status = %q", status.Status)
	}
}

func TestAdminClientIssueAndList(t *testing.T) {
	socket, _, _ := adminUDSFixture(t)
	c, _ := NewAdminClient(socket)

	var issued tokenResult
	if err := c.Do("POST", "/tokens", map[string]any{"user": "alice", "role": "member"}, &issued); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(issued.Token, "evt_") {
		t.Errorf("token = %q", issued.Token)
	}

	var listResp struct {
		Tokens []map[string]any `json:"tokens"`
	}
	if err := c.Do("GET", "/tokens", nil, &listResp); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, t := range listResp.Tokens {
		if t["user"] == "alice" {
			found = true
		}
	}
	if !found {
		t.Errorf("alice not in list: %+v", listResp.Tokens)
	}
}

func TestAdminClientErrorBubblesUp(t *testing.T) {
	socket, _, _ := adminUDSFixture(t)
	c, _ := NewAdminClient(socket)
	err := c.Do("POST", "/tokens", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error for missing fields")
	}
	if !strings.Contains(err.Error(), "Missing required") {
		t.Errorf("err = %v", err)
	}
}

func TestAdminClientMissingSocket(t *testing.T) {
	_, err := NewAdminClient("/tmp/no-such-socket")
	if err == nil {
		t.Fatal("expected error for missing socket")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("err = %v", err)
	}
}

// silence unused import if running tests in isolation
var _ = net.Listen
var _ = http.StatusOK
var _ = os.Stat
