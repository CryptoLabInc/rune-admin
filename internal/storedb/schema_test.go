package storedb

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CryptoLabInc/rune-console/internal/db"
)

// openTestDB opens a fresh strict database under t.TempDir (never :memory: —
// the pool hands each connection its own private in-memory DB).
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := db.OpenStrict(filepath.Join(t.TempDir(), "runeconsole.db"))
	if err != nil {
		t.Fatalf("OpenStrict: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

// ensureTestSchema opens a test DB with schema v1 installed.
func ensureTestSchema(t *testing.T) *sql.DB {
	t.Helper()
	database := openTestDB(t)
	if err := EnsureSchema(database); err != nil {
		t.Fatal(err)
	}
	return database
}

// countRows returns SELECT COUNT(*) FROM table.
func countRows(t *testing.T, database *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := database.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func TestEnsureSchemaIdempotent(t *testing.T) {
	database := openTestDB(t)
	if err := EnsureSchema(database); err != nil {
		t.Fatalf("first EnsureSchema: %v", err)
	}
	if err := EnsureSchema(database); err != nil {
		t.Fatalf("second EnsureSchema: %v", err)
	}

	for _, table := range []string{
		"schema_migrations", "import_journal", "members", "invites",
		"roles", "tokens", "groups", "memberships",
	} {
		var name string
		err := database.QueryRow(
			`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&name)
		if err != nil {
			t.Errorf("table %s missing: %v", table, err)
		}
	}
	for _, trigger := range []string{
		"members_email_immutable", "invites_status_forward_only", "memberships_granted_at_immutable",
	} {
		var name string
		err := database.QueryRow(
			`SELECT name FROM sqlite_master WHERE type = 'trigger' AND name = ?`, trigger).Scan(&name)
		if err != nil {
			t.Errorf("trigger %s missing: %v", trigger, err)
		}
	}

	// EnsureSchema never stamps a version — only the importer does, in the
	// same transaction as the imported rows.
	if n := countRows(t, database, "schema_migrations"); n != 0 {
		t.Errorf("schema_migrations rows = %d, want 0 before import", n)
	}
}

// seedInvite inserts a minimal valid invite row with the given status.
func seedInvite(t *testing.T, database *sql.DB, handle, lease, status string, tokenValue any) {
	t.Helper()
	_, err := database.Exec(
		`INSERT INTO invites (handle, lease_id, member_id, email, token_value, role, created_at, expires_at, status)
		 VALUES (?, ?, '11111111-1111-1111-1111-111111111111', 'u@corp.com', ?, 'member', '2026-01-01T00:00:00Z', NULL, ?)`,
		handle, lease, tokenValue, status)
	if err != nil {
		t.Fatalf("seed invite: %v", err)
	}
}

func TestInviteStatusForwardOnlyTrigger(t *testing.T) {
	database := ensureTestSchema(t)
	h := strings.Repeat("a", 32)
	seedInvite(t, database, h, strings.Repeat("b", 32), "pending", "evt_x")

	setStatus := func(to string) error {
		// Scrub alongside the transition so the token_value CHECK never
		// interferes with what this test pins (the trigger).
		_, err := database.Exec(
			`UPDATE invites SET status = ?, token_value = NULL WHERE handle = ?`, to, h)
		return err
	}

	// pending → revoked is allowed (branch decision 3) and terminal.
	if err := setStatus("revoked"); err != nil {
		t.Fatalf("pending→revoked rejected: %v", err)
	}
	for _, to := range []string{"pending", "consumed", "expired", "compromised"} {
		if err := setStatus(to); err == nil {
			t.Errorf("revoked→%s accepted, want trigger abort (revoked is terminal)", to)
		}
	}

	// pending → consumed → compromised is the legal consume path;
	// consumed → pending must be unrepresentable.
	h2 := strings.Repeat("c", 32)
	seedInvite(t, database, h2, strings.Repeat("d", 32), "pending", "evt_y")
	set2 := func(to string) error {
		_, err := database.Exec(`UPDATE invites SET status = ?, token_value = NULL WHERE handle = ?`, to, h2)
		return err
	}
	if err := set2("consumed"); err != nil {
		t.Fatalf("pending→consumed rejected: %v", err)
	}
	if err := set2("pending"); err == nil {
		t.Error("consumed→pending accepted, want trigger abort")
	}
	if err := set2("compromised"); err != nil {
		t.Errorf("consumed→compromised rejected: %v", err)
	}
}

func TestInviteChecks(t *testing.T) {
	database := ensureTestSchema(t)

	// Unknown status enum value.
	_, err := database.Exec(
		`INSERT INTO invites (handle, lease_id, member_id, email, token_value, role, created_at, status)
		 VALUES (?, ?, 'm', 'e@corp.com', NULL, 'member', '2026-01-01T00:00:00Z', 'bogus')`,
		strings.Repeat("a", 32), strings.Repeat("b", 32))
	if err == nil {
		t.Error("invite with status 'bogus' accepted, want CHECK violation")
	}

	// token_value on a non-pending row violates the scrub CHECK.
	_, err = database.Exec(
		`INSERT INTO invites (handle, lease_id, member_id, email, token_value, role, created_at, status)
		 VALUES (?, ?, 'm', 'e@corp.com', 'evt_leak', 'member', '2026-01-01T00:00:00Z', 'consumed')`,
		strings.Repeat("c", 32), strings.Repeat("d", 32))
	if err == nil {
		t.Error("consumed invite with token_value accepted, want CHECK violation")
	}

	// Non-hex / wrong-length handle.
	_, err = database.Exec(
		`INSERT INTO invites (handle, lease_id, member_id, email, token_value, role, created_at, status)
		 VALUES ('shorthandle', ?, 'm', 'e@corp.com', NULL, 'member', '2026-01-01T00:00:00Z', 'pending')`,
		strings.Repeat("e", 32))
	if err == nil {
		t.Error("invite with malformed handle accepted, want CHECK violation")
	}
}

func TestMemberEmailImmutableTrigger(t *testing.T) {
	database := ensureTestSchema(t)
	id := "11111111-1111-1111-1111-111111111111"
	if _, err := database.Exec(
		`INSERT INTO members (id, email, status, created_at) VALUES (?, 'a@corp.com', 'active', '2026-01-01T00:00:00Z')`, id); err != nil {
		t.Fatal(err)
	}

	if _, err := database.Exec(`UPDATE members SET email = 'b@corp.com' WHERE id = ?`, id); err == nil {
		t.Error("email change accepted, want trigger abort")
	}
	// Same-value write passes (the trigger fires only on an actual change).
	if _, err := database.Exec(`UPDATE members SET email = 'a@corp.com' WHERE id = ?`, id); err != nil {
		t.Errorf("same-email update rejected: %v", err)
	}
	// Hard delete stays legal — the trigger guards UPDATE only.
	if _, err := database.Exec(`DELETE FROM members WHERE id = ?`, id); err != nil {
		t.Errorf("member delete rejected: %v", err)
	}
}

func TestMemberChecks(t *testing.T) {
	database := ensureTestSchema(t)

	// Bad status enum.
	if _, err := database.Exec(
		`INSERT INTO members (id, email, status, created_at) VALUES ('11111111-1111-1111-1111-111111111111', 'a@corp.com', 'bogus', 'x')`); err == nil {
		t.Error("member with status 'bogus' accepted, want CHECK violation")
	}
	// Non-lowercase / short id (the members CHECK keeps the 36-char shape).
	if _, err := database.Exec(
		`INSERT INTO members (id, email, status, created_at) VALUES ('AAAA', 'a@corp.com', 'active', 'x')`); err == nil {
		t.Error("member with malformed id accepted, want CHECK violation")
	}
	// disabled_from on a non-disabled row.
	if _, err := database.Exec(
		`INSERT INTO members (id, email, status, disabled_from, created_at)
		 VALUES ('22222222-2222-2222-2222-222222222222', 'b@corp.com', 'active', 'invited', 'x')`); err == nil {
		t.Error("active member with disabled_from accepted, want CHECK violation")
	}
}

// seedGroupAndMembership inserts one group and one membership on it.
func seedGroupAndMembership(t *testing.T, database *sql.DB) {
	t.Helper()
	if _, err := database.Exec(
		`INSERT INTO groups (id, name, parent_id, created_at) VALUES ('g1', 'Team', '', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(
		`INSERT INTO memberships (user, group_id, role, granted_by, granted_at)
		 VALUES ('11111111-1111-1111-1111-111111111111', 'g1', 'write', 'local-admin:o@corp.com', '2026-01-02T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
}

func TestMembershipGrantedAtImmutableTrigger(t *testing.T) {
	database := ensureTestSchema(t)
	seedGroupAndMembership(t, database)

	if _, err := database.Exec(
		`UPDATE memberships SET granted_at = '2026-02-01T00:00:00Z' WHERE group_id = 'g1'`); err == nil {
		t.Error("granted_at change accepted, want trigger abort")
	}
	// Role changes (the Grant upsert shape) stay legal and keep granted_at.
	if _, err := database.Exec(
		`UPDATE memberships SET role = 'read', granted_by = 'local-admin:new@corp.com' WHERE group_id = 'g1'`); err != nil {
		t.Errorf("role change rejected: %v", err)
	}
	var grantedAt string
	if err := database.QueryRow(`SELECT granted_at FROM memberships WHERE group_id = 'g1'`).Scan(&grantedAt); err != nil {
		t.Fatal(err)
	}
	if grantedAt != "2026-01-02T00:00:00Z" {
		t.Errorf("granted_at = %q, want original join time", grantedAt)
	}
}

func TestMembershipChecksAndGroupFKRestrict(t *testing.T) {
	database := ensureTestSchema(t)
	seedGroupAndMembership(t, database)

	// Role outside read/write/edit (group roles never include 'admin').
	if _, err := database.Exec(
		`INSERT INTO memberships (user, group_id, role, granted_by, granted_at)
		 VALUES ('22222222-2222-2222-2222-222222222222', 'g1', 'admin', 'x', 'y')`); err == nil {
		t.Error("membership with role 'admin' accepted, want CHECK violation")
	}
	// Membership pointing at an unknown group is unrepresentable (the one FK).
	if _, err := database.Exec(
		`INSERT INTO memberships (user, group_id, role, granted_by, granted_at)
		 VALUES ('22222222-2222-2222-2222-222222222222', 'ghost', 'read', 'x', 'y')`); err == nil {
		t.Error("dangling membership accepted, want FK violation")
	}
	// Deleting a group with memberships is refused (ON DELETE RESTRICT)…
	if _, err := database.Exec(`DELETE FROM groups WHERE id = 'g1'`); err == nil {
		t.Error("group delete with memberships accepted, want FK RESTRICT")
	}
	// …and allowed once the memberships go first (the console team-delete shape).
	if _, err := database.Exec(`DELETE FROM memberships WHERE group_id = 'g1'`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`DELETE FROM groups WHERE id = 'g1'`); err != nil {
		t.Errorf("group delete after membership delete rejected: %v", err)
	}

	// Empty group id is rejected; non-UUID ids stay legal (branch decision 6:
	// today's loader accepts them, and ids are opaque FHE tags).
	if _, err := database.Exec(
		`INSERT INTO groups (id, name, parent_id, created_at) VALUES ('', 'X', '', 'x')`); err == nil {
		t.Error("group with empty id accepted, want CHECK violation")
	}
	if _, err := database.Exec(
		`INSERT INTO groups (id, name, parent_id, created_at) VALUES ('aaaa', 'X', '', 'x')`); err != nil {
		t.Errorf("group with short non-UUID id rejected: %v", err)
	}
}

// TestSweepUsesPendingExpiryIndex pins the query plan of the aged-pending
// sweep: it must be served by the partial index idx_invites_pending_expiry,
// not a table scan — the sweep runs at the head of every invite write
// transaction and invites is the one table whose rows are never deleted, so
// an unindexed sweep is the only unbounded scan in the system.
func TestSweepUsesPendingExpiryIndex(t *testing.T) {
	database := ensureTestSchema(t)
	rows, err := database.Query(
		`EXPLAIN QUERY PLAN
		 UPDATE invites SET status = 'expired', token_value = NULL
		 WHERE status = 'pending' AND expires_at IS NOT NULL AND expires_at <= ?`,
		"2026-07-17T00:00:00.000Z")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var plan strings.Builder
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(detail)
		plan.WriteString("\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.String(), "idx_invites_pending_expiry") {
		t.Errorf("sweep plan does not use the partial index:\n%s", plan.String())
	}
}
