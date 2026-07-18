package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/CryptoLabInc/rune-console/internal/groups"
	"github.com/CryptoLabInc/rune-console/internal/invites"
	"github.com/CryptoLabInc/rune-console/internal/members"
	"github.com/CryptoLabInc/rune-console/internal/tokens"
)

// consoleAPIFixture bundles the domain handler under test with the stores it
// operates on, so tests can seed state directly and assert over HTTP.
type consoleAPIFixture struct {
	ts      *httptest.Server
	v       *Console
	members *members.Store
	invites *invites.Store
}

func newConsoleAPIFixture(t *testing.T) *consoleAPIFixture {
	t.Helper()
	cfg := &Config{
		Tokens: TokensConfig{TeamSecret: "test-secret"},
		Keys:   KeysConfig{Path: t.TempDir(), EmbeddingDim: 1024},
	}
	tokStore := tokens.NewStore()
	tokStore.LoadDefaultsWithDemoToken()
	gs := groups.NewStore()
	gs.SetPersonKeyValidator(members.ValidateID) // memberships keyed by member UUID
	v := NewConsole(cfg, tokStore, gs, nil, nil)

	memStore := members.NewStore()
	invStore := invites.NewStore()
	mailer := NewLogMailer(filepath.Join(t.TempDir(), "mail.log"))
	h := NewConsoleAPIHandler(v, memStore, invStore, mailer, InviteConnInfo{ConsoleEndpoint: "c.example:8443"}, 30*time.Minute)
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return &consoleAPIFixture{ts: ts, v: v, members: memStore, invites: invStore}
}

func (f *consoleAPIFixture) do(t *testing.T, method, path, body string) (int, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, f.ts.URL+path, rdr)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func TestConsoleTeamsTreeAndCreate(t *testing.T) {
	f := newConsoleAPIFixture(t)

	// Empty tree is [] with 200, not 404.
	status, body := f.do(t, http.MethodGet, "/teams/tree", "")
	if status != http.StatusOK || strings.TrimSpace(string(body)) != "[]" {
		t.Fatalf("empty tree: status=%d body=%s", status, body)
	}

	// Create a team.
	status, body = f.do(t, http.MethodPost, "/teams", `{"name":"Platform"}`)
	if status != http.StatusCreated {
		t.Fatalf("create team status=%d body=%s", status, body)
	}
	var created map[string]any
	_ = json.Unmarshal(body, &created)
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("created team has no id: %s", body)
	}

	// Tree now has the node with memberCount 0 and childCount 0.
	status, body = f.do(t, http.MethodGet, "/teams/tree", "")
	var tree []map[string]any
	_ = json.Unmarshal(body, &tree)
	if status != http.StatusOK || len(tree) != 1 {
		t.Fatalf("tree after create: status=%d body=%s", status, body)
	}
	if tree[0]["memberCount"].(float64) != 0 || tree[0]["childCount"].(float64) != 0 {
		t.Errorf("new team counts non-zero: %v", tree[0])
	}

	// Unknown team detail → 404 with the doc error code.
	status, body = f.do(t, http.MethodGet, "/teams/no-such-id", "")
	if status != http.StatusNotFound || !strings.Contains(string(body), "TEAM_NOT_FOUND") {
		t.Fatalf("unknown team detail: status=%d body=%s", status, body)
	}
}

func TestConsoleUsersListPagingAndStatus(t *testing.T) {
	f := newConsoleAPIFixture(t)
	for i := 0; i < 3; i++ {
		if _, err := f.members.Add(fmt.Sprintf("u%d@x.com", i), ""); err != nil {
			t.Fatal(err)
		}
	}

	// size=2 → first page has 2 of 3.
	status, body := f.do(t, http.MethodGet, "/users?size=2&page=1", "")
	if status != http.StatusOK {
		t.Fatalf("users list status=%d body=%s", status, body)
	}
	var env struct {
		Total int              `json:"total"`
		Items []map[string]any `json:"items"`
	}
	_ = json.Unmarshal(body, &env)
	if env.Total != 3 || len(env.Items) != 2 {
		t.Fatalf("paging: total=%d items=%d, want 3 and 2", env.Total, len(env.Items))
	}
	// A freshly registered member with no invite/token derives invite_pending.
	if env.Items[0]["status"] != "invite_pending" {
		t.Errorf("status = %v, want invite_pending", env.Items[0]["status"])
	}

	// Page past the end → 200 with empty items (not an error).
	status, body = f.do(t, http.MethodGet, "/users?size=2&page=9", "")
	_ = json.Unmarshal(body, &env)
	if status != http.StatusOK || len(env.Items) != 0 {
		t.Fatalf("page past end: status=%d items=%d", status, len(env.Items))
	}

	// size over the 100 cap → 400.
	if status, _ := f.do(t, http.MethodGet, "/users?size=101", ""); status != http.StatusBadRequest {
		t.Errorf("size=101 status=%d, want 400", status)
	}
}

// P4 hardening: an out-of-enum status filter is a 400, not a silent empty 200.
func TestConsoleUsersListStatusEnumValidation(t *testing.T) {
	f := newConsoleAPIFixture(t)
	if _, err := f.members.Add("u@x.com", ""); err != nil {
		t.Fatal(err)
	}
	// A valid enum value filters normally.
	if status, body := f.do(t, http.MethodGet, "/users?status=invite_pending", ""); status != http.StatusOK {
		t.Fatalf("valid status filter: %d %s", status, body)
	}
	// An unknown value is rejected with 400 VALIDATION_ERROR.
	status, body := f.do(t, http.MethodGet, "/users?status=bogus", "")
	if status != http.StatusBadRequest || !strings.Contains(string(body), "VALIDATION_ERROR") {
		t.Fatalf("bogus status = %d %s, want 400 VALIDATION_ERROR", status, body)
	}
}

// GET /users honors the sort enum (doc §users): last_invited (default) |
// account. An unknown sort is a 400, not a silently ignored no-op — the pre-fix
// behavior that made the account-sort dropdown appear dead.
func TestConsoleUsersListSort(t *testing.T) {
	f := newConsoleAPIFixture(t)
	for _, acct := range []string{"charlie@x.com", "alice@x.com", "bob@x.com"} {
		if _, err := f.members.Add(acct, ""); err != nil {
			t.Fatal(err)
		}
	}

	// sort=account → accounts ascending.
	status, body := f.do(t, http.MethodGet, "/users?sort=account", "")
	if status != http.StatusOK {
		t.Fatalf("sort=account status=%d body=%s", status, body)
	}
	var env struct {
		Items []struct {
			Account string `json:"account"`
		} `json:"items"`
	}
	_ = json.Unmarshal(body, &env)
	got := make([]string, len(env.Items))
	for i, it := range env.Items {
		got[i] = it.Account
	}
	if want := []string{"alice@x.com", "bob@x.com", "charlie@x.com"}; !slices.Equal(got, want) {
		t.Errorf("sort=account order = %v, want %v", got, want)
	}

	// An unknown sort value is rejected with 400 VALIDATION_ERROR (not ignored).
	if s, b := f.do(t, http.MethodGet, "/users?sort=bogus", ""); s != http.StatusBadRequest || !strings.Contains(string(b), "VALIDATION_ERROR") {
		t.Fatalf("sort=bogus = %d %s, want 400 VALIDATION_ERROR", s, b)
	}
}

