package console

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// DBSchemaVersion is the newest console-session.db schema this binary can
// read. Version 1 freezes the schema at baseline commit f4ad3db; the first
// release tag must point to a commit that includes this migration runner.
const DBSchemaVersion = 1

// ErrDBSchemaTooNew is returned when console-session.db was already opened by
// a newer runeconsole binary. Refusing to continue prevents an older binary
// from interpreting or mutating a schema it does not understand.
var ErrDBSchemaTooNew = errors.New("console: session database schema is newer than this binary")

const schemaMigrationsDDL = `
CREATE TABLE IF NOT EXISTS console_schema_migrations (
  version     INTEGER PRIMARY KEY,
  applied_at  TEXT NOT NULL,
  description TEXT NOT NULL
);`

// dbSchemaV1 is the exact console-session.db layout at baseline commit
// f4ad3db. Applied migrations are immutable: later releases add a new ordered
// migration instead of editing this baseline.
const dbSchemaV1 = `
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

type dbSchemaMigration struct {
	version     int
	description string
	statements  []string
}

var dbSchemaMigrations = []dbSchemaMigration{
	{
		version:     1,
		description: "f4ad3db baseline console schema",
		statements:  []string{dbSchemaV1},
	},
}

// EnsureDBSchema brings console-session.db to DBSchemaVersion. Each migration
// and its ledger row commit in one SQLite transaction, so startup can never
// observe a partially applied version. Successfully committed versions remain
// restart points if a later version fails.
//
// A database created at f4ad3db has the three v1 tables but no migration
// ledger. Running the idempotent v1 DDL over it preserves every row and records
// v1 as its baseline. A fresh database follows the same path. A database
// carrying a version newer than this binary is rejected before any change is
// committed.
func EnsureDBSchema(database *sql.DB) error {
	if database == nil {
		return errors.New("console: ensure session database schema: nil database")
	}
	if err := validateDBSchemaMigrations(dbSchemaMigrations, DBSchemaVersion); err != nil {
		return err
	}
	return applyDBSchemaMigrations(database, dbSchemaMigrations)
}

// applyDBSchemaMigrations is split from EnsureDBSchema so tests can prove that
// future appended migrations run in order and roll back atomically on failure
// without changing the production migration list.
func applyDBSchemaMigrations(database *sql.DB, migrations []dbSchemaMigration) error {
	if len(migrations) == 0 {
		return errors.New("console: session database migration plan is empty")
	}
	if err := validateDBSchemaMigrations(migrations, migrations[len(migrations)-1].version); err != nil {
		return err
	}

	target := migrations[len(migrations)-1].version
	for {
		done, err := applyNextDBSchemaMigration(database, migrations, target)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}
}

// applyNextDBSchemaMigration applies at most one version. Re-reading the
// ledger in every transaction avoids making migration decisions from stale
// state, while a committed version stays applied if a later one fails.
func applyNextDBSchemaMigration(database *sql.DB, migrations []dbSchemaMigration, target int) (bool, error) {
	tx, err := database.Begin()
	if err != nil {
		return false, fmt.Errorf("console: begin session database migration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(schemaMigrationsDDL); err != nil {
		return false, fmt.Errorf("console: create session database migration ledger: %w", err)
	}

	current, err := readDBSchemaVersion(tx, target)
	if err != nil {
		return false, err
	}
	if current == target {
		if err := tx.Commit(); err != nil {
			return false, fmt.Errorf("console: commit session database migration check: %w", err)
		}
		return true, nil
	}

	migration := migrations[current] // contiguous versions are indexed by the current version
	for i, statement := range migration.statements {
		if _, err := tx.Exec(statement); err != nil {
			return false, fmt.Errorf("console: apply session database migration %d statement %d: %w",
				migration.version, i+1, err)
		}
	}
	if _, err := tx.Exec(
		`INSERT INTO console_schema_migrations (version, applied_at, description) VALUES (?, ?, ?)`,
		migration.version, time.Now().UTC().Format(time.RFC3339Nano), migration.description,
	); err != nil {
		return false, fmt.Errorf("console: record session database migration %d: %w", migration.version, err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("console: commit session database migration %d: %w", migration.version, err)
	}
	return migration.version == target, nil
}

func validateDBSchemaMigrations(migrations []dbSchemaMigration, wantVersion int) error {
	if len(migrations) == 0 {
		return errors.New("console: session database migration plan is empty")
	}
	for i, migration := range migrations {
		want := i + 1
		if migration.version != want {
			return fmt.Errorf("console: session database migration plan is not contiguous: got version %d, want %d",
				migration.version, want)
		}
		if migration.description == "" {
			return fmt.Errorf("console: session database migration %d has no description", migration.version)
		}
		if len(migration.statements) == 0 {
			return fmt.Errorf("console: session database migration %d has no statements", migration.version)
		}
	}
	if got := migrations[len(migrations)-1].version; got != wantVersion {
		return fmt.Errorf("console: session database migration plan ends at version %d, DBSchemaVersion is %d",
			got, wantVersion)
	}
	return nil
}

// readDBSchemaVersion validates that the ledger is an ordered, gap-free prefix
// of the migration plan and returns its highest applied version.
func readDBSchemaVersion(tx *sql.Tx, supported int) (int, error) {
	rows, err := tx.Query(`SELECT version FROM console_schema_migrations ORDER BY version`)
	if err != nil {
		return 0, fmt.Errorf("console: read session database migration ledger: %w", err)
	}

	current := 0
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			_ = rows.Close()
			return 0, fmt.Errorf("console: read session database migration version: %w", err)
		}
		if version > supported {
			_ = rows.Close()
			return 0, fmt.Errorf("%w (database version %d, binary supports %d)",
				ErrDBSchemaTooNew, version, supported)
		}
		if want := current + 1; version != want {
			_ = rows.Close()
			return 0, fmt.Errorf("console: session database migration ledger is not contiguous: got version %d, want %d",
				version, want)
		}
		current = version
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, fmt.Errorf("console: read session database migration ledger: %w", err)
	}
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("console: close session database migration ledger: %w", err)
	}
	return current, nil
}
