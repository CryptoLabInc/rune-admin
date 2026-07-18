package groups

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
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
// shape: NewStore + LoadFromDB, default person-key contract).
func newDBStore(t *testing.T, database *sql.DB) *Store {
	t.Helper()
	s := NewStore()
	if err := s.LoadFromDB(database); err != nil {
		t.Fatalf("LoadFromDB: %v", err)
	}
	return s
}

func TestCreateGroupBasics(t *testing.T) {
	s := NewStore()
	g, err := s.CreateGroup("hq", "")
	if err != nil {
		t.Fatal(err)
	}
	if g.ID == "" || g.CreatedAt == "" {
		t.Errorf("group missing id/created_at: %+v", g)
	}
	if len(g.ID) != 36 || strings.Count(g.ID, "-") != 4 {
		t.Errorf("id %q is not a canonical UUID", g.ID)
	}
	if _, err := s.CreateGroup("hq", ""); !errors.As(err, new(ErrDuplicateName)) {
		t.Errorf("duplicate name = %v, want ErrDuplicateName", err)
	}
	if _, err := s.CreateGroup("", ""); err == nil {
		t.Error("empty name should be rejected")
	}
	if _, err := s.CreateGroup("a/b", ""); err == nil {
		t.Error("name with '/' should be rejected")
	}
	if _, err := s.CreateGroup("orphan", "no-such-parent"); !errors.As(err, new(ErrGroupNotFound)) {
		t.Error("unknown parent should be ErrGroupNotFound")
	}
	if byID, err := s.ResolveGroup(g.ID); err != nil || byID.ID != g.ID {
		t.Errorf("ResolveGroup(id) = (%+v, %v)", byID, err)
	}
	if byName, err := s.ResolveGroup("hq"); err != nil || byName.ID != g.ID {
		t.Errorf("ResolveGroup(name) = (%+v, %v)", byName, err)
	}
}

// TestValidateGroupName pins the team-name rule to the console client's
// TEAM_NAME_PATTERN: digits, Latin letters, Hangul, '-' and '_' only, ≤50 runes.
// Spaces, dots, parens, slashes, and other punctuation are rejected.
func TestValidateGroupName(t *testing.T) {
	valid := []string{"eng", "C-Level", "AX-Sub", "dev_team", "본부", "팀123", strings.Repeat("a", 50)}
	for _, n := range valid {
		if err := validateGroupName(n); err != nil {
			t.Errorf("validateGroupName(%q) = %v, want nil", n, err)
		}
	}
	invalid := []string{"", "   ", "Data Science", "Team.A", "a/b", "team(1)", " eng", strings.Repeat("a", 51)}
	for _, n := range invalid {
		if err := validateGroupName(n); err == nil {
			t.Errorf("validateGroupName(%q) = nil, want error", n)
		}
	}
}

func TestCreateGroupDepthCap(t *testing.T) {
	s := NewStore()
	parent := ""
	for i := 1; i <= MaxTreeDepth; i++ {
		g, err := s.CreateGroup(fmt.Sprintf("g%d", i), parent)
		if err != nil {
			t.Fatalf("create depth %d: %v", i, err)
		}
		parent = g.ID
	}
	if _, err := s.CreateGroup("too-deep", parent); err == nil {
		t.Errorf("creating at depth %d should be rejected", MaxTreeDepth+1)
	}
}

func TestRenameKeepsImmutableID(t *testing.T) {
	s := NewStore()
	g, _ := s.CreateGroup("old-name", "")
	if _, err := s.CreateGroup("taken", ""); err != nil {
		t.Fatal(err)
	}
	renamed, err := s.RenameGroup("old-name", "new-name")
	if err != nil {
		t.Fatal(err)
	}
	if renamed.ID != g.ID {
		t.Errorf("rename changed ID: %s -> %s", g.ID, renamed.ID)
	}
	if _, err := s.ResolveGroup("old-name"); err == nil {
		t.Error("old name still resolves")
	}
	if got, err := s.ResolveGroup("new-name"); err != nil || got.ID != g.ID {
		t.Errorf("new name resolve = (%+v, %v)", got, err)
	}
	if _, err := s.RenameGroup("new-name", "taken"); !errors.As(err, new(ErrDuplicateName)) {
		t.Errorf("rename to taken name = %v, want ErrDuplicateName", err)
	}
	if _, err := s.RenameGroup("new-name", "new-name"); err != nil {
		t.Errorf("rename to same name = %v, want nil", err)
	}
}

