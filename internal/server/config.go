// Package server hosts the daemon transports (gRPC, admin UDS), audit log,
// and runtime configuration. Pure crypto/token logic lives in internal/crypto
// and internal/tokens respectively.
package server

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ConfigLookupPaths lists, in priority order, the on-disk locations that
// LoadConfig probes when the caller doesn't pass an explicit path.
var ConfigLookupPaths = []string{
	"/opt/runeconsole/configs/runeconsole.conf",
	"./runeconsole.conf",
}

// 0640: group-readable so runeconsole group members can run CLI commands without sudo.
const expectedSecretMode fs.FileMode = 0o640

// Config is the in-memory shape of runeconsole.conf. Field names follow the
// YAML schema exactly so the loader can decode without an intermediate type.
type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Cloud     CloudConfig     `yaml:"cloud"`
	Keys      KeysConfig      `yaml:"keys"`
	Runespace RunespaceConfig `yaml:"runespace"`
	Tokens    TokensConfig    `yaml:"tokens"`
	Groups    GroupsConfig    `yaml:"groups"`
	Members   MembersConfig   `yaml:"members"`
	Audit     AuditConfig     `yaml:"audit"`
	Storage   StorageConfig   `yaml:"storage"`

	// Source records where this Config was loaded from (resolved absolute
	// path), populated by LoadConfig. Empty for in-memory test configs.
	Source string `yaml:"-" json:"-"`
}

type ServerConfig struct {
	GRPC    GRPCConfig    `yaml:"grpc"`
	Console ConsoleConfig `yaml:"console"`
}

type GRPCConfig struct {
	Host string    `yaml:"host"`
	Port int       `yaml:"port"`
	TLS  TLSConfig `yaml:"tls"`
}

type TLSConfig struct {
	Cert    string `yaml:"cert"`
	Key     string `yaml:"key"`
	CA      string `yaml:"ca"` // installer-issued ca.pem, served to clients over GetCACert (bootstrap)
	Disable bool   `yaml:"disable"`
}

// ConsoleConfig configures the local console HTTP listener that hosts the
// BFF auth endpoints, the embedded SPA, and the cookie-gated admin/API
// surface. It binds 127.0.0.1 only (loopback OAuth redirect + security
// invariant); the bind host is not configurable. TLS is never used (plain
// loopback HTTP). Disabled by default so a headless daemon stays gRPC-only.
type ConsoleConfig struct {
	Enabled bool `yaml:"enabled"`
	Port    int  `yaml:"port"` // default 8787
	// FrontendDir, when set, serves the SPA from that directory; empty serves
	// the binary's embedded build.
	FrontendDir string `yaml:"frontend_dir"`
	// DBPath is the SQLite file backing the console session store; it holds
	// the runespace-cloud session token at rest (kept 0600). Defaults beside
	// tokens_file.
	DBPath string `yaml:"db_path"`
}

// CloudConfig points the BFF at the runespace-cloud control plane: APIBaseURL
// is the server-to-server API origin, WebBaseURL the visible /signin page the
// browser is routed through.
type CloudConfig struct {
	APIBaseURL string `yaml:"api_base_url"` // e.g. https://api.runespace.click
	WebBaseURL string `yaml:"web_base_url"` // e.g. https://runespace.click
}

// ConsolePort returns the console listener port, defaulting to 8787.
func (c *Config) ConsolePort() int {
	if c.Server.Console.Port == 0 {
		return 8787
	}
	return c.Server.Console.Port
}

// ConsoleDBPath returns the session-store path, defaulting beside tokens_file.
func (c *Config) ConsoleDBPath() string {
	if c.Server.Console.DBPath != "" {
		return c.Server.Console.DBPath
	}
	return filepath.Join(filepath.Dir(c.Tokens.TokensFile), "console-session.db")
}

type KeysConfig struct {
	Path         string `yaml:"path"`
	EmbeddingDim int    `yaml:"embedding_dim"`
}

// RunespaceConfig points the data-plane engine at the runespace it talks to:
// the gRPC endpoint plus an optional access token (inline api_key or an
// api_key_file at 0600; if both are set, api_key_file wins and Resolve()
// materialises it into APIKey). Insecure dials plaintext (local dev only).
//
// This is a static endpoint today; the per-user, provisioned runespace
// connection (session -> bootstrap -> access JWT -> dial) is the pending
// data-plane layer.
type RunespaceConfig struct {
	Endpoint   string `yaml:"endpoint"`
	APIKey     string `yaml:"api_key"`
	APIKeyFile string `yaml:"api_key_file"`
	Insecure   bool   `yaml:"insecure"` // true = plaintext dial (local dev)
}

