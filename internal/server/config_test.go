package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// minimalValidConfig returns a YAML body that satisfies Validate().
func minimalValidConfig(t *testing.T) string {
	t.Helper()
	return `server:
  grpc:
    host: 127.0.0.1
    port: 50051
    tls:
      cert: /tmp/rune-console-test.pem
      key: /tmp/rune-console-test.key
keys:
  path: /tmp/rune-console-keys
  embedding_dim: 1024
runespace:
  endpoint: https://example.com
  api_key: inline-api-key
  insecure: true
tokens:
  team_secret: inline-team-secret-deadbeef
audit:
  mode: stdout
storage:
  data_dir: /tmp
`
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "runeconsole.conf")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadConfigMinimalValid(t *testing.T) {
	path := writeConfig(t, minimalValidConfig(t))
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.GRPC.Port != 50051 {
		t.Errorf("port = %d, want 50051", cfg.Server.GRPC.Port)
	}
	if cfg.Tokens.TeamSecret != "inline-team-secret-deadbeef" {
		t.Errorf("team_secret = %q, want inline value", cfg.Tokens.TeamSecret)
	}
	if cfg.Source != path {
		// Source may be absolute even if path is already absolute (it should match).
		abs, _ := filepath.Abs(path)
		if cfg.Source != abs {
			t.Errorf("Source = %q, want %q", cfg.Source, abs)
		}
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

func TestLoadConfigMissingNamesAllPaths(t *testing.T) {
	_, err := LoadConfig("/tmp/this/path/does/not/exist/runeconsole.conf")
	if err == nil {
		t.Fatal("expected error for missing config")
	}
	if !strings.Contains(err.Error(), "/tmp/this/path/does/not/exist/runeconsole.conf") {
		t.Errorf("err missing override path: %v", err)
	}
}

func TestLoadConfigDefaultLookupErrorListsPaths(t *testing.T) {
	// Stash the package-level lookup list and restore.
	orig := ConfigLookupPaths
	defer func() { ConfigLookupPaths = orig }()
	ConfigLookupPaths = []string{"/nope/a.conf", "/nope/b.conf"}

	_, err := LoadConfig("")
	if err == nil {
		t.Fatal("expected error")
	}
	for _, p := range ConfigLookupPaths {
		if !strings.Contains(err.Error(), p) {
			t.Errorf("err missing %s: %v", p, err)
		}
	}
}

// TestLoadConfigUnknownFieldsRejected — strict decoding refuses an unknown key
// at every depth, not just the top level. A typo nested inside a section is the
// likelier operator mistake, and it is the one that fails silently if
// KnownFields only reached the outermost mapping: the section still decodes,
// so the daemon boots with the intended setting quietly ignored.
func TestLoadConfigUnknownFieldsRejected(t *testing.T) {
	cases := map[string]string{
		"top level": minimalValidConfig(t) + "extra_unknown_field: 42\n",
		// Inside keys:, a section that already decodes fine. Deliberately not
		// one of the removed keys — TestLoadConfigRemovedKeysGuidance owns
		// those, and they take a different path (tailored guidance).
		"nested in a section": strings.Replace(minimalValidConfig(t),
			"  embedding_dim: 1024",
			"  embedding_dim: 1024\n  extra_unknown_field: 42",
			1),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadConfig(writeConfig(t, body)); err == nil {
				t.Error("unknown field accepted, want strict error")
			}
		})
	}
}

func TestLoadConfigOrgAdminsRemovedGuidance(t *testing.T) {
	// groups.org_admins shipped in v1.0.0-alpha and is no longer honored: the
	// org admin is derived from the first-login console owner. A config still
	// carrying the field must be REFUSED (silently ignoring it would change who
	// holds admin authority without saying so) and the refusal must name the
	// replacement, not just the field.
	body := minimalValidConfig(t) + `groups:
  org_admins:
    - admin@corp.com
`
	path := writeConfig(t, body)
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("config with org_admins accepted, want migration error")
	}
	for _, want := range []string{"groups.org_admins is no longer supported", "first logs in", "remove the org_admins entry"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want it to contain %q", err, want)
		}
	}
	// The guidance must come from the decoded value, not from matching the yaml
	// library's strict-decode wording — a dependency bump must not silently
	// downgrade this to a bare "field not found in type server.GroupsConfig".
	if strings.Contains(err.Error(), "not found in type") {
		t.Errorf("err came from the strict-decode path, not the value check: %v", err)
	}
}

