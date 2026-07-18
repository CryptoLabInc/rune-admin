package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/CryptoLabInc/rune-console/internal/db"
	"github.com/CryptoLabInc/rune-console/internal/groups"
	"github.com/CryptoLabInc/rune-console/internal/invites"
	"github.com/CryptoLabInc/rune-console/internal/members"
	"github.com/CryptoLabInc/rune-console/internal/storedb"
	"github.com/CryptoLabInc/rune-console/internal/tokens"
)

// memberAdminServer wires a member subsystem over the given console and returns
// the test server plus the mail-log path. Like the daemon, it injects the
// member-UUID person-key contract into the groups store: with a member
// subsystem wired, memberships are keyed by member id, never by email.
func memberAdminServer(t *testing.T, v *Console) (*httptest.Server, string) {
	t.Helper()
	v.Groups().SetPersonKeyValidator(members.ValidateID)
	mailLog := filepath.Join(t.TempDir(), "mail.log")
	ms := &memberSubsystem{
		members: members.NewStore(),
		invites: invites.NewStore(),
		mailer:  NewLogMailer(mailLog),
		conn:    InviteConnInfo{ConsoleEndpoint: "console.example:8443"},
		ttl:     30 * time.Minute,
	}
	ts := httptest.NewServer(buildAdminMux(v, ms))
	t.Cleanup(ts.Close)
	return ts, mailLog
}

func newFileAuditConsole(t *testing.T, auditPath string) *Console {
	t.Helper()
	cfg := &Config{
		Tokens: TokensConfig{TeamSecret: "test-secret"},
		Keys:   KeysConfig{Path: t.TempDir(), EmbeddingDim: 1024},
	}
	store := tokens.NewStore()
	store.LoadDefaultsWithDemoToken()
	audit, err := NewAuditLogger(AuditConfig{Mode: "file", Path: auditPath})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = audit.Close() })
	return NewConsole(cfg, store, groups.NewStore(), nil, audit)
}

func createMember(t *testing.T, ts *httptest.Server, email string) string {
	t.Helper()
	body := `{"email":"` + email + `","display_name":"X","actor":"heeyeon"}`
	resp, err := http.Post(ts.URL+"/members", "application/json", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create member status = %d", resp.StatusCode)
	}
	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatal(err)
	}
	return m["id"].(string)
}

