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
      disable: true
  admin:
    socket: /tmp/admin.sock
keys:
  path: /tmp/vault-keys
  embedding_dim: 1024
envector:
  endpoint: https://example.com
  api_key: inline-api-key
tokens:
  team_secret: inline-team-secret-deadbeef
  roles_file: /tmp/roles.yml
  tokens_file: /tmp/tokens.yml
audit:
  mode: stdout
`
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "runevault.conf")
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
	_, err := LoadConfig("/tmp/this/path/does/not/exist/runevault.conf")
	if err == nil {
		t.Fatal("expected error for missing config")
	}
	if !strings.Contains(err.Error(), "/tmp/this/path/does/not/exist/runevault.conf") {
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

func TestLoadConfigUnknownFieldsRejected(t *testing.T) {
	body := minimalValidConfig(t) + "extra_unknown_field: 42\n"
	path := writeConfig(t, body)
	_, err := LoadConfig(path)
	if err == nil {
		t.Error("unknown top-level field accepted, want strict error")
	}
}

func TestLoadConfigAPIKeyFileIndirection(t *testing.T) {
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "envector.key")
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
	if cfg.Envector.APIKey != "file-api-key" {
		t.Errorf("api_key = %q, want file-api-key", cfg.Envector.APIKey)
	}
	if cfg.Envector.APIKeyFile != "" {
		t.Errorf("api_key_file should be cleared after Resolve, got %q", cfg.Envector.APIKeyFile)
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
		Envector: EnvectorConfig{APIKey: "deadbeef", APIKeyFile: "/x"},
		Tokens:   TokensConfig{TeamSecret: "supersecret", TeamSecretFile: "/y"},
	}
	r := cfg.Redact()
	if r.Envector.APIKey != "[REDACTED]" {
		t.Errorf("api_key not redacted: %q", r.Envector.APIKey)
	}
	if r.Envector.APIKeyFile != "[REDACTED]" {
		t.Errorf("api_key_file not redacted: %q", r.Envector.APIKeyFile)
	}
	if r.Tokens.TeamSecret != "[REDACTED]" {
		t.Errorf("team_secret not redacted: %q", r.Tokens.TeamSecret)
	}
	if r.Tokens.TeamSecretFile != "[REDACTED]" {
		t.Errorf("team_secret_file not redacted: %q", r.Tokens.TeamSecretFile)
	}
	// Original must be untouched.
	if cfg.Envector.APIKey != "deadbeef" {
		t.Errorf("Redact mutated original")
	}
}

func TestValidateRejectsMissingFields(t *testing.T) {
	cases := map[string]func(*Config){
		"missing socket":      func(c *Config) { c.Server.Admin.Socket = "" },
		"missing port":        func(c *Config) { c.Server.GRPC.Port = 0 },
		"missing keys.path":   func(c *Config) { c.Keys.Path = "" },
		"missing dim":         func(c *Config) { c.Keys.EmbeddingDim = 0 },
		"missing roles_file":  func(c *Config) { c.Tokens.RolesFile = "" },
		"missing tokens_file": func(c *Config) { c.Tokens.TokensFile = "" },
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
	body := strings.Replace(
		minimalValidConfig(t),
		"      disable: true",
		"      disable: false",
		1,
	)
	path := writeConfig(t, body)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate(); err == nil {
		t.Error("Validate accepted TLS enabled without cert/key")
	}
}

func TestExampleConfigParsesCleanly(t *testing.T) {
	// The committed example file should at least parse — operators copy it.
	data, err := os.ReadFile("testdata/runevault.conf.example")
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
