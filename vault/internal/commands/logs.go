package commands

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/CryptoLabInc/rune-admin/vault/internal/server"
)

// newLogsCmd returns the "logs" subcommand which tails the daemon log output.
// On Linux it delegates to journalctl; on macOS it tails the service stderr file.
func newLogsCmd() *cobra.Command {
	var follow bool
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Show daemon log output",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLogs(follow)
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow log output (like tail -f)")
	return cmd
}

func runLogs(follow bool) error {
	if runtime.GOOS == "linux" {
		args := []string{"-u", "runevault", "--no-pager"}
		if follow {
			args = append(args, "-f")
		}
		c := exec.Command("journalctl", args...)
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		return c.Run()
	}

	cfg, err := server.LoadConfig(globals.configPath)
	if err != nil {
		return err
	}
	logPath := daemonStderrLogPath(cfg)

	var args []string
	if follow {
		args = append(args, "-f")
	}
	args = append(args, logPath)
	c := exec.Command("tail", args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

// daemonStderrLogPath derives the launchd stderr log path from the config
// source location: /opt/runevault/configs/runevault.conf → /opt/runevault/logs/runevault.stderr.log
func daemonStderrLogPath(cfg *server.Config) string {
	if cfg.Source != "" {
		prefix := filepath.Dir(filepath.Dir(cfg.Source))
		return filepath.Join(prefix, "logs", "runevault.stderr.log")
	}
	return "/opt/runevault/logs/runevault.stderr.log"
}
