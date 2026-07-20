package console

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// f4ad3dbSchemaV1 is copied from the baseline commit rather than referencing
// dbSchemaV1: this fixture must keep representing the database an installation
// at that commit brings to an upgrade, even after new migrations are appended.
const f4ad3dbSchemaV1 = `
CREATE TABLE IF NOT EXISTS console_session (
  session_id      TEXT PRIMARY KEY,
  runespace_token TEXT NOT NULL,
  cookie_name     TEXT NOT NULL,
  me              BLOB,
  created_at      TEXT NOT NULL,
  expires_at      TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS console_owner (
  id       INTEGER PRIMARY KEY CHECK (id = 1),
  email    TEXT NOT NULL,
  me       BLOB,
  bound_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS dataplane_credential (
  id            INTEGER PRIMARY KEY CHECK (id = 1),
  refresh_token TEXT NOT NULL,
  runespace_id  TEXT NOT NULL,
  addr          TEXT NOT NULL,
  created_at    TEXT NOT NULL
);`

func TestDBSchemaV1BaselineIsFrozen(t *testing.T) {
	if dbSchemaV1 != f4ad3dbSchemaV1 {
		t.Fatal("dbSchemaV1 changed; restore the f4ad3db baseline and append a migration")
	}
}

func schemaMigrationCount(t *testing.T, database *sql.DB) int {
	t.Helper()
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM console_schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("count console schema migrations: %v", err)
	}
	return count
}

