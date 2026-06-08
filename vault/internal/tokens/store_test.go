package tokens

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s := NewStore()
	for n, r := range DefaultRoles() {
		s.roles[n] = r
	}
	return s
}

func intp(v int) *int { return &v }

// ── add / validate / revoke ────────────────────────────────────────

func TestAddAndValidateToken(t *testing.T) {
	s := newTestStore(t)
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
	s := newTestStore(t)
	_, _, err := s.Validate("nonexistent_token")
	if !errors.Is(err, ErrTokenNotFound{}) {
		t.Errorf("err = %v, want ErrTokenNotFound", err)
	}
}

func TestExpiredTokenRaises(t *testing.T) {
	s := newTestStore(t)
	tok, err := s.AddToken("bob", "member", intp(1))
	if err != nil {
		t.Fatalf("AddToken: %v", err)
	}
	tok.Expires = time.Now().AddDate(0, 0, -1).Format(dateFormat)

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
	s := newTestStore(t)
	tok, _ := s.AddToken("charlie", "member", nil)
	if !s.RevokeToken("charlie") {
		t.Fatal("RevokeToken returned false")
	}
	_, _, err := s.Validate(tok.Token)
	if !errors.Is(err, ErrTokenNotFound{}) {
		t.Errorf("err after revoke = %v, want ErrTokenNotFound", err)
	}
}

func TestRevokeNonexistentReturnsFalse(t *testing.T) {
	s := newTestStore(t)
	if s.RevokeToken("nobody") {
		t.Error("RevokeToken(nobody) = true, want false")
	}
}

func TestDuplicateUserRejected(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.AddToken("alice", "member", nil); err != nil {
		t.Fatalf("first AddToken: %v", err)
	}
	_, err := s.AddToken("alice", "member", nil)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("err = %v, want 'already exists'", err)
	}
}

func TestInvalidRoleRejected(t *testing.T) {
	s := newTestStore(t)
	_, err := s.AddToken("alice", "nonexistent_role", nil)
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("err = %v, want 'does not exist'", err)
	}
}

func TestListTokensHidesValues(t *testing.T) {
	s := newTestStore(t)
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

func TestRateLimitingPerUser(t *testing.T) {
	s := newTestStore(t)
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
	s := newTestStore(t)
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
	s := newTestStore(t)
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
}

func TestPersistAndReload(t *testing.T) {
	dir := t.TempDir()
	rolesPath := filepath.Join(dir, "roles.yml")
	tokensPath := filepath.Join(dir, "tokens.yml")

	s1 := NewStore()
	if err := s1.LoadFromFiles(rolesPath, tokensPath); err != nil {
		t.Fatalf("LoadFromFiles: %v", err)
	}
	if _, err := s1.AddRole("researcher", []string{"get_public_key", "decrypt_scores"}, 3, "10/60s"); err != nil {
		t.Fatalf("AddRole: %v", err)
	}
	tok, err := s1.AddToken("alice", "member", intp(90))
	if err != nil {
		t.Fatalf("AddToken: %v", err)
	}
	s1.Flush()

	s2 := NewStore()
	if err := s2.LoadFromFiles(rolesPath, tokensPath); err != nil {
		t.Fatalf("reload LoadFromFiles: %v", err)
	}
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
	s := newTestStore(t)
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
	s := newTestStore(t)
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
	s := newTestStore(t)
	old, _ := s.AddToken("alice", "member", nil)
	if _, err := s.RotateToken("alice"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Validate(old.Token); !errors.Is(err, ErrTokenNotFound{}) {
		t.Errorf("err = %v, want ErrTokenNotFound", err)
	}
}

func TestRotateNewTokenValidates(t *testing.T) {
	s := newTestStore(t)
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
	s := newTestStore(t)
	_, err := s.RotateToken("nobody")
	if err == nil || !strings.Contains(err.Error(), "no token found") {
		t.Errorf("err = %v, want 'no token found'", err)
	}
}

func TestRotateAll(t *testing.T) {
	s := newTestStore(t)
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

func TestRotatePersists(t *testing.T) {
	dir := t.TempDir()
	rolesPath := filepath.Join(dir, "roles.yml")
	tokensPath := filepath.Join(dir, "tokens.yml")

	s1 := NewStore()
	if err := s1.LoadFromFiles(rolesPath, tokensPath); err != nil {
		t.Fatal(err)
	}
	if _, err := s1.AddToken("alice", "member", intp(30)); err != nil {
		t.Fatal(err)
	}
	newTok, err := s1.RotateToken("alice")
	if err != nil {
		t.Fatal(err)
	}
	s1.Flush()

	s2 := NewStore()
	if err := s2.LoadFromFiles(rolesPath, tokensPath); err != nil {
		t.Fatal(err)
	}
	user, role, err := s2.Validate(newTok.Token)
	if err != nil {
		t.Fatal(err)
	}
	if user != "alice" || role.Name != "member" {
		t.Errorf("got (%q, %q), want (alice, member)", user, role.Name)
	}
}

// ── role CRUD ──────────────────────────────────────────────────────

func TestCreateRole(t *testing.T) {
	s := newTestStore(t)
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
	s := newTestStore(t)
	_, err := s.AddRole("admin", []string{"get_public_key"}, 5, "30/60s")
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("err = %v, want 'already exists'", err)
	}
}

func TestUpdateRole(t *testing.T) {
	s := newTestStore(t)
	r, err := s.UpdateRole("member", UpdateRoleOpts{TopK: intp(8)})
	if err != nil {
		t.Fatal(err)
	}
	if r.TopK != 8 || r.Name != "member" {
		t.Errorf("got (%q, %d), want (member, 8)", r.Name, r.TopK)
	}
}

func TestUpdateNonexistentRoleRejected(t *testing.T) {
	s := newTestStore(t)
	_, err := s.UpdateRole("nonexistent", UpdateRoleOpts{TopK: intp(5)})
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("err = %v, want 'does not exist'", err)
	}
}

func TestDeleteCustomRole(t *testing.T) {
	s := newTestStore(t)
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
}

func TestDeleteDefaultRoleRejected(t *testing.T) {
	s := newTestStore(t)
	for _, name := range []string{"admin", "member"} {
		err := s.DeleteRole(name)
		if err == nil || !strings.Contains(err.Error(), "Cannot delete default") {
			t.Errorf("delete %s: err = %v, want 'Cannot delete default'", name, err)
		}
	}
}

func TestDeleteRoleWithActiveTokensRejected(t *testing.T) {
	s := newTestStore(t)
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
	s := newTestStore(t)
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

func TestUpdateRoleClearsRateLimiters(t *testing.T) {
	s := newTestStore(t)
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

// ── persistence file content sanity ──────────────────────────────

func TestPersistedTokensFileContainsToken(t *testing.T) {
	dir := t.TempDir()
	rolesPath := filepath.Join(dir, "roles.yml")
	tokensPath := filepath.Join(dir, "tokens.yml")

	s := NewStore()
	if err := s.LoadFromFiles(rolesPath, tokensPath); err != nil {
		t.Fatal(err)
	}
	tok, _ := s.AddToken("alice", "member", intp(7))
	s.Flush()

	body, err := os.ReadFile(tokensPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), tok.Token) {
		t.Errorf("tokens.yml missing token %q", tok.Token)
	}
}