func TestAdminPostMembers(t *testing.T) {
	ts, _ := memberAdminServer(t, newAdminTestConsole(t))

	// Valid create → 201 with registered status (invite not yet issued).
	resp, err := http.Post(ts.URL+"/members", "application/json",
		bytes.NewReader([]byte(`{"email":"alice@corp.com","display_name":"Alice","actor":"heeyeon"}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	var m map[string]any
	json.NewDecoder(resp.Body).Decode(&m)
	if m["status"] != "registered" || m["email"] != "alice@corp.com" {
		t.Errorf("member = %+v", m)
	}

	// Missing email → 400.
	resp2, err := http.Post(ts.URL+"/members", "application/json", bytes.NewReader([]byte(`{"display_name":"x"}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusBadRequest {
		t.Errorf("missing email status = %d, want 400", resp2.StatusCode)
	}

	// Duplicate email → 409.
	resp3, err := http.Post(ts.URL+"/members", "application/json",
		bytes.NewReader([]byte(`{"email":"alice@corp.com","display_name":"Al2"}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusConflict {
		t.Errorf("duplicate status = %d, want 409", resp3.StatusCode)
	}
}

func TestAdminPatchMembers(t *testing.T) {
	ts, _ := memberAdminServer(t, newAdminTestConsole(t))
	id := createMember(t, ts, "patch@corp.com")

	// Partial update: change display_name only.
	req, _ := http.NewRequest("PATCH", ts.URL+"/members/"+id,
		bytes.NewReader([]byte(`{"display_name":"Renamed","actor":"heeyeon"}`)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch status = %d, want 200", resp.StatusCode)
	}
	var m map[string]any
	json.NewDecoder(resp.Body).Decode(&m)
	if m["display_name"] != "Renamed" || m["email"] != "patch@corp.com" {
		t.Errorf("patched member = %+v", m)
	}

	// Status update still works: disabling a registered member lands 200.
	respS := patchMember(t, ts, id, `{"status":"disabled","actor":"heeyeon"}`)
	defer respS.Body.Close()
	if respS.StatusCode != http.StatusOK {
		t.Fatalf("status patch = %d, want 200", respS.StatusCode)
	}
	if status, _ := memberStatusByEmail(t, ts, "patch@corp.com"); status != "disabled" {
		t.Errorf("status after PATCH = %q, want disabled", status)
	}

	// A body with none of the updatable fields → 400.
	respN := patchMember(t, ts, id, `{"actor":"heeyeon"}`)
	defer respN.Body.Close()
	if respN.StatusCode != http.StatusBadRequest {
		t.Errorf("no-field patch = %d, want 400", respN.StatusCode)
	}

	// Non-existent id → 404.
	req2, _ := http.NewRequest("PATCH", ts.URL+"/members/no-such-id",
		bytes.NewReader([]byte(`{"display_name":"x"}`)))
	req2.Header.Set("Content-Type", "application/json")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotFound {
		t.Errorf("patch missing status = %d, want 404", resp2.StatusCode)
	}
}

func TestAdminInviteIssuesWrappedToken(t *testing.T) {
	v := newAdminTestConsole(t)
	ts, mailLog := memberAdminServer(t, v)
	id := createMember(t, ts, "invitee@corp.com")

	resp, err := http.Post(ts.URL+"/members/"+id+"/invite", "application/json",
		bytes.NewReader([]byte(`{"role":"member","actor":"heeyeon"}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("invite status = %d, want 201", resp.StatusCode)
	}
	raw, _ := io.ReadAll(resp.Body)
	// The response is the clear bundle: a handle, but never the evt_ token.
	if !strings.Contains(string(raw), `"handle"`) {
		t.Errorf("invite response missing handle: %s", raw)
	}
	if strings.Contains(string(raw), "evt_") {
		t.Errorf("invite response leaked an evt_ token: %s", raw)
	}

	// The token store now holds a token for the member email.
	found := false
	for _, tk := range v.Tokens().ListTokens() {
		if tk.User == "invitee@corp.com" {
			found = true
		}
	}
	if !found {
		t.Error("no token issued for invitee@corp.com")
	}

	// The mailer recorded exactly one line.
	data, err := os.ReadFile(mailLog)
	if err != nil {
		t.Fatal(err)
	}
	lines := 0
	for _, ln := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if strings.TrimSpace(ln) != "" {
			lines++
		}
	}
	if lines != 1 {
		t.Errorf("mail log has %d lines, want 1: %s", lines, data)
	}
	if strings.Contains(string(data), "evt_") {
		t.Errorf("mail log leaked an evt_ token: %s", data)
	}
}

func TestAdminInviteAuditEntry(t *testing.T) {
	auditPath := filepath.Join(t.TempDir(), "audit.log")
	v := newFileAuditConsole(t, auditPath)
	ts, _ := memberAdminServer(t, v)
	id := createMember(t, ts, "audited@corp.com")

	resp, err := http.Post(ts.URL+"/members/"+id+"/invite", "application/json",
		bytes.NewReader([]byte(`{"role":"member","actor":"heeyeon"}`)))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("invite status = %d, want 201", resp.StatusCode)
	}

	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"method":"admin.invite.issue"`) {
		t.Errorf("audit missing admin.invite.issue: %s", data)
	}
	if !strings.Contains(string(data), `"user_id":"local-admin:heeyeon"`) {
		t.Errorf("audit missing local-admin actor: %s", data)
	}
}

// emptyTagStats satisfies groups.TagStatsProvider with zero counts so the
// group-delete sole-tag guard passes in tests without a live runespace.
type emptyTagStats struct{}

func (emptyTagStats) GetTagStats([]string) (map[string]groups.TagStat, error) {
	return map[string]groups.TagStat{}, nil
}

// TestAdminAuditTargets: every admin mutation must record WHAT it acted on
// (AuditEntry.target) — the member's email, the group (delete also keeps the
// immutable id since names can be reused), and the "user @ group" membership.
// TestAdminGroupRevokeCutsInheritedRead pins that POST /groups/{ref}/revoke cuts
// the member's ACCESS to the group, not just the stored direct row: a member who
// also inherits the group from an ancestor keeps inherited read otherwise (the
// hole d0f451a fixed on the console removal axes; this operator path had stayed
// on plain Revoke). Fails against the old plain-Revoke behavior.
func TestAdminGroupRevokeCutsInheritedRead(t *testing.T) {
	v := newAdminTestConsole(t)
	gs := v.Groups()
	parent, err := gs.CreateGroup("Div", "")
	if err != nil {
		t.Fatal(err)
	}
	child, err := gs.CreateGroup("Team", parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	ts, _ := memberAdminServer(t, v)
	uid := createMember(t, ts, "lead@corp.com")
	// Direct member of the child (a row to remove) AND of the ancestor (so the
	// child is also reached by inheritance).
	if _, err := gs.Grant(uid, child.ID, groups.RoleRead, "seed"); err != nil {
		t.Fatal(err)
	}
	if _, err := gs.Grant(uid, parent.ID, groups.RoleEdit, "seed"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(gs.RecallScope(uid), child.ID) {
		t.Fatal("precondition: child should be in the member's recall scope")
	}

	resp, err := http.Post(ts.URL+"/groups/"+child.ID+"/revoke", "application/json",
		bytes.NewReader([]byte(`{"user":"lead@corp.com","actor":"heeyeon"}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("revoke status = %d, want 200", resp.StatusCode)
	}

	// The inherited read is cut even though the member still holds the ancestor.
	if slices.Contains(gs.RecallScope(uid), child.ID) {
		t.Errorf("RecallScope still contains the child after revoke — inherited read was NOT cut")
	}
	if !slices.Contains(gs.RecallScope(uid), parent.ID) {
		t.Errorf("RecallScope lost the ancestor — revoke must cut the child only")
	}
}

func TestAdminAuditTargets(t *testing.T) {
	auditPath := filepath.Join(t.TempDir(), "audit.log")
	v := newFileAuditConsole(t, auditPath)
	v.SetTagStats(emptyTagStats{})
	ts, _ := memberAdminServer(t, v)
	createMember(t, ts, "kim@corp.com")

	post := func(path, body, label string, want int) {
		t.Helper()
		resp, err := http.Post(ts.URL+path, "application/json", bytes.NewReader([]byte(body)))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Fatalf("%s status = %d, want %d", label, resp.StatusCode, want)
		}
	}
	post("/groups", `{"name":"팀A","actor":"heeyeon"}`, "create group", http.StatusCreated)
	post("/groups/팀A/grant", `{"user":"kim@corp.com","role":"write","actor":"heeyeon"}`, "grant", http.StatusCreated)
	post("/groups/팀A/revoke", `{"user":"kim@corp.com","actor":"heeyeon"}`, "revoke", http.StatusOK)
	del, _ := http.NewRequest("DELETE", ts.URL+"/groups/팀A?actor=heeyeon", nil)
	resp, err := http.DefaultClient.Do(del)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete group status = %d", resp.StatusCode)
	}

	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatal(err)
	}
	byMethod := map[string]AuditEntry{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var e AuditEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("bad audit line %q: %v", line, err)
		}
		byMethod[e.Method] = e
	}
	for method, target := range map[string]string{
		"admin.member.create": "kim@corp.com",
		"admin.group.create":  "팀A",
		"admin.group.grant":   "kim@corp.com @ 팀A (write)",
		"admin.group.revoke":  "kim@corp.com @ 팀A",
	} {
		e, ok := byMethod[method]
		if !ok {
			t.Errorf("audit missing %s entry", method)
			continue
		}
		if e.Target != target {
			t.Errorf("%s target = %q, want %q", method, e.Target, target)
		}
	}
	// Delete target carries "name (immutable-id)" — assert the name half and
	// the parenthesized-id shape without hardcoding the UUID.
	if e, ok := byMethod["admin.group.delete"]; !ok {
		t.Error("audit missing admin.group.delete entry")
	} else if !strings.HasPrefix(e.Target, "팀A (") || !strings.HasSuffix(e.Target, ")") {
		t.Errorf("admin.group.delete target = %q, want \"팀A (<id>)\"", e.Target)
	}
}

