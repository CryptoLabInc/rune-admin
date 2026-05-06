package commands

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/CryptoLabInc/rune-admin/vault/internal/crypto"
	"github.com/CryptoLabInc/rune-admin/vault/internal/server"
	"github.com/CryptoLabInc/rune-admin/vault/internal/tokens"
)

func newDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "daemon",
		Short:  "Manage the runevault daemon process",
		Hidden: true,
	}
	cmd.AddCommand(newDaemonStartCmd())
	return cmd
}

func newDaemonStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the daemon in the foreground",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDaemonStart(cmd.Context())
		},
	}
}

func runDaemonStart(ctx context.Context) error {
	cfg, err := server.LoadConfig(globals.configPath)
	if err != nil {
		return err
	}
	if globals.adminSocket != "" {
		cfg.Server.Admin.Socket = globals.adminSocket
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	store := tokens.NewStore()
	if err := store.LoadFromFiles(cfg.Tokens.RolesFile, cfg.Tokens.TokensFile); err != nil {
		return err
	}
	defer store.Shutdown()

	keyParams := crypto.KeysParams{
		Root:  cfg.Keys.Path,
		KeyID: "vault-key",
		Dim:   cfg.Keys.EmbeddingDim,
	}
	if err := crypto.EnsureKeys(keyParams); err != nil {
		return fmt.Errorf("daemon: ensure keys: %w", err)
	}
	keys, err := crypto.OpenSecretKey(keyParams)
	if err != nil {
		return fmt.Errorf("daemon: open sec key: %w", err)
	}
	defer keys.Close()

	audit, err := server.NewAuditLogger(cfg.Audit)
	if err != nil {
		return err
	}
	defer audit.Close()

	v := server.NewVault(cfg, store, keys, audit)
	defer v.Close()

	slog.Info("vault: starting daemon",
		"pid", os.Getpid(),
		"config", cfg.Source,
		"grpc_addr", fmt.Sprintf("%s:%d", cfg.Server.GRPC.Host, cfg.Server.GRPC.Port),
		"admin_socket", cfg.Server.Admin.Socket)

	return server.Serve(ctx, v, server.AdminFromConfig)
}
