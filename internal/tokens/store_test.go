package tokens

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CryptoLabInc/rune-console/internal/db"
	"github.com/CryptoLabInc/rune-console/internal/storedb"
)

// openTestDB opens the strict store database at path with schema v1
// installed. Restart tests reopen the same path with a fresh handle; never
// ":memory:" (the pool hands each connection its own private database).
func openTestDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	database, err := db.OpenStrict(path)
	if err != nil {
		t.Fatalf("OpenStrict: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := storedb.EnsureSchema(database); err != nil {
		t.Fatal(err)
	}
	return database
}

// newTestDB opens a fresh store database under t.TempDir.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	return openTestDB(t, filepath.Join(t.TempDir(), "runeconsole.db"))
}

// newDBStore builds a store attached to database (the daemon construction
// shape: NewStore + LoadFromDB, which also seeds the default roles).
func newDBStore(t *testing.T, database *sql.DB) *Store {
	t.Helper()
	s := NewStore()
	if err := s.LoadFromDB(database); err != nil {
		t.Fatalf("LoadFromDB: %v", err)
	}
	return s
}

// newTestStore builds a DB-backed store on a fresh database, returning both.
// LoadFromDB seeds the default admin/member roles, replacing the old
// fixture's direct map write.
func newTestStore(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	database := newTestDB(t)
	return newDBStore(t, database), database
}

func intp(v int) *int { return &v }

// tokenRowCount counts tokens rows for user straight from the database,
// bypassing the in-memory cache.
func tokenRowCount(t *testing.T, database *sql.DB, user string) int {
	t.Helper()
	var n int
	if err := database.QueryRow(`SELECT COUNT(*) FROM tokens WHERE user = ?`, user).Scan(&n); err != nil {
		t.Fatalf("count token rows for %s: %v", user, err)
	}
	return n
}

// ── add / validate / revoke ────────────────────────────────────────

func TestAddAndValidateToken(t *testing.T) {
	s, _ := newTestStore(t)
	tok, err := s.AddToken("alice", "member", intp(90))
	if err != nil {
		t.Fatalf("AddToken: %v", err)
	}
	if tok.User != "alice" {
		t.Errorf("user = %q, want alice", tok.User)
	}
	if !strings.HasPrefix(tok.Token, "evt_") {
		t.Errorf("token = %q, want evt_ prefix", tok.Token)
	}
	if len(tok.Token) != 36 {
		t.Errorf("token length = %d, want 36", len(tok.Token))
	}
	if tok.Role != "member" {
		t.Errorf("role = %q, want member", tok.Role)
	}

	user, role, err := s.Validate(tok.Token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if user != "alice" || role.Name != "member" {
		t.Errorf("got (%q, %q), want (alice, member)", user, role.Name)
	}
}

func TestInvalidTokenRaises(t *testing.T) {
	s, _ := newTestStore(t)
	_, _, err := s.Validate("nonexistent_token")
	if !errors.Is(err, ErrTokenNotFound{}) {
		t.Errorf("err = %v, want ErrTokenNotFound", err)
	}
}

func TestExpiredTokenRaises(t *testing.T) {
	s, _ := newTestStore(t)
	// The store returns value copies, so expiry cannot be forced by mutating
	// the returned token (the old fixture's trick) — advance the injected
	// clock past the one-day expiry instead.
	base := time.Date(2026, 7, 16, 9, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return base }
	tok, err := s.AddToken("bob", "member", intp(1))
	if err != nil {
		t.Fatalf("AddToken: %v", err)
	}
	s.now = func() time.Time { return base.AddDate(0, 0, 3) }

	_, _, err = s.Validate(tok.Token)
	var exp ErrTokenExpired
	if !errors.As(err, &exp) {
		t.Fatalf("err = %v, want ErrTokenExpired", err)
	}
	if exp.User != "bob" {
		t.Errorf("user = %q, want bob", exp.User)
	}
}

func TestRevokeToken(t *testing.T) {
	s, database := newTestStore(t)
	tok, _ := s.AddToken("charlie", "member", nil)
	revoked, err := s.RevokeToken("charlie")
	if err != nil {
		t.Fatalf("RevokeToken: %v", err)
	}
	if !revoked {
		t.Fatal("RevokeToken returned false")
	}
	_, _, err = s.Validate(tok.Token)
	if !errors.Is(err, ErrTokenNotFound{}) {
		t.Errorf("err after revoke = %v, want ErrTokenNotFound", err)
	}
	if n := tokenRowCount(t, database, "charlie"); n != 0 {
		t.Errorf("token rows after revoke = %d, want 0", n)
	}
}

func TestRevokeNonexistentReturnsFalse(t *testing.T) {
	s, _ := newTestStore(t)
	revoked, err := s.RevokeToken("nobody")
	if err != nil {
		t.Fatalf("RevokeToken(nobody): %v", err)
	}
	if revoked {
		t.Error("RevokeToken(nobody) = true, want false")
	}
}

func TestDuplicateUserRejected(t *testing.T) {
	s, database := newTestStore(t)
	if _, err := s.AddToken("alice", "member", nil); err != nil {
		t.Fatalf("first AddToken: %v", err)
	}
	_, err := s.AddToken("alice", "member", nil)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("err = %v, want 'already exists'", err)
	}
	// One-token-per-user is also the tokens.user PRIMARY KEY: exactly one
	// row survives.
	if n := tokenRowCount(t, database, "alice"); n != 1 {
		t.Errorf("token rows for alice = %d, want 1", n)
	}
}

