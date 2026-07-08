package crypto

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	runespace "github.com/jh-lee-cryptolab/runespace-go-sdk"
)

// KeysParams names the on-disk key bundle and FHE dimension.
type KeysParams struct {
	// Root is the parent directory containing <KeyID>/.
	// E.g., "/opt/runevault/vault-keys" with KeyID "vault-key" reads from
	// "/opt/runevault/vault-keys/vault-key/{Enc,Sec,Eval}Key(.json|.bin)".
	Root  string
	KeyID string
	Dim   int
}

func (p KeysParams) keyDir() string { return filepath.Join(p.Root, p.KeyID) }

// KeysExist reports whether the bundle is present under Root/KeyID.
func KeysExist(p KeysParams) bool {
	return runespace.KeysExist(
		runespace.WithKeyPath(p.keyDir()),
		runespace.WithKeyID(p.KeyID),
		runespace.WithKeyDim(p.Dim),
	)
}

// EnsureKeys generates a fresh key set (Enc/Eval/Sec, RMP+MM) if none exists.
// No-op if a bundle is already present (GenerateKeys never overwrites).
func EnsureKeys(p KeysParams) error {
	if KeysExist(p) {
		return nil
	}
	if err := os.MkdirAll(p.keyDir(), 0o700); err != nil {
		return fmt.Errorf("crypto: mkdir key dir: %w", err)
	}
	if err := runespace.GenerateKeys(
		runespace.WithKeyPath(p.keyDir()),
		runespace.WithKeyID(p.KeyID),
		runespace.WithKeyDim(p.Dim),
	); err != nil && !errors.Is(err, runespace.ErrKeysAlreadyExist) {
		return fmt.Errorf("crypto: generate keys: %w", err)
	}
	return nil
}

// EngineParams configures the vault's runespace client.
type EngineParams struct {
	Keys     KeysParams
	Endpoint string // runespace gRPC address
	Token    string // optional runespace access token
	Insecure bool   // true for local plaintext dev
}

// Engine is the vault's runespace client. It owns the full FHE key set
// (Enc+Eval+Sec) and the gRPC connection, and is the SOLE talker to the
// runespace engine: Insert encrypts locally then appends; Search sends the
// plaintext query, decrypts the score blobs, and returns ranked hits.
type Engine struct {
	client *runespace.Client
	keys   *runespace.Keys
}

// OpenEngine opens the full key set, dials runespace, and makes sure the eval
// key is registered (RegisterKeys on first run, UseKeys thereafter).
func OpenEngine(ctx context.Context, p EngineParams) (*Engine, error) {
	keys, err := runespace.OpenKeys(
		runespace.WithKeyPath(p.Keys.keyDir()),
		runespace.WithKeyID(p.Keys.KeyID),
		runespace.WithKeyDim(p.Keys.Dim),
	) // default parts: Enc (encrypt) + Sec (decrypt); eval streamed by RegisterKeys
	if err != nil {
		return nil, fmt.Errorf("crypto: open keys: %w", err)
	}

	opts := []runespace.ClientOption{}
	if p.Token != "" {
		opts = append(opts, runespace.WithAccessToken(p.Token))
	}
	if p.Insecure {
		opts = append(opts, runespace.WithInsecure())
	}
	client, err := runespace.Dial(p.Endpoint, opts...)
	if err != nil {
		_ = keys.Close()
		return nil, fmt.Errorf("crypto: dial runespace %s: %w", p.Endpoint, err)
	}

	// Register the eval key once; if the engine already serves this key set,
	// just bind locally (UseKeys) so restarts are idempotent.
	if info, ierr := client.Info(ctx); ierr == nil && len(info.RegisteredKeys) > 0 {
		client.UseKeys(keys)
	} else if err := client.RegisterKeys(ctx, keys); err != nil {
		_ = client.Close()
		_ = keys.Close()
		return nil, fmt.Errorf("crypto: register keys: %w", err)
	}

	return &Engine{client: client, keys: keys}, nil
}

// Dim returns the FHE slot dimension the key set was opened with.
func (e *Engine) Dim() int { return e.keys.Dim() }

// Insert encrypts vec (EncKey, RMP+MM) and appends it under a fresh opaque id,
// which it returns. sealedMeta is stored verbatim in the manifest — the caller
// is responsible for sealing it (agent_dek) beforehand.
func (e *Engine) Insert(ctx context.Context, vec []float32, sealedMeta string) (string, error) {
	return e.client.Insert(ctx, vec, sealedMeta)
}

// SearchHit is one decrypted, ranked result. Metadata is the stored envelope
// (still sealed); the caller opens it with the agent_dek.
type SearchHit struct {
	ID       string
	Score    float64
	Metadata string
}

// Search runs the blind search (plaintext query → decrypted scores → ranked
// hits with metadata resolved).
func (e *Engine) Search(ctx context.Context, vec []float32, topK int) ([]SearchHit, error) {
	matches, err := e.client.Search(ctx, vec, topK)
	if err != nil {
		return nil, err
	}
	out := make([]SearchHit, 0, len(matches))
	for _, m := range matches {
		out = append(out, SearchHit{ID: m.ID, Score: m.Score, Metadata: m.Metadata})
	}
	return out, nil
}

// Close releases the gRPC connection and the cgo key handles. Idempotent.
func (e *Engine) Close() error {
	var first error
	if e.client != nil {
		if err := e.client.Close(); err != nil {
			first = err
		}
		e.client = nil
	}
	if e.keys != nil {
		if err := e.keys.Close(); err != nil && first == nil {
			first = err
		}
		e.keys = nil
	}
	return first
}
