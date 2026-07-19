package server

import (
	"database/sql"
	"testing"

	"github.com/CryptoLabInc/rune-console/internal/db"
	"github.com/CryptoLabInc/rune-console/internal/storedb"
)

// openTokensTestDB opens a fresh hardened store database at path with schema v1
// installed — the on-disk shape the daemon boots against.
func openTokensTestDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	database, err := db.OpenStrict(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := storedb.EnsureSchema(database); err != nil {
		t.Fatal(err)
	}
	return database
}