type TokensConfig struct {
	TeamSecret     string `yaml:"team_secret"`
	TeamSecretFile string `yaml:"team_secret_file"`
	TokensFile     string `yaml:"tokens_file"`
}

// GroupsConfig configures the group RBAC store: top_k caps default to plan
// §5 values (read=10, write and above=50). The org admin is NOT configured
// here: it is derived from the first-login console owner (a config-declared
// org_admins entry was removed — LoadConfig turns a leftover line into
// migration guidance).
type GroupsConfig struct {
	TopKRead  int `yaml:"topk_read"`
	TopKWrite int `yaml:"topk_write"`
}

// MembersConfig configures the member registry + invite flow. Every field is
// optional: the TTL defaults to 24 hours and the mail log defaults beside the
// other store artifacts.
type MembersConfig struct {
	InviteTTLMinutes int    `yaml:"invite_ttl_minutes"` // default: 1440 = 24h (§8.3 wrap TTL as revised 2026-07-13: console UX policy adopted, residual risk offset by revoke/rotate)
	ConsoleEndpoint  string `yaml:"console_endpoint"`   // ridden in the invite mail (conn info)
	CAPemURL         string `yaml:"ca_pem_url"`
	CAPemSHA256      string `yaml:"ca_pem_sha256"`
	MailLogFile      string `yaml:"mail_log_file"` // LogMailer output; default: invite-mail.log
}

// InviteTTL returns the wrap TTL, defaulting to 24 hours when unset — the
// invite-code lifetime the console UX promises ("valid for 24 hours from
// issue", §8.3 [high] as revised 2026-07-13; per-request override stays
// available via the invite endpoint's ttl_minutes).
func (c *Config) InviteTTL() time.Duration {
	m := c.Members.InviteTTLMinutes
	if m <= 0 {
		m = 1440
	}
	return time.Duration(m) * time.Minute
}

// MailLogFile returns the LogMailer output path, defaulting beside tokens_file.
func (c *Config) MailLogFile() string {
	if c.Members.MailLogFile != "" {
		return c.Members.MailLogFile
	}
	return filepath.Join(filepath.Dir(c.Tokens.TokensFile), "invite-mail.log")
}

// AuditConfig.Mode is one of: "", "file", "stdout", "file+stdout".
// Empty disables audit logging.
type AuditConfig struct {
	Mode string `yaml:"mode"`
	Path string `yaml:"path"`
}

// StorageConfig configures the unified store database (runeconsole.db) that
// replaces the per-store YAML files. The whole section is optional: when
// db_path is unset the file defaults beside tokens_file, like every other
// store artifact. Deliberately never written into generated or example
// configs' active lines — LoadConfig decodes strictly (KnownFields), so a
// config carrying storage: would make a pre-migration binary refuse to
// start and break the documented rollback path.
type StorageConfig struct {
	DBPath string `yaml:"db_path"`
}

// StoreDBPath returns the unified store database path, defaulting to
// runeconsole.db beside tokens_file (the de-facto data directory every other
// store path derives from).
func (c *Config) StoreDBPath() string {
	if c.Storage.DBPath != "" {
		return c.Storage.DBPath
	}
	return filepath.Join(filepath.Dir(c.Tokens.TokensFile), "runeconsole.db")
}

// LoadConfig resolves the config path (caller override → ConfigLookupPaths)
// and decodes the YAML at that location. The returned Config has
// *_file indirection materialised into the corresponding inline fields
// and Source set to the resolved absolute path.
//
// Missing config produces an error that names every path probed so the
// operator can copy the example file into place.
func LoadConfig(override string) (*Config, error) {
	path, searched, err := resolveConfigPath(override)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	var cfg Config
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		// groups.org_admins was removed when admin identity moved to the
		// first-login console owner; KnownFields turns a leftover line into a
		// hard parse error, so translate it into migration guidance instead
		// of a bare "field not found".
		if strings.Contains(err.Error(), "field org_admins not found") {
			return nil, fmt.Errorf("parse config %s: groups.org_admins is no longer supported — the org admin is the account that first logs in to the console; remove the org_admins entry from this file and restart: %w", path, err)
		}
		return nil, fmt.Errorf("parse config %s: %w (searched: %s)", path, err, strings.Join(searched, ", "))
	}
	cfg.Source = path

	if err := checkSecretMode(path, "runeconsole.conf"); err != nil {
		return nil, err
	}

	if err := cfg.Resolve(); err != nil {
		return nil, fmt.Errorf("resolve config %s: %w", path, err)
	}
	return &cfg, nil
}