// getMembers returns the decoded member list from GET /members.
func getMembers(t *testing.T, ts *httptest.Server) []map[string]any {
	t.Helper()
	resp, err := http.Get(ts.URL + "/members")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Members []map[string]any `json:"members"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out.Members
}

// memberStatusByEmail finds a member by email and returns its status; ok is
// false when no member has that email.
func memberStatusByEmail(t *testing.T, ts *httptest.Server, email string) (string, bool) {
	t.Helper()
	for _, m := range getMembers(t, ts) {
		if m["email"] == email {
			return m["status"].(string), true
		}
	}
	return "", false
}

func TestAdminRegisterWithGroupGrantAtomic(t *testing.T) {
	v := newAdminTestConsole(t)
	g, err := v.Groups().CreateGroup("eng", "")
	if err != nil {
		t.Fatal(err)
	}
	ts, _ := memberAdminServer(t, v)

	body := `{"email":"reg@corp.com","display_name":"Reg","group":"` + g.ID + `","group_role":"write","actor":"heeyeon"}`
	resp, err := http.Post(ts.URL+"/members", "application/json", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	var m map[string]any
	json.NewDecoder(resp.Body).Decode(&m)
	if m["status"] != "registered" {
		t.Errorf("status = %v, want registered (invite not yet issued)", m["status"])
	}
	// The grant committed: a membership keyed by the member UUID (never the
	// email) exists on the group.
	memberID := m["id"].(string)
	found := false
	for _, ms := range v.Groups().ListMemberships() {
		if ms.User == "reg@corp.com" {
			t.Errorf("membership keyed by email %q, want member UUID", ms.User)
		}
		if ms.User == memberID && ms.GroupID == g.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("no membership for member id %s (reg@corp.com) on group %s", memberID, g.ID)
	}
}

func TestAdminRegisterGrantRollsBackMemberOnFailure(t *testing.T) {
	v := newAdminTestConsole(t)
	ts, _ := memberAdminServer(t, v)

	// Grant onto a group that does not exist: the register+grant transaction
	// fails and the just-created member must be rolled back entirely.
	body := `{"email":"ghost@corp.com","display_name":"G","group":"no-such-group","group_role":"write","actor":"heeyeon"}`
	resp, err := http.Post(ts.URL+"/members", "application/json", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusCreated {
		t.Fatalf("status = 201, want a failure (grant should have been rejected)")
	}
	// Rollback: the member left no trace.
	if _, ok := memberStatusByEmail(t, ts, "ghost@corp.com"); ok {
		t.Errorf("member ghost@corp.com survived a failed grant (rollback did not run)")
	}
	// And the freed email can be registered again (plain register).
	if status := createAndStatus(t, ts, "ghost@corp.com"); status != "registered" {
		t.Errorf("re-register after rollback status = %q, want registered", status)
	}
}

// createAndStatus posts a plain member (no group) and returns its status.
func createAndStatus(t *testing.T, ts *httptest.Server, email string) string {
	t.Helper()
	body := `{"email":"` + email + `","display_name":"X"}`
	resp, err := http.Post(ts.URL+"/members", "application/json", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var m map[string]any
	json.NewDecoder(resp.Body).Decode(&m)
	return m["status"].(string)
}

func TestAdminInviteAdvancesMemberToInvited(t *testing.T) {
	v := newAdminTestConsole(t)
	ts, _ := memberAdminServer(t, v)
	id := createMember(t, ts, "adv@corp.com")

	// A freshly created member is registered, not invited.
	if status, _ := memberStatusByEmail(t, ts, "adv@corp.com"); status != "registered" {
		t.Fatalf("pre-invite status = %q, want registered", status)
	}
	resp, err := http.Post(ts.URL+"/members/"+id+"/invite", "application/json",
		bytes.NewReader([]byte(`{"role":"member","actor":"heeyeon"}`)))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("invite status = %d, want 201", resp.StatusCode)
	}
	// Issuing the envelope advanced the member registered→invited.
	if status, _ := memberStatusByEmail(t, ts, "adv@corp.com"); status != "invited" {
		t.Errorf("post-invite status = %q, want invited", status)
	}
}

// newDBTokensConsole builds a console whose tokens store writes through a
// real store database (LoadFromDB seeds the default roles), so tests can
// assert what is durable at the exact moment a handler returns by reloading
// a second store from the same path.
func newDBTokensConsole(t *testing.T) (*Console, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "runeconsole.db")
	store := tokens.NewStore()
	if err := store.LoadFromDB(openTokensTestDB(t, dbPath)); err != nil {
		t.Fatal(err)
	}
	cfg := &Config{
		Tokens: TokensConfig{TeamSecret: "test-secret"},
		Keys:   KeysConfig{Path: t.TempDir(), EmbeddingDim: 1024},
	}
	audit, _ := NewAuditLogger(AuditConfig{Mode: ""})
	return NewConsole(cfg, store, groups.NewStore(), nil, audit), dbPath
}

// openTokensTestDB opens the strict store database at path with schema v1
// installed; reopening the same path models a daemon restart.
func openTokensTestDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	database, err := db.OpenStrict(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := storedb.EnsureSchema(database); err != nil {
		t.Fatal(err)
	}
	return database
}

// reloadTokensStore loads a fresh tokens store from the database at path —
// the "what would a restart see" probe.
func reloadTokensStore(t *testing.T, path string) *tokens.Store {
	t.Helper()
	s := tokens.NewStore()
	if err := s.LoadFromDB(openTokensTestDB(t, path)); err != nil {
		t.Fatal(err)
	}
	return s
}

// patchMember PATCHes /members/{id} with the given JSON body.
func patchMember(t *testing.T, ts *httptest.Server, id, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest("PATCH", ts.URL+"/members/"+id, bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// TestAdminPatchDisableRevokesTokenDurably: disabling a member must cut
// access, not just relabel the row — the member's token stops validating the
// moment the PATCH returns, and the revocation is already committed to the
// store database (write-through; a crash cannot resurrect it).
func TestAdminPatchDisableRevokesTokenDurably(t *testing.T) {
	v, dbPath := newDBTokensConsole(t)
	ts, _ := memberAdminServer(t, v)
	id := createMember(t, ts, "dis@corp.com")

	// AddToken is write-through: the token is durable when it returns.
	tok, err := v.Tokens().AddToken("dis@corp.com", "member", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := v.Tokens().Validate(tok.Token); err != nil {
		t.Fatalf("sanity: fresh token should validate: %v", err)
	}

	resp := patchMember(t, ts, id, `{"status":"disabled","actor":"heeyeon"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("disable status = %d, want 200", resp.StatusCode)
	}

	// In memory: the evt_ credential no longer authenticates.
	if _, _, err := v.Tokens().Validate(tok.Token); !errors.As(err, new(tokens.ErrTokenNotFound)) {
		t.Errorf("disabled member token still validates: %v", err)
	}
	// Durable: a store reloaded from the same database refuses it too.
	if _, _, err := reloadTokensStore(t, dbPath).Validate(tok.Token); err == nil {
		t.Error("revocation not durable when PATCH returned (a crash would resurrect the token)")
	}
}

