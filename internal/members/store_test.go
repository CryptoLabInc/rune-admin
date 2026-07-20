package members

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CryptoLabInc/rune-console/internal/db"
	"github.com/CryptoLabInc/rune-console/internal/storedb"
)

func strptr(s string) *string { return &s }

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

func TestAddAssignsUUIDAndRegisteredStatus(t *testing.T) {
	s := NewStore()
	m, err := s.Add("alice@corp.com", "Alice")
	if err != nil {
		t.Fatal(err)
	}
	if !isCanonicalUUID(m.ID) {
		t.Errorf("id %q is not a canonical UUID", m.ID)
	}
	if m.Status != StatusRegistered {
		t.Errorf("status = %q, want %q", m.Status, StatusRegistered)
	}
	if m.DisplayName != "Alice" || m.Email != "alice@corp.com" {
		t.Errorf("member = %+v", m)
	}
	// Add must write the canonical storedb.TimeFormat: parseable RFC3339 AND
	// already in the one fixed-width millisecond rendering (a value that
	// canonicalizes to itself), so stored members sort textually.
	if canonical, err := storedb.CanonicalizeTime(m.CreatedAt); err != nil || m.CreatedAt != canonical {
		t.Errorf("created_at %q is not canonical (want %q, err %v)", m.CreatedAt, canonical, err)
	}
	if got, err := s.Get(m.ID); err != nil || got.ID != m.ID {
		t.Errorf("Get(id) = (%+v, %v)", got, err)
	}
	if got, err := s.GetByEmail("alice@corp.com"); err != nil || got.ID != m.ID {
		t.Errorf("GetByEmail = (%+v, %v)", got, err)
	}
}

func TestAddDuplicateEmailRejected(t *testing.T) {
	s := NewStore()
	if _, err := s.Add("bob@corp.com", "Bob"); err != nil {
		t.Fatal(err)
	}
	_, err := s.Add("bob@corp.com", "Bob Two")
	if !errors.As(err, new(ErrDuplicateEmail)) {
		t.Errorf("duplicate email = %v, want ErrDuplicateEmail", err)
	}
}

func TestAddInvalidEmailRejected(t *testing.T) {
	s := NewStore()
	bad := []string{
		"plainname",       // no '@'
		"a@b@corp.com",    // two '@'
		"al ice@corp.com", // whitespace
		"@corp.com",       // empty local part
		"alice@",          // empty domain
		"",                // empty
	}
	for _, e := range bad {
		if _, err := s.Add(e, "x"); !errors.As(err, new(ErrInvalidEmail)) {
			t.Errorf("Add(%q) err = %v, want ErrInvalidEmail", e, err)
		}
	}
}

// TestEmailIsFixedAtAddOnly locks in that email is set exactly once, at Add():
// Update has no email parameter (compile-level: no caller can express a rename)
// and touching the other fields never moves the address or its index.
func TestEmailIsFixedAtAddOnly(t *testing.T) {
	s := NewStore()
	m, _ := s.Add("only@corp.com", "C")
	// Update the mutable fields; the email must be carried through untouched.
	updated, err := s.Update(m.ID, strptr("Renamed"), strptr(StatusDisabled))
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != m.ID {
		t.Errorf("Update changed ID: %s -> %s", m.ID, updated.ID)
	}
	if updated.Email != "only@corp.com" {
		t.Errorf("email changed by Update: %q, want only@corp.com", updated.Email)
	}
	if updated.DisplayName != "Renamed" {
		t.Errorf("display_name = %q, want Renamed", updated.DisplayName)
	}
	// The address still resolves to the same row and stays reserved: a second
	// member cannot claim it.
	if got, err := s.GetByEmail("only@corp.com"); err != nil || got.ID != m.ID {
		t.Errorf("GetByEmail = (%+v, %v)", got, err)
	}
	if _, err := s.Add("only@corp.com", "D"); !errors.As(err, new(ErrDuplicateEmail)) {
		t.Errorf("re-add of in-use email = %v, want ErrDuplicateEmail", err)
	}
}

