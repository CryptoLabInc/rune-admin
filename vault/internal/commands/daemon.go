package commands

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/CryptoLabInc/rune-admin/vault/internal/crypto"
	"github.com/CryptoLabInc/rune-admin/vault/internal/server"
	"github.com/CryptoLabInc/rune-admin/vault/internal/tokens"
)

func newDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the runevault daemon process",
	}
	cmd.AddCommand(
		newDaemonStartCmd(),
		newDaemonStopCmd(),
		newDaemonRestartCmd(),
	)
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

	pid, err := server.AcquirePIDFile(cfg.Daemon.PIDFile)
	if err != nil {
		return err
	}
	defer pid.Release()

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

func newDaemonStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Signal a running daemon to terminate",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDaemonStop(cmd)
		},
	}
}

func runDaemonStop(cmd *cobra.Command) error {
	cfg, err := server.LoadConfig(globals.configPath)
	if err != nil {
		return err
	}
	timeout, err := time.ParseDuration(globals.timeout)
	if err != nil {
		return fmt.Errorf("invalid --timeout %q: %w", globals.timeout, err)
	}

	pid, err := server.ReadPIDFile(cfg.Daemon.PIDFile)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no PID file at %s — daemon not running?", cfg.Daemon.PIDFile)
		}
		return err
	}
	if !server.PIDLive(pid) {
		return fmt.Errorf("PID %d not alive (stale PID file %s)", pid, cfg.Daemon.PIDFile)
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return fmt.Errorf("send SIGTERM to %d: %w", pid, err)
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !server.PIDLive(pid) {
			fmt.Fprintf(cmd.OutOrStdout(), "Daemon (pid %d) stopped.\n", pid)
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("daemon (pid %d) did not exit within %s", pid, timeout)
}

func newDaemonRestartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restart",
		Short: "Stop then start the daemon",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := runDaemonStop(cmd); err != nil {
				// "not running" is fine for restart — just continue to start.
				if !errors.Is(err, os.ErrNotExist) && !isNotRunning(err) {
					return err
				}
				slog.Warn("daemon: stop reported", "err", err)
			}
			return runDaemonStart(cmd.Context())
		},
	}
}

func isNotRunning(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return contains(msg, "no PID file") || contains(msg, "not alive")
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// signalCancellableContext wraps ctx so SIGTERM/SIGINT cancel it. Used
// internally in tests; the daemon itself relies on server.Serve's signal
// handling.
func signalCancellableContext(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		select {
		case <-ctx.Done():
		case <-ch:
			cancel()
		}
		signal.Stop(ch)
	}()
	return ctx, cancel
}
