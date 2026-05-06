package crypto

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	envector "github.com/CryptoLabInc/envector-go-sdk"
)

// EnvectorKeys is a thin wrapper around envector-go-sdk's *Keys handle that
// constrains usage to decrypt-only (KeyPartSec) — Vault never encrypts.
//
// On-disk layout matches pyenvector: <root>/<keyID>/{Enc,Sec,Eval}Key.json.
// envector-go-sdk reads pyenvector's JSON envelope natively, so existing
// installs work without migration.
type EnvectorKeys struct {
	keys *envector.Keys
}

// KeysParams names the on-disk key bundle and FHE dimension.
type KeysParams struct {
	// Root is the parent directory containing <KeyID>/.
	// E.g., "/opt/runevault/vault-keys" with KeyID "vault-key" reads from
	// "/opt/runevault/vault-keys/vault-key/{Enc,Sec,Eval}Key.json".
	Root  string
	KeyID string
	Dim   int
}

func (p KeysParams) keyDir() string { return filepath.Join(p.Root, p.KeyID) }

// KeysExist reports whether the bundle is present under Root/KeyID.
func KeysExist(p KeysParams) bool {
	return envector.KeysExist(
		envector.WithKeyPath(p.keyDir()),
		envector.WithKeyID(p.KeyID),
		envector.WithKeyDim(p.Dim),
	)
}

// EnsureKeys generates a fresh bundle if none exists. No-op if any of the
// three slots is already present (envector.GenerateKeys never overwrites).
func EnsureKeys(p KeysParams) error {
	if KeysExist(p) {
		return nil
	}
	if err := os.MkdirAll(p.keyDir(), 0o700); err != nil {
		return fmt.Errorf("crypto: mkdir key dir: %w", err)
	}
	if err := envector.GenerateKeys(
		envector.WithKeyPath(p.keyDir()),
		envector.WithKeyID(p.KeyID),
		envector.WithKeyDim(p.Dim),
	); err != nil && !errors.Is(err, envector.ErrKeysAlreadyExist) {
		return fmt.Errorf("crypto: generate keys: %w", err)
	}
	return nil
}

// OpenSecretKey loads SecKey.json only — Vault is decrypt-only.
// Returns EnvectorKeys whose Decrypt method is wired to envector-go-sdk's CKKS
// decryptor; encryption is unavailable.
func OpenSecretKey(p KeysParams) (*EnvectorKeys, error) {
	k, err := envector.OpenKeysFromFile(
		envector.WithKeyPath(p.keyDir()),
		envector.WithKeyID(p.KeyID),
		envector.WithKeyDim(p.Dim),
		envector.WithKeyParts(envector.KeyPartSec),
	)
	if err != nil {
		return nil, fmt.Errorf("crypto: open sec key: %w", err)
	}
	return &EnvectorKeys{keys: k}, nil
}

// Decrypt unpacks a CiphertextScore proto blob into per-shard score
// vectors. The returned slices are aligned: scores[i] is the score vector
// for shard shardIdx[i]. The gRPC layer flattens these into ScoreEntry
// rows and applies Top-K.
func (f *EnvectorKeys) Decrypt(blob []byte) (scores [][]float64, shardIdx []int32, err error) {
	if f == nil || f.keys == nil {
		return nil, nil, errors.New("crypto: EnvectorKeys closed")
	}
	return f.keys.Decrypt(blob)
}

// ReadEncKey reads EncKey.json from disk and returns its contents verbatim
// for inclusion in the GetAgentManifest gRPC response.
func ReadEncKey(p KeysParams) (string, error) {
	enc, err := os.ReadFile(filepath.Join(p.keyDir(), "EncKey.json"))
	if err != nil {
		return "", fmt.Errorf("crypto: read EncKey.json: %w", err)
	}
	return string(enc), nil
}

func (f *EnvectorKeys) Close() error {
	if f == nil || f.keys == nil {
		return nil
	}
	err := f.keys.Close()
	f.keys = nil
	return err
}