// TestAdminPatchEmailIsImmutable: email is the person's join key across three
// ledgers (member registry, tokens, group memberships), so a PATCH carrying an
// "email" key is rejected outright with 400 and the member row is left exactly
// as it was — renaming a person is not a supported operation on this model.
func TestAdminPatchEmailIsImmutable(t *testing.T) {
	v := newAdminTestConsole(t)
	ts, _ := memberAdminServer(t, v)
	id := createMember(t, ts, "immutable@corp.com")

	// Even email-alongside-a-legit-field is rejected: the field is not
	// updatable, so its mere presence fails the whole request.
	resp := patchMember(t, ts, id, `{"email":"renamed@corp.com","display_name":"Nope","actor":"heeyeon"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("patch with email status = %d, want 400", resp.StatusCode)
	}
	// The row is untouched on reload: the address and the display name both
	// stand, proving the rejected PATCH wrote nothing.
	for _, m := range getMembers(t, ts) {
		if m["id"] != id {
			continue
		}
		if m["email"] != "immutable@corp.com" {
			t.Errorf("email changed by a rejected PATCH: %v", m["email"])
		}
		if m["display_name"] != "X" {
			t.Errorf("display_name changed by a rejected PATCH: %v", m["display_name"])
		}
	}
	// The would-be new address never became a member.
	if _, ok := memberStatusByEmail(t, ts, "renamed@corp.com"); ok {
		t.Error("rejected rename leaked a member row for renamed@corp.com")
	}
}

// TestAdminInviteTokenDurableBeforeBundleReturns guards the invite-time
// durability invariant: a returned bundle implies the wrapped token is
// durable. The envelope is commit-before-return in the invites store; the
// token must already be committed too, or a crash right here would leave an
// envelope wrapping a token that no longer exists.
func TestAdminInviteTokenDurableBeforeBundleReturns(t *testing.T) {
	v, dbPath := newDBTokensConsole(t)
	ts, _ := memberAdminServer(t, v)
	id := createMember(t, ts, "durable@corp.com")

	resp, err := http.Post(ts.URL+"/members/"+id+"/invite", "application/json",
		bytes.NewReader([]byte(`{"role":"member","actor":"heeyeon"}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("invite status = %d, want 201", resp.StatusCode)
	}

	// Read the tokens persistence exactly as the database stands when the
	// bundle escaped — no Flush/Shutdown here.
	found := false
	for _, tk := range reloadTokensStore(t, dbPath).ListTokens() {
		if tk.User == "durable@corp.com" {
			found = true
		}
	}
	if !found {
		t.Error("bundle returned but the wrapped token is not durable (a crash here orphans the envelope)")
	}
}

