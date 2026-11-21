// Package cli defines the kbcli cobra command tree.
package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/projanvil/kbcli/internal/app"
	"github.com/projanvil/kbcli/internal/config"
)

// errNoIndex is returned by commands that need an existing vector file.
var errNoIndex = errors.New("no kbcli index found; run 'kbcli index <dir>' first")

// globals holds the persistent flag values. Binding flags to a struct keeps
// lookups independent of cobra's flag-set merging across subcommands.
type globals struct {
	index string
	dir   string
}

// NewRootCmd builds the complete kbcli command tree. cfg is the active
// settings object; mutating commands (index, config set) update it in place.
func NewRootCmd(cfg *config.Config, version string) *cobra.Command {
	g := &globals{}

	root := &cobra.Command{
		Use:           "kbcli",
		Short:         "Chat with and search a local document directory",
		Long:          "kbcli indexes a directory into one portable vector file and opens a two-pane chat interface. Subcommands manage the index; running kbcli with no subcommand opens the interface.",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: false,
		Version:       version,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := g.resolveRoot("")
			if err != nil {
				return err
			}
			indexPath := g.index
			if indexPath == "" {
				indexPath = app.Discover(dir, cfg)
			}
			a, err := app.Open(cfg, dir, indexPath)
			if err != nil {
				return fmt.Errorf("open knowledge base: %w", err)
			}
			return a.TUI()
		},
	}

	root.PersistentFlags().StringVar(&g.index, "index", "", "path to the kbcli vector index file")
	root.PersistentFlags().StringVar(&g.dir, "dir", "", "working directory (default: current directory)")

	root.AddCommand(newIndexCmd(cfg, g))
	root.AddCommand(newReindexCmd(cfg, g))
	root.AddCommand(newUpdateCmd(cfg, g))
	root.AddCommand(newConfigCmd(cfg))
	root.AddCommand(newSettingsCmd(cfg, g))
	root.AddCommand(newSearchCmd(cfg, g))
	root.AddCommand(newVersionCmd(version))

	return root
}

// resolveRoot turns an explicit positional directory (or the --dir flag) into
// an absolute path.
func (g *globals) resolveRoot(explicit string) (string, error) {
	arg := explicit
	if arg == "" {
		arg = g.dir
	}
	return app.ResolveRoot(arg)
}

// openExisting resolves the working root and an index path, then opens the
// app and verifies a real index is loaded. Commands that require an existing
// index use this helper. The returned path is the one reported to the user.
func (g *globals) openExisting(cfg *config.Config) (*app.App, string, error) {
	root, err := g.resolveRoot("")
	if err != nil {
		return nil, "", err
	}
	indexPath := g.index
	if indexPath == "" {
		indexPath = app.Discover(root, cfg)
	}
	a, err := app.Open(cfg, root, indexPath)
	if err != nil {
		return nil, "", fmt.Errorf("open knowledge base: %w", err)
	}
	// Ready() reflects an actually loaded index and agent rather than a
	// discovered path, so it also covers an explicit --index pointing at a
	// missing file. A bare Reindex on an empty session would otherwise rebuild
	// an index instead of reporting that none exists.
	if !a.Ready() {
		return nil, "", errNoIndex
	}
	return a, indexPath, nil
}

// commandContext returns the command context, falling back to a background
// context when the command was invoked outside cobra's Execute path.
func commandContext(cmd *cobra.Command) context.Context {
	if ctx := cmd.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}
