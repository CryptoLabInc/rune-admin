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

	"gopkg.in/yaml.v3"
)

// ConfigLookupPaths lists, in priority order, the on-disk locations that
// LoadConfig probes when the caller doesn't pass an explicit path.
var ConfigLookupPaths = []string{
	"/opt/runevault/configs/runevault.conf",
	"./runevault.conf",
}

// 0640: group-readable so runevault group members can run CLI commands without sudo.
const expectedSecretMode fs.FileMode = 0o640

// Config is the in-memory shape of runevault.conf. Field names follow the
// YAML schema exactly so the loader can decode without an intermediate type.
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Keys     KeysConfig     `yaml:"keys"`
	Envector EnvectorConfig `yaml:"envector"`
	Tokens   TokensConfig   `yaml:"tokens"`
	Audit    AuditConfig    `yaml:"audit"`

	// Source records where this Config was loaded from (resolved absolute
	// path), populated by LoadConfig. Empty for in-memory test configs.
	Source string `yaml:"-" json:"-"`
}

type ServerConfig struct {
	GRPC  GRPCConfig  `yaml:"grpc"`
	Admin AdminConfig `yaml:"admin"`
}

type GRPCConfig struct {
	Host string    `yaml:"host"`
	Port int       `yaml:"port"`
	TLS  TLSConfig `yaml:"tls"`
}

type TLSConfig struct {
	Cert    string `yaml:"cert"`
	Key     string `yaml:"key"`
	Disable bool   `yaml:"disable"`
}

type AdminConfig struct {
	Socket string `yaml:"socket"`
}

type KeysConfig struct {
	Path         string `yaml:"path"`
	IndexName    string `yaml:"index_name"`
	EmbeddingDim int    `yaml:"embedding_dim"`
}

// EnvectorConfig accepts either an inline api_key or an api_key_file
// pointing at a 0600-mode file containing the same value. If both are
// set, api_key_file wins. Resolve() materialises the final string into
// APIKey and clears APIKeyFile.
type EnvectorConfig struct {
	Endpoint   string `yaml:"endpoint"`
	APIKey     string `yaml:"api_key"`
	APIKeyFile string `yaml:"api_key_file"`
}

type TokensConfig struct {
	TeamSecret     string `yaml:"team_secret"`
	TeamSecretFile string `yaml:"team_secret_file"`
	RolesFile      string `yaml:"roles_file"`
	TokensFile     string `yaml:"tokens_file"`
}

// AuditConfig.Mode is one of: "", "file", "stdout", "file+stdout".
// Empty disables audit logging.
type AuditConfig struct {
	Mode string `yaml:"mode"`
	Path string `yaml:"path"`
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
		return nil, fmt.Errorf("parse config %s: %w (searched: %s)", path, err, strings.Join(searched, ", "))
	}
	cfg.Source = path

	if err := checkSecretMode(path, "runevault.conf"); err != nil {
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
	if c.Envector.APIKeyFile != "" {
		val, err := readSecretFile(c.Envector.APIKeyFile, "envector.api_key_file")
		if err != nil {
			return err
		}
		c.Envector.APIKey = val
		c.Envector.APIKeyFile = ""
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
	if out.Envector.APIKey != "" {
		out.Envector.APIKey = "[REDACTED]"
	}
	if out.Envector.APIKeyFile != "" {
		out.Envector.APIKeyFile = "[REDACTED]"
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
	if c.Server.Admin.Socket == "" {
		errs = append(errs, "server.admin.socket is required")
	}
	if c.Server.GRPC.Port == 0 {
		errs = append(errs, "server.grpc.port is required")
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
	if c.Tokens.RolesFile == "" {
		errs = append(errs, "tokens.roles_file is required")
	}
	if c.Tokens.TokensFile == "" {
		errs = append(errs, "tokens.tokens_file is required")
	}
	if len(errs) > 0 {
		return errors.New("config invalid:\n  - " + strings.Join(errs, "\n  - "))
	}
	return nil
}