// TestLoadConfigRemovedStorePathsRejected pins only the refusal, not any
// wording: a config carrying the removed per-store paths must not boot. There
// is no upgrade path to guide anyone through — the supported move from the
// v1.0.0-alpha layout is a fresh install — so strict decoding refusing the
// unknown key is the whole contract.
func TestLoadConfigRemovedStorePathsRejected(t *testing.T) {
	body := strings.Replace(minimalValidConfig(t),
		"  team_secret: inline-team-secret-deadbeef",
		"  team_secret: inline-team-secret-deadbeef\n  roles_file: /o/roles.yml\n  tokens_file: /o/tokens.yml",
		1)
	if _, err := LoadConfig(writeConfig(t, body)); err == nil {
		t.Error("config with removed store paths accepted, want strict-decode refusal")
	}
}

func TestLoadConfigOrgAdminsEmptyIsAccepted(t *testing.T) {
	// The field is declared only to produce the migration error above. A config
	// that merely has a groups section (or an empty list) must still boot —
	// the refusal is about a DECLARED admin, not about the key existing.
	body := minimalValidConfig(t) + `groups:
  topk_read: 7
`
	path := writeConfig(t, body)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("groups section without org_admins refused: %v", err)
	}
	if cfg.Groups.TopKRead != 7 {
		t.Errorf("topk_read = %d, want 7", cfg.Groups.TopKRead)
	}
}

func TestLoadConfigAPIKeyFileIndirection(t *testing.T) {
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "runespace.key")
	if err := os.WriteFile(keyFile, []byte("file-api-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	body := strings.Replace(
		minimalValidConfig(t),
		"  api_key: inline-api-key",
		"  api_key_file: "+keyFile,
		1,
	)
	path := writeConfig(t, body)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Runespace.APIKey != "file-api-key" {
		t.Errorf("api_key = %q, want file-api-key", cfg.Runespace.APIKey)
	}
	if cfg.Runespace.APIKeyFile != "" {
		t.Errorf("api_key_file should be cleared after Resolve, got %q", cfg.Runespace.APIKeyFile)
	}
}

func TestLoadConfigTeamSecretFileIndirection(t *testing.T) {
	dir := t.TempDir()
	secretFile := filepath.Join(dir, "team.secret")
	if err := os.WriteFile(secretFile, []byte("file-team-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	body := strings.Replace(
		minimalValidConfig(t),
		"  team_secret: inline-team-secret-deadbeef",
		"  team_secret_file: "+secretFile,
		1,
	)
	path := writeConfig(t, body)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tokens.TeamSecret != "file-team-secret" {
		t.Errorf("team_secret = %q, want file-team-secret", cfg.Tokens.TeamSecret)
	}
}

func TestLoadConfigRejectsWorldReadableConfig(t *testing.T) {
	path := writeConfig(t, minimalValidConfig(t))
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for world-readable config, got nil")
	}
	if !strings.Contains(err.Error(), "too permissive") {
		t.Errorf("err missing 'too permissive': %v", err)
	}
}

func TestLoadConfigRejectsWorldReadableSecretFile(t *testing.T) {
	dir := t.TempDir()
	secretFile := filepath.Join(dir, "team.secret")
	if err := os.WriteFile(secretFile, []byte("file-team-secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	body := strings.Replace(
		minimalValidConfig(t),
		"  team_secret: inline-team-secret-deadbeef",
		"  team_secret_file: "+secretFile,
		1,
	)
	path := writeConfig(t, body)
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for world-readable team_secret_file, got nil")
	}
	if !strings.Contains(err.Error(), "too permissive") {
		t.Errorf("err missing 'too permissive': %v", err)
	}
}