// TestSiblingScopedNameUniqueness pins the design-doc rule (plan §6 — "형제
// 팀 내 동일 이름"): team names are unique only among siblings, not globally.
func TestSiblingScopedNameUniqueness(t *testing.T) {
	s := NewStore()
	kr, err := s.CreateGroup("kr", "")
	if err != nil {
		t.Fatal(err)
	}
	us, err := s.CreateGroup("us", "")
	if err != nil {
		t.Fatal(err)
	}

	// Same name under two different parents is allowed.
	krPay, err := s.CreateGroup("payments", kr.ID)
	if err != nil {
		t.Fatalf("payments under kr: %v", err)
	}
	usPay, err := s.CreateGroup("payments", us.ID)
	if err != nil {
		t.Fatalf("payments under a different parent should be allowed: %v", err)
	}
	if krPay.ID == usPay.ID {
		t.Fatal("distinct groups share an ID")
	}

	// A second "payments" under the SAME parent (a real sibling) is rejected.
	if _, err := s.CreateGroup("payments", kr.ID); !errors.As(err, new(ErrDuplicateName)) {
		t.Errorf("sibling duplicate = %v, want ErrDuplicateName", err)
	}
	// Root groups are siblings of one another.
	if _, err := s.CreateGroup("kr", ""); !errors.As(err, new(ErrDuplicateName)) {
		t.Errorf("root duplicate = %v, want ErrDuplicateName", err)
	}

	// A bare name shared by two groups is ambiguous to resolve; the ID is exact.
	if _, err := s.ResolveGroup("payments"); !errors.As(err, new(ErrAmbiguousName)) {
		t.Errorf("ResolveGroup(ambiguous) = %v, want ErrAmbiguousName", err)
	}
	if g, err := s.ResolveGroup(krPay.ID); err != nil || g.ID != krPay.ID {
		t.Errorf("ResolveGroup(id) = (%+v, %v)", g, err)
	}

	// Rename honours the same scope: to a sibling's name → rejected; to a name
	// only used under a different parent → allowed.
	if _, err := s.CreateGroup("billing", us.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RenameGroup(usPay.ID, "billing"); !errors.As(err, new(ErrDuplicateName)) {
		t.Errorf("rename to sibling name = %v, want ErrDuplicateName", err)
	}
	if _, err := s.RenameGroup(usPay.ID, "kr"); err != nil {
		t.Errorf("rename to a name used only under a different parent should be allowed: %v", err)
	}

	// With the duplicate renamed away, the bare name resolves unambiguously again.
	if g, err := s.ResolveGroup("payments"); err != nil || g.ID != krPay.ID {
		t.Errorf("ResolveGroup(payments) after de-dup = (%+v, %v), want kr/payments", g, err)
	}
}

// A re-grant is a role change, not a new join: the original GrantedAt (the
// console's joinedAt) must survive, while role and grantedBy update.
func TestGrantPreservesJoinTimeOnRoleChange(t *testing.T) {
	s := NewStore()
	g, err := s.CreateGroup("hq", "")
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return t0 }
	if _, err := s.Grant("a@corp.com", g.ID, RoleRead, "actor-1"); err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return t0.Add(48 * time.Hour) }
	m, err := s.Grant("a@corp.com", g.ID, RoleEdit, "actor-2")
	if err != nil {
		t.Fatal(err)
	}
	if m.GrantedAt != storedb.FormatTime(t0) {
		t.Errorf("GrantedAt after role change = %s, want the original %s", m.GrantedAt, storedb.FormatTime(t0))
	}
	if m.Role != RoleEdit || m.GrantedBy != "actor-2" {
		t.Errorf("role/grantedBy after re-grant = %s/%s, want edit/actor-2", m.Role, m.GrantedBy)
	}
}

func TestGrantRevokeAndAuditFields(t *testing.T) {
	s := NewStore()
	g, _ := s.CreateGroup("hq", "")

	m, err := s.Grant("alice@corp.com", g.ID, RoleWrite, "local-admin:heeyeon")
	if err != nil {
		t.Fatal(err)
	}
	if m.GrantedBy != "local-admin:heeyeon" || m.GrantedAt == "" {
		t.Errorf("audit fields missing: %+v", m)
	}

	// Re-grant replaces the role (upsert, still one membership row).
	if _, err := s.Grant("alice@corp.com", g.ID, RoleEdit, "local-admin:heeyeon"); err != nil {
		t.Fatal(err)
	}
	all := s.ListMemberships()
	if len(all) != 1 || all[0].Role != RoleEdit {
		t.Errorf("memberships after re-grant = %+v", all)
	}

	if _, err := s.Grant("alice@corp.com", g.ID, Role("boss"), "x"); err == nil {
		t.Error("invalid role should be rejected")
	}
	if _, err := s.Grant("not-an-email", g.ID, RoleRead, "x"); err == nil {
		t.Error("non-email user should be rejected (key is an email)")
	}
	if _, err := s.Grant("bob@corp.com", "no-group", RoleRead, "x"); !errors.As(err, new(ErrGroupNotFound)) {
		t.Error("grant on unknown group should be ErrGroupNotFound")
	}

	ok, err := s.RevokeDirectGrant("alice@corp.com", g.ID)
	if err != nil || !ok {
		t.Fatalf("Revoke = (%v, %v)", ok, err)
	}
	ok, err = s.RevokeDirectGrant("alice@corp.com", g.ID)
	if err != nil || ok {
		t.Errorf("second Revoke = (%v, %v), want (false, nil)", ok, err)
	}
}

