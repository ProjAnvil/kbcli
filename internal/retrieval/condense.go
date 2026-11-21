package retrieval

import (
	"context"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/projanvil/kbcli/internal/config"
	"github.com/projanvil/kbcli/internal/i18n"
)

// zhAnaphora and enAnaphora are the pronoun/ellipsis markers Porto's condense
// gate looks for. They also drive the offline rule fallback.
var zhAnaphora = []string{
	"它", "他", "她", "该", "上述", "前者", "后者", "这个", "那个",
	"其", "此", "这套", "那边", "刚才", "前面",
}

var enAnaphora = []string{
	" it ", " they ", " this ", " that ", " these ", " those ",
	" its ", " their ", " he ", " she ", " it?", "it's ", "them ",
}

func hasAnaphora(query string) bool {
	padded := " " + strings.ToLower(query) + " "
	for _, marker := range enAnaphora {
		if strings.Contains(padded, marker) {
			return true
		}
	}
	// Word-level detection catches trailing punctuation such as "them?" and
	// avoids substring false positives like "theme".
	words := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for _, w := range words {
		switch w {
		case "it", "its", "they", "them", "their", "this", "that", "these", "those", "he", "she", "her", "his":
			return true
		}
	}
	for _, marker := range zhAnaphora {
		if strings.Contains(query, marker) {
			return true
		}
	}
	return false
}

// condenseQuery is the LLM gate. lang is config.LangEn or config.LangZh. It
// returns the rewritten standalone query and whether a rewrite was applied.
// Every failure fails open to the original query.
func condenseQuery(ctx context.Context, query string, history []Turn, complete CompleteFunc, lang string) (string, bool) {
	if complete == nil || len(history) == 0 {
		return query, false
	}
	l := i18n.Parse(lang)
	key := "condense.system_en"
	if l == i18n.LangZh {
		key = "condense.system_zh"
	}

	start := len(history) - 6
	if start < 0 {
		start = 0
	}
	var b strings.Builder
	for _, t := range history[start:] {
		if t.Role == "assistant" {
			b.WriteString("assistant: ")
		} else {
			b.WriteString("user: ")
		}
		b.WriteString(truncateRunes(t.Content, 200))
		b.WriteByte('\n')
	}
	user := "Conversation:\n" + b.String() + "\nQuestion: " + query
	out, err := callLLM(ctx, complete, l.T(key), user)
	if err != nil || out == "" {
		return query, false
	}
	cleaned := strings.TrimSpace(out)
	cleaned = strings.Trim(cleaned, "\"'`“”‘’")
	cleaned = strings.TrimRight(cleaned, "。.!！?？")
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" || strings.EqualFold(cleaned, "PASS") || cleaned == query {
		return query, false
	}
	if utf8.RuneCountInString(cleaned) > 1000 {
		return query, false
	}
	return cleaned, true
}

// ruleCondense is the offline fallback. It prepends the last user turn as an
// entity anchor so "does it need a rebuild?" becomes searchable without an
// LLM. Porto has no equivalent; this keeps anaphora handling usable offline.
func ruleCondense(query string, history []Turn) (string, bool) {
	if !hasAnaphora(query) || len(history) == 0 {
		return query, false
	}
	var anchor string
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role != "user" {
			continue
		}
		anchor = strings.TrimSpace(history[i].Content)
		anchor = strings.TrimRight(anchor, "?？。.!！")
		if anchor != "" {
			break
		}
	}
	if anchor == "" {
		return query, false
	}
	anchor = truncateRunes(anchor, 80)
	rewritten := anchor + " " + query
	if utf8.RuneCountInString(rewritten) > 1000 {
		return query, false
	}
	return rewritten, true
}

// resolveQuery applies the Porto condense gate, then the rule fallback. The
// returned meta is safe to publish to the UI.
func resolveQuery(
	ctx context.Context,
	query string,
	history []Turn,
	cfg *config.Config,
	complete CompleteFunc,
) (string, CondenseMeta) {
	meta := CondenseMeta{Original: query}
	if cfg == nil || !cfg.CondenseEnabled || len(history) == 0 {
		return query, meta
	}
	if rewritten, ok := condenseQuery(ctx, query, history, complete, cfg.Language); ok {
		meta.Applied, meta.Rewritten, meta.Method = true, rewritten, "llm"
		return rewritten, meta
	}
	if rewritten, ok := ruleCondense(query, history); ok {
		meta.Applied, meta.Rewritten, meta.Method = true, rewritten, "rule"
		return rewritten, meta
	}
	return query, meta
}