func TestLoadConfigSecretFileMissing(t *testing.T) {
	body := strings.Replace(
		minimalValidConfig(t),
		"  team_secret: inline-team-secret-deadbeef",
		"  team_secret_file: /nope/team.secret",
		1,
	)
	path := writeConfig(t, body)
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for missing team_secret_file")
	}
	if !strings.Contains(err.Error(), "team_secret_file") {
		t.Errorf("err missing label: %v", err)
	}
}

func TestRedactMasksSecrets(t *testing.T) {
	cfg := &Config{
		Runespace: RunespaceConfig{APIKey: "deadbeef", APIKeyFile: "/x"},
		Tokens:    TokensConfig{TeamSecret: "supersecret", TeamSecretFile: "/y"},
	}
	r := cfg.Redact()
	if r.Runespace.APIKey != "[REDACTED]" {
		t.Errorf("api_key not redacted: %q", r.Runespace.APIKey)
	}
	if r.Runespace.APIKeyFile != "[REDACTED]" {
		t.Errorf("api_key_file not redacted: %q", r.Runespace.APIKeyFile)
	}
	if r.Tokens.TeamSecret != "[REDACTED]" {
		t.Errorf("team_secret not redacted: %q", r.Tokens.TeamSecret)
	}
	if r.Tokens.TeamSecretFile != "[REDACTED]" {
		t.Errorf("team_secret_file not redacted: %q", r.Tokens.TeamSecretFile)
	}
	// Original must be untouched.
	if cfg.Runespace.APIKey != "deadbeef" {
		t.Errorf("Redact mutated original")
	}
}

func TestValidateRejectsMissingFields(t *testing.T) {
	cases := map[string]func(*Config){
		"missing port":      func(c *Config) { c.Server.GRPC.Port = 0 },
		"missing keys.path": func(c *Config) { c.Keys.Path = "" },
		"missing dim":       func(c *Config) { c.Keys.EmbeddingDim = 0 },
		"missing data_dir":  func(c *Config) { c.Storage.DataDir = "" },
	}
	base := func() *Config {
		path := writeConfig(t, minimalValidConfig(t))
		c, err := LoadConfig(path)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			c := base()
			mut(c)
			if err := c.Validate(); err == nil {
				t.Errorf("Validate accepted %s", name)
			}
		})
	}
}

func TestValidateRejectsTLSWithoutCertKey(t *testing.T) {
	// TLS is mandatory: dropping cert/key from an otherwise-valid config must
	// fail Validate (there is no disable escape hatch).
	body := strings.Replace(
		minimalValidConfig(t),
		"      cert: /tmp/rune-console-test.pem\n      key: /tmp/rune-console-test.key\n",
		"",
		1,
	)
	path := writeConfig(t, body)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate(); err == nil {
		t.Error("Validate accepted TLS without cert/key")
	}
}

func TestExampleConfigParsesCleanly(t *testing.T) {
	// The committed example file should at least parse — operators copy it.
	data, err := os.ReadFile("testdata/runeconsole.conf.example")
	if err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	path := filepath.Join(tmp, "example.conf")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("example file failed to parse: %v", err)
	}
	if cfg.Server.GRPC.Port != 50051 {
		t.Errorf("example: port = %d", cfg.Server.GRPC.Port)
	}
}

func TestStoreDBPathDefaultsIntoDataDir(t *testing.T) {
	// With no explicit db_path the store database lands inside
	// storage.data_dir, the directory every other runtime artifact
	// also defaults into.
	path := writeConfig(t, minimalValidConfig(t))
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cfg.StoreDBPath(), "/tmp/runeconsole.db"; got != want {
		t.Errorf("StoreDBPath = %q, want %q", got, want)
	}
}

func TestStoreDBPathExplicitOverride(t *testing.T) {
	// minimalValidConfig ends with the storage block, so an extra indented key
	// continues that mapping instead of opening a duplicate storage:.
	body := minimalValidConfig(t) + "  db_path: /var/lib/runeconsole/store.db\n"
	path := writeConfig(t, body)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := cfg.StoreDBPath(), "/var/lib/runeconsole/store.db"; got != want {
		t.Errorf("StoreDBPath = %q, want %q", got, want)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate with storage.db_path: %v", err)
	}
}
