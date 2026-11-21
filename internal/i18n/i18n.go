// Package i18n holds the bilingual string catalog for the TUI, prompts and
// session memory. English is the pre-existing wording; Chinese translations
// are selected with config.LangZh.
package i18n

import (
	"fmt"
	"sort"
	"strings"
)

// Lang is a UI language selector.
type Lang string

const (
	// LangEn selects English.
	LangEn Lang = "en"
	// LangZh selects Simplified Chinese.
	LangZh Lang = "zh"
)

// Parse normalizes a configured language string. Unknown values fall back to
// English so a malformed config file never breaks the UI.
func Parse(s string) Lang {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "zh", "zh-cn", "zh-hans", "中文", "cn":
		return LangZh
	default:
		return LangEn
	}
}

func (l Lang) catalog() map[string]string {
	if l == LangZh {
		return catalogZh
	}
	return catalogEn
}

// T returns the localized string for key. Missing keys are returned verbatim
// so adding a new UI string never panics.
func (l Lang) T(key string) string {
	if v, ok := l.catalog()[key]; ok {
		return v
	}
	if v, ok := catalogEn[key]; ok {
		return v
	}
	return key
}

// Tf formats a localized string with fmt.Sprintf.
func (l Lang) Tf(key string, args ...any) string {
	return fmt.Sprintf(l.T(key), args...)
}

// FieldLabel returns the localized label of a config field key.
func (l Lang) FieldLabel(key string) string {
	k := "field." + key + ".label"
	if v, ok := l.catalog()[k]; ok {
		return v
	}
	if v, ok := catalogEn[k]; ok {
		return v
	}
	return key
}

// GroupLabel returns the localized label of a config field group.
func (l Lang) GroupLabel(group string) string {
	k := "group." + group
	if v, ok := l.catalog()[k]; ok {
		return v
	}
	if v, ok := catalogEn[k]; ok {
		return v
	}
	return group
}

// Keys returns every catalog key in sorted order.
func Keys() []string {
	seen := make(map[string]bool, len(catalogEn))
	for k := range catalogEn {
		seen[k] = true
	}
	for k := range catalogZh {
		seen[k] = true
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
