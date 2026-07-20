// Package storedb owns the unified store database (runeconsole.db): the
// versioned schema migrations under migrations/ and the runner that applies
// them. It is deliberately a LEAF package (imports no store packages) so every
// store package, and its own tests, can depend on the schema without an import
// cycle.
package storedb

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

const migrationsDir = "migrations"

// migration is one versioned forward step: the SQL of
// migrations/NNN_label.up.sql, keyed by its NNN version.
type migration struct {
	version int
	label   string
	up      string
}

// EnsureSchema brings the store database to the latest schema by applying every
// pending migration in ascending version order, then recording each in the
// schema_migrations ledger. It creates the ledger if absent and skips any
// version already recorded, so it is safe to call on every boot and is the
// single schema-setup entry point for tests.
//
// The ledger is the sole record of what has run. An install shipped before this
// runner existed carries a version=1 row (the old schema baseline); this runner
// reads it as "001 applied" and leaves that database untouched, while a fresh
// install applies 001 from scratch. That is why 001's version matches the
// baseline it replaces — see migrations/001_init.up.sql.
func EnsureSchema(database *sql.DB) error {
	if err := ensureVersionTable(database); err != nil {
		return err
	}
	migs, err := loadMigrations()
	if err != nil {
		return err
	}
	applied, err := appliedVersions(database)
	if err != nil {
		return err
	}
	for _, m := range migs {
		if applied[m.version] {
			continue
		}
		if strings.TrimSpace(m.up) == "" {
			return fmt.Errorf("storedb: migration %03d_%s: empty .up.sql", m.version, m.label)
		}
		if err := apply(database, m); err != nil {
			return fmt.Errorf("storedb: apply migration %03d_%s: %w", m.version, m.label, err)
		}
	}
	return nil
}

// apply runs one migration's script and records it in the ledger within a
// single transaction: a failure rolls back the schema change together with its
// bookkeeping, so a version is never recorded applied when it was not.
// modernc's SQLite driver executes the multi-statement script in one Exec.
func apply(database *sql.DB, m migration) error {
	tx, err := database.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(m.up); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.Exec(
		`INSERT INTO schema_migrations (version, applied_at, description) VALUES (?, ?, ?)`,
		m.version, FormatTime(time.Now()), m.label); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func ensureVersionTable(database *sql.DB) error {
	_, err := database.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
  version     INTEGER PRIMARY KEY,
  applied_at  TEXT NOT NULL,
  description TEXT NOT NULL
)`)
	return err
}

func appliedVersions(database *sql.DB) (map[int]bool, error) {
	rows, err := database.Query(`SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	applied := make(map[int]bool)
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		applied[v] = true
	}
	return applied, rows.Err()
}

// loadMigrations parses the embedded migrations/NNN_label.up.sql files and
// returns them in ascending version order. Every embedded .sql file must be an
// .up.sql (this is a forward-only runner); an unexpected name is a build-time
// mistake and fails loudly rather than being silently skipped.
func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationsFS, migrationsDir)
	if err != nil {
		return nil, err
	}
	out := make([]migration, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".up.sql") {
			return nil, fmt.Errorf("storedb: migration %q must be named NNN_label.up.sql", name)
		}
		base := strings.TrimSuffix(name, ".up.sql") // NNN_label
		numStr, label, ok := strings.Cut(base, "_")
		if !ok {
			return nil, fmt.Errorf("storedb: migration %q must be named NNN_label.up.sql", name)
		}
		version, err := strconv.Atoi(numStr)
		if err != nil {
			return nil, fmt.Errorf("storedb: migration %q: non-numeric version %q", name, numStr)
		}
		body, err := migrationsFS.ReadFile(migrationsDir + "/" + name)
		if err != nil {
			return nil, err
		}
		out = append(out, migration{version: version, label: label, up: string(body)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}