func TestInvalidRoleRejected(t *testing.T) {
	s, _ := newTestStore(t)
	_, err := s.AddToken("alice", "nonexistent_role", nil)
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("err = %v, want 'does not exist'", err)
	}
}

func TestListTokensHidesValues(t *testing.T) {
	s, _ := newTestStore(t)
	if _, err := s.AddToken("alice", "member", intp(30)); err != nil {
		t.Fatalf("AddToken: %v", err)
	}
	res := s.ListTokens()
	if len(res) != 1 {
		t.Fatalf("len = %d, want 1", len(res))
	}
	if res[0].User != "alice" {
		t.Errorf("user = %q, want alice", res[0].User)
	}
	// TokenInfo struct intentionally has no Token field.
}

func TestGetUsername(t *testing.T) {
	s, _ := newTestStore(t)
	tok, err := s.AddToken("alice", "member", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.GetUsername(tok.Token); got != "alice" {
		t.Errorf("GetUsername = %q, want alice", got)
	}
	if got := s.GetUsername("evt_00000000000000000000000000000000"); got != "" {
		t.Errorf("GetUsername(unknown) = %q, want empty", got)
	}
}

func TestRateLimitingPerUser(t *testing.T) {
	s, _ := newTestStore(t)
	if _, err := s.AddRole("limited", []string{"get_public_key"}, 5, "2/60s"); err != nil {
		t.Fatalf("AddRole: %v", err)
	}
	tok, err := s.AddToken("ratelimited_user", "limited", nil)
	if err != nil {
		t.Fatalf("AddToken: %v", err)
	}
	if _, _, err := s.Validate(tok.Token); err != nil {
		t.Fatalf("first Validate: %v", err)
	}
	if _, _, err := s.Validate(tok.Token); err != nil {
		t.Fatalf("second Validate: %v", err)
	}
	_, _, err = s.Validate(tok.Token)
	var rl ErrRateLimit
	if !errors.As(err, &rl) {
		t.Fatalf("third Validate err = %v, want ErrRateLimit", err)
	}
}

func TestTopKFromRole(t *testing.T) {
	s, _ := newTestStore(t)
	tok, _ := s.AddToken("alice", "member", nil)
	_, role, err := s.Validate(tok.Token)
	if err != nil {
		t.Fatal(err)
	}
	if role.TopK != 10 {
		t.Errorf("top_k = %d, want 10", role.TopK)
	}
}

func TestNeverExpiresToken(t *testing.T) {
	s, database := newTestStore(t)
	tok, err := s.AddToken("permanent_user", "admin", nil)
	if err != nil {
		t.Fatal(err)
	}
	if tok.Expires != "" {
		t.Errorf("expires = %q, want empty", tok.Expires)
	}
	if tok.IsExpired() {
		t.Error("IsExpired = true, want false")
	}
	user, _, err := s.Validate(tok.Token)
	if err != nil {
		t.Fatal(err)
	}
	if user != "permanent_user" {
		t.Errorf("user = %q, want permanent_user", user)
	}
	// "" maps to SQL NULL (never expires).
	var expires sql.NullString
	if err := database.QueryRow(`SELECT expires FROM tokens WHERE user = 'permanent_user'`).Scan(&expires); err != nil {
		t.Fatal(err)
	}
	if expires.Valid {
		t.Errorf("expires column = %q, want NULL", expires.String)
	}
}

// TestPersistAndReload: a role and a token written through one store are
// served by a second store loaded from the same database path (restart
// shape). Replaces the YAML round-trip test.
func TestPersistAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runeconsole.db")
	s1 := newDBStore(t, openTestDB(t, path))
	if _, err := s1.AddRole("researcher", []string{"get_public_key", "decrypt_scores"}, 3, "10/60s"); err != nil {
		t.Fatalf("AddRole: %v", err)
	}
	tok, err := s1.AddToken("alice", "member", intp(90))
	if err != nil {
		t.Fatalf("AddToken: %v", err)
	}

	s2 := newDBStore(t, openTestDB(t, path))
	user, role, err := s2.Validate(tok.Token)
	if err != nil {
		t.Fatalf("reload Validate: %v", err)
	}
	if user != "alice" || role.Name != "member" {
		t.Errorf("got (%q, %q), want (alice, member)", user, role.Name)
	}

	roles := s2.ListRoles()
	found := false
	for _, r := range roles {
		if r.Name == "researcher" {
			found = true
			break
		}
	}
	if !found {
		t.Error("researcher role missing after reload")
	}
}