func TestUpdateStatusDisabledIsSoftDelete(t *testing.T) {
	s := NewStore()
	m, _ := s.Add("e@corp.com", "E")
	if _, err := s.Update(m.ID, nil, strptr(StatusDisabled)); err != nil {
		t.Fatal(err)
	}
	// Row is retained: Get still returns it, with the disabled status.
	got, err := s.Get(m.ID)
	if err != nil {
		t.Fatalf("disabled member should still be gettable: %v", err)
	}
	if got.Status != StatusDisabled {
		t.Errorf("status = %q, want disabled", got.Status)
	}
	list := s.List()
	if len(list) != 1 || list[0].Status != StatusDisabled {
		t.Errorf("List = %+v, want one disabled member", list)
	}
	// Restore returns to the PRIOR status only (restore-to-prior-status): this
	// member was registered when disabled, so active — a seat-counted label
	// owned by Activate — is rejected, and registered is allowed.
	if _, err := s.Update(m.ID, nil, strptr(StatusActive)); !errors.As(err, new(ErrInvalidStatus)) {
		t.Errorf("restore registered->disabled->active = %v, want ErrInvalidStatus", err)
	}
	if _, err := s.Update(m.ID, nil, strptr(StatusRegistered)); err != nil {
		t.Errorf("restore disabled->registered failed: %v", err)
	}
}

func TestUpdateInvalidStatusRejected(t *testing.T) {
	s := NewStore()
	m, _ := s.Add("f@corp.com", "F")
	// Unknown enum value.
	if _, err := s.Update(m.ID, nil, strptr("bogus")); !errors.As(err, new(ErrInvalidStatus)) {
		t.Errorf("bogus status = %v, want ErrInvalidStatus", err)
	}
	// Forward lifecycle hops are not reachable via Update (PATCH): a fresh
	// member is "registered", and registered->active is not allowed here.
	if _, err := s.Update(m.ID, nil, strptr(StatusActive)); !errors.As(err, new(ErrInvalidStatus)) {
		t.Errorf("registered->active via Update = %v, want ErrInvalidStatus", err)
	}
	// MarkInvited performs registered->invited.
	if err := s.MarkInvited(m.ID); err != nil {
		t.Errorf("MarkInvited = %v", err)
	}
	// invited->active is still not reachable via Update — it is owned by Activate.
	if _, err := s.Update(m.ID, nil, strptr(StatusActive)); !errors.As(err, new(ErrInvalidStatus)) {
		t.Errorf("invited->active via Update = %v, want ErrInvalidStatus", err)
	}
	// Activate performs the invited->active transition.
	if err := s.Activate(m.ID); err != nil {
		t.Errorf("Activate = %v", err)
	}
	if got, _ := s.Get(m.ID); got.Status != StatusActive {
		t.Errorf("status after Activate = %q, want active", got.Status)
	}
}

func TestMarkInvitedIsIdempotentAndGated(t *testing.T) {
	s := NewStore()
	m, _ := s.Add("mi@corp.com", "MI")
	if got, _ := s.Get(m.ID); got.Status != StatusRegistered {
		t.Fatalf("fresh member status = %q, want registered", got.Status)
	}
	if err := s.MarkInvited(m.ID); err != nil {
		t.Fatalf("MarkInvited = %v", err)
	}
	if got, _ := s.Get(m.ID); got.Status != StatusInvited {
		t.Errorf("status after MarkInvited = %q, want invited", got.Status)
	}
	// Idempotent for an already-invited member.
	if err := s.MarkInvited(m.ID); err != nil {
		t.Errorf("MarkInvited (idempotent) = %v", err)
	}
	// An active member cannot be re-invited.
	if err := s.Activate(m.ID); err != nil {
		t.Fatalf("Activate = %v", err)
	}
	if err := s.MarkInvited(m.ID); !errors.As(err, new(ErrInvalidStatus)) {
		t.Errorf("MarkInvited(active) = %v, want ErrInvalidStatus", err)
	}
	// A missing id is ErrMemberNotFound.
	if err := s.MarkInvited("no-such-id"); !errors.As(err, new(ErrMemberNotFound)) {
		t.Errorf("MarkInvited(missing) = %v, want ErrMemberNotFound", err)
	}
}

