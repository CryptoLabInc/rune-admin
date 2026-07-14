package commands

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/CryptoLabInc/rune-console/runeconsole/internal/crypto"
	"github.com/CryptoLabInc/rune-console/runeconsole/internal/server"
	"github.com/CryptoLabInc/rune-console/runeconsole/internal/tokens"
)

func newDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "daemon",
		Short:  "Manage the runeconsole daemon process",
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
	// Open the full key set, dial runespace, and register the eval key.
	eng, err := crypto.OpenEngine(ctx, crypto.EngineParams{
		Keys:     keyParams,
		Endpoint: cfg.Runespace.Endpoint,
		Token:    cfg.Runespace.Token,
		Insecure: cfg.Runespace.Insecure, // TLS by default; set runespace.insecure=true only for a localhost runespace
	})
	if err != nil {
		return fmt.Errorf("daemon: open runespace engine: %w", err)
	}
	defer eng.Close()

	audit, err := server.NewAuditLogger(cfg.Audit)
	if err != nil {
		return err
	}
	defer audit.Close()

	v := server.NewConsole(cfg, store, eng, audit)
	defer v.Close()

	slog.Info("console: starting daemon",
		"pid", os.Getpid(),
		"config", cfg.Source,
		"grpc_addr", fmt.Sprintf("%s:%d", cfg.Server.GRPC.Host, cfg.Server.GRPC.Port),
		"admin_socket", cfg.Server.Admin.Socket)

	return server.Serve(ctx, v, server.AdminFromConfig)
}
