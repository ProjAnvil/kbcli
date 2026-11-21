package cli

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ProjAnvil/kbcli/internal/app"
	"github.com/ProjAnvil/kbcli/internal/config"
	"github.com/ProjAnvil/kbcli/internal/index"
	"github.com/ProjAnvil/kbcli/internal/settings"
)

// settingsStore resolves the workspace root from --dir (or cwd) and opens
// settings.json. Every settings subcommand shares this so the relation file
// and the root key are always consistent.
func (g *globals) settingsStore() (string, settings.Store, error) {
	root, err := g.resolveRoot("")
	if err != nil {
		return "", nil, err
	}
	st, err := settings.Open()
	if err != nil {
		return "", nil, fmt.Errorf("open settings: %w", err)
	}
	return root, st, nil
}

// sortedOverrideKeys returns the override keys of ws in stable order.
func sortedOverrideKeys(ws settings.Workspace) []string {
	keys := make([]string, 0, len(ws.Overrides))
	for k := range ws.Overrides {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// copyOverrides clones an override map so a command never mutates the store's
// defensive copy in place.
func copyOverrides(in map[string]any) map[string]any {
	out := make(map[string]any, len(in)+1)
	for k, v := range in {
		out[k] = v
	}
	return out
}

// toOverrideValue converts a typed config value into a type the settings store
// accepts. encoding/json yields string/float64/bool/json.Number, so a Go int
// must become float64; otherwise Resolve on the same store instance would
// reject it as an unsupported type.
func toOverrideValue(v any) any {
	if n, ok := v.(int); ok {
		return float64(n)
	}
	return v
}

// staleStatus describes whether the index for root was built with the same
// index-affecting settings as the effective configuration. It reads only the
// vector-file header, so no embedder is instantiated.
func (g *globals) staleStatus(effective *config.Config, root string) string {
	path := g.index
	if path == "" {
		path = app.Discover(root, effective)
	}
	if path == "" {
		return "index: none - run 'kbcli index <dir>' to build one"
	}
	sig, err := index.PeekBuildSignature(path)
	if err != nil {
		// An explicit --index pointing at a missing file is simply "no index
		// yet", not a corrupt vector file. PeekBuildSignature wraps os.Open
		// with %w, so os.ErrNotExist stays detectable through the chain.
		if errors.Is(err, os.ErrNotExist) {
			return "index: none - run 'kbcli index <dir>' to build one"
		}
		return fmt.Sprintf("index: build signature unreadable (%v)", err)
	}
	changed := config.IndexSignatureChanges(sig, effective)
	if len(changed) == 0 {
		return "index: up to date"
	}
	return fmt.Sprintf("index: STALE - %s changed; run 'kbcli reindex'", strings.Join(changed, ", "))
}

// newSettingsCmd builds "kbcli settings" and its children.
func newSettingsCmd(cfg *config.Config, g *globals) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "settings",
		Short: "Inspect and manage per-workspace configuration",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	pathCmd := &cobra.Command{
		Use:   "path",
		Short: "Print the settings.json path",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, st, err := g.settingsStore()
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), st.Path())
			return nil
		},
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "Print every workspace record",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, st, err := g.settingsStore()
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			records := st.List()
			if len(records) == 0 {
				fmt.Fprintln(w, "no workspace overrides")
				return nil
			}
			roots := make([]string, 0, len(records))
			for r := range records {
				roots = append(roots, r)
			}
			sort.Strings(roots)
			for _, r := range roots {
				ws := records[r]
				keys := sortedOverrideKeys(ws)
				keyText := "(none)"
				if len(keys) > 0 {
					keyText = strings.Join(keys, ", ")
				}
				fmt.Fprintf(w, "%s\n", r)
				fmt.Fprintf(w, "  mode: %s\n", ws.Mode)
				fmt.Fprintf(w, "  overrides: %s\n", keyText)
				fmt.Fprintf(w, "  index_path: %s\n", ws.IndexPath)
				fmt.Fprintf(w, "  updated_at: %s\n", ws.UpdatedAt)
			}
			return nil
		},
	}

	showCmd := &cobra.Command{
		Use:   "show",
		Short: "Print the effective config and index status for this workspace",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, st, err := g.settingsStore()
			if err != nil {
				return err
			}
			effective, err := st.Resolve(cfg, root)
			if err != nil {
				return fmt.Errorf("settings show: resolve: %w", err)
			}
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "workspace: %s\n", root)
			fmt.Fprintf(w, "mode: %s\n", st.Get(root).Mode)
			fmt.Fprintf(w, "settings file: %s\n", st.Path())
			for _, k := range config.FieldKeys() {
				fmt.Fprintf(w, "  %-28s %s\n", k, effective.ValueString(k))
			}
			fmt.Fprintln(w, g.staleStatus(effective, root))
			return nil
		},
	}

	makeModeCmd := func(name, mode, short string) *cobra.Command {
		return &cobra.Command{
			Use:   name,
			Short: short,
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				root, st, err := g.settingsStore()
				if err != nil {
					return err
				}
				ws := st.Get(root)
				ws.Mode = mode
				if err := st.Set(root, ws); err != nil {
					return fmt.Errorf("settings %s: %w", name, err)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "workspace %s now uses %s settings\n", root, mode)
				return nil
			},
		}
	}

	setCmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Add one workspace override and switch to workspace mode",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, st, err := g.settingsStore()
			if err != nil {
				return err
			}
			key, value := args[0], args[1]

			// Parse through the shared catalog so the stored value keeps its
			// concrete type and per-field validation is applied up front.
			draft := cfg.Clone()
			if err := draft.SetField(key, value); err != nil {
				return fmt.Errorf("settings set %s: %w", key, err)
			}
			typed, err := draft.GetField(key)
			if err != nil {
				return fmt.Errorf("settings set %s: %w", key, err)
			}

			ws := st.Get(root)
			overrides := copyOverrides(ws.Overrides)
			overrides[key] = toOverrideValue(typed)
			ws.Mode = settings.ModeWorkspace
			ws.Overrides = overrides
			if err := st.Set(root, ws); err != nil {
				return fmt.Errorf("settings set: %w", err)
			}
			// Resolve applies every stored override and validates the merged
			// config, so an invalid relation is reported before we return.
			if _, err := st.Resolve(cfg, root); err != nil {
				return fmt.Errorf("settings set: validate: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "set %s = %v for %s (workspace mode)\n", key, typed, root)
			return nil
		},
	}

	unsetCmd := &cobra.Command{
		Use:   "unset <key>",
		Short: "Remove one workspace override",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, st, err := g.settingsStore()
			if err != nil {
				return err
			}
			key := args[0]
			ws := st.Get(root)
			overrides := copyOverrides(ws.Overrides)
			if _, ok := overrides[key]; !ok {
				fmt.Fprintf(cmd.OutOrStdout(), "override %s is not set for %s\n", key, root)
				return nil
			}
			delete(overrides, key)
			ws.Overrides = overrides
			if err := st.Set(root, ws); err != nil {
				return fmt.Errorf("settings unset: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "removed override %s for %s\n", key, root)
			return nil
		},
	}

	resetCmd := &cobra.Command{
		Use:   "reset",
		Short: "Delete this workspace's settings record",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, st, err := g.settingsStore()
			if err != nil {
				return err
			}
			if err := st.Delete(root); err != nil {
				return fmt.Errorf("settings reset: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "removed settings for %s (now inherits global)\n", root)
			return nil
		},
	}

	cmd.AddCommand(pathCmd, listCmd, showCmd,
		makeModeCmd("use-global", settings.ModeGlobal, "Use global settings for this workspace"),
		makeModeCmd("use-workspace", settings.ModeWorkspace, "Use workspace overrides for this workspace"),
		setCmd, unsetCmd, resetCmd)
	return cmd
}