// ── rotation ───────────────────────────────────────────────────────

func TestRotateToken(t *testing.T) {
	s, _ := newTestStore(t)
	old, _ := s.AddToken("alice", "member", nil)
	newTok, err := s.RotateToken("alice")
	if err != nil {
		t.Fatal(err)
	}
	if newTok.User != "alice" || newTok.Role != "member" {
		t.Errorf("got (%q, %q), want (alice, member)", newTok.User, newTok.Role)
	}
	if !strings.HasPrefix(newTok.Token, "evt_") {
		t.Errorf("token = %q, want evt_ prefix", newTok.Token)
	}
	if newTok.Token == old.Token {
		t.Error("new token equals old token")
	}
}

func TestRotatePreservesExpiry(t *testing.T) {
	s, _ := newTestStore(t)
	if _, err := s.AddToken("alice", "member", intp(90)); err != nil {
		t.Fatal(err)
	}
	newTok, err := s.RotateToken("alice")
	if err != nil {
		t.Fatal(err)
	}
	if newTok.Expires == "" {
		t.Fatal("expires empty after rotation")
	}
	got, err := time.Parse(dateFormat, newTok.Expires)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Now().UTC().AddDate(0, 0, 90).Format(dateFormat)
	if got.Format(dateFormat) != want {
		t.Errorf("expires = %s, want %s", got.Format(dateFormat), want)
	}
}

func TestRotateInvalidatesOldToken(t *testing.T) {
	s, _ := newTestStore(t)
	old, _ := s.AddToken("alice", "member", nil)
	if _, err := s.RotateToken("alice"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Validate(old.Token); !errors.Is(err, ErrTokenNotFound{}) {
		t.Errorf("err = %v, want ErrTokenNotFound", err)
	}
}

func TestRotateNewTokenValidates(t *testing.T) {
	s, _ := newTestStore(t)
	if _, err := s.AddToken("alice", "member", nil); err != nil {
		t.Fatal(err)
	}
	newTok, err := s.RotateToken("alice")
	if err != nil {
		t.Fatal(err)
	}
	user, role, err := s.Validate(newTok.Token)
	if err != nil {
		t.Fatal(err)
	}
	if user != "alice" || role.Name != "member" {
		t.Errorf("got (%q, %q), want (alice, member)", user, role.Name)
	}
}

func TestRotateNonexistentUserRaises(t *testing.T) {
	s, _ := newTestStore(t)
	_, err := s.RotateToken("nobody")
	if err == nil || !strings.Contains(err.Error(), "no token found") {
		t.Errorf("err = %v, want 'no token found'", err)
	}
}

func TestRotateAll(t *testing.T) {
	s, _ := newTestStore(t)
	tokA, _ := s.AddToken("alice", "member", nil)
	tokB, _ := s.AddToken("bob", "admin", nil)
	res, err := s.RotateAllTokens()
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 {
		t.Fatalf("len = %d, want 2", len(res))
	}
	got := map[string]bool{}
	for _, tk := range res {
		got[tk.User] = true
	}
	if !got["alice"] || !got["bob"] {
		t.Errorf("got users = %v, want alice + bob", got)
	}
	if _, _, err := s.Validate(tokA.Token); !errors.Is(err, ErrTokenNotFound{}) {
		t.Errorf("alice old token still valid")
	}
	if _, _, err := s.Validate(tokB.Token); !errors.Is(err, ErrTokenNotFound{}) {
		t.Errorf("bob old token still valid")
	}
}

// TestRotatePersists: a rotation committed through one store is what a
// restart serves — the old secret is gone from the database, the new one
// validates. Replaces the YAML reload test.
func TestRotatePersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runeconsole.db")
	s1 := newDBStore(t, openTestDB(t, path))
	old, err := s1.AddToken("alice", "member", intp(30))
	if err != nil {
		t.Fatal(err)
	}
	newTok, err := s1.RotateToken("alice")
	if err != nil {
		t.Fatal(err)
	}

	s2 := newDBStore(t, openTestDB(t, path))
	user, role, err := s2.Validate(newTok.Token)
	if err != nil {
		t.Fatal(err)
	}
	if user != "alice" || role.Name != "member" {
		t.Errorf("got (%q, %q), want (alice, member)", user, role.Name)
	}
	if _, _, err := s2.Validate(old.Token); !errors.Is(err, ErrTokenNotFound{}) {
		t.Errorf("old secret still validates after reload: %v", err)
	}
}