func TestRemoveHardDeletesAndFreesEmail(t *testing.T) {
	database := newTestDB(t)
	s := newDBStore(t, database)
	m, _ := s.Add("rm@corp.com", "RM")
	if err := s.Remove(m.ID); err != nil {
		t.Fatalf("Remove = %v", err)
	}
	if _, err := s.Get(m.ID); !errors.As(err, new(ErrMemberNotFound)) {
		t.Errorf("Get after Remove = %v, want ErrMemberNotFound", err)
	}
	// Hard delete all the way down: the database row is gone too, not merely
	// hidden from the cache.
	var n int
	if err := database.QueryRow(`SELECT COUNT(*) FROM members WHERE id = ?`, m.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("removed member still has %d database row(s), want 0", n)
	}
	// The email is freed (hard delete, not soft): it may be reused.
	if _, err := s.Add("rm@corp.com", "RM2"); err != nil {
		t.Errorf("re-add freed email = %v, want nil", err)
	}
	if err := s.Remove("no-such-id"); !errors.As(err, new(ErrMemberNotFound)) {
		t.Errorf("Remove(missing) = %v, want ErrMemberNotFound", err)
	}
}

// TestPersistAndReloadRoundtrip: every mutation is committed to the store
// database before it returns, so a restart — a second store built on the
// same database path — sees the exact state, timestamps byte-exact.
func TestPersistAndReloadRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runeconsole.db")

	d1 := openTestDB(t, path)
	s := newDBStore(t, d1)
	m1, err := s.Add("g@corp.com", "G")
	if err != nil {
		t.Fatal(err)
	}
	m2, err := s.Add("h@corp.com", "H")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(m2.ID, nil, strptr(StatusDisabled)); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSessionExpired(m1.ID); err != nil {
		t.Fatal(err)
	}
	stamped, err := s.Get(m1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := d1.Close(); err != nil {
		t.Fatal(err)
	}

	// Restart: reopen the same path, load a fresh store from it.
	s2 := newDBStore(t, openTestDB(t, path))
	got1, err := s2.Get(m1.ID)
	if err != nil || got1.Email != "g@corp.com" || got1.Status != StatusRegistered {
		t.Errorf("reloaded m1 = (%+v, %v)", got1, err)
	}
	if got1.CreatedAt != m1.CreatedAt {
		t.Errorf("created_at not preserved: %q -> %q", m1.CreatedAt, got1.CreatedAt)
	}
	if got1.SessionExpiredAt != stamped.SessionExpiredAt {
		t.Errorf("session_expired_at not preserved: %q -> %q", stamped.SessionExpiredAt, got1.SessionExpiredAt)
	}
	// The empty marker round-trips through NULL back to "".
	if got1.DisabledFrom != "" {
		t.Errorf("DisabledFrom of never-disabled member = %q, want empty", got1.DisabledFrom)
	}
	got2, err := s2.Get(m2.ID)
	if err != nil || got2.Status != StatusDisabled || got2.DisabledFrom != StatusRegistered {
		t.Errorf("reloaded m2 = (%+v, %v), want disabled with DisabledFrom=registered", got2, err)
	}
}

// TestLoadFromDBFreshIsEmpty: a brand-new database (schema installed, no
// rows) loads an empty registry — the fresh-console boot state.
func TestLoadFromDBFreshIsEmpty(t *testing.T) {
	s := newDBStore(t, newTestDB(t))
	if len(s.List()) != 0 {
		t.Errorf("store should be empty, got %+v", s.List())
	}
}

