// Command kbcli is a local knowledge-base chat application. The CLI and the
// TUI are one program: subcommands manage the vector index, and running the
// bare command opens the two-pane chat interface.
package main

import (
	"fmt"
	"os"
	"runtime/debug"

	"github.com/ProjAnvil/kbcli/internal/cli"
	"github.com/ProjAnvil/kbcli/internal/config"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "dev"

// effectiveVersion prefers an ldflags-injected version. For a plain
// `go install pkg@version` build there is no injection, so fall back to the
// module version recorded in the binary's build info.
func effectiveVersion() string {
	if version != "" && version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return version
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "kbcli:", err)
		os.Exit(1)
	}
	if err := cli.NewRootCmd(cfg, effectiveVersion()).Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "kbcli:", err)
		os.Exit(1)
	}
}