func TestRemoveUser(t *testing.T) {
	s := NewStore()
	a, _ := s.CreateGroup("a", "")
	b, _ := s.CreateGroup("b", "")
	if _, err := s.Grant("u@corp.com", a.ID, RoleRead, "x"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Grant("u@corp.com", b.ID, RoleWrite, "x"); err != nil {
		t.Fatal(err)
	}
	if n, err := s.RemoveUser("u@corp.com"); err != nil || n != 2 {
		t.Errorf("RemoveUser = (%d, %v), want (2, nil)", n, err)
	}
	if n, err := s.RemoveUser("u@corp.com"); err != nil || n != 0 {
		t.Errorf("RemoveUser again = (%d, %v), want (0, nil)", n, err)
	}
	if got := s.ListMemberships(); len(got) != 0 {
		t.Errorf("memberships after RemoveUser = %+v", got)
	}
}

func TestListGroupsTreeOrderAndDepth(t *testing.T) {
	s, hq, dev, search := testTree(t)
	infos := s.ListGroups()
	if len(infos) != 3 {
		t.Fatalf("ListGroups len = %d", len(infos))
	}
	want := []struct {
		id    string
		depth int
	}{{hq.ID, 1}, {dev.ID, 2}, {search.ID, 3}}
	for i, w := range want {
		if infos[i].ID != w.id || infos[i].Depth != w.depth {
			t.Errorf("ListGroups[%d] = %+v, want id=%s depth=%d", i, infos[i], w.id, w.depth)
		}
	}
}

// TestPersistAndReloadRoundtrip: every mutation is committed to the store
// database before it returns, so a restart — a second store built on the
// same database path — sees the exact state, timestamps byte-exact.
func TestPersistAndReloadRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runeconsole.db")

	d1 := openTestDB(t, path)
	s := newDBStore(t, d1)
	hq, err := s.CreateGroup("hq", "")
	if err != nil {
		t.Fatal(err)
	}
	dev, err := s.CreateGroup("dev-team", hq.ID)
	if err != nil {
		t.Fatal(err)
	}
	granted, err := s.Grant("jisoo@corp.com", hq.ID, RoleWrite, "local-admin:heeyeon")
	if err != nil {
		t.Fatal(err)
	}
	if err := d1.Close(); err != nil {
		t.Fatal(err)
	}

	// Restart: reopen the same path, load a fresh store from it.
	s2 := newDBStore(t, openTestDB(t, path))
	g, err := s2.ResolveGroup("dev-team")
	if err != nil || g.ID != dev.ID || g.ParentID != hq.ID {
		t.Errorf("reloaded dev-team = (%+v, %v)", g, err)
	}
	if g.CreatedAt != dev.CreatedAt {
		t.Errorf("created_at not preserved: %q -> %q", dev.CreatedAt, g.CreatedAt)
	}
	ms := s2.ListMemberships()
	if len(ms) != 1 || ms[0].User != "jisoo@corp.com" || ms[0].Role != RoleWrite || ms[0].GrantedBy != "local-admin:heeyeon" {
		t.Errorf("reloaded memberships = %+v", ms)
	}
	if len(ms) == 1 && ms[0].GrantedAt != granted.GrantedAt {
		t.Errorf("granted_at not preserved: %q -> %q", granted.GrantedAt, ms[0].GrantedAt)
	}
	if r, ok := s2.EffectiveRole("jisoo@corp.com", dev.ID); !ok || r != RoleWrite {
		t.Errorf("EffectiveRole after reload = (%q, %v)", r, ok)
	}
}

// TestLoadFromDBFreshIsEmpty: a brand-new database (schema installed, no
// rows) loads an empty store — the fresh-console boot state.
func TestLoadFromDBFreshIsEmpty(t *testing.T) {
	s := newDBStore(t, newTestDB(t))
	if got := s.ListGroups(); len(got) != 0 {
		t.Errorf("groups = %+v, want empty", got)
	}
	if got := s.ListMemberships(); len(got) != 0 {
		t.Errorf("memberships = %+v, want empty", got)
	}
}

