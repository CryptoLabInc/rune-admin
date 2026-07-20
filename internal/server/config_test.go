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
	if cfg.ConfigVersion != CurrentConfigVersion {
		t.Errorf("config version = %d, want %d", cfg.ConfigVersion, CurrentConfigVersion)
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

func TestLoadConfigVersionContract(t *testing.T) {
	t.Run("explicit current version", func(t *testing.T) {
		body := "config_version: 1\n" + minimalValidConfig(t)
		cfg, err := LoadConfig(writeConfig(t, body))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.ConfigVersion != CurrentConfigVersion {
			t.Fatalf("config version = %d, want %d", cfg.ConfigVersion, CurrentConfigVersion)
		}
	})

	t.Run("newer config is refused on downgrade", func(t *testing.T) {
		body := "config_version: 2\n" + minimalValidConfig(t)
		_, err := LoadConfig(writeConfig(t, body))
		if err == nil || !strings.Contains(err.Error(), "unsupported config_version 2") {
			t.Fatalf("LoadConfig error = %v, want unsupported version", err)
		}
	})
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

func TestRedactMasksSecrets(t *testing.T) {
	cfg := &Config{
		Tokens: TokensConfig{TeamSecret: "supersecret"},
	}
	r := cfg.Redact()
	if r.Tokens.TeamSecret != "[REDACTED]" {
		t.Errorf("team_secret not redacted: %q", r.Tokens.TeamSecret)
	}
	// Original must be untouched.
	if cfg.Tokens.TeamSecret != "supersecret" {
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
