package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/CryptoLabInc/rune-admin/vault/internal/commands"
	"github.com/CryptoLabInc/rune-admin/vault/internal/server"
)

func main() {
	if err := commands.Execute(); err != nil {
		// ErrRestartRequested is intentional: exit 1 silently so the service
		// manager (systemd Restart=on-failure / launchd KeepAlive) restarts
		// the process without noisy stderr output.
		if !errors.Is(err, server.ErrRestartRequested) {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
}