// TestLoadFromDBRejectsBadTrees — tree-shape validation stays in Go (the
// schema has no self-FK on parent_id): a cyclic, over-deep, or unknown-parent
// row set in the database fails the load, and with it daemon startup.
func TestLoadFromDBRejectsBadTrees(t *testing.T) {
	const ts = "2026-07-06T00:00:00Z"

	t.Run("cycle", func(t *testing.T) {
		database := newTestDB(t)
		if _, err := database.Exec(
			`INSERT INTO groups (id, name, parent_id, created_at) VALUES
			 ('aaaa', 'a', 'bbbb', ?), ('bbbb', 'b', 'aaaa', ?)`, ts, ts); err != nil {
			t.Fatal(err)
		}
		if err := NewStore().LoadFromDB(database); !errors.As(err, new(ErrCycle)) {
			t.Errorf("cyclic load = %v, want ErrCycle (startup must fail)", err)
		}
	})

	t.Run("over-deep chain", func(t *testing.T) {
		database := newTestDB(t)
		for i := 1; i <= MaxTreeDepth+1; i++ {
			parent := ""
			if i > 1 {
				parent = fmt.Sprintf("id%d", i-1)
			}
			if _, err := database.Exec(
				`INSERT INTO groups (id, name, parent_id, created_at) VALUES (?, ?, ?, ?)`,
				fmt.Sprintf("id%d", i), fmt.Sprintf("g%d", i), parent, ts); err != nil {
				t.Fatal(err)
			}
		}
		if err := NewStore().LoadFromDB(database); !errors.As(err, new(ErrCycle)) {
			t.Errorf("over-deep load = %v, want ErrCycle", err)
		}
	})

	t.Run("unknown parent", func(t *testing.T) {
		database := newTestDB(t)
		if _, err := database.Exec(
			`INSERT INTO groups (id, name, parent_id, created_at) VALUES ('aaaa', 'a', 'ghost', ?)`, ts); err != nil {
			t.Fatal(err)
		}
		if err := NewStore().LoadFromDB(database); err == nil || !strings.Contains(err.Error(), "unknown parent") {
			t.Errorf("unknown-parent load = %v, want unknown-parent error", err)
		}
	})
}

// TestLoadFromDBUsesPersonKeyValidator — the database load routes every
// membership row through the ACTIVE person-key validator: a row the validator
// refuses fails the boot.
func TestLoadFromDBUsesPersonKeyValidator(t *testing.T) {
	const ts = "2026-07-06T00:00:00Z"
	seed := func(t *testing.T, user string) *sql.DB {
		t.Helper()
		database := newTestDB(t)
		if _, err := database.Exec(
			`INSERT INTO groups (id, name, parent_id, created_at) VALUES ('aaaa', 'a', '', ?)`, ts); err != nil {
			t.Fatal(err)
		}
		if _, err := database.Exec(
			`INSERT INTO memberships (user, group_id, role, granted_by, granted_at) VALUES (?, 'aaaa', 'read', 'x', ?)`,
			user, ts); err != nil {
			t.Fatal(err)
		}
		return database
	}

	// Default (email) contract: refuses the member-key row, loads the email row.
	if err := NewStore().LoadFromDB(seed(t, "member-42")); err == nil {
		t.Error("default validator should refuse a member-key row (load must fail)")
	}
	if err := NewStore().LoadFromDB(seed(t, "alice@corp.com")); err != nil {
		t.Errorf("default validator should load the email row: %v", err)
	}

	// Injected contract: mirror image.
	s := NewStore()
	s.SetPersonKeyValidator(fakeMemberKeyValidator)
	if err := s.LoadFromDB(seed(t, "member-42")); err != nil {
		t.Fatalf("injected validator should load the member-key row: %v", err)
	}
	s2 := NewStore()
	s2.SetPersonKeyValidator(fakeMemberKeyValidator)
	if err := s2.LoadFromDB(seed(t, "alice@corp.com")); err == nil {
		t.Error("injected validator should refuse an email row (load must fail)")
	}
}

