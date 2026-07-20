package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/CryptoLabInc/rune-console/internal/server"
	"github.com/CryptoLabInc/rune-console/internal/updater"
)

type updateOptions struct {
	version    string
	archive    string
	checksums  string
	check      bool
	dryRun     bool
	backupDir  string
	binaryPath string
}

type updateCommandRunner func(context.Context, io.Writer, updateOptions) error

func newUpdateCmd() *cobra.Command {
	return newUpdateCmdWithRunner(runUpdate)
}

func newUpdateCmdWithRunner(runner updateCommandRunner) *cobra.Command {
	var opts updateOptions
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Safely update the runeconsole service binary",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			offlineCount := 0
			for _, value := range []string{opts.archive, opts.checksums} {
				if value != "" {
					offlineCount++
				}
			}
			if offlineCount > 0 && (opts.archive == "" || opts.checksums == "" || opts.version == "") {
				return errors.New("offline update requires --archive, --checksums, and --version together")
			}
			return runner(cmd.Context(), cmd.OutOrStdout(), opts)
		},
	}
	cmd.Flags().StringVar(&opts.version, "version", "", "Release tag to install (default: latest)")
	cmd.Flags().StringVar(&opts.archive, "archive", "", "Offline release archive (.tar.gz)")
	cmd.Flags().StringVar(&opts.checksums, "checksums", "", "Offline SHA256SUMS file")
	cmd.Flags().BoolVar(&opts.check, "check", false, "Verify the target release without changing the service")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "Alias for --check")
	cmd.Flags().StringVar(&opts.backupDir, "backup-dir", updater.DefaultBackupDir, "Private directory for durable-state backups")
	cmd.Flags().StringVar(&opts.binaryPath, "binary-path", "", "Explicit service binary path (required when invoking a copied CLI)")
	return cmd
}

func runUpdate(ctx context.Context, output io.Writer, opts updateOptions) error {
	ctx, stopSignals := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	checkOnly := opts.check || opts.dryRun
	if !checkOnly && os.Geteuid() != 0 {
		return errors.New("runeconsole update must run as root (use sudo); --check is read-only")
	}
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return fmt.Errorf("runeconsole update is unsupported on %s", runtime.GOOS)
	}

	cfg, err := server.LoadConfig(globals.configPath)
	if err != nil {
		return err
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return errors.New("identify running runeconsole executable failed")
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return errors.New("resolve running runeconsole executable failed")
	}

	binaryPath := executable
	if !checkOnly {
		binaryPath, err = updater.ResolveServiceBinary(ctx, updater.BinaryResolutionOptions{
			GOOS:               runtime.GOOS,
			ExplicitPath:       opts.binaryPath,
			ExecutablePath:     executable,
			ExpectedConfigPath: cfg.Source,
		})
		if err != nil {
			return err
		}
	}

	var source updater.ReleaseSource
	if opts.archive != "" || opts.checksums != "" {
		source = updater.LocalSource{
			ArchivePath:   opts.archive,
			ChecksumsPath: opts.checksums,
			Version:       opts.version,
		}
	} else {
		source = updater.GitHubSource{}
	}

	statePaths := durableStatePaths(cfg)
	health := updater.HealthProbe{}
	if cfg.Server.Console.Enabled {
		health.HTTPURL = "http://127.0.0.1:" + strconv.Itoa(cfg.ConsolePort()) + "/healthz"
	} else {
		health.TCPAddr = net.JoinHostPort("127.0.0.1", strconv.Itoa(cfg.Server.GRPC.Port))
	}
	engine := updater.Engine{
		Source: source,
		Service: updater.SystemService{
			GOOS: runtime.GOOS,
		},
		Health:   health,
		Locker:   updater.FileLocker{Path: filepath.Join(opts.backupDir, "update.lock")},
		Verifier: updater.BuildInfoVerifier{},
	}
	result, err := engine.Run(ctx, updater.Request{
		CurrentVersion:   buildVersion,
		RequestedVersion: opts.version,
		GOOS:             runtime.GOOS,
		GOARCH:           runtime.GOARCH,
		BinaryPath:       binaryPath,
		PreviousPath:     binaryPath + ".previous",
		BackupDir:        opts.backupDir,
		StatePaths:       statePaths,
		ManagedRoots:     []string{cfg.Storage.DataDir, cfg.Keys.Path},
		ImmutablePaths:   []string{cfg.Source},
		CheckOnly:        checkOnly,
	})
	if err != nil {
		return err
	}
	switch {
	case result.Checked && checkOnly:
		fmt.Fprintf(output, "Verified runeconsole %s for %s/%s; no local state was changed.\n", result.TargetVersion, runtime.GOOS, runtime.GOARCH)
	case result.AlreadyCurrent:
		fmt.Fprintf(output, "runeconsole %s is already installed; release integrity was verified.\n", result.TargetVersion)
	case result.Updated:
		fmt.Fprintf(output, "Updated runeconsole %s to %s; configuration and durable data were preserved. Backup: %s\n", result.CurrentVersion, result.TargetVersion, result.BackupPath)
	default:
		return errors.New("update finished without a terminal result")
	}
	return nil
}

func durableStatePaths(cfg *server.Config) []string {
	paths := []string{cfg.Source, cfg.Storage.DataDir}
	for _, database := range []string{cfg.StoreDBPath(), cfg.ConsoleDBPath()} {
		paths = append(paths, database, database+"-wal", database+"-shm")
	}
	paths = append(paths,
		cfg.Keys.Path,
		cfg.Server.GRPC.TLS.Cert,
		cfg.Server.GRPC.TLS.Key,
		cfg.Server.GRPC.TLS.CA,
	)
	filtered := paths[:0]
	for _, path := range paths {
		if path != "" {
			filtered = append(filtered, path)
		}
	}
	return filtered
}
