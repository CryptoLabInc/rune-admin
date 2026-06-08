// Package tests provides shared helpers and hosts cross-package end-to-end tests.
// Integration tests require the GPG-encrypted fixtures to be decrypted into
// tests/fixtures/ first (run `mise run fixtures:decrypt`).
package tests

import (
	"os"
	"path/filepath"
	"runtime"
)

// RepoRoot returns the absolute path to the rune-admin repository root,
// resolved relative to this source file. Works regardless of the test's
// cwd, so tests can locate tests/fixtures/ from any subpackage.
func RepoRoot() string {
	_, thisFile, _, _ := runtime.Caller(0)
	// thisFile = .../vault/internal/tests/fixtures.go → repo root is 3 levels up.
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
}

// FixturesDir returns the absolute path to tests/fixtures/.
func FixturesDir() string {
	return filepath.Join(RepoRoot(), "tests", "fixtures")
}

// FixturesAvailable reports whether the GPG-encrypted fixtures have been
// decrypted into tests/fixtures/. Use as a guard in TestMain or as the
// condition for t.Skip in integration tests.
func FixturesAvailable() bool {
	_, err := os.Stat(filepath.Join(FixturesDir(), "config.json"))
	return err == nil
}

// SkipReason returns the standard skip message for tests that require
// decrypted fixtures.
const SkipReason = "fixtures not decrypted — run `mise run fixtures:decrypt` (requires FIXTURES_GPG_PASSPHRASE)"