// ── role CRUD ──────────────────────────────────────────────────────

func TestCreateRole(t *testing.T) {
	s, _ := newTestStore(t)
	r, err := s.AddRole("researcher", []string{"get_public_key", "decrypt_scores"}, 3, "10/60s")
	if err != nil {
		t.Fatal(err)
	}
	if r.Name != "researcher" || r.TopK != 3 {
		t.Errorf("got (%q, %d), want (researcher, 3)", r.Name, r.TopK)
	}
	if err := r.CheckScope("get_public_key"); err != nil {
		t.Errorf("CheckScope(get_public_key): %v", err)
	}
}

func TestCreateDuplicateRoleRejected(t *testing.T) {
	s, _ := newTestStore(t)
	_, err := s.AddRole("admin", []string{"get_public_key"}, 5, "30/60s")
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("err = %v, want 'already exists'", err)
	}
}

func TestUpdateRole(t *testing.T) {
	s, _ := newTestStore(t)
	r, err := s.UpdateRole("member", UpdateRoleOpts{TopK: intp(8)})
	if err != nil {
		t.Fatal(err)
	}
	if r.TopK != 8 || r.Name != "member" {
		t.Errorf("got (%q, %d), want (member, 8)", r.Name, r.TopK)
	}
}

func TestUpdateNonexistentRoleRejected(t *testing.T) {
	s, _ := newTestStore(t)
	_, err := s.UpdateRole("nonexistent", UpdateRoleOpts{TopK: intp(5)})
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("err = %v, want 'does not exist'", err)
	}
}

