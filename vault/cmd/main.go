package main

import (
	"fmt"
	"os"

	"github.com/CryptoLabInc/rune-console/vault/internal/commands"
)

func main() {
	if err := commands.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
