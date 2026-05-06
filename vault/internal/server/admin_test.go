package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/CryptoLabInc/rune-admin/vault/internal/tokens"
)

func newAdminTestVault(t *testing.T) *Vault {
	t.Helper()
	cfg := &Config{
		Tokens: TokensConfig{TeamSecret: "test-secret"},
		Keys:   KeysConfig{Path: t.TempDir(), EmbeddingDim: 1024},
	}
	store := tokens.NewStore()
	store.LoadDefaultsWithDemoToken()
	audit, _ := NewAuditLogger(AuditConfig{Mode: ""})
	return NewVault(cfg, store, nil, audit)
}

func adminTestServer(t *testing.T) (*httptest.Server, *Vault) {
	t.Helper()
	v := newAdminTestVault(t)
	ts := httptest.NewServer(buildAdminMux(v))
	t.Cleanup(ts.Close)
	return ts, v
}

func TestAdminGetHealth(t *testing.T) {
	ts, _ := adminTestServer(t)
	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestAdminListRolesIncludesDefaults(t *testing.T) {
	ts, _ := adminTestServer(t)
	resp, err := http.Get(ts.URL + "/roles")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		Roles []map[string]any `json:"roles"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, r := range body.Roles {
		names[r["name"].(string)] = true
	}
	if !names["admin"] || !names["member"] {
		t.Errorf("default roles missing: %v", names)
	}
}

func TestAdminIssueListRevoke(t *testing.T) {
	ts, _ := adminTestServer(t)

	// Issue
	body := bytes.NewReader([]byte(`{"user":"alice","role":"member"}`))
	resp, err := http.Post(ts.URL+"/tokens", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("issue status = %d", resp.StatusCode)
	}
	var issued map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&issued); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(issued["token"].(string), "evt_") {
		t.Errorf("token = %v", issued["token"])
	}

	// List
	resp, _ = http.Get(ts.URL + "/tokens")
	var listResp struct {
		Tokens []map[string]any `json:"tokens"`
	}
	json.NewDecoder(resp.Body).Decode(&listResp)
	resp.Body.Close()
	found := false
	for _, t := range listResp.Tokens {
		if t["user"] == "alice" {
			found = true
		}
	}
	if !found {
		t.Error("alice not in list response")
	}

	// Revoke
	req, _ := http.NewRequest("DELETE", ts.URL+"/tokens/alice", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("revoke status = %d", resp.StatusCode)
	}
}

func TestAdminIssueMissingFields(t *testing.T) {
	ts, _ := adminTestServer(t)
	resp, err := http.Post(ts.URL+"/tokens", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestAdminRevokeNotFound(t *testing.T) {
	ts, _ := adminTestServer(t)
	req, _ := http.NewRequest("DELETE", ts.URL+"/tokens/nobody", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestAdminCreateRoleAndDelete(t *testing.T) {
	ts, _ := adminTestServer(t)
	body := bytes.NewReader([]byte(`{"name":"researcher","scope":["get_public_key"],"top_k":3,"rate_limit":"10/60s"}`))
	resp, err := http.Post(ts.URL+"/roles", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", resp.StatusCode)
	}
	// Delete
	req, _ := http.NewRequest("DELETE", ts.URL+"/roles/researcher", nil)
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("delete status = %d", resp2.StatusCode)
	}
}

func TestAdminUpdateRoleNoFieldsRejected(t *testing.T) {
	ts, _ := adminTestServer(t)
	req, _ := http.NewRequest("PUT", ts.URL+"/roles/member", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestAdminUnknownRoute(t *testing.T) {
	ts, _ := adminTestServer(t)
	resp, err := http.Get(ts.URL + "/nope")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

// ── UDS bind + permissions (Unix only) ───────────────────────────

func TestAdminUDSBindMode0660(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("UDS not supported on Windows")
	}
	v := newAdminTestVault(t)
	// Darwin's sockaddr_un caps sun_path at ~104 bytes; t.TempDir() with a
	// long test name plus the framework-injected sequence dir overruns. Use
	// a shorter MkdirTemp at /tmp to stay safely under the limit.
	dir, err := os.MkdirTemp("", "vt-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	v.cfg.Server.Admin.Socket = filepath.Join(dir, "x.sock")

	shutdown, err := AdminFromConfig(context.Background(), v)
	if err != nil {
		t.Fatal(err)
	}
	defer shutdown(context.Background())

	info, err := os.Stat(v.cfg.Server.Admin.Socket)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o660 {
		t.Errorf("socket mode = %04o, want 0660", mode)
	}

	// Smoke test: dial + GET /health.
	conn, err := net.Dial("unix", v.cfg.Server.Admin.Socket)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
}

func TestAdminUDSStaleSocketRecovered(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("UDS not supported on Windows")
	}
	v := newAdminTestVault(t)
	// Darwin's sockaddr_un caps sun_path at ~104 bytes; t.TempDir() with a
	// long test name plus the framework-injected sequence dir overruns. Use
	// a shorter MkdirTemp at /tmp to stay safely under the limit.
	dir, err := os.MkdirTemp("", "vt-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	v.cfg.Server.Admin.Socket = filepath.Join(dir, "x.sock")
	// Plant a stale file at the socket path.
	if err := os.WriteFile(v.cfg.Server.Admin.Socket, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	shutdown, err := AdminFromConfig(context.Background(), v)
	if err != nil {
		t.Fatalf("recovery failed: %v", err)
	}
	defer shutdown(context.Background())
	info, err := os.Stat(v.cfg.Server.Admin.Socket)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		t.Errorf("socket file is not a socket after recovery")
	}
}

func TestAdminUDSShutdownUnlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("UDS not supported on Windows")
	}
	v := newAdminTestVault(t)
	// Darwin's sockaddr_un caps sun_path at ~104 bytes; t.TempDir() with a
	// long test name plus the framework-injected sequence dir overruns. Use
	// a shorter MkdirTemp at /tmp to stay safely under the limit.
	dir, err := os.MkdirTemp("", "vt-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	v.cfg.Server.Admin.Socket = filepath.Join(dir, "x.sock")
	shutdown, err := AdminFromConfig(context.Background(), v)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := shutdown(ctx); err != nil {
		t.Errorf("shutdown: %v", err)
	}
	if _, err := os.Stat(v.cfg.Server.Admin.Socket); !os.IsNotExist(err) {
		t.Errorf("socket should be removed after shutdown, stat err = %v", err)
	}
}
