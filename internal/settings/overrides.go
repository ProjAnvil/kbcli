package settings

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	"github.com/projanvil/kbcli/internal/config"
)

// overridableKeys lists every field a workspace may override.
func overridableKeys() []string {
	return config.FieldKeys()
}

// applyOverrides copies values from overrides into cfg. Keys are processed in
// sorted order for deterministic behaviour. Values originate from
// encoding/json, so numbers arrive as float64 (or json.Number). SetField
// validates ranges and enum membership; Validate checks the whole config.
// Defaults are deliberately not re-applied afterwards, so a legal zero value
// is never silently replaced.
func applyOverrides(cfg *config.Config, overrides map[string]any) error {
	if cfg == nil {
		return fmt.Errorf("settings: cannot apply overrides to a nil config")
	}

	keys := make([]string, 0, len(overrides))
	for k := range overrides {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		if _, ok := config.FieldByKey(key); !ok {
			return fmt.Errorf("settings: unknown override key %q", key)
		}
		str, err := overrideToString(key, overrides[key])
		if err != nil {
			return err
		}
		if err := cfg.SetField(key, str); err != nil {
			return fmt.Errorf("settings: apply override %q: %w", key, err)
		}
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("settings: config invalid after overrides: %w", err)
	}
	return nil
}

// overrideToString renders one JSON-derived override value as the string that
// config.SetField expects, rejecting any unsupported type.
func overrideToString(key string, v any) (string, error) {
	switch x := v.(type) {
	case string:
		return x, nil
	case bool:
		return strconv.FormatBool(x), nil
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), nil
	case json.Number:
		return x.String(), nil
	default:
		return "", fmt.Errorf("settings: override %q has unsupported type %T", key, v)
	}
}
