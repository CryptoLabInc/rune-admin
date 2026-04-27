package commands

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

var (
	buildVersion = "dev"
	buildCommit  = "none"
	buildDate    = "unknown"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print runevault version (works without daemon or socket)",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintf(cmd.OutOrStdout(),
				"runevault %s (commit %s, built %s, %s/%s, %s)\n",
				buildVersion, buildCommit, buildDate,
				runtime.GOOS, runtime.GOARCH, runtime.Version())
			return nil
		},
	}
}