func TestDeleteCustomRole(t *testing.T) {
	s, database := newTestStore(t)
	if _, err := s.AddRole("temp", []string{"get_public_key"}, 1, "5/60s"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteRole("temp"); err != nil {
		t.Fatal(err)
	}
	for _, r := range s.ListRoles() {
		if r.Name == "temp" {
			t.Error("temp role still present after delete")
		}
	}
	var n int
	if err := database.QueryRow(`SELECT COUNT(*) FROM roles WHERE name = 'temp'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("role rows after delete = %d, want 0", n)
	}
}

func TestDeleteDefaultRoleRejected(t *testing.T) {
	s, _ := newTestStore(t)
	for _, name := range []string{"admin", "member"} {
		err := s.DeleteRole(name)
		if err == nil || !strings.Contains(err.Error(), "Cannot delete default") {
			t.Errorf("delete %s: err = %v, want 'Cannot delete default'", name, err)
		}
	}
}

// TestDeleteDefaultRoleGuardPrecedesExistence pins the guard ORDER: the
// default-name check runs before the existence check, so deleting "admin" on
// a completely empty store still reports "Cannot delete default", never
// "does not exist".
func TestDeleteDefaultRoleGuardPrecedesExistence(t *testing.T) {
	s := NewStore() // sink-less AND role-less
	err := s.DeleteRole("admin")
	if err == nil || !strings.Contains(err.Error(), "Cannot delete default") {
		t.Errorf("err = %v, want 'Cannot delete default' even with no roles loaded", err)
	}
}

func TestDeleteRoleWithActiveTokensRejected(t *testing.T) {
	s, _ := newTestStore(t)
	if _, err := s.AddRole("temp", []string{"get_public_key"}, 1, "5/60s"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddToken("user1", "temp", nil); err != nil {
		t.Fatal(err)
	}
	err := s.DeleteRole("temp")
	if err == nil || !strings.Contains(err.Error(), "token for user") {
		t.Errorf("err = %v, want 'token for user'", err)
	}
}

func TestListRoles(t *testing.T) {
	s, _ := newTestStore(t)
	roles := s.ListRoles()
	if len(roles) < 2 {
		t.Fatalf("len = %d, want >= 2", len(roles))
	}
	names := map[string]bool{}
	for _, r := range roles {
		names[r.Name] = true
	}
	if !names["admin"] || !names["member"] {
		t.Errorf("missing default roles, got %v", names)
	}
}

// TestDefaultRolesSeededWhenAbsentAndOverridesKept pins the boot seeding
// contract: a fresh database gets admin/member as rows, and a stored
// operator override of a default role is respected (seed rows are inserted
// only when absent — never an unconditional merge).
func TestDefaultRolesSeededWhenAbsentAndOverridesKept(t *testing.T) {
	// Fresh database: LoadFromDB materializes both defaults as rows.
	_, database := newTestStore(t)
	var n int
	if err := database.QueryRow(`SELECT COUNT(*) FROM roles WHERE name IN ('admin','member')`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("seeded default role rows = %d, want 2", n)
	}

	// Pre-existing override: an operator-modified admin row survives boot.
	database2 := newTestDB(t)
	if _, err := database2.Exec(
		`INSERT INTO roles (name, scope, top_k, rate_limit) VALUES ('admin', '["get_public_key"]', 99, '1/60s')`); err != nil {
		t.Fatal(err)
	}
	s2 := newDBStore(t, database2)
	for _, r := range s2.ListRoles() {
		if r.Name == "admin" && r.TopK != 99 {
			t.Errorf("admin override lost at boot: top_k = %d, want 99", r.TopK)
		}
	}
}

// TestAddRoleMaterializesWriteTimeDefaults pins the branch decision that the
// legacy read-time role defaults materialize at write: a role added with
// top_k 0 and an empty rate_limit reads back 5 / "30/60s" immediately AND
// after a restart — the old "0 until reboot, then silently 5" flip is gone.
func TestAddRoleMaterializesWriteTimeDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runeconsole.db")
	s1 := newDBStore(t, openTestDB(t, path))
	r, err := s1.AddRole("zeroed", []string{"get_public_key"}, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if r.TopK != 5 || r.RateLimit != "30/60s" {
		t.Errorf("AddRole returned (%d, %q), want (5, 30/60s)", r.TopK, r.RateLimit)
	}
	assertRole := func(s *Store, when string) {
		t.Helper()
		for _, ri := range s.ListRoles() {
			if ri.Name != "zeroed" {
				continue
			}
			if ri.TopK != 5 || ri.RateLimit != "30/60s" {
				t.Errorf("%s: role = (%d, %q), want (5, 30/60s)", when, ri.TopK, ri.RateLimit)
			}
			return
		}
		t.Errorf("%s: role zeroed missing", when)
	}
	assertRole(s1, "before reopen")
	assertRole(newDBStore(t, openTestDB(t, path)), "after reopen")
}

// TestUpdateRoleMaterializesZeroTopK: UpdateRole had the same load-time
// dependency for top_k 0 (stored 0, flipped to 5 on the next boot), so it
// materializes the default at write exactly like AddRole.
func TestUpdateRoleMaterializesZeroTopK(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runeconsole.db")
	s1 := newDBStore(t, openTestDB(t, path))
	if _, err := s1.AddRole("patchy", []string{"get_public_key"}, 3, "10/60s"); err != nil {
		t.Fatal(err)
	}
	r, err := s1.UpdateRole("patchy", UpdateRoleOpts{TopK: intp(0)})
	if err != nil {
		t.Fatal(err)
	}
	if r.TopK != 5 {
		t.Errorf("UpdateRole(top_k 0) = %d, want 5", r.TopK)
	}
	s2 := newDBStore(t, openTestDB(t, path))
	for _, ri := range s2.ListRoles() {
		if ri.Name == "patchy" && ri.TopK != 5 {
			t.Errorf("after reopen top_k = %d, want 5", ri.TopK)
		}
	}
}

func TestUpdateRoleClearsRateLimiters(t *testing.T) {
	s, _ := newTestStore(t)
	tok, _ := s.AddToken("alice", "member", nil)
	if _, _, err := s.Validate(tok.Token); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.rateLimiters["alice"]; !ok {
		t.Fatal("rate limiter not created on validate")
	}
	rl := "100/60s"
	if _, err := s.UpdateRole("member", UpdateRoleOpts{RateLimit: &rl}); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.rateLimiters["alice"]; ok {
		t.Error("rate limiter not cleared after rate_limit change")
	}
}

func TestRoleRateLimitParsed(t *testing.T) {
	r := &Role{Name: "test", RateLimit: "30/60s"}
	maxReq, window, err := r.RateLimitParsed()
	if err != nil {
		t.Fatal(err)
	}
	if maxReq != 30 {
		t.Errorf("max = %d, want 30", maxReq)
	}
	if window != 60*time.Second {
		t.Errorf("window = %v, want 60s", window)
	}
}

// ── scope check ────────────────────────────────────────────────────

func TestScopeAllowsValidMethod(t *testing.T) {
	r := &Role{Name: "member", Scope: []string{"get_public_key", "decrypt_scores"}}
	if err := r.CheckScope("get_public_key"); err != nil {
		t.Errorf("err = %v, want nil", err)
	}
}

func TestScopeRejectsInvalidMethod(t *testing.T) {
	r := &Role{Name: "limited", Scope: []string{"get_public_key"}}
	err := r.CheckScope("decrypt_scores")
	var se ErrScope
	if !errors.As(err, &se) {
		t.Fatalf("err = %v, want ErrScope", err)
	}
	if se.Method != "decrypt_scores" || se.RoleName != "limited" {
		t.Errorf("got (%q, %q), want (decrypt_scores, limited)", se.Method, se.RoleName)
	}
}

// ── TopKExceeded ───────────────────────────────────────────────────

func TestTopKExceededMessage(t *testing.T) {
	err := ErrTopKExceeded{Requested: 15, MaxTopK: 10, RoleName: "admin"}
	msg := err.Error()
	for _, want := range []string{"15", "10", "admin"} {
		if !strings.Contains(msg, want) {
			t.Errorf("msg = %q, missing %q", msg, want)
		}
	}
}

// ── demo token loader ─────────────────────────────────────────────

func TestLoadDefaultsWithDemoToken(t *testing.T) {
	s := NewStore()
	s.LoadDefaultsWithDemoToken()
	user, role, err := s.Validate(DemoToken)
	if err != nil {
		t.Fatal(err)
	}
	if user != "demo" || role.Name != "admin" {
		t.Errorf("got (%q, %q), want (demo, admin)", user, role.Name)
	}
}

// ── copy-out contract (no live pointers escape) ───────────────────

// TestReturnedValuesAreCopies pins the value-copy contract: mutating what
// Validate/AddToken/RotateToken hand back must never change store state
// (grpc handlers read the returned role after the store lock is released,
// while UpdateRole mutates the live record under it).
func TestReturnedValuesAreCopies(t *testing.T) {
	s, _ := newTestStore(t)
	tok, err := s.AddToken("alice", "member", intp(30))
	if err != nil {
		t.Fatal(err)
	}
	// Sabotage the returned token: the store must not notice.
	tok.Expires = "1999-01-01"
	if _, _, err := s.Validate(tok.Token); err != nil {
		t.Errorf("Validate after mutating returned token: %v (live pointer escaped AddToken)", err)
	}

	_, role, err := s.Validate(tok.Token)
	if err != nil {
		t.Fatal(err)
	}
	role.TopK = 999
	role.Scope[0] = "mutated"
	if _, fresh, _ := s.Validate(tok.Token); fresh.TopK == 999 || fresh.Scope[0] == "mutated" {
		t.Error("mutating Validate's returned role changed store state (live pointer escaped)")
	}

	rot, err := s.RotateToken("alice")
	if err != nil {
		t.Fatal(err)
	}
	rot.Expires = "1999-01-01"
	if _, _, err := s.Validate(rot.Token); err != nil {
		t.Errorf("Validate after mutating rotated token: %v (live pointer escaped RotateToken)", err)
	}
}

// ── dangling role (no FK on tokens.role by design) ────────────────

// danglingRoleStore loads a store from a database holding a token whose role
// row does not exist — the state a hand-edited legacy file could produce and
// the reason tokens.role has no FK.
func danglingRoleStore(t *testing.T) (*Store, string) {
	t.Helper()
	database := newTestDB(t)
	const secret = "evt_dddddddddddddddddddddddddddddddd"
	if _, err := database.Exec(
		`INSERT INTO tokens (user, token, role, issued_at, expires, last_used)
		 VALUES ('ghosted', ?, 'ghost', '2026-01-01', NULL, NULL)`, secret); err != nil {
		t.Fatal(err)
	}
	return newDBStore(t, database), secret
}

// TestDanglingRoleListsAsQuestionMarksAndFailsClosed pins the tolerate-and-
// display pair: a token whose role is missing stays visible in listings with
// TopK/RateLimit rendered as "?", while Validate refuses it with
// ErrTokenNotFound (fail-closed, indistinguishable from "no such token").
func TestDanglingRoleListsAsQuestionMarksAndFailsClosed(t *testing.T) {
	s, secret := danglingRoleStore(t)
	infos := s.ListTokens()
	if len(infos) != 1 {
		t.Fatalf("ListTokens len = %d, want 1", len(infos))
	}
	if infos[0].Role != "ghost" || infos[0].TopK != "?" || infos[0].RateLimit != "?" {
		t.Errorf("dangling-role row = %+v, want role ghost with '?' attributes", infos[0])
	}
	if _, _, err := s.Validate(secret); !errors.Is(err, ErrTokenNotFound{}) {
		t.Errorf("Validate = %v, want ErrTokenNotFound (fail closed)", err)
	}
}

// TestRotateTokenKeepsDanglingRole pins the deliberate legacy gap: rotation
// preserves the role name without checking it still exists, and the rotated
// token keeps failing Validate closed.
func TestRotateTokenKeepsDanglingRole(t *testing.T) {
	s, _ := danglingRoleStore(t)
	rot, err := s.RotateToken("ghosted")
	if err != nil {
		t.Fatalf("RotateToken over a dangling role: %v (legacy behavior allows it)", err)
	}
	if rot.Role != "ghost" {
		t.Errorf("rotated role = %q, want ghost", rot.Role)
	}
	if _, _, err := s.Validate(rot.Token); !errors.Is(err, ErrTokenNotFound{}) {
		t.Errorf("rotated dangling-role token Validate = %v, want ErrTokenNotFound", err)
	}
}

// ── plaintext-at-rest contract (this release) ─────────────────────

// TestMintedTokenPlaintextStoredInDB replaces the YAML byte-grep: token
// secrets are deliberately stored in plaintext this release (rollback to the
// YAML binary must keep working; hashing is a named follow-up), guarded by
// the database file's 0600 fail-closed posture.
func TestMintedTokenPlaintextStoredInDB(t *testing.T) {
	s, database := newTestStore(t)
	tok, err := s.AddToken("alice", "member", intp(7))
	if err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := database.QueryRow(`SELECT token FROM tokens WHERE user = 'alice'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != tok.Token {
		t.Errorf("stored token = %q, want the minted plaintext %q", stored, tok.Token)
	}
}

// ── last_used: in-memory stamp + async durability ─────────────────

// TestValidateStampsLastUsedThrottled checks that Validate records the
// token's last-access time in memory, only rewrites it once per throttle
// window, persists it asynchronously (awaited via the syncLastUsed seam),
// and — new with the SQLite migration — serves it across a restart.
func TestValidateStampsLastUsedThrottled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runeconsole.db")
	s := newDBStore(t, openTestDB(t, path))
	base := time.Date(2026, 7, 16, 9, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return base }
	tok, err := s.AddToken("alice", "member", nil)
	if err != nil {
		t.Fatalf("AddToken: %v", err)
	}

	if _, _, err := s.Validate(tok.Token); err != nil {
		t.Fatalf("validate 1: %v", err)
	}
	if got := s.tokensByUser["alice"].LastUsed; got != storedb.FormatTime(base) {
		t.Fatalf("LastUsed after first use = %q, want %q", got, storedb.FormatTime(base))
	}

	// Within the throttle window: not rewritten.
	s.now = func() time.Time { return base.Add(lastUsedThrottle / 2) }
	if _, _, err := s.Validate(tok.Token); err != nil {
		t.Fatalf("validate 2: %v", err)
	}
	if got := s.tokensByUser["alice"].LastUsed; got != storedb.FormatTime(base) {
		t.Errorf("LastUsed rewritten within throttle: %q", got)
	}

	// Past the throttle window: updated.
	later := base.Add(lastUsedThrottle + time.Second)
	s.now = func() time.Time { return later }
	if _, _, err := s.Validate(tok.Token); err != nil {
		t.Fatalf("validate 3: %v", err)
	}
	if got := s.tokensByUser["alice"].LastUsed; got != storedb.FormatTime(later) {
		t.Errorf("LastUsed after throttle = %q, want %q", got, storedb.FormatTime(later))
	}

	// ListTokens surfaces LastUsed and non-expired.
	infos := s.ListTokens()
	if len(infos) != 1 || infos[0].LastUsed != storedb.FormatTime(later) || infos[0].Expired {
		t.Errorf("ListTokens = %+v", infos)
	}

	// Durability leg (new behavior): the stamp reaches the database via the
	// async writer — awaited through the seam, never by sleeping — and a
	// store reopened from the same path serves it.
	s.syncLastUsed()
	var stored sql.NullString
	if err := s.db.QueryRow(`SELECT last_used FROM tokens WHERE user = 'alice'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored.String != storedb.FormatTime(later) {
		t.Errorf("persisted last_used = %q, want %q", stored.String, storedb.FormatTime(later))
	}
	s2 := newDBStore(t, openTestDB(t, path))
	infos2 := s2.ListTokens()
	if len(infos2) != 1 || infos2[0].LastUsed != storedb.FormatTime(later) {
		t.Errorf("restart lost last_used: %+v", infos2)
	}
}

// TestLastUsedWriterThrottlesPerToken drives the async writer directly (the
// public path cannot: the 60s in-memory throttle spaces stamps wider than
// the 10s persist interval, so the writer-side throttle exists as a
// safety bound). Two stamps 5s apart persist once; a third past the
// interval persists again.
func TestLastUsedWriterThrottlesPerToken(t *testing.T) {
	s, database := newTestStore(t)
	tok, err := s.AddToken("alice", "member", nil)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 7, 16, 9, 0, 0, 0, time.UTC)
	enqueue := func(at time.Time) {
		s.lastUsedCh <- lastUsedEvent{user: "alice", token: tok.Token, stamp: storedb.FormatTime(at), at: at}
	}
	readStamp := func() string {
		t.Helper()
		var v sql.NullString
		if err := database.QueryRow(`SELECT last_used FROM tokens WHERE user = 'alice'`).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v.String
	}

	enqueue(base)
	enqueue(base.Add(5 * time.Second)) // inside lastUsedPersistInterval: dropped
	s.syncLastUsed()
	if got := readStamp(); got != storedb.FormatTime(base) {
		t.Errorf("stamp after throttled pair = %q, want %q", got, storedb.FormatTime(base))
	}

	next := base.Add(lastUsedPersistInterval + time.Second)
	enqueue(next)
	s.syncLastUsed()
	if got := readStamp(); got != storedb.FormatTime(next) {
		t.Errorf("stamp past interval = %q, want %q", got, storedb.FormatTime(next))
	}
}

// TestStaleLastUsedStampAfterRotationIsDropped pins the writer's UPDATE
// being keyed on user AND token: a stamp enqueued for the pre-rotation
// secret must not backdate the rotated row.
func TestStaleLastUsedStampAfterRotationIsDropped(t *testing.T) {
	s, database := newTestStore(t)
	old, err := s.AddToken("alice", "member", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RotateToken("alice"); err != nil {
		t.Fatal(err)
	}
	stale := time.Date(2026, 7, 16, 9, 0, 0, 0, time.UTC)
	s.lastUsedCh <- lastUsedEvent{user: "alice", token: old.Token, stamp: storedb.FormatTime(stale), at: stale}
	s.syncLastUsed()
	var v sql.NullString
	if err := database.QueryRow(`SELECT last_used FROM tokens WHERE user = 'alice'`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v.Valid {
		t.Errorf("rotated row got the stale pre-rotation stamp %q", v.String)
	}
}
