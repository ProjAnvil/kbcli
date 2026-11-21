package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/projanvil/kbcli/internal/config"
)

// configKeys returns the shared field catalog. Both "config list" and the
// valid-key checks use this one source of truth so they can never drift.
func configKeys() []string {
	return config.FieldKeys()
}

// configValue returns the raw value of a setting for "config get". Secrets are
// intentionally returned unmasked here; "config list" masks them.
func configValue(cfg *config.Config, key string) (string, bool) {
	v, err := cfg.GetField(key)
	if err != nil {
		return "", false
	}
	return fmt.Sprint(v), true
}

// configAssign parses and stores one setting through the shared catalog, which
// validates numeric syntax, ranges and enum membership before mutating cfg.
func configAssign(cfg *config.Config, key, value string) error {
	if _, ok := config.FieldByKey(key); !ok {
		return fmt.Errorf("config: unknown key %q (valid keys: %s)", key, strings.Join(configKeys(), ", "))
	}
	return cfg.SetField(key, value)
}

// fieldKindName renders a field kind for "config keys".
func fieldKindName(k config.FieldKind) string {
	switch k {
	case config.FieldInt:
		return "int"
	case config.FieldFloat:
		return "float"
	case config.FieldBool:
		return "bool"
	case config.FieldEnum:
		return "enum"
	case config.FieldString:
		return "string"
	default:
		return "unknown"
	}
}

// fieldRange renders the accepted values or numeric range of a field.
func fieldRange(f config.Field) string {
	if len(f.Options) > 0 {
		return strings.Join(f.Options, "|")
	}
	if f.Min != 0 || f.Max != 0 {
		return fmt.Sprintf("%g..%g (step %g)", f.Min, f.Max, f.Step)
	}
	return "-"
}

// newConfigCmd builds "kbcli config" and its set/get/list/path/keys children.
func newConfigCmd(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Read and write kbcli settings",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	setCmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a setting and save it to disk",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := configAssign(cfg, args[0], args[1]); err != nil {
				return err
			}
			if err := cfg.Save(); err != nil {
				return fmt.Errorf("save config: %w", err)
			}
			return nil
		},
	}

	getCmd := &cobra.Command{
		Use:   "get <key>",
		Short: "Print one setting (raw value, secrets included)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			v, ok := configValue(cfg, args[0])
			if !ok {
				return fmt.Errorf("config: unknown key %q (valid keys: %s)", args[0], strings.Join(configKeys(), ", "))
			}
			fmt.Fprintln(cmd.OutOrStdout(), v)
			return nil
		},
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "Print all settings (secret values are masked)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			w := cmd.OutOrStdout()
			for _, k := range configKeys() {
				fmt.Fprintf(w, "%s=%s\n", k, cfg.ValueString(k))
			}
			return nil
		},
	}

	pathCmd := &cobra.Command{
		Use:   "path",
		Short: "Print the settings file path",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := config.Path()
			if err != nil {
				return fmt.Errorf("config path: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), p)
			return nil
		},
	}

	keysCmd := &cobra.Command{
		Use:   "keys",
		Short: "List every configuration key with its group, type and range",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "%-28s %-18s %-8s %s\n", "KEY", "GROUP", "KIND", "RANGE")
			for _, f := range config.Fields() {
				fmt.Fprintf(w, "%-28s %-18s %-8s %s\n", f.Key, f.Group, fieldKindName(f.Kind), fieldRange(f))
			}
			return nil
		},
	}

	cmd.AddCommand(setCmd, getCmd, listCmd, pathCmd, keysCmd)
	return cmd
}