// resolveConfigPath returns the path to use plus the list of all paths
// searched (for error messages). Override wins if non-empty.
func resolveConfigPath(override string) (path string, searched []string, err error) {
	if override != "" {
		searched = append(searched, override)
		if _, statErr := os.Stat(override); statErr != nil {
			return "", searched, fmt.Errorf("config file not found at --config %s: %w", override, statErr)
		}
		abs, _ := filepath.Abs(override)
		return abs, searched, nil
	}
	for _, p := range ConfigLookupPaths {
		searched = append(searched, p)
		if _, statErr := os.Stat(p); statErr == nil {
			abs, _ := filepath.Abs(p)
			return abs, searched, nil
		}
	}
	return "", searched, fmt.Errorf("config file not found (searched: %s)", strings.Join(searched, ", "))
}

// Resolve materialises *_file indirections into their inline equivalents.
// Returns an error if any referenced secret file has a permissive mode
// (anything looser than 0o640). Idempotent.
func (c *Config) Resolve() error {
	if c.Runespace.APIKeyFile != "" {
		val, err := readSecretFile(c.Runespace.APIKeyFile, "runespace.api_key_file")
		if err != nil {
			return err
		}
		c.Runespace.APIKey = val
		c.Runespace.APIKeyFile = ""
	}
	if c.Tokens.TeamSecretFile != "" {
		val, err := readSecretFile(c.Tokens.TeamSecretFile, "tokens.team_secret_file")
		if err != nil {
			return err
		}
		c.Tokens.TeamSecret = val
		c.Tokens.TeamSecretFile = ""
	}
	return nil
}

func readSecretFile(path, label string) (string, error) {
	if err := checkSecretMode(path, label); err != nil {
		return "", err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s %s: %w", label, path, err)
	}
	return strings.TrimRight(string(b), "\n"), nil
}

// checkSecretMode returns an error if the file's mode permits any access
// beyond owner read/write and group read (i.e., any bit outside 0o640).
// A missing file is treated as "not our problem" — the caller's subsequent
// read surfaces the not-found error with the right context.
func checkSecretMode(path, label string) error {
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	mode := info.Mode().Perm()
	if mode&^expectedSecretMode != 0 {
		return fmt.Errorf("config: %s %s mode %04o is too permissive (expected at most 0640)", label, path, mode)
	}
	return nil
}

// Redact returns a copy of c with secret fields replaced by sentinel
// strings. Use this for any debug dumps, structured log payloads, or
// admin endpoints that surface configuration to operators.
func (c *Config) Redact() Config {
	out := *c
	if out.Runespace.APIKey != "" {
		out.Runespace.APIKey = "[REDACTED]"
	}
	if out.Runespace.APIKeyFile != "" {
		out.Runespace.APIKeyFile = "[REDACTED]"
	}
	if out.Tokens.TeamSecret != "" {
		out.Tokens.TeamSecret = "[REDACTED]"
	}
	if out.Tokens.TeamSecretFile != "" {
		out.Tokens.TeamSecretFile = "[REDACTED]"
	}
	return out
}

// Validate enforces invariants the daemon needs at startup.
// Returns nil for fully populated configs.
func (c *Config) Validate() error {
	var errs []string
	if c.Server.GRPC.Port == 0 {
		errs = append(errs, "server.grpc.port is required")
	}
	if c.Server.Console.Enabled && c.Cloud.APIBaseURL == "" {
		errs = append(errs, "cloud.api_base_url is required when server.console.enabled")
	}
	if !c.Server.GRPC.TLS.Disable {
		if c.Server.GRPC.TLS.Cert == "" || c.Server.GRPC.TLS.Key == "" {
			errs = append(errs, "server.grpc.tls.cert and server.grpc.tls.key are required (or set server.grpc.tls.disable=true)")
		}
	}
	if c.Keys.Path == "" {
		errs = append(errs, "keys.path is required")
	}
	if c.Keys.EmbeddingDim == 0 {
		errs = append(errs, "keys.embedding_dim is required")
	}
	if c.Tokens.TokensFile == "" {
		errs = append(errs, "tokens.tokens_file is required")
	}
	if len(errs) > 0 {
		return errors.New("config invalid:\n  - " + strings.Join(errs, "\n  - "))
	}
	return nil
}