// TestForeignKeyGuardsMembershipsAtDBLayer — the ONE cross-table foreign key
// (memberships.group_id -> groups.id ON DELETE RESTRICT) makes the legacy
// dangling-membership crash class unrepresentable: a direct SQL DELETE of a
// group that still has memberships aborts, and a direct SQL INSERT of a
// membership without its group aborts. The Go-level ErrHasMembers guard in
// DeleteGroup stays the first line; this is the schema backstop.
func TestForeignKeyGuardsMembershipsAtDBLayer(t *testing.T) {
	database := newTestDB(t)
	s := newDBStore(t, database)
	g, err := s.CreateGroup("hq", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Grant("alice@corp.com", g.ID, RoleRead, "x"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`DELETE FROM groups WHERE id = ?`, g.ID); err == nil {
		t.Error("direct DELETE of a group with memberships accepted, want FK RESTRICT abort")
	}
	if _, err := database.Exec(
		`INSERT INTO memberships (user, group_id, role, granted_by, granted_at)
		 VALUES ('bob@corp.com', 'no-such-group', 'read', 'x', '2026-07-06T00:00:00Z')`); err == nil {
		t.Error("direct INSERT of a dangling membership accepted, want FK abort")
	}
	// The row set is intact after both refusals.
	var n int
	if err := database.QueryRow(`SELECT COUNT(*) FROM memberships`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("memberships rows = %d, want 1", n)
	}
}

// TestLoadFromDBRejectsDanglingMembership — should a dangling membership row
// exist anyway (only reachable by editing the database externally with
// foreign keys off), the load fails closed instead of silently dropping it:
// unlike the legacy YAML pair, a SQLite file is repairable in place.
func TestLoadFromDBRejectsDanglingMembership(t *testing.T) {
	database := newTestDB(t)
	if _, err := database.Exec(`PRAGMA foreign_keys=OFF`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(
		`INSERT INTO memberships (user, group_id, role, granted_by, granted_at)
		 VALUES ('dangling@corp.com', 'deleted-group', 'edit', 'x', '2026-07-06T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		t.Fatal(err)
	}
	if err := NewStore().LoadFromDB(database); err == nil || !strings.Contains(err.Error(), "missing group") {
		t.Errorf("dangling-membership load = %v, want missing-group refusal", err)
	}
}

// TestLoadFromDBRejectsInvalidRole — the load refuses a membership row whose
// role is outside read/write/edit. The CHECK constraint makes such a row
// unrepresentable through this binary, so the seed suspends check constraints
// the way TestLoadFromDBRejectsDanglingMembership suspends foreign keys: the
// Go-side guard is what stands between an externally edited database and a
// membership carrying a role no judge path knows how to rank.
func TestLoadFromDBRejectsInvalidRole(t *testing.T) {
	const ts = "2026-07-06T00:00:00Z"
	database := newTestDB(t)
	if _, err := database.Exec(
		`INSERT INTO groups (id, name, parent_id, created_at) VALUES ('aaaa', 'a', '', ?)`, ts); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`PRAGMA ignore_check_constraints=ON`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(
		`INSERT INTO memberships (user, group_id, role, granted_by, granted_at)
		 VALUES ('u@corp.com', 'aaaa', 'superuser', 'x', ?)`, ts); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`PRAGMA ignore_check_constraints=OFF`); err != nil {
		t.Fatal(err)
	}
	if err := NewStore().LoadFromDB(database); err == nil || !strings.Contains(err.Error(), "invalid role") {
		t.Errorf("invalid-role load = %v, want invalid-role refusal", err)
	}
}

// TestGrantedAtImmutableAtDBLayer — the memberships_granted_at_immutable
// trigger is the schema twin of the c362afb invariant: a direct SQL UPDATE
// of granted_at aborts, and a role-change re-grant through the store leaves
// the stored granted_at byte-identical, surviving a reopen.
func TestGrantedAtImmutableAtDBLayer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runeconsole.db")
	d1 := openTestDB(t, path)
	s := newDBStore(t, d1)
	g, err := s.CreateGroup("hq", "")
	if err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return t0 }
	if _, err := s.Grant("a@corp.com", g.ID, RoleRead, "actor-1"); err != nil {
		t.Fatal(err)
	}
	want := storedb.FormatTime(t0)

	if _, err := d1.Exec(
		`UPDATE memberships SET granted_at = '2027-01-01T00:00:00Z' WHERE user = 'a@corp.com' AND group_id = ?`,
		g.ID); err == nil {
		t.Error("direct granted_at UPDATE accepted, want trigger abort")
	}

	s.now = func() time.Time { return t0.Add(48 * time.Hour) }
	if _, err := s.Grant("a@corp.com", g.ID, RoleEdit, "actor-2"); err != nil {
		t.Fatal(err)
	}
	var got string
	if err := d1.QueryRow(
		`SELECT granted_at FROM memberships WHERE user = 'a@corp.com' AND group_id = ?`, g.ID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("granted_at after role change = %q, want the original %q", got, want)
	}
	if err := d1.Close(); err != nil {
		t.Fatal(err)
	}

	s2 := newDBStore(t, openTestDB(t, path))
	ms := s2.ListMemberships()
	if len(ms) != 1 || ms[0].GrantedAt != want || ms[0].Role != RoleEdit || ms[0].GrantedBy != "actor-2" {
		t.Errorf("reloaded membership = %+v, want granted_at %q with edit/actor-2", ms, want)
	}
}

// TestNonUUIDGroupIDRoundTripsByteVerbatim — group ids are the opaque FHE
// record tags: non-UUID ids are legal (the schema only requires non-empty)
// and must round-trip through the database byte-verbatim.
func TestNonUUIDGroupIDRoundTripsByteVerbatim(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runeconsole.db")
	d1 := openTestDB(t, path)
	const ts = "2026-07-06T00:00:00Z"
	if _, err := d1.Exec(
		`INSERT INTO groups (id, name, parent_id, created_at) VALUES ('aaaa', 'legacy', '', ?)`, ts); err != nil {
		t.Fatal(err)
	}
	s := newDBStore(t, d1)
	g, err := s.ResolveGroup("aaaa")
	if err != nil || g.ID != "aaaa" {
		t.Fatalf("ResolveGroup(aaaa) = (%+v, %v)", g, err)
	}
	if _, err := s.Grant("alice@corp.com", "aaaa", RoleRead, "x"); err != nil {
		t.Fatal(err)
	}
	if err := d1.Close(); err != nil {
		t.Fatal(err)
	}

	s2 := newDBStore(t, openTestDB(t, path))
	g2, err := s2.ResolveGroup("aaaa")
	if err != nil || g2.ID != "aaaa" || g2.CreatedAt != ts {
		t.Errorf("reloaded group = (%+v, %v), want id 'aaaa' byte-verbatim", g2, err)
	}
	ms := s2.ListMemberships()
	if len(ms) != 1 || ms[0].GroupID != "aaaa" {
		t.Errorf("reloaded memberships = %+v, want one on 'aaaa'", ms)
	}
}

// TestSiblingUniquenessBackedBySQL — the UNIQUE(parent_id, name) constraint
// (with the ” root sentinel making roots siblings of one another) is the
// schema twin of nameTakenBySiblingLocked.
func TestSiblingUniquenessBackedBySQL(t *testing.T) {
	database := newTestDB(t)
	const ts = "2026-07-06T00:00:00Z"
	if _, err := database.Exec(
		`INSERT INTO groups (id, name, parent_id, created_at) VALUES ('r1', 'kr', '', ?)`, ts); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(
		`INSERT INTO groups (id, name, parent_id, created_at) VALUES ('r2', 'kr', '', ?)`, ts); err == nil {
		t.Error("duplicate root-sibling name accepted, want UNIQUE violation")
	}
	if _, err := database.Exec(
		`INSERT INTO groups (id, name, parent_id, created_at) VALUES ('r3', 'kr', 'r1', ?)`, ts); err != nil {
		t.Errorf("same name under a different parent should be allowed: %v", err)
	}
}

// TestMutatorSQLFailureLeavesMapsUnchanged pins the write-through discipline:
// when the transaction cannot commit, the mutator returns the error and the
// in-memory cache is untouched — the cache never gets ahead of the database.
func TestMutatorSQLFailureLeavesMapsUnchanged(t *testing.T) {
	database := newTestDB(t)
	s := newDBStore(t, database)
	hq, err := s.CreateGroup("hq", "")
	if err != nil {
		t.Fatal(err)
	}
	empty, err := s.CreateGroup("empty", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Grant("alice@corp.com", hq.ID, RoleRead, "x"); err != nil {
		t.Fatal(err)
	}
	// Kill the sink: every transaction from here on fails.
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := s.CreateGroup("new", ""); err == nil {
		t.Fatal("CreateGroup with a dead sink succeeded")
	}
	if _, err := s.ResolveGroup("new"); err == nil {
		t.Error("failed CreateGroup leaked into the cache")
	}
	if _, err := s.RenameGroup(hq.ID, "renamed"); err == nil {
		t.Fatal("RenameGroup with a dead sink succeeded")
	}
	if g, err := s.ResolveGroup("hq"); err != nil || g.ID != hq.ID {
		t.Errorf("failed RenameGroup mutated the cache: (%+v, %v)", g, err)
	}
	if _, err := s.Grant("bob@corp.com", hq.ID, RoleRead, "x"); err == nil {
		t.Fatal("Grant with a dead sink succeeded")
	}
	if _, err := s.Grant("alice@corp.com", hq.ID, RoleEdit, "x"); err == nil {
		t.Fatal("re-Grant with a dead sink succeeded")
	}
	ms := s.ListMemberships()
	if len(ms) != 1 || ms[0].User != "alice@corp.com" || ms[0].Role != RoleRead {
		t.Errorf("failed Grants mutated the cache: %+v", ms)
	}
	if _, err := s.RevokeDirectGrant("alice@corp.com", hq.ID); err == nil {
		t.Fatal("Revoke with a dead sink succeeded")
	}
	if _, err := s.RemoveUser("alice@corp.com"); err == nil {
		t.Fatal("RemoveUser with a dead sink succeeded")
	}
	if got := s.ListMemberships(); len(got) != 1 {
		t.Errorf("failed Revoke/RemoveUser evicted the cache row: %+v", got)
	}
	if _, err := s.DeleteGroup(empty.ID, &stubStats{m: map[string]TagStat{}}); err == nil {
		t.Fatal("DeleteGroup with a dead sink succeeded")
	}
	if _, err := s.ResolveGroup(empty.ID); err != nil {
		t.Errorf("failed DeleteGroup evicted the group: %v", err)
	}
	if _, _, err := s.DeleteGroupWithMembers(hq.ID); err == nil {
		t.Fatal("DeleteGroupWithMembers with a dead sink succeeded")
	}
	if _, err := s.ResolveGroup(hq.ID); err != nil {
		t.Errorf("failed DeleteGroupWithMembers evicted the group: %v", err)
	}
	if got := s.ListMemberships(); len(got) != 1 {
		t.Errorf("failed DeleteGroupWithMembers evicted memberships: %+v", got)
	}
}

func TestDeleteGroupUpdatesParentChildren(t *testing.T) {
	s, hq, dev, search := testTree(t)
	stats := &stubStats{m: map[string]TagStat{}}
	if _, err := s.DeleteGroup(search.ID, stats); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeleteGroup(dev.ID, stats); err != nil {
		t.Fatalf("delete dev after child removed: %v", err)
	}
	if _, err := s.DeleteGroup(hq.ID, stats); err != nil {
		t.Fatalf("delete hq after subtree removed: %v", err)
	}
	if got := s.ListGroups(); len(got) != 0 {
		t.Errorf("groups after full teardown = %+v", got)
	}
}

// fakeMemberKeyValidator stands in for the member-UUID validator that
// member deployments inject at boot. It is defined here on purpose —
// the groups package (and its tests) must not import members; the store
// only ever sees an opaque key plus a validator.
func fakeMemberKeyValidator(key string) error {
	if !strings.HasPrefix(key, "member-") {
		return fmt.Errorf("person key %q is not a member key", key)
	}
	return nil
}

// TestPersonKeyValidatorDefault pins the default contract in BOTH
// directions: an email passes and a member UUID fails, on Grant and on
// the CanGrant judge alike. Core deployments rely on this staying
// unchanged (the validator seam exists for the member-invite branch).
func TestPersonKeyValidatorDefault(t *testing.T) {
	const memberUUID = "6f1d0e3a-1b2c-4d5e-8f90-a1b2c3d4e5f6"
	s := NewStore()
	g, _ := s.CreateGroup("hq", "")
	if _, err := s.Grant("alice@corp.com", g.ID, RoleRead, "x"); err != nil {
		t.Errorf("default validator rejected an email: %v", err)
	}
	if _, err := s.Grant(memberUUID, g.ID, RoleRead, "x"); err == nil {
		t.Error("default validator accepted a member UUID (key must be an email)")
	}
	s.SetOrgAdmins("owner@corp.com")
	if err := s.CanGrant("owner@corp.com", "bob@corp.com", g.ID, RoleRead); err != nil {
		t.Errorf("CanGrant on an email target = %v, want nil", err)
	}
	if err := s.CanGrant("owner@corp.com", memberUUID, g.ID, RoleRead); err == nil {
		t.Error("CanGrant accepted a member UUID target under the default validator")
	}
}

// TestPersonKeyValidatorInjected — with an injected validator the store
// accepts exactly what the validator accepts and refuses what it
// refuses; SetPersonKeyValidator(nil) restores the email default.
func TestPersonKeyValidatorInjected(t *testing.T) {
	s := NewStore()
	s.SetPersonKeyValidator(fakeMemberKeyValidator)
	g, _ := s.CreateGroup("hq", "")
	if _, err := s.Grant("member-42", g.ID, RoleRead, "x"); err != nil {
		t.Errorf("injected validator rejected its own key: %v", err)
	}
	if _, err := s.Grant("alice@corp.com", g.ID, RoleRead, "x"); err == nil {
		t.Error("injected validator should refuse an email key")
	}
	s.SetOrgAdmins("owner@corp.com")
	if err := s.CanGrant("owner@corp.com", "member-42", g.ID, RoleRead); err != nil {
		t.Errorf("CanGrant with injected validator = %v, want nil", err)
	}
	if err := s.CanGrant("owner@corp.com", "bob@corp.com", g.ID, RoleRead); err == nil {
		t.Error("CanGrant should refuse an email target under the injected validator")
	}

	// nil restores the default contract, both directions again.
	s.SetPersonKeyValidator(nil)
	if _, err := s.Grant("alice@corp.com", g.ID, RoleWrite, "x"); err != nil {
		t.Errorf("default not restored — email rejected: %v", err)
	}
	if _, err := s.Grant("member-43", g.ID, RoleWrite, "x"); err == nil {
		t.Error("default not restored — member key still accepted")
	}
}

func TestParseRole(t *testing.T) {
	for _, good := range []string{"read", "write", "edit"} {
		if _, err := ParseRole(good); err != nil {
			t.Errorf("ParseRole(%s) = %v", good, err)
		}
	}
	// "admin" is no longer a group role (single org-wide Owner instead).
	for _, bad := range []string{"", "admin", "Admin", "member", "owner"} {
		if _, err := ParseRole(bad); err == nil {
			t.Errorf("ParseRole(%q) should fail", bad)
		}
	}
	if !RoleEdit.AtLeast(RoleRead) || RoleRead.AtLeast(RoleWrite) {
		t.Error("role ordering broken")
	}
	if Role("bogus").AtLeast(RoleRead) {
		t.Error("unknown role must never be AtLeast anything")
	}
}

// TestReadExclusionPersistsAcrossReload pins the durability of a removed
// inherited read: an exclusion that evaporated on restart would silently hand
// back memory read an admin took away, so the denial must survive a reopen of
// the same database path.
func TestReadExclusionPersistsAcrossReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runeconsole.db")
	s := newDBStore(t, openTestDB(t, path))
	hq, _ := s.CreateGroup("hq", "")
	dev, _ := s.CreateGroup("dev-team", hq.ID)
	sub, _ := s.CreateGroup("sub-part", dev.ID)
	if _, err := s.Grant("ceo@corp.com", hq.ID, RoleWrite, "local-admin:heeyeon"); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.ExcludeRead("ceo@corp.com", dev.ID, "local-admin:heeyeon"); !ok || err != nil {
		t.Fatalf("ExcludeRead = (%v, %v)", ok, err)
	}

	s2 := NewStore()
	if err := s2.LoadFromDB(openTestDB(t, path)); err != nil {
		t.Fatalf("reload: %v", err)
	}
	scope := s2.RecallScope("ceo@corp.com")
	want := []string{hq.ID, sub.ID}
	sort.Strings(want)
	if !reflect.DeepEqual(scope, want) {
		t.Errorf("reloaded RecallScope = %v, want %v (dev still excluded, sub-part still inherited)", scope, want)
	}
}

// TestRevokeIsOneTransaction pins the drawer remove-team
// semantics: dropping the direct grant and cutting the still-inherited read
// are ONE transaction — done as two, a crash between them hands the team
// back as inherited read with its memory recallable.
func TestRevokeIsOneTransaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runeconsole.db")
	database := openTestDB(t, path)
	s := newDBStore(t, database)
	hq, _ := s.CreateGroup("hq", "")
	dev, _ := s.CreateGroup("dev-team", hq.ID)
	if _, err := s.Grant("ceo@corp.com", hq.ID, RoleWrite, "local-admin:hy"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Grant("ceo@corp.com", dev.ID, RoleEdit, "local-admin:hy"); err != nil {
		t.Fatal(err)
	}

	// Direct grant on dev + inheritance from hq: one call must revoke AND exclude.
	revoked, excluded, err := s.Revoke("ceo@corp.com", dev.ID, "local-admin:hy")
	if err != nil || !revoked || !excluded {
		t.Fatalf("Revoke = (%v, %v, %v), want (true, true, nil)", revoked, excluded, err)
	}
	scope := s.RecallScope("ceo@corp.com")
	if len(scope) != 1 || scope[0] != hq.ID {
		t.Errorf("scope after removal = %v, want [%s] only", scope, hq.ID)
	}
	// Both rows changed durably: membership gone, exclusion present.
	var n int
	if err := database.QueryRow(`SELECT COUNT(*) FROM memberships WHERE user = 'ceo@corp.com' AND group_id = ?`, dev.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Error("membership row survived the combined removal")
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM read_exclusions WHERE user = 'ceo@corp.com' AND group_id = ?`, dev.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Error("exclusion row missing after the combined removal")
	}
	// Reopen: the pair survives restart together.
	s2 := NewStore()
	if err := s2.LoadFromDB(openTestDB(t, path)); err != nil {
		t.Fatal(err)
	}
	if scope := s2.RecallScope("ceo@corp.com"); len(scope) != 1 || scope[0] != hq.ID {
		t.Errorf("reloaded scope = %v, want [%s] only", scope, hq.ID)
	}
}

// TestRevokeVariants pins the no-op and partial cases: the
// combined call must match the sequential pair's decision table.
func TestRevokeVariants(t *testing.T) {
	s := newDBStore(t, newTestDB(t))
	hq, _ := s.CreateGroup("hq", "")
	dev, _ := s.CreateGroup("dev-team", hq.ID)
	solo, _ := s.CreateGroup("solo", "")

	// Inherited-only (member of hq, removing dev): exclusion without revoke.
	if _, err := s.Grant("a@corp.com", hq.ID, RoleWrite, "x"); err != nil {
		t.Fatal(err)
	}
	revoked, excluded, err := s.Revoke("a@corp.com", dev.ID, "x")
	if err != nil || revoked || !excluded {
		t.Fatalf("inherited-only = (%v, %v, %v), want (false, true, nil)", revoked, excluded, err)
	}
	// Already excluded: nothing left to remove.
	revoked, excluded, err = s.Revoke("a@corp.com", dev.ID, "x")
	if err != nil || revoked || excluded {
		t.Fatalf("already-excluded = (%v, %v, %v), want (false, false, nil)", revoked, excluded, err)
	}
	// Direct-only with no inheritance path: revoke without exclusion.
	if _, err := s.Grant("b@corp.com", solo.ID, RoleRead, "x"); err != nil {
		t.Fatal(err)
	}
	revoked, excluded, err = s.Revoke("b@corp.com", solo.ID, "x")
	if err != nil || !revoked || excluded {
		t.Fatalf("direct-only = (%v, %v, %v), want (true, false, nil)", revoked, excluded, err)
	}
	// No access at all.
	revoked, excluded, err = s.Revoke("b@corp.com", solo.ID, "x")
	if err != nil || revoked || excluded {
		t.Fatalf("no-access = (%v, %v, %v), want (false, false, nil)", revoked, excluded, err)
	}
	// Unknown team resolves to ErrGroupNotFound.
	if _, _, err := s.Revoke("b@corp.com", "no-such-team", "x"); err == nil {
		t.Fatal("unknown team did not error")
	}
}
