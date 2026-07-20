package commands

import (
	"github.com/spf13/cobra"
)

type globalFlags struct {
	configPath string
}

var globals globalFlags

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "runeconsole",
		Short:         "Rune Console daemon server",
		SilenceUsage:  true,
		SilenceErrors: true,
		CompletionOptions: cobra.CompletionOptions{
			HiddenDefaultCmd: true,
		},
	}

	cmd.PersistentFlags().StringVar(&globals.configPath, "config", "",
		"Path to runeconsole.conf (default: /opt/runeconsole/configs/runeconsole.conf, then ./runeconsole.conf)")

	cmd.AddCommand(
		newVersionCmd(),
		newUpdateCmd(),
		newUpdateAgentCmd(),
		newDaemonCmd(),
		newLogsCmd(),
	)

	return cmd
}

func Execute() error {
	return newRootCmd().Execute()
}