// P4 hardening: the teamId filter is an id only (doc §users); a value that
// happens to equal a team NAME must not match.
func TestConsoleUsersTeamFilterIsIdOnly(t *testing.T) {
	f := newConsoleAPIFixture(t)
	m, err := f.members.Add("u@x.com", "")
	if err != nil {
		t.Fatal(err)
	}
	team, err := f.v.Groups().CreateGroup("Platform", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.v.Groups().Grant(m.ID, team.ID, groups.RoleRead, "actor"); err != nil {
		t.Fatal(err)
	}
	var env struct {
		Total int `json:"total"`
	}
	// By id → matches.
	_, body := f.do(t, http.MethodGet, "/users?teamId="+team.ID, "")
	_ = json.Unmarshal(body, &env)
	if env.Total != 1 {
		t.Fatalf("teamId=<id> total=%d, want 1 (body=%s)", env.Total, body)
	}
	// By name → no match, 200 empty (not a 404, not a stray hit).
	status, body := f.do(t, http.MethodGet, "/users?teamId=Platform", "")
	_ = json.Unmarshal(body, &env)
	if status != http.StatusOK || env.Total != 0 {
		t.Fatalf("teamId=<name> status=%d total=%d, want 200/0 (body=%s)", status, env.Total, body)
	}
}

// TestConsoleUsersInheritedMemberships covers the derived-read policy on the
// user projection: a parent-group member gets read on descendant groups. On the
// wire these inherited entries are flattened into the single `memberships` list
// (no separate inheritedMemberships field), direct entries leading. The teamId
// filter still matches DIRECT memberships only, and an explicit grant
// (promotion) turns an inherited read into a direct membership with its own role.
func TestConsoleUsersInheritedMemberships(t *testing.T) {
	f := newConsoleAPIFixture(t)
	gs := f.v.Groups()
	// Tree: C-Level > AX > AX-Sub.
	clevel, err := gs.CreateGroup("C-Level", "")
	if err != nil {
		t.Fatal(err)
	}
	ax, err := gs.CreateGroup("AX", clevel.ID)
	if err != nil {
		t.Fatal(err)
	}
	axsub, err := gs.CreateGroup("AX-Sub", ax.ID)
	if err != nil {
		t.Fatal(err)
	}
	ceo, err := f.members.Add("ceo@corp.com", "")
	if err != nil {
		t.Fatal(err)
	}
	// CEO is a direct edit member of C-Level only.
	if _, err := gs.Grant(ceo.ID, clevel.ID, groups.RoleEdit, "actor"); err != nil {
		t.Fatal(err)
	}

	type mdto struct {
		TeamID   string `json:"teamId"`
		TeamName string `json:"teamName"`
		Role     string `json:"role"`
	}
	type udto struct {
		Memberships []mdto `json:"memberships"`
	}
	getDetail := func() (udto, []byte) {
		status, body := f.do(t, http.MethodGet, "/users/"+ceo.ID, "")
		if status != http.StatusOK {
			t.Fatalf("detail status=%d body=%s", status, body)
		}
		var u udto
		if err := json.Unmarshal(body, &u); err != nil {
			t.Fatalf("unmarshal detail: %v (body=%s)", err, body)
		}
		return u, body
	}
	// roleByTeam collapses the flat memberships list to team -> role for
	// order-independent assertions on the merged set.
	roleByTeam := func(u udto) map[string]string {
		m := map[string]string{}
		for _, ms := range u.Memberships {
			m[ms.TeamID] = ms.Role
		}
		return m
	}

	// Before promotion the wire list merges direct + inherited:
	// C-Level edit (direct), AX read + AX-Sub read (inherited). Direct leads.
	u, body := getDetail()
	if strings.Contains(string(body), "inheritedMemberships") {
		t.Fatalf("response must not expose inheritedMemberships; body=%s", body)
	}
	if len(u.Memberships) != 3 {
		t.Fatalf("merged memberships = %+v, want C-Level + AX + AX-Sub", u.Memberships)
	}
	if u.Memberships[0].TeamID != clevel.ID || u.Memberships[0].Role != "edit" {
		t.Fatalf("memberships[0] = %+v, want direct [C-Level edit] first", u.Memberships[0])
	}
	if got := roleByTeam(u); got[clevel.ID] != "edit" || got[ax.ID] != "read" || got[axsub.ID] != "read" {
		t.Fatalf("roles = %v, want C-Level edit, AX read, AX-Sub read", got)
	}

	// teamId filter matches the client-visible set (direct + inherited): the CEO
	// surfaces when filtering by the inherited AX and by the direct C-Level alike.
	var env struct {
		Total int `json:"total"`
	}
	_, body = f.do(t, http.MethodGet, "/users?teamId="+ax.ID, "")
	_ = json.Unmarshal(body, &env)
	if env.Total != 1 {
		t.Fatalf("filter by inherited team AX total=%d, want 1 (merged filter)", env.Total)
	}
	_, body = f.do(t, http.MethodGet, "/users?teamId="+clevel.ID, "")
	_ = json.Unmarshal(body, &env)
	if env.Total != 1 {
		t.Fatalf("filter by direct team C-Level total=%d, want 1", env.Total)
	}

	// Promotion: admin grants the CEO write on AX. AX turns from an inherited
	// read into a direct write; only AX-Sub stays inherited read. The merged
	// list still holds all three, with AX now carrying its granted role.
	if _, err := gs.Grant(ceo.ID, ax.ID, groups.RoleWrite, "actor"); err != nil {
		t.Fatal(err)
	}
	u, _ = getDetail()
	if len(u.Memberships) != 3 {
		t.Fatalf("after promotion merged = %+v, want C-Level + AX + AX-Sub", u.Memberships)
	}
	if got := roleByTeam(u); got[clevel.ID] != "edit" || got[ax.ID] != "write" || got[axsub.ID] != "read" {
		t.Fatalf("after promotion roles = %v, want C-Level edit, AX write, AX-Sub read", got)
	}
	// The promoted team now matches the direct-only teamId filter.
	_, body = f.do(t, http.MethodGet, "/users?teamId="+ax.ID, "")
	_ = json.Unmarshal(body, &env)
	if env.Total != 1 {
		t.Fatalf("after promotion filter by AX total=%d, want 1", env.Total)
	}
}

// TestConsoleUserRolesPromotesInheritedRead covers the PUT role path on an
// inherited-read team: it has no stored membership row, so the update is a
// first-time grant that creates a direct membership carrying the requested role.
// A team the user neither holds nor inherits stays NOT_TEAM_MEMBER.
func TestConsoleUserRolesPromotesInheritedRead(t *testing.T) {
	f := newConsoleAPIFixture(t)
	gs := f.v.Groups()
	// Tree: C-Level > AX. CEO is a direct edit member of C-Level, so AX is an
	// inherited read (no stored row). "Isolated" is unrelated — no access.
	clevel, err := gs.CreateGroup("C-Level", "")
	if err != nil {
		t.Fatal(err)
	}
	ax, err := gs.CreateGroup("AX", clevel.ID)
	if err != nil {
		t.Fatal(err)
	}
	isolated, err := gs.CreateGroup("Isolated", "")
	if err != nil {
		t.Fatal(err)
	}
	ceo, err := f.members.Add("ceo@corp.com", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gs.Grant(ceo.ID, clevel.ID, groups.RoleEdit, "actor"); err != nil {
		t.Fatal(err)
	}

	// Sanity: AX has no direct row yet (it is inherited-only).
	if _, direct, _ := gs.DirectRole(ceo.ID, ax.ID); direct {
		t.Fatalf("precondition: AX should be inherited-only, but a direct row exists")
	}

	var res struct {
		Succeeded []string `json:"succeeded"`
		Failed    []struct {
			ID   string `json:"id"`
			Code string `json:"code"`
		} `json:"failed"`
	}
	body := `{"updates":[{"teamId":"` + ax.ID + `","role":"write"},{"teamId":"` + isolated.ID + `","role":"write"}]}`
	status, raw := f.do(t, http.MethodPut, "/users/"+ceo.ID+"/members/roles", body)
	if status != http.StatusOK {
		t.Fatalf("put roles status=%d body=%s", status, raw)
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatalf("unmarshal: %v (body=%s)", err, raw)
	}

	// AX (inherited) was promoted; Isolated (no access) was rejected.
	if len(res.Succeeded) != 1 || res.Succeeded[0] != ax.ID {
		t.Fatalf("succeeded = %v, want [AX]", res.Succeeded)
	}
	if len(res.Failed) != 1 || res.Failed[0].ID != isolated.ID || res.Failed[0].Code != "NOT_TEAM_MEMBER" {
		t.Fatalf("failed = %+v, want [Isolated NOT_TEAM_MEMBER]", res.Failed)
	}

	// The promotion created a real direct membership with the requested role.
	role, direct, rerr := gs.DirectRole(ceo.ID, ax.ID)
	if rerr != nil || !direct || role != groups.RoleWrite {
		t.Fatalf("after promotion DirectRole(AX) = (%q, %v, %v), want (write, true, nil)", role, direct, rerr)
	}
}

// P4 hardening: a page far past the end is a 200 empty window, never a panic
// (guards the (page-1)*size overflow in pageSlice).
func TestConsolePaginationOverflowSafe(t *testing.T) {
	f := newConsoleAPIFixture(t)
	if _, err := f.members.Add("u@x.com", ""); err != nil {
		t.Fatal(err)
	}
	status, body := f.do(t, http.MethodGet, "/users?page=1000000000000000000", "")
	if status != http.StatusOK {
		t.Fatalf("huge page status=%d body=%s, want 200 (no panic)", status, body)
	}
	var env struct {
		Items []map[string]any `json:"items"`
	}
	_ = json.Unmarshal(body, &env)
	if len(env.Items) != 0 {
		t.Errorf("huge page items=%d, want 0", len(env.Items))
	}
}

// P4 hardening: out-of-enum sort on GET /invitations → 400; empty userId on
// resend/cancel → 400 (required-field contract).
func TestConsoleInvitationsHardening(t *testing.T) {
	f := newConsoleAPIFixture(t)
	if status, body := f.do(t, http.MethodGet, "/invitations?sort=bogus", ""); status != http.StatusBadRequest {
		t.Errorf("bad sort status=%d body=%s, want 400", status, body)
	}
	if status, _ := f.do(t, http.MethodGet, "/invitations", ""); status != http.StatusOK {
		t.Errorf("default sort status=%d, want 200", status)
	}
	if status, body := f.do(t, http.MethodPost, "/invitations/resend", `{}`); status != http.StatusBadRequest {
		t.Errorf("resend empty userId status=%d body=%s, want 400", status, body)
	}
	if status, body := f.do(t, http.MethodPost, "/invitations/cancel", `{"userId":""}`); status != http.StatusBadRequest {
		t.Errorf("cancel empty userId status=%d body=%s, want 400", status, body)
	}
}

func TestConsoleUsersDeleteBatchPartialFailure(t *testing.T) {
	f := newConsoleAPIFixture(t)
	m, err := f.members.Add("real@x.com", "")
	if err != nil {
		t.Fatal(err)
	}

	status, body := f.do(t, http.MethodDelete, "/users?userIds="+m.ID+",bogus-id", "")
	if status != http.StatusOK {
		t.Fatalf("batch delete status=%d body=%s", status, body)
	}
	var res struct {
		Succeeded []string `json:"succeeded"`
		Failed    []struct {
			ID   string `json:"id"`
			Code string `json:"code"`
		} `json:"failed"`
	}
	_ = json.Unmarshal(body, &res)
	if len(res.Succeeded) != 1 || res.Succeeded[0] != m.ID {
		t.Errorf("succeeded = %v, want [%s]", res.Succeeded, m.ID)
	}
	if len(res.Failed) != 1 || res.Failed[0].Code != "USER_NOT_FOUND" {
		t.Errorf("failed = %+v, want one USER_NOT_FOUND", res.Failed)
	}

	// Missing userIds param → 400 (guard against whole-collection delete).
	if status, _ := f.do(t, http.MethodDelete, "/users", ""); status != http.StatusBadRequest {
		t.Errorf("delete without userIds status=%d, want 400", status)
	}
}

func TestConsoleTeamDeletePurge(t *testing.T) {
	f := newConsoleAPIFixture(t)
	fake := &fakeEngine{}
	f.v.ConnectEngine(fake) // attach an engine so the memory op can run

	status, body := f.do(t, http.MethodPost, "/teams", `{"name":"Doomed"}`)
	if status != http.StatusCreated {
		t.Fatalf("create team: %d %s", status, body)
	}
	var created map[string]any
	_ = json.Unmarshal(body, &created)
	id := created["id"].(string)

	status, body = f.do(t, http.MethodDelete, "/teams/"+id+"?memoryAction=purge", "")
	if status != http.StatusNoContent {
		t.Fatalf("purge delete status=%d body=%s", status, body)
	}
	if fake.removedTag != id {
		t.Errorf("RemoveTag called with %q, want team id %q", fake.removedTag, id)
	}
	// Team is gone.
	if status, _ := f.do(t, http.MethodGet, "/teams/"+id, ""); status != http.StatusNotFound {
		t.Errorf("team still present after delete: status=%d", status)
	}
}

func TestConsoleInvitationConflictAndBadEmail(t *testing.T) {
	f := newConsoleAPIFixture(t)
	status, body := f.do(t, http.MethodPost, "/teams", `{"name":"T"}`)
	if status != http.StatusCreated {
		t.Fatalf("create team: %d %s", status, body)
	}
	var created map[string]any
	_ = json.Unmarshal(body, &created)
	id := created["id"].(string)

	// First invite of a new user → 201.
	inv := `{"account":"kim@corp.com","memberships":[{"teamId":"` + id + `","role":"read"}]}`
	if status, body = f.do(t, http.MethodPost, "/invitations", inv); status != http.StatusCreated {
		t.Fatalf("first invite: %d %s", status, body)
	}
	// Re-inviting the same (now existing) user to the same team → 409.
	status, body = f.do(t, http.MethodPost, "/invitations", inv)
	if status != http.StatusConflict || !strings.Contains(string(body), "ALREADY_TEAM_MEMBER") {
		t.Fatalf("duplicate invite: status=%d body=%s (want 409 ALREADY_TEAM_MEMBER)", status, body)
	}
	// Malformed account → 400 VALIDATION_ERROR in the console error shape.
	bad := `{"account":"not-an-email","memberships":[{"teamId":"` + id + `","role":"read"}]}`
	status, body = f.do(t, http.MethodPost, "/invitations", bad)
	if status != http.StatusBadRequest || !strings.Contains(string(body), "VALIDATION_ERROR") {
		t.Fatalf("bad email: status=%d body=%s (want 400 VALIDATION_ERROR)", status, body)
	}
}

func TestConsoleTeamRolesBatchRejectsBadRole(t *testing.T) {
	f := newConsoleAPIFixture(t)
	status, body := f.do(t, http.MethodPost, "/teams", `{"name":"T"}`)
	if status != http.StatusCreated {
		t.Fatalf("create team: %d %s", status, body)
	}
	var created map[string]any
	_ = json.Unmarshal(body, &created)
	id := created["id"].(string)

	m, err := f.members.Add("u@corp.com", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.v.Groups().Grant(m.ID, id, groups.RoleRead, "actor"); err != nil {
		t.Fatal(err)
	}

	// A malformed role rejects the WHOLE batch with 400 (no partial apply).
	badRole := `{"updates":[{"userId":"` + m.ID + `","role":"superadmin"}]}`
	status, body = f.do(t, http.MethodPut, "/teams/"+id+"/members/roles", badRole)
	if status != http.StatusBadRequest || !strings.Contains(string(body), "VALIDATION_ERROR") {
		t.Fatalf("bad role batch: status=%d body=%s (want 400 VALIDATION_ERROR)", status, body)
	}
	// A valid role succeeds.
	okRole := `{"updates":[{"userId":"` + m.ID + `","role":"write"}]}`
	status, body = f.do(t, http.MethodPut, "/teams/"+id+"/members/roles", okRole)
	if status != http.StatusOK || !strings.Contains(string(body), m.ID) {
		t.Fatalf("good role batch: status=%d body=%s", status, body)
	}
}

// A member without an ACTIVE session (e.g. invite_pending) must get 409
// SESSION_NOT_ACTIVE — and keep their wrapped invite token intact (the
// unconditional revoke used to destroy it, silently voiding the mailed code).
func TestConsoleSessionDeactivateRequiresActiveSession(t *testing.T) {
	f := newConsoleAPIFixture(t)
	m, err := f.members.Add("pend@corp.com", "")
	if err != nil {
		t.Fatal(err)
	}
	// Simulate the invite-time wrapped token: a token row exists, but the
	// member is not active, so the derived status is invite_pending.
	tok, err := f.v.Tokens().AddToken("pend@corp.com", "member", nil)
	if err != nil {
		t.Fatal(err)
	}

	status, body := f.do(t, http.MethodDelete, "/users/"+m.ID+"/session", "")
	if status != http.StatusConflict || !strings.Contains(string(body), "SESSION_NOT_ACTIVE") {
		t.Fatalf("deactivate on invite_pending = %d %s, want 409 SESSION_NOT_ACTIVE", status, body)
	}
	// The wrapped invite token must survive the refused deactivate.
	if _, _, err := f.v.Tokens().Validate(tok.Token); err != nil {
		t.Errorf("invite token was destroyed by the refused deactivate: %v", err)
	}
}

func TestConsoleUserLiveness(t *testing.T) {
	f := newConsoleAPIFixture(t)
	m, err := f.members.Add("live@corp.com", "")
	if err != nil {
		t.Fatal(err)
	}
	// Drive the lifecycle to active (registered -> invited -> active).
	if err := f.members.MarkInvited(m.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.members.Activate(m.ID); err != nil {
		t.Fatal(err)
	}
	// Mint a session token and use it once — Validate stamps LastUsed.
	tok, err := f.v.Tokens().AddToken("live@corp.com", "member", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.v.Tokens().Validate(tok.Token); err != nil {
		t.Fatal(err)
	}

	// Active + valid token + recent use => online with a lastAccessAt.
	_, body := f.do(t, http.MethodGet, "/users/"+m.ID, "")
	var u map[string]any
	_ = json.Unmarshal(body, &u)
	if u["status"] != "online" {
		t.Fatalf("status = %v, want online (body=%s)", u["status"], body)
	}
	if u["lastAccessAt"] == nil {
		t.Errorf("lastAccessAt is null, want a timestamp (body=%s)", body)
	}

	// Deactivate the session → session_expired with a sessionExpiredAt.
	if status, b := f.do(t, http.MethodDelete, "/users/"+m.ID+"/session", ""); status != http.StatusOK {
		t.Fatalf("deactivate: %d %s", status, b)
	}
	_, body = f.do(t, http.MethodGet, "/users/"+m.ID, "")
	_ = json.Unmarshal(body, &u)
	if u["status"] != "session_expired" {
		t.Fatalf("status after deactivate = %v, want session_expired (body=%s)", u["status"], body)
	}
	if u["sessionExpiredAt"] == nil {
		t.Errorf("sessionExpiredAt is null after deactivate (body=%s)", body)
	}
	if u["lastAccessAt"] != nil {
		t.Errorf("lastAccessAt should be null after the token is destroyed (body=%s)", body)
	}
}

func TestInvitePendingLive(t *testing.T) {
	now := time.Date(2026, 7, 16, 9, 0, 0, 0, time.UTC)
	live := invites.InviteView{Status: invites.StatusPending, ExpiresAt: now.Add(time.Hour).Format(time.RFC3339)}
	if !invitePendingLive(live, now) {
		t.Error("a pending code before its expiry should be live")
	}
	dead := invites.InviteView{Status: invites.StatusPending, ExpiresAt: now.Add(-time.Hour).Format(time.RFC3339)}
	if invitePendingLive(dead, now) {
		t.Error("a pending code past its expiry should not be live")
	}
	if invitePendingLive(invites.InviteView{Status: invites.StatusConsumed}, now) {
		t.Error("a consumed invite is not a live pending code")
	}
	// An admin-revoked invite is dead too: needsCode must send a fresh code.
	if invitePendingLive(invites.InviteView{Status: invites.StatusRevoked, ExpiresAt: now.Add(time.Hour).Format(time.RFC3339)}, now) {
		t.Error("a revoked invite is not a live pending code")
	}
}

// TestConsoleRevokedInviteRendersInviteExpired pins the derived-status
// mapping for the new 'revoked' invite status: a member whose latest invite
// was administratively canceled must read invite_expired — NOT
// invite_pending, which would show a canceled invitation as awaiting
// acceptance and contradict the cancel endpoint's own response.
func TestConsoleRevokedInviteRendersInviteExpired(t *testing.T) {
	f := newConsoleAPIFixture(t)
	m, err := f.members.Add("revoked@corp.com", "")
	if err != nil {
		t.Fatal(err)
	}
	_ = f.members.MarkInvited(m.ID)
	if _, err := f.invites.Issue(invites.IssueParams{
		MemberID: m.ID, Email: m.Email, Role: "member",
		TokenValue: "evt_rvk", CreationPath: inviteCreationPath, TTL: 30 * time.Minute,
	}); err != nil {
		t.Fatal(err)
	}

	// Live code → invite_pending.
	status, body := f.do(t, http.MethodGet, "/users/"+m.ID, "")
	var u map[string]any
	_ = json.Unmarshal(body, &u)
	if status != http.StatusOK || u["status"] != "invite_pending" {
		t.Fatalf("before cancel: status=%d userStatus=%v (body=%s)", status, u["status"], body)
	}

	// Cancel via the endpoint (which drives RevokePending → 'revoked').
	status, body = f.do(t, http.MethodPost, "/invitations/cancel", `{"userId":"`+m.ID+`"}`)
	if status != http.StatusOK || !strings.Contains(string(body), "invite_expired") {
		t.Fatalf("cancel: status=%d body=%s", status, body)
	}
	if got := f.invites.ListByMember(m.ID); len(got) != 1 || got[0].Status != invites.StatusRevoked {
		t.Fatalf("stored invite after cancel = %+v, want status revoked", got)
	}

	// The revoked code now renders invite_expired, consistently with the
	// cancel response above.
	status, body = f.do(t, http.MethodGet, "/users/"+m.ID, "")
	_ = json.Unmarshal(body, &u)
	if status != http.StatusOK || u["status"] != "invite_expired" {
		t.Fatalf("after cancel: status=%d userStatus=%v, want invite_expired (body=%s)", status, u["status"], body)
	}
}

func TestConsoleNeedsCode(t *testing.T) {
	f := newConsoleAPIFixture(t)
	h := &consoleAPI{v: f.v, ms: &memberSubsystem{
		members: f.members, invites: f.invites,
		mailer: NewLogMailer(filepath.Join(t.TempDir(), "mail.log")), ttl: 30 * time.Minute,
	}}

	// registered (brand-new, no code yet) → send.
	reg, _ := f.members.Add("reg@x.com", "")
	if !h.needsCode(reg) {
		t.Error("registered member should need a code")
	}
	// online (active + valid token) → no code. Re-fetch after status changes:
	// members.Get returns a copy, so the Add result stays "registered".
	on, _ := f.members.Add("on@x.com", "")
	_ = f.members.MarkInvited(on.ID)
	_ = f.members.Activate(on.ID)
	if _, err := f.v.Tokens().AddToken("on@x.com", "member", nil); err != nil {
		t.Fatal(err)
	}
	on, _ = f.members.Get(on.ID)
	if h.needsCode(on) {
		t.Error("online member should NOT need a code")
	}
	// session_expired (active, token destroyed) → send (reconnect).
	se, _ := f.members.Add("se@x.com", "")
	_ = f.members.MarkInvited(se.ID)
	_ = f.members.Activate(se.ID)
	se, _ = f.members.Get(se.ID)
	if !h.needsCode(se) {
		t.Error("session_expired member should need a code")
	}
	// invite_expired (invited, code expired, but the invite-time token LINGERS) →
	// send. This is the #3/#5 bug: the old any-token gate skipped the code here.
	ie, _ := f.members.Add("ie@x.com", "")
	_ = f.members.MarkInvited(ie.ID)
	if _, err := f.v.Tokens().AddToken("ie@x.com", "member", nil); err != nil {
		t.Fatal(err)
	}
	b, err := f.invites.Issue(invites.IssueParams{MemberID: ie.ID, Email: "ie@x.com", Role: "member", TokenValue: "evt_ie", CreationPath: inviteCreationPath, TTL: 30 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	_ = f.invites.RevokePending(b.Handle) // → status revoked (renders invite_expired)
	if !h.needsCode(ie) {
		t.Error("invite_expired member should need a code despite a lingering token (the fix)")
	}
	// invite_pending (invited, live code) → no code.
	ip, _ := f.members.Add("ip@x.com", "")
	_ = f.members.MarkInvited(ip.ID)
	if _, err := f.invites.Issue(invites.IssueParams{MemberID: ip.ID, Email: "ip@x.com", Role: "member", TokenValue: "evt_ip", CreationPath: inviteCreationPath, TTL: 30 * time.Minute}); err != nil {
		t.Fatal(err)
	}
	if h.needsCode(ip) {
		t.Error("invite_pending member should NOT need a code (a live code exists)")
	}
}

func TestConsoleResendSessionExpiredBecomesPending(t *testing.T) {
	f := newConsoleAPIFixture(t)
	m, err := f.members.Add("se@corp.com", "")
	if err != nil {
		t.Fatal(err)
	}
	// active with no token = session_expired.
	_ = f.members.MarkInvited(m.ID)
	_ = f.members.Activate(m.ID)
	_ = f.members.SetSessionExpired(m.ID)

	status, body := f.do(t, http.MethodPost, "/invitations/resend", `{"userId":"`+m.ID+`"}`)
	if status != http.StatusOK {
		t.Fatalf("resend status=%d body=%s", status, body)
	}
	var res map[string]string
	_ = json.Unmarshal(body, &res)
	if res["status"] != "invite_pending" {
		t.Fatalf("resend on session_expired → status %q, want invite_pending (body=%s)", res["status"], body)
	}
}

func TestConsoleTeamDeleteMissingMemoryAction(t *testing.T) {
	f := newConsoleAPIFixture(t)
	status, body := f.do(t, http.MethodPost, "/teams", `{"name":"T"}`)
	if status != http.StatusCreated {
		t.Fatalf("create: %d %s", status, body)
	}
	var created map[string]any
	_ = json.Unmarshal(body, &created)
	id := created["id"].(string)

	// No memoryAction → 400 VALIDATION_ERROR (before any memory op).
	status, body = f.do(t, http.MethodDelete, "/teams/"+id, "")
	if status != http.StatusBadRequest || !strings.Contains(string(body), "VALIDATION_ERROR") {
		t.Fatalf("missing memoryAction: status=%d body=%s", status, body)
	}
}

// TestConsoleUserRemoveCutsInheritedRead — deleting a team from a user must
// actually cut their access, including the read they would keep INHERITING from
// an ancestor membership. Without the carve-out the team would reappear in the
// list and its memory would stay readable, so "remove" would be a lie.
func TestConsoleUserRemoveCutsInheritedRead(t *testing.T) {
	f := newConsoleAPIFixture(t)
	gs := f.v.Groups()
	// 대표이사 > 사업실 > AX사업팀 (leaf) — the reported case.
	clevel, err := gs.CreateGroup("C-Level", "")
	if err != nil {
		t.Fatal(err)
	}
	biz, err := gs.CreateGroup("Biz", clevel.ID)
	if err != nil {
		t.Fatal(err)
	}
	ax, err := gs.CreateGroup("AX", biz.ID)
	if err != nil {
		t.Fatal(err)
	}
	ceo, err := f.members.Add("ceo@corp.com", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gs.Grant(ceo.ID, clevel.ID, groups.RoleEdit, "actor"); err != nil {
		t.Fatal(err)
	}
	// Baseline: AX is readable purely by inheritance.
	if !slices.Contains(gs.RecallScope(ceo.ID), ax.ID) {
		t.Fatal("precondition: AX should be in the CEO's recall scope")
	}

	status, body := f.do(t, http.MethodDelete, "/users/"+ceo.ID+"/members/roles?teamIds="+ax.ID, "")
	if status != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", status, body)
	}
	var res struct {
		Succeeded []string `json:"succeeded"`
		Failed    []struct {
			ID   string `json:"id"`
			Code string `json:"code"`
		} `json:"failed"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("unmarshal: %v (%s)", err, body)
	}
	if len(res.Succeeded) != 1 || res.Succeeded[0] != ax.ID || len(res.Failed) != 0 {
		t.Fatalf("result = %+v, want succeeded=[AX]", res)
	}

	// The memory gate itself: AX is out of the recall scope.
	if slices.Contains(gs.RecallScope(ceo.ID), ax.ID) {
		t.Errorf("RecallScope = %v, still contains AX — memory read was NOT blocked", gs.RecallScope(ceo.ID))
	}
	// Node-only: the ancestor the CEO actually belongs to is untouched.
	if !slices.Contains(gs.RecallScope(ceo.ID), biz.ID) {
		t.Errorf("RecallScope lost Biz — the denial must cut AX only")
	}
	// And AX is gone from the user's flattened memberships.
	_, detail := f.do(t, http.MethodGet, "/users/"+ceo.ID, "")
	if strings.Contains(string(detail), ax.ID) {
		t.Errorf("user detail still lists AX: %s", detail)
	}

	// A team the user never reached is still NOT_TEAM_MEMBER.
	_, body = f.do(t, http.MethodDelete, "/users/"+ceo.ID+"/members/roles?teamIds="+ax.ID, "")
	_ = json.Unmarshal(body, &res)
	if len(res.Failed) != 1 || res.Failed[0].Code != "NOT_TEAM_MEMBER" {
		t.Errorf("re-delete = %+v, want NOT_TEAM_MEMBER (already blocked)", res)
	}
}

// TestConsoleTeamMemberRemoveCutsInheritedRead is the team-screen twin of
// TestConsoleUserRemoveCutsInheritedRead. Removing a member from a team via the
// team member list (DELETE /teams/{id}/members) must cut their access to that
// team even when they ALSO reach it by inheritance from an ancestor they belong
// to — otherwise the team comes straight back as inherited read with its memory
// still readable. d0f451a fixed this on the user-drawer axis but left the
// team-screen axis on the plain Revoke; this pins the fix on this axis too.
func TestConsoleTeamMemberRemoveCutsInheritedRead(t *testing.T) {
	f := newConsoleAPIFixture(t)
	gs := f.v.Groups()
	// C-Level > Biz > AX (leaf).
	clevel, err := gs.CreateGroup("C-Level", "")
	if err != nil {
		t.Fatal(err)
	}
	biz, err := gs.CreateGroup("Biz", clevel.ID)
	if err != nil {
		t.Fatal(err)
	}
	ax, err := gs.CreateGroup("AX", biz.ID)
	if err != nil {
		t.Fatal(err)
	}
	u, err := f.members.Add("lead@corp.com", "")
	if err != nil {
		t.Fatal(err)
	}
	// The member is a DIRECT member of AX (so the team screen lists them and has
	// a row to remove) AND a direct member of the ancestor C-Level (so they also
	// reach AX by inheritance — the exact condition that made plain Revoke a lie).
	if _, err := gs.Grant(u.ID, ax.ID, groups.RoleRead, "actor"); err != nil {
		t.Fatal(err)
	}
	if _, err := gs.Grant(u.ID, clevel.ID, groups.RoleEdit, "actor"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(gs.RecallScope(u.ID), ax.ID) {
		t.Fatal("precondition: AX should be in the member's recall scope")
	}

	status, body := f.do(t, http.MethodDelete, "/teams/"+ax.ID+"/members?userIds="+u.ID, "")
	if status != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", status, body)
	}
	var res struct {
		Succeeded []string `json:"succeeded"`
		Failed    []struct {
			ID   string `json:"id"`
			Code string `json:"code"`
		} `json:"failed"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("unmarshal: %v (%s)", err, body)
	}
	if len(res.Succeeded) != 1 || res.Succeeded[0] != u.ID || len(res.Failed) != 0 {
		t.Fatalf("result = %+v, want succeeded=[member]", res)
	}

	// The memory gate itself: AX is out of the recall scope even though the
	// member is still on the C-Level ancestor. With the old plain Revoke this
	// assertion FAILS — AX returns via inheritance with its memory readable.
	if slices.Contains(gs.RecallScope(u.ID), ax.ID) {
		t.Errorf("RecallScope = %v, still contains AX — inherited read was NOT cut", gs.RecallScope(u.ID))
	}
	// The denial cuts AX only: the ancestor the member actually belongs to and
	// its other reachable nodes stay.
	if !slices.Contains(gs.RecallScope(u.ID), clevel.ID) {
		t.Errorf("RecallScope lost C-Level — the denial must cut AX only")
	}
	if !slices.Contains(gs.RecallScope(u.ID), biz.ID) {
		t.Errorf("RecallScope lost Biz — the denial must cut AX only")
	}

	// Re-removing the now-fully-cut team reports NOT_TEAM_MEMBER (idempotent).
	_, body = f.do(t, http.MethodDelete, "/teams/"+ax.ID+"/members?userIds="+u.ID, "")
	_ = json.Unmarshal(body, &res)
	if len(res.Failed) != 1 || res.Failed[0].Code != "NOT_TEAM_MEMBER" {
		t.Errorf("re-delete = %+v, want NOT_TEAM_MEMBER (already blocked)", res)
	}
}

// ── wire boundary: second-precision timestamps ─────────────────────────

// TestWireTimeTruncatesToSecondPrecision pins the wireTime/wireTimePtr
// contract: stored canonical millisecond RFC3339 renders at the doc's second
// precision UTC; offsets normalize to UTC; already-second values are stable;
// unparseable values (the date-only token expiry fallback) and "" pass
// through untouched.
func TestWireTimeTruncatesToSecondPrecision(t *testing.T) {
	cases := map[string]string{
		"2026-07-07T08:12:00.123Z":      "2026-07-07T08:12:00Z", // canonical stored form
		"2026-07-07T08:12:00.999Z":      "2026-07-07T08:12:00Z", // truncate, never round up
		"2026-07-07T08:12:00Z":          "2026-07-07T08:12:00Z", // already wire-shaped: stable
		"2026-07-07T17:12:00.500+09:00": "2026-07-07T08:12:00Z", // offset → UTC
		"2026-12-31":                    "2026-12-31",           // date-only fallback: passthrough
		"":                              "",                     // empty: passthrough
	}
	for in, want := range cases {
		if got := wireTime(in); got != want {
			t.Errorf("wireTime(%q) = %q, want %q", in, got, want)
		}
	}
	if got := wireTimePtr(""); got != nil {
		t.Errorf("wireTimePtr(\"\") = %q, want nil", *got)
	}
	if got := wireTimePtr("2026-07-07T08:12:00.123Z"); got == nil || *got != "2026-07-07T08:12:00Z" {
		t.Errorf("wireTimePtr(ms) = %v, want 2026-07-07T08:12:00Z", got)
	}
}

// TestUserDTOTimestampsRenderSecondPrecision pins the userDTO/memberDTO wire
// boundary: the stores now hold canonical millisecond timestamps, but every
// console field must keep the design doc's second-precision RFC3339 UTC
// shape ("2026-07-07T08:12:00Z"), exactly what origin/develop rendered.
func TestUserDTOTimestampsRenderSecondPrecision(t *testing.T) {
	m := members.Member{
		ID:               "11111111-1111-1111-1111-111111111111",
		Email:            "ms@corp.com",
		Status:           members.StatusActive,
		SessionExpiredAt: "2026-07-07T09:30:00.777Z",
	}
	idx := &userIndex{
		now:               time.Date(2026, 7, 8, 0, 0, 0, 0, time.UTC),
		groupNames:        map[string]string{},
		memberByID:        map[string]members.Member{m.ID: m},
		tokenByEmail:      map[string]tokenLive{},
		membershipsByUser: map[string][]membershipDTO{},
		inheritedByUser:   map[string][]membershipDTO{},
		latestInvite: map[string]invites.InviteView{
			m.ID: {MemberID: m.ID, Status: invites.StatusConsumed, CreatedAt: "2026-07-07T08:12:00.123Z"},
		},
	}
	u := idx.userDTO(m)
	if u.LastInvitedAt == nil || *u.LastInvitedAt != "2026-07-07T08:12:00Z" {
		t.Errorf("lastInvitedAt = %v, want the ms store value truncated to 2026-07-07T08:12:00Z", u.LastInvitedAt)
	}
	if u.SessionExpiredAt == nil || *u.SessionExpiredAt != "2026-07-07T09:30:00Z" {
		t.Errorf("sessionExpiredAt = %v, want 2026-07-07T09:30:00Z", u.SessionExpiredAt)
	}

	// With a token whose last_used carries milliseconds: lastAccessAt truncates.
	idx.tokenByEmail[m.Email] = tokenLive{lastUsed: "2026-07-07T08:12:00.123Z"}
	u = idx.userDTO(m)
	if u.LastAccessAt == nil || *u.LastAccessAt != "2026-07-07T08:12:00Z" {
		t.Errorf("lastAccessAt = %v, want 2026-07-07T08:12:00Z", u.LastAccessAt)
	}

	// memberDTO (team members listing): joinedAt == granted_at truncated.
	dto := idx.memberDTO(groups.Membership{
		User: m.ID, GroupID: "g1", Role: groups.RoleRead,
		GrantedAt: "2026-07-07T08:12:00.900Z",
	})
	if dto.JoinedAt == nil || *dto.JoinedAt != "2026-07-07T08:12:00Z" {
		t.Errorf("joinedAt = %v, want 2026-07-07T08:12:00Z", dto.JoinedAt)
	}
	// An inherited-read member has no grant row, so joinedAt is null.
	if inh := idx.inheritedMemberDTO(m.ID); inh.JoinedAt != nil {
		t.Errorf("inherited joinedAt = %v, want nil", *inh.JoinedAt)
	}
}

// TestConsoleTeamMembersIncludeInherited proves the team-member listing surfaces
// inherited-read users (parent-group members) alongside direct members, rendered
// as read with a null joinedAt, and that the team role batch PROMOTES such an
// inherited user by creating a first-time direct grant — the whole point of the
// inherited-inclusion change, driven end-to-end over HTTP.
func TestConsoleTeamMembersIncludeInherited(t *testing.T) {
	f := newConsoleAPIFixture(t)
	gs := f.v.Groups()
	// Tree: HQ > TeamA.
	hq, err := gs.CreateGroup("HQ", "")
	if err != nil {
		t.Fatal(err)
	}
	teamA, err := gs.CreateGroup("TeamA", hq.ID)
	if err != nil {
		t.Fatal(err)
	}
	// A direct write member of TeamA, plus an HQ member who only INHERITS read on it.
	direct, err := f.members.Add("direct@x.com", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gs.Grant(direct.ID, teamA.ID, groups.RoleWrite, "actor"); err != nil {
		t.Fatal(err)
	}
	boss, err := f.members.Add("boss@x.com", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gs.Grant(boss.ID, hq.ID, groups.RoleEdit, "actor"); err != nil {
		t.Fatal(err)
	}

	type row struct {
		UserID   string  `json:"userId"`
		Account  string  `json:"account"`
		Role     string  `json:"role"`
		JoinedAt *string `json:"joinedAt"`
	}
	rowsByUser := func() (int, map[string]row) {
		_, body := f.do(t, http.MethodGet, "/teams/"+teamA.ID+"/members", "")
		var p struct {
			Total int   `json:"total"`
			Items []row `json:"items"`
		}
		if err := json.Unmarshal(body, &p); err != nil {
			t.Fatalf("unmarshal members: %v (body=%s)", err, body)
		}
		byUser := make(map[string]row, len(p.Items))
		for _, r := range p.Items {
			byUser[r.UserID] = r
		}
		return p.Total, byUser
	}

	// TeamA lists BOTH the direct member and the inherited boss.
	total, byUser := rowsByUser()
	if total != 2 {
		t.Fatalf("member total=%d, want 2 (%+v)", total, byUser)
	}
	if d := byUser[direct.ID]; d.Role != "write" || d.JoinedAt == nil {
		t.Errorf("direct member = %+v, want role=write with a non-null joinedAt", d)
	}
	if b := byUser[boss.ID]; b.Account != "boss@x.com" || b.Role != "read" || b.JoinedAt != nil {
		t.Errorf("inherited member = %+v, want account=boss@x.com role=read joinedAt=null", b)
	}

	// Promote the inherited boss to write on TeamA via the team role batch — a
	// user with no direct row on TeamA must be accepted, not rejected as
	// NOT_TEAM_MEMBER.
	status, body := f.do(t, http.MethodPut, "/teams/"+teamA.ID+"/members/roles",
		`{"updates":[{"userId":"`+boss.ID+`","role":"write"}]}`)
	if status != http.StatusOK {
		t.Fatalf("promote status=%d body=%s", status, body)
	}
	var res struct {
		Succeeded []string `json:"succeeded"`
		Failed    []any    `json:"failed"`
	}
	_ = json.Unmarshal(body, &res)
	if len(res.Succeeded) != 1 || len(res.Failed) != 0 {
		t.Fatalf("promote result = %s, want succeeded=[boss] failed=[]", body)
	}

	// boss is now a DIRECT member: role=write with a real joinedAt, still 2 rows.
	total, byUser = rowsByUser()
	if total != 2 {
		t.Fatalf("post-promotion total=%d, want 2 (%+v)", total, byUser)
	}
	if b := byUser[boss.ID]; b.Role != "write" || b.JoinedAt == nil {
		t.Errorf("after promotion boss = %+v, want role=write with a non-null joinedAt", b)
	}
}

// TestConsoleTimestampsSecondPrecisionOverHTTP drives the real stores (which
// stamp canonical millisecond values) through the HTTP surface and asserts
// no console response leaks sub-second precision: team createdAt, member
// joinedAt, and the invitations history issuedAt all match the doc's
// second-precision shape while the underlying store rows keep milliseconds.
func TestConsoleTimestampsSecondPrecisionOverHTTP(t *testing.T) {
	f := newConsoleAPIFixture(t)
	wireShape := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$`)

	// Team createdAt (POST /teams response and GET /teams/{id}).
	status, body := f.do(t, http.MethodPost, "/teams", `{"name":"Platform"}`)
	if status != http.StatusCreated {
		t.Fatalf("create team: %d %s", status, body)
	}
	var team struct {
		ID        string `json:"id"`
		CreatedAt string `json:"createdAt"`
	}
	_ = json.Unmarshal(body, &team)
	if !wireShape.MatchString(team.CreatedAt) {
		t.Errorf("created team createdAt = %q, want second-precision RFC3339 UTC", team.CreatedAt)
	}
	// The store row itself keeps the canonical millisecond form.
	raw := f.v.Groups().ListGroups()[0].CreatedAt
	if !strings.Contains(raw, ".") {
		t.Errorf("stored group created_at = %q, want canonical millisecond form", raw)
	}
	if want := wireTime(raw); team.CreatedAt != want {
		t.Errorf("wire createdAt = %q, want %q (the stored instant truncated)", team.CreatedAt, want)
	}
	status, body = f.do(t, http.MethodGet, "/teams/"+team.ID, "")
	_ = json.Unmarshal(body, &team)
	if status != http.StatusOK || !wireShape.MatchString(team.CreatedAt) {
		t.Errorf("team detail createdAt = %q (status %d), want second-precision RFC3339 UTC", team.CreatedAt, status)
	}

	// Member joinedAt (GET /teams/{id}/members) from a real Grant (ms stored).
	m, err := f.members.Add("wire@corp.com", "")
	if err != nil {
		t.Fatal(err)
	}
	granted, err := f.v.Groups().Grant(m.ID, team.ID, groups.RoleRead, "local-admin:test")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(granted.GrantedAt, ".") {
		t.Errorf("stored granted_at = %q, want canonical millisecond form", granted.GrantedAt)
	}
	status, body = f.do(t, http.MethodGet, "/teams/"+team.ID+"/members", "")
	var env struct {
		Items []struct {
			JoinedAt string `json:"joinedAt"`
		} `json:"items"`
	}
	_ = json.Unmarshal(body, &env)
	if status != http.StatusOK || len(env.Items) != 1 {
		t.Fatalf("team members: %d %s", status, body)
	}
	if got, want := env.Items[0].JoinedAt, wireTime(granted.GrantedAt); got != want || !wireShape.MatchString(got) {
		t.Errorf("joinedAt = %q, want %q", got, want)
	}

	// Invitations history issuedAt from a real invite Issue (ms stored).
	if _, err := f.invites.Issue(invites.IssueParams{
		MemberID: m.ID, Email: m.Email, Role: "member",
		TokenValue: "evt_wire", CreationPath: inviteCreationPath, TTL: time.Hour,
	}); err != nil {
		t.Fatal(err)
	}
	status, body = f.do(t, http.MethodGet, "/invitations", "")
	var hist struct {
		Items []struct {
			IssuedAt string `json:"issuedAt"`
		} `json:"items"`
	}
	_ = json.Unmarshal(body, &hist)
	if status != http.StatusOK || len(hist.Items) != 1 {
		t.Fatalf("invitations history: %d %s", status, body)
	}
	if !wireShape.MatchString(hist.Items[0].IssuedAt) {
		t.Errorf("issuedAt = %q, want second-precision RFC3339 UTC", hist.Items[0].IssuedAt)
	}
}
