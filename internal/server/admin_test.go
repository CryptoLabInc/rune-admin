package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CryptoLabInc/rune-console/internal/crypto"
	"github.com/CryptoLabInc/rune-console/internal/groups"
	"github.com/CryptoLabInc/rune-console/internal/tokens"
)

func newAdminTestConsole(t *testing.T) *Console {
	t.Helper()
	cfg := &Config{
		Tokens: TokensConfig{TeamSecret: "test-secret"},
		Keys:   KeysConfig{Path: t.TempDir(), EmbeddingDim: 1024},
	}
	store := tokens.NewStore()
	store.LoadDefaultsWithDemoToken()
	audit, _ := NewAuditLogger(AuditConfig{Mode: ""})
	return NewConsole(cfg, store, groups.NewStore(), nil, audit)
}

func adminTestServer(t *testing.T) (*httptest.Server, *Console) {
	t.Helper()
	v := newAdminTestConsole(t)
	ts := httptest.NewServer(buildAdminMux(v, nil))
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

// TestAdminActorPrefersSessionPrincipal pins the anti-forgery contract on the
// /admin surface: withActor tags each request with the authenticated session
// principal, and adminActor must prefer it over any client-supplied actor so the
// audit/grant identity cannot be forged. The declared value is used only as a
// fallback when no session principal is present.
func TestAdminActorPrefersSessionPrincipal(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/tokens", nil)
	withPrincipal := req.WithContext(WithActor(req.Context(), "owner@corp.com"))
	if got := adminActor(withPrincipal, "attacker@evil.com"); got != "owner@corp.com" {
		t.Errorf("adminActor with a session principal = %q, want owner@corp.com (the declared value must not win)", got)
	}
	if got := adminActor(req, "cli-operator"); got != "cli-operator" {
		t.Errorf("adminActor without a session principal = %q, want the declared fallback cli-operator", got)
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

// TestAdminTokenLifecycleAudited: issue/rotate/rotate_all/revoke must each
// leave an audit entry naming the operation (method), who acted (user_id),
// and whose credential changed (target) — the credential lifecycle is the
// first thing an auditor asks about.
func TestAdminTokenLifecycleAudited(t *testing.T) {
	v := newAdminTestConsole(t)
	logPath := filepath.Join(t.TempDir(), "audit.log")
	audit, err := NewAuditLogger(AuditConfig{Mode: "file", Path: logPath})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { audit.Close() })
	v.audit = audit
	ts := httptest.NewServer(buildAdminMux(v, nil))
	t.Cleanup(ts.Close)

	do := func(req *http.Request, wantStatus int, label string) {
		t.Helper()
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != wantStatus {
			t.Fatalf("%s status = %d, want %d", label, resp.StatusCode, wantStatus)
		}
	}

	issue, _ := http.NewRequest("POST", ts.URL+"/tokens",
		bytes.NewReader([]byte(`{"user":"alice","role":"member","actor":"boss@x.io"}`)))
	issue.Header.Set("Content-Type", "application/json")
	do(issue, http.StatusCreated, "issue")
	rotate, _ := http.NewRequest("POST", ts.URL+"/tokens/alice/rotate?actor=boss@x.io", nil)
	do(rotate, http.StatusOK, "rotate")
	rotateAll, _ := http.NewRequest("POST", ts.URL+"/tokens/_rotate_all?actor=boss@x.io", nil)
	do(rotateAll, http.StatusOK, "rotate_all")
	revoke, _ := http.NewRequest("DELETE", ts.URL+"/tokens/alice?actor=boss@x.io", nil)
	do(revoke, http.StatusOK, "revoke")

	audit.Close() // flush before reading
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]AuditEntry{} // method → entry
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var e AuditEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("bad audit line %q: %v", line, err)
		}
		got[e.Method] = e
	}
	for method, target := range map[string]string{
		"admin.token.issue":  "alice",
		"admin.token.rotate": "alice",
		"admin.token.revoke": "alice",
	} {
		e, ok := got[method]
		if !ok {
			t.Errorf("no audit entry for %s (got %v)", method, keysOf(got))
			continue
		}
		if e.Target != target {
			t.Errorf("%s target = %q, want %q", method, e.Target, target)
		}
		if e.UserID != "local-admin:boss@x.io" {
			t.Errorf("%s user_id = %q, want local-admin:<actor>", method, e.UserID)
		}
	}
	if _, ok := got["admin.token.rotate_all"]; !ok {
		t.Errorf("no audit entry for admin.token.rotate_all (got %v)", keysOf(got))
	}
}

func keysOf(m map[string]AuditEntry) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
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

