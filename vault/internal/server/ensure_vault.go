package server

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"

	envector "github.com/CryptoLabInc/envector-go-sdk"
)

// EnsureVault connects to enVector Cloud and idempotently performs the two
// cloud-side setup steps that the Python vault_core.ensure_vault() ran at
// startup before the gRPC server began accepting requests:
//
//  1. ActivateKeys — registers the EvalKey bundle if absent, unloads any
//     other resident key, then loads the target key (4-RPC sequence in the
//     SDK, already idempotent).
//  2. Index — creates the team index if it does not yet exist; no-op when
//     the index is already present.
//
// Returns nil immediately (offline mode) when envector.endpoint or
// envector.api_key is unset, matching the Python "warn and skip" behaviour.
func EnsureVault(ctx context.Context, cfg *Config) error {
	if cfg.Envector.Endpoint == "" || cfg.Envector.APIKey == "" {
		slog.Warn("vault: envector.endpoint / envector.api_key not set — skipping cloud key registration and index setup (offline mode)")
		return nil
	}
	if cfg.Keys.IndexName == "" {
		slog.Warn("vault: keys.index_name not set — skipping index creation")
		return nil
	}

	slog.Info("vault: connecting to enVector Cloud", "endpoint", cfg.Envector.Endpoint)

	client, err := envector.NewClient(
		envector.WithAddress(cfg.Envector.Endpoint),
		envector.WithAccessToken(cfg.Envector.APIKey),
	)
	if err != nil {
		return fmt.Errorf("ensure vault: dial enVector: %w", err)
	}
	defer client.Close()

	// Only EvalKey is needed: ActivateKeys uploads it to the cloud, and
	// Index/createIndex uses Dim() and ID() which are set from options
	// regardless of which key parts are loaded.
	keyDir := filepath.Join(cfg.Keys.Path, defaultKeyID(cfg))
	keys, err := envector.OpenKeysFromFile(
		envector.WithKeyPath(keyDir),
		envector.WithKeyID(defaultKeyID(cfg)),
		envector.WithKeyDim(cfg.Keys.EmbeddingDim),
		envector.WithKeyParts(envector.KeyPartEval),
	)
	if err != nil {
		return fmt.Errorf("ensure vault: open eval key: %w", err)
	}
	defer keys.Close()

	slog.Info("vault: activating FHE keys on enVector Cloud", "key_id", defaultKeyID(cfg))
	if err := client.ActivateKeys(ctx, keys); err != nil {
		return fmt.Errorf("ensure vault: activate keys: %w", err)
	}
	slog.Info("vault: FHE keys activated")

	slog.Info("vault: ensuring team index", "index", cfg.Keys.IndexName)
	if _, err := client.Index(ctx,
		envector.WithIndexName(cfg.Keys.IndexName),
		envector.WithIndexKeys(keys),
	); err != nil {
		return fmt.Errorf("ensure vault: ensure index: %w", err)
	}
	slog.Info("vault: team index ready", "index", cfg.Keys.IndexName)

	return nil
}