// TestMutatorSQLFailureLeavesMapsUnchanged pins the write-through discipline:
// when the transaction cannot commit, the mutator returns the error and the
// in-memory cache is untouched — the cache never gets ahead of the database.
func TestMutatorSQLFailureLeavesMapsUnchanged(t *testing.T) {
	database := newTestDB(t)
	s := newDBStore(t, database)
	m, err := s.Add("ok@corp.com", "OK")
	if err != nil {
		t.Fatal(err)
	}
	// Kill the sink: every transaction from here on fails.
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Add("new@corp.com", "New"); err == nil {
		t.Fatal("Add with a dead sink succeeded")
	}
	if _, err := s.GetByEmail("new@corp.com"); !errors.As(err, new(ErrMemberNotFound)) {
		t.Errorf("failed Add leaked into the cache: %v", err)
	}
	if _, err := s.Update(m.ID, strptr("Renamed"), strptr(StatusDisabled)); err == nil {
		t.Fatal("Update with a dead sink succeeded")
	}
	got, gerr := s.Get(m.ID)
	if gerr != nil || got.DisplayName != "OK" || got.Status != StatusRegistered || got.DisabledFrom != "" {
		t.Errorf("failed Update mutated the cache: (%+v, %v)", got, gerr)
	}
	if err := s.MarkInvited(m.ID); err == nil {
		t.Fatal("MarkInvited with a dead sink succeeded")
	}
	if got, _ := s.Get(m.ID); got.Status != StatusRegistered {
		t.Errorf("failed MarkInvited mutated the cache: status = %q", got.Status)
	}
	if err := s.Remove(m.ID); err == nil {
		t.Fatal("Remove with a dead sink succeeded")
	}
	if _, err := s.Get(m.ID); err != nil {
		t.Errorf("failed Remove evicted the cache row: %v", err)
	}
}

// TestEmailImmutableAtDBLayer: the store API cannot express an email change
// (Update has no email parameter) and the members_email_immutable trigger
// backs that at the database layer; the store's own UPDATE never lists the
// email column, so legitimate mutations pass the armed trigger.
func TestEmailImmutableAtDBLayer(t *testing.T) {
	database := newTestDB(t)
	s := newDBStore(t, database)
	m, err := s.Add("fixed@corp.com", "F")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE members SET email = 'moved@corp.com' WHERE id = ?`, m.ID); err == nil {
		t.Error("direct email UPDATE accepted, want trigger abort")
	}
	if _, err := s.Update(m.ID, strptr("Renamed"), strptr(StatusDisabled)); err != nil {
		t.Errorf("store mutation alongside the armed trigger: %v", err)
	}
	var email string
	if err := database.QueryRow(`SELECT email FROM members WHERE id = ?`, m.ID).Scan(&email); err != nil {
		t.Fatal(err)
	}
	if email != "fixed@corp.com" {
		t.Errorf("email = %q, want fixed@corp.com", email)
	}
}

// TestValidateIDAcceptsCanonicalUUIDOnly covers the exported member-id format
// contract on its own, with no store attached — the shape it runs in on the
// live paths (the daemon injects it as the groups person-key validator at
// boot, and the gRPC layer calls it to tell a member id from a token email).
// The schema CHECK on members.id only pins length and lower-case, so the full
// 8-4-4-4-12 hex shape is enforced here or nowhere.
func TestValidateIDAcceptsCanonicalUUIDOnly(t *testing.T) {
	// A freshly minted id is by definition the accepted shape.
	good, err := newMemberID()
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateID(good); err != nil {
		t.Errorf("ValidateID(%q) = %v, want nil", good, err)
	}
	bad := []string{
		"alice@corp.com",                        // an email is not a member id
		"not-a-uuid",                            // free-form text
		"",                                      // empty
		"11111111-1111-4111-8111-11111111111",   // 35 chars
		"11111111-1111-4111-8111-1111111111111", // 37 chars
		"111111111-111-4111-8111-111111111111",  // right length, dashes misplaced
		"11111111-1111-4111-8111-11111111111g",  // non-hex digit
		"AAAAAAAA-1111-4111-8111-111111111111",  // upper-case hex
	}
	for _, id := range bad {
		err := ValidateID(id)
		if err == nil {
			t.Errorf("ValidateID(%q) = nil, want an error", id)
			continue
		}
		if !strings.Contains(err.Error(), "member id") {
			t.Errorf("ValidateID(%q) err = %v, want a member id error", id, err)
		}
	}
}