// errMailer always fails delivery, exercising the best-effort mail contract.
type errMailer struct{}

func (errMailer) SendInvite(_ context.Context, _ string, _ invites.ClearBundle, _ InviteConnInfo) error {
	return fmt.Errorf("smtp unavailable")
}

func TestAdminInviteMailFailureIsNonFatal(t *testing.T) {
	v := newAdminTestConsole(t)
	ms := &memberSubsystem{
		members: members.NewStore(),
		invites: invites.NewStore(),
		mailer:  errMailer{},
		conn:    InviteConnInfo{ConsoleEndpoint: "console.example:8443"},
		ttl:     30 * time.Minute,
	}
	ts := httptest.NewServer(buildAdminMux(v, ms))
	t.Cleanup(ts.Close)

	id := createMember(t, ts, "mailfail@corp.com")
	resp, err := http.Post(ts.URL+"/members/"+id+"/invite", "application/json",
		bytes.NewReader([]byte(`{"role":"member","actor":"heeyeon"}`)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// Mail delivery failed, but the invite was durably issued → still 201.
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("invite status = %d, want 201 (mail failure must be non-fatal)", resp.StatusCode)
	}
	// The invite stands: a token was minted and the member advanced to invited.
	found := false
	for _, tk := range v.Tokens().ListTokens() {
		if tk.User == "mailfail@corp.com" {
			found = true
		}
	}
	if !found {
		t.Error("token not issued despite a successful (mail-failed) invite")
	}
	if status, _ := memberStatusByEmail(t, ts, "mailfail@corp.com"); status != "invited" {
		t.Errorf("status = %q, want invited", status)
	}
}
