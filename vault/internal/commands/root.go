package commands

import (
	"github.com/spf13/cobra"
)

type globalFlags struct {
	configPath  string
	adminSocket string
}

var globals globalFlags

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "runevault",
		Short:         "Rune Vault daemon server with admin CLI",
		SilenceUsage:  true,
		SilenceErrors: true,
		CompletionOptions: cobra.CompletionOptions{
			HiddenDefaultCmd: true,
		},
	}

	cmd.PersistentFlags().StringVar(&globals.configPath, "config", "",
		"Path to runevault.conf (default: /opt/runevault/configs/runevault.conf, then ./runevault.conf)")
	cmd.PersistentFlags().StringVar(&globals.adminSocket, "admin-socket", "",
		"Override server.admin.socket from config")

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
