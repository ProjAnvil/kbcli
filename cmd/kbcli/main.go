// Command kbcli is a local knowledge-base chat application. The CLI and the
// TUI are one program: subcommands manage the vector index, and running the
// bare command opens the two-pane chat interface.
package main

import (
	"fmt"
	"os"

	"github.com/projanvil/kbcli/internal/cli"
	"github.com/projanvil/kbcli/internal/config"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "kbcli:", err)
		os.Exit(1)
	}
	if err := cli.NewRootCmd(cfg, version).Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "kbcli:", err)
		os.Exit(1)
	}
}