// TestAdminRoleLifecycleAudited: create/update/delete on /roles must each leave
// an audit entry. A role defines a token's scope and ceilings, so a role
// mutation IS a permission-policy change — the exact thing an auditor must be
// able to reconstruct. Mirrors TestAdminTokenLifecycleAudited.
func TestAdminRoleLifecycleAudited(t *testing.T) {
	v := newAdminTestConsole(t)
	logPath := filepath.Join(t.TempDir(), "audit.log")
	audit, err := NewAuditLogger(AuditConfig{Mode: "file", Path: logPath})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { audit.Close() })
	v.audit = audit
	ts := httptest.NewServer(buildAdminMux(v, nil))
	t.Cleanup(ts.Close)

	do := func(req *http.Request, wantStatus int, label string) {
		t.Helper()
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != wantStatus {
			t.Fatalf("%s status = %d, want %d", label, resp.StatusCode, wantStatus)
		}
	}

	create, _ := http.NewRequest("POST", ts.URL+"/roles",
		bytes.NewReader([]byte(`{"name":"auditor","scope":["get_public_key"],"top_k":3,"rate_limit":"10/60s","actor":"boss@x.io"}`)))
	do(create, http.StatusCreated, "create")
	update, _ := http.NewRequest("PUT", ts.URL+"/roles/auditor",
		bytes.NewReader([]byte(`{"top_k":5,"actor":"boss@x.io"}`)))
	do(update, http.StatusOK, "update")
	del, _ := http.NewRequest("DELETE", ts.URL+"/roles/auditor?actor=boss@x.io", nil)
	do(del, http.StatusOK, "delete")

	audit.Close() // flush before reading
	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]AuditEntry{} // method → entry
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var e AuditEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("bad audit line %q: %v", line, err)
		}
		got[e.Method] = e
	}
	for _, method := range []string{"admin.role.create", "admin.role.update", "admin.role.delete"} {
		e, ok := got[method]
		if !ok {
			t.Errorf("no audit entry for %s (got %v)", method, keysOf(got))
			continue
		}
		if e.Target != "auditor" {
			t.Errorf("%s target = %q, want %q", method, e.Target, "auditor")
		}
		if e.UserID != "local-admin:boss@x.io" {
			t.Errorf("%s user_id = %q, want local-admin:<actor>", method, e.UserID)
		}
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

// deleteTestGroup creates a group over the admin API and returns its id.
func deleteTestGroup(t *testing.T, ts *httptest.Server, name string) string {
	t.Helper()
	resp, err := http.Post(ts.URL+"/groups", "application/json",
		bytes.NewReader([]byte(`{"name":"`+name+`"}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create group status = %d", resp.StatusCode)
	}
	var g struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&g); err != nil {
		t.Fatal(err)
	}
	return g.ID
}

// TestAdminDeleteGroupPurgesTag: a successful group delete must hand the dead
// group's id (its tag) to the engine's PurgeTag sweep and report the outcome
// in the response, without affecting the delete result.
func TestAdminDeleteGroupPurgesTag(t *testing.T) {
	ts, v := adminTestServer(t)
	fe := &fakeEngine{purgeRes: crypto.PurgeResult{Purged: 3}}
	v.engine = fe
	v.UseEngineTagStats() // fake stats report no sole-tag records: guard passes

	id := deleteTestGroup(t, ts, "purge-me")
	req, _ := http.NewRequest("DELETE", ts.URL+"/groups/purge-me", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete status = %d", resp.StatusCode)
	}
	if fe.purgedTag != id {
		t.Errorf("PurgeTag got tag %q, want group id %q", fe.purgedTag, id)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body["tag_cleanup"], "removed from 3 items") {
		t.Errorf("tag_cleanup = %q, want purge count report", body["tag_cleanup"])
	}
}

// TestAdminDeleteGroupPurgeUnsupportedIsBenign: while the runespace bulk
// tag-removal RPC has not shipped, PurgeTag returns ErrPurgeTagUnsupported —
// the delete must still succeed and the response must say the sweep was
// skipped.
func TestAdminDeleteGroupPurgeUnsupportedIsBenign(t *testing.T) {
	ts, v := adminTestServer(t)
	fe := &fakeEngine{purgeErr: crypto.ErrPurgeTagUnsupported}
	v.engine = fe
	v.UseEngineTagStats()

	deleteTestGroup(t, ts, "purge-later")
	req, _ := http.NewRequest("DELETE", ts.URL+"/groups/purge-later", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete status = %d", resp.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body["tag_cleanup"], "not yet available") {
		t.Errorf("tag_cleanup = %q, want unsupported-skip report", body["tag_cleanup"])
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
