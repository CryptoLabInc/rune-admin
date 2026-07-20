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
// shape: NewStore + LoadFromDB).
func newDBStore(t *testing.T, database *sql.DB) *Store {
	t.Helper()
	s := NewStore()
	if err := s.LoadFromDB(database); err != nil {
		t.Fatalf("LoadFromDB: %v", err)
	}
	return s
}

// newTestStore builds a DB-backed store on a fresh database, returning both.
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
	tok, err := s.AddToken("alice", intp(90))
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

	user, err := s.Validate(tok.Token)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if user != "alice" {
		t.Errorf("user = %q, want alice", user)
	}
}

func TestInvalidTokenRaises(t *testing.T) {
	s, _ := newTestStore(t)
	_, err := s.Validate("nonexistent_token")
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
	tok, err := s.AddToken("bob", intp(1))
	if err != nil {
		t.Fatalf("AddToken: %v", err)
	}
	s.now = func() time.Time { return base.AddDate(0, 0, 3) }

	_, err = s.Validate(tok.Token)
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
	tok, _ := s.AddToken("charlie", nil)
	revoked, err := s.RevokeToken("charlie")
	if err != nil {
		t.Fatalf("RevokeToken: %v", err)
	}
	if !revoked {
		t.Fatal("RevokeToken returned false")
	}
	_, err = s.Validate(tok.Token)
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
	if _, err := s.AddToken("alice", nil); err != nil {
		t.Fatalf("first AddToken: %v", err)
	}
	_, err := s.AddToken("alice", nil)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("err = %v, want 'already exists'", err)
	}
	// One-token-per-user is also the tokens.user PRIMARY KEY: exactly one
	// row survives.
	if n := tokenRowCount(t, database, "alice"); n != 1 {
		t.Errorf("token rows for alice = %d, want 1", n)
	}
}