func requireTable(t *testing.T, database *sql.DB, table string) {
	t.Helper()
	var got string
	if err := database.QueryRow(
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`, table,
	).Scan(&got); err != nil {
		t.Fatalf("table %s missing: %v", table, err)
	}
}

func TestEnsureDBSchemaFreshBaseline(t *testing.T) {
	database := openTestDB(t)
	if err := EnsureDBSchema(database); err != nil {
		t.Fatalf("EnsureDBSchema: %v", err)
	}

	for _, table := range []string{
		"console_schema_migrations",
		"console_session",
		"console_owner",
		"dataplane_credential",
	} {
		requireTable(t, database, table)
	}

	if got := schemaMigrationCount(t, database); got != 1 {
		t.Fatalf("migration rows = %d, want 1", got)
	}
	var version int
	var appliedAt, description string
	if err := database.QueryRow(
		`SELECT version, applied_at, description FROM console_schema_migrations`,
	).Scan(&version, &appliedAt, &description); err != nil {
		t.Fatal(err)
	}
	if version != DBSchemaVersion {
		t.Errorf("baseline version = %d, want %d", version, DBSchemaVersion)
	}
	if _, err := time.Parse(time.RFC3339Nano, appliedAt); err != nil {
		t.Errorf("applied_at = %q, want RFC3339: %v", appliedAt, err)
	}
	if description != dbSchemaMigrations[0].description {
		t.Errorf("description = %q, want %q", description, dbSchemaMigrations[0].description)
	}
}

func TestEnsureDBSchemaAdoptsF4ad3dbWithoutChangingRows(t *testing.T) {
	database := openTestDB(t)
	if _, err := database.Exec(f4ad3dbSchemaV1); err != nil {
		t.Fatalf("install f4ad3db schema: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO console_session
		  (session_id, runespace_token, cookie_name, me, created_at, expires_at)
		VALUES
		  ('session-1', 'cloud-secret', 'rune_session', '{"email":"owner@example.com"}',
		   '2026-07-20T00:00:00Z', '2030-07-20T00:00:00Z');
		INSERT INTO console_owner (id, email, me, bound_at)
		VALUES (1, 'owner@example.com', '{"email":"owner@example.com"}', '2026-07-20T00:00:00Z');
		INSERT INTO dataplane_credential (id, refresh_token, runespace_id, addr, created_at)
		VALUES (1, 'refresh-secret', 'space-1', 'space.example:443', '2026-07-20T00:00:00Z');
	`); err != nil {
		t.Fatalf("seed f4ad3db rows: %v", err)
	}

	if err := EnsureDBSchema(database); err != nil {
		t.Fatalf("adopt f4ad3db schema: %v", err)
	}

	var sessionToken, ownerEmail, refreshToken string
	if err := database.QueryRow(
		`SELECT runespace_token FROM console_session WHERE session_id = 'session-1'`,
	).Scan(&sessionToken); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT email FROM console_owner WHERE id = 1`).Scan(&ownerEmail); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(
		`SELECT refresh_token FROM dataplane_credential WHERE id = 1`,
	).Scan(&refreshToken); err != nil {
		t.Fatal(err)
	}
	if sessionToken != "cloud-secret" || ownerEmail != "owner@example.com" || refreshToken != "refresh-secret" {
		t.Errorf("rows changed during adoption: session=%q owner=%q refresh=%q",
			sessionToken, ownerEmail, refreshToken)
	}
	if got := schemaMigrationCount(t, database); got != 1 {
		t.Errorf("migration rows = %d, want one adopted v1 baseline", got)
	}
}

func TestEnsureDBSchemaIdempotent(t *testing.T) {
	database := openTestDB(t)
	if err := EnsureDBSchema(database); err != nil {
		t.Fatal(err)
	}
	var firstAppliedAt string
	if err := database.QueryRow(
		`SELECT applied_at FROM console_schema_migrations WHERE version = ?`, DBSchemaVersion,
	).Scan(&firstAppliedAt); err != nil {
		t.Fatal(err)
	}

	if err := EnsureDBSchema(database); err != nil {
		t.Fatalf("second EnsureDBSchema: %v", err)
	}
	var secondAppliedAt string
	if err := database.QueryRow(
		`SELECT applied_at FROM console_schema_migrations WHERE version = ?`, DBSchemaVersion,
	).Scan(&secondAppliedAt); err != nil {
		t.Fatal(err)
	}
	if got := schemaMigrationCount(t, database); got != 1 {
		t.Errorf("migration rows after second ensure = %d, want 1", got)
	}
	if secondAppliedAt != firstAppliedAt {
		t.Errorf("v1 applied_at changed on second ensure: %q -> %q", firstAppliedAt, secondAppliedAt)
	}
}

func TestConstructorsRejectNewerDBSchema(t *testing.T) {
	tests := map[string]func(*sql.DB) error{
		"session": func(database *sql.DB) error {
			_, err := newSessionStore(database, nil)
			return err
		},
		"owner": func(database *sql.DB) error {
			_, err := newOwnerStore(database, nil)
			return err
		},
		"dataplane": func(database *sql.DB) error {
			_, err := newDataplane(database, nil, nil, nil, "")
			return err
		},
	}

	for name, construct := range tests {
		t.Run(name, func(t *testing.T) {
			database := openTestDB(t)
			if err := EnsureDBSchema(database); err != nil {
				t.Fatal(err)
			}
			newer := DBSchemaVersion + 1
			if _, err := database.Exec(
				`INSERT INTO console_schema_migrations (version, applied_at, description) VALUES (?, ?, ?)`,
				newer, "2030-01-01T00:00:00Z", "future migration",
			); err != nil {
				t.Fatal(err)
			}

			err := construct(database)
			if !errors.Is(err, ErrDBSchemaTooNew) {
				t.Fatalf("constructor error = %v, want ErrDBSchemaTooNew", err)
			}
			if !strings.Contains(err.Error(), fmt.Sprintf("database version %d", newer)) {
				t.Errorf("error does not report versions: %v", err)
			}
		})
	}
}

func TestApplyDBSchemaMigrationsSupportsOrderedExtension(t *testing.T) {
	database := openTestDB(t)
	if err := EnsureDBSchema(database); err != nil {
		t.Fatal(err)
	}

	extended := append([]dbSchemaMigration(nil), dbSchemaMigrations...)
	extended = append(extended, dbSchemaMigration{
		version:     DBSchemaVersion + 1,
		description: "test future migration",
		statements: []string{
			`ALTER TABLE console_owner ADD COLUMN note TEXT NOT NULL DEFAULT ''`,
		},
	})
	if err := applyDBSchemaMigrations(database, extended); err != nil {
		t.Fatalf("apply appended migration: %v", err)
	}

	var note string
	if _, err := database.Exec(
		`INSERT INTO console_owner (id, email, bound_at, note) VALUES (1, 'owner@example.com', 'now', 'kept')`,
	); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT note FROM console_owner WHERE id = 1`).Scan(&note); err != nil {
		t.Fatal(err)
	}
	if note != "kept" {
		t.Errorf("future column round trip = %q, want kept", note)
	}
	if got := schemaMigrationCount(t, database); got != 2 {
		t.Errorf("migration rows = %d, want 2", got)
	}
	if err := applyDBSchemaMigrations(database, extended); err != nil {
		t.Fatalf("reapply appended migration: %v", err)
	}
	if got := schemaMigrationCount(t, database); got != 2 {
		t.Errorf("migration rows after reapply = %d, want 2", got)
	}
}

func TestApplyDBSchemaMigrationsRollsBackFailedVersion(t *testing.T) {
	database := openTestDB(t)
	if err := EnsureDBSchema(database); err != nil {
		t.Fatal(err)
	}

	broken := append([]dbSchemaMigration(nil), dbSchemaMigrations...)
	broken = append(broken, dbSchemaMigration{
		version:     DBSchemaVersion + 1,
		description: "committed future migration",
		statements: []string{
			`CREATE TABLE committed_before_failure (id INTEGER PRIMARY KEY)`,
		},
	})
	broken = append(broken, dbSchemaMigration{
		version:     DBSchemaVersion + 2,
		description: "broken later migration",
		statements: []string{
			`CREATE TABLE must_rollback (id INTEGER PRIMARY KEY)`,
			`THIS IS NOT VALID SQL`,
		},
	})
	if err := applyDBSchemaMigrations(database, broken); err == nil {
		t.Fatal("broken migration succeeded")
	}

	var name string
	err := database.QueryRow(
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'must_rollback'`,
	).Scan(&name)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("table from failed migration survived rollback: name=%q err=%v", name, err)
	}
	requireTable(t, database, "committed_before_failure")
	if got := schemaMigrationCount(t, database); got != 2 {
		t.Errorf("migration rows after rollback = %d, want v1 and committed v2", got)
	}
}
