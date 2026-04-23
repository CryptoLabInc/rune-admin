package commands

import (
	"github.com/spf13/cobra"
)

type globalFlags struct {
	configPath  string
	adminSocket string
	timeout     string
}

var globals globalFlags

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "runevault",
		Short:         "Rune Vault daemon server with admin CLI",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.PersistentFlags().StringVar(&globals.configPath, "config", "",
		"Path to runevault.conf (default: /opt/rune-vault/configs/runevault.conf, then ./runevault.conf)")
	cmd.PersistentFlags().StringVar(&globals.adminSocket, "admin-socket", "",
		"Override server.admin.socket from config")
	cmd.PersistentFlags().StringVar(&globals.timeout, "timeout", "10s",
		"Operation timeout (e.g. 10s, 1m)")

	cmd.AddCommand(
		newVersionCmd(),
		newDaemonCmd(),
		newTokenCmd(),
		newRoleCmd(),
		newStatusCmd(),
	)

	return cmd
}

func Execute() error {
	return newRootCmd().Execute()
}