func TestListTokensHidesValues(t *testing.T) {
	s, _ := newTestStore(t)
	if _, err := s.AddToken("alice", intp(30)); err != nil {
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
	tok, err := s.AddToken("alice", nil)
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

func TestNeverExpiresToken(t *testing.T) {
	s, database := newTestStore(t)
	tok, err := s.AddToken("permanent_user", nil)
	if err != nil {
		t.Fatal(err)
	}
	if tok.Expires != "" {
		t.Errorf("expires = %q, want empty", tok.Expires)
	}
	if tok.IsExpired() {
		t.Error("IsExpired = true, want false")
	}
	user, err := s.Validate(tok.Token)
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

// TestPersistAndReload: a token written through one store is served by a
// second store loaded from the same database path (restart shape).
func TestPersistAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runeconsole.db")
	s1 := newDBStore(t, openTestDB(t, path))
	tok, err := s1.AddToken("alice", intp(90))
	if err != nil {
		t.Fatalf("AddToken: %v", err)
	}

	s2 := newDBStore(t, openTestDB(t, path))
	user, err := s2.Validate(tok.Token)
	if err != nil {
		t.Fatalf("reload Validate: %v", err)
	}
	if user != "alice" {
		t.Errorf("user = %q, want alice", user)
	}
}

// ── rotation ───────────────────────────────────────────────────────

func TestRotateToken(t *testing.T) {
	s, _ := newTestStore(t)
	old, _ := s.AddToken("alice", nil)
	newTok, err := s.RotateToken("alice")
	if err != nil {
		t.Fatal(err)
	}
	if newTok.User != "alice" {
		t.Errorf("user = %q, want alice", newTok.User)
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
	if _, err := s.AddToken("alice", intp(90)); err != nil {
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
	old, _ := s.AddToken("alice", nil)
	if _, err := s.RotateToken("alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Validate(old.Token); !errors.Is(err, ErrTokenNotFound{}) {
		t.Errorf("err = %v, want ErrTokenNotFound", err)
	}
}

func TestRotateNewTokenValidates(t *testing.T) {
	s, _ := newTestStore(t)
	if _, err := s.AddToken("alice", nil); err != nil {
		t.Fatal(err)
	}
	newTok, err := s.RotateToken("alice")
	if err != nil {
		t.Fatal(err)
	}
	user, err := s.Validate(newTok.Token)
	if err != nil {
		t.Fatal(err)
	}
	if user != "alice" {
		t.Errorf("user = %q, want alice", user)
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
	tokA, _ := s.AddToken("alice", nil)
	tokB, _ := s.AddToken("bob", nil)
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
	if _, err := s.Validate(tokA.Token); !errors.Is(err, ErrTokenNotFound{}) {
		t.Errorf("alice old token still valid")
	}
	if _, err := s.Validate(tokB.Token); !errors.Is(err, ErrTokenNotFound{}) {
		t.Errorf("bob old token still valid")
	}
}

// TestRotatePersists: a rotation committed through one store is what a
// restart serves — the old secret is gone from the database, the new one
// validates.
func TestRotatePersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runeconsole.db")
	s1 := newDBStore(t, openTestDB(t, path))
	old, err := s1.AddToken("alice", intp(30))
	if err != nil {
		t.Fatal(err)
	}
	newTok, err := s1.RotateToken("alice")
	if err != nil {
		t.Fatal(err)
	}

	s2 := newDBStore(t, openTestDB(t, path))
	user, err := s2.Validate(newTok.Token)
	if err != nil {
		t.Fatal(err)
	}
	if user != "alice" {
		t.Errorf("user = %q, want alice", user)
	}
	if _, err := s2.Validate(old.Token); !errors.Is(err, ErrTokenNotFound{}) {
		t.Errorf("old secret still validates after reload: %v", err)
	}
}

// ── demo token loader ─────────────────────────────────────────────

func TestLoadDefaultsWithDemoToken(t *testing.T) {
	s := NewStore()
	s.LoadDefaultsWithDemoToken()
	user, err := s.Validate(DemoToken)
	if err != nil {
		t.Fatal(err)
	}
	if user != "demo" {
		t.Errorf("user = %q, want demo", user)
	}
}

// ── copy-out contract (no live pointers escape) ───────────────────

// TestReturnedValuesAreCopies pins the value-copy contract: mutating what
// AddToken/RotateToken hand back must never change store state.
func TestReturnedValuesAreCopies(t *testing.T) {
	s, _ := newTestStore(t)
	tok, err := s.AddToken("alice", intp(30))
	if err != nil {
		t.Fatal(err)
	}
	// Sabotage the returned token: the store must not notice.
	tok.Expires = "1999-01-01"
	if _, err := s.Validate(tok.Token); err != nil {
		t.Errorf("Validate after mutating returned token: %v (live pointer escaped AddToken)", err)
	}

	rot, err := s.RotateToken("alice")
	if err != nil {
		t.Fatal(err)
	}
	rot.Expires = "1999-01-01"
	if _, err := s.Validate(rot.Token); err != nil {
		t.Errorf("Validate after mutating rotated token: %v (live pointer escaped RotateToken)", err)
	}
}

// ── plaintext-at-rest contract (this release) ─────────────────────

// TestMintedTokenPlaintextStoredInDB replaces the YAML byte-grep: token
// secrets are deliberately stored in plaintext this release (rollback to the
// YAML binary must keep working; hashing is a named follow-up), guarded by
// the database file's 0600 fail-closed posture.
func TestMintedTokenPlaintextStoredInDB(t *testing.T) {
	s, database := newTestStore(t)
	tok, err := s.AddToken("alice", intp(7))
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
	tok, err := s.AddToken("alice", nil)
	if err != nil {
		t.Fatalf("AddToken: %v", err)
	}

	if _, err := s.Validate(tok.Token); err != nil {
		t.Fatalf("validate 1: %v", err)
	}
	if got := s.tokensByUser["alice"].LastUsed; got != storedb.FormatTime(base) {
		t.Fatalf("LastUsed after first use = %q, want %q", got, storedb.FormatTime(base))
	}

	// Within the throttle window: not rewritten.
	s.now = func() time.Time { return base.Add(lastUsedThrottle / 2) }
	if _, err := s.Validate(tok.Token); err != nil {
		t.Fatalf("validate 2: %v", err)
	}
	if got := s.tokensByUser["alice"].LastUsed; got != storedb.FormatTime(base) {
		t.Errorf("LastUsed rewritten within throttle: %q", got)
	}

	// Past the throttle window: updated.
	later := base.Add(lastUsedThrottle + time.Second)
	s.now = func() time.Time { return later }
	if _, err := s.Validate(tok.Token); err != nil {
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
	tok, err := s.AddToken("alice", nil)
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
	old, err := s.AddToken("alice", nil)
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
