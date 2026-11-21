package session

import (
	"context"
	"strings"

	"github.com/ProjAnvil/kbcli/internal/i18n"
)

// Compact summarizes older turns once history exceeds compact_after_turns.
// Porto caches the summary keyed by the last summarized message; kbcli keeps
// the full turn list and caches the boundary index instead.
func (s *Store) Compact(ctx context.Context) bool {
	s.mu.RLock()
	enabled, after, recentN := s.opts.Enabled, s.opts.CompactAfterTurns, s.opts.RecentContextTurns
	complete, lang := s.opts.Complete, s.opts.Lang
	turns := append([]Turn(nil), s.data.Turns...)
	upTo := s.data.SummaryUpTo
	old := s.data.Summary
	s.mu.RUnlock()

	if !enabled || len(turns) <= after {
		return false
	}
	boundary := len(turns) - recentN
	if boundary <= 0 || upTo >= boundary {
		return false
	}

	oldTurns := turns[upTo:boundary]
	var h strings.Builder
	for _, t := range oldTurns {
		h.WriteString(t.Role)
		h.WriteString(": ")
		h.WriteString(trimRunes(t.Content, 300))
		h.WriteByte('\n')
	}

	summary := ""
	if complete != nil {
		key := "compact.system_en"
		if lang == i18n.LangZh {
			key = "compact.system_zh"
		}
		if out, err := complete(ctx, lang.T(key), h.String()); err == nil {
			summary = strings.TrimSpace(out)
		}
	}
	if summary == "" {
		summary = offlineSummary(oldTurns)
	}
	summary = trimRunes(summary, 800)
	if summary == "" || summary == old {
		return false
	}

	s.mu.Lock()
	s.data.Summary = summary
	s.data.SummaryUpTo = boundary
	s.mu.Unlock()
	return true
}

// offlineSummary concatenates the first sentence of each older user turn.
func offlineSummary(turns []Turn) string {
	var parts []string
	total := 0
	for _, t := range turns {
		if t.Role != "user" {
			continue
		}
		text := strings.TrimSpace(t.Content)
		if idx := strings.IndexAny(text, "。.!！?？\n"); idx > 0 {
			text = text[:idx]
		}
		text = trimRunes(text, 120)
		if text == "" {
			continue
		}
		if total+runeLen(text) > 800 {
			break
		}
		total += runeLen(text)
		parts = append(parts, "- "+text)
	}
	return strings.Join(parts, "\n")
}

// ContextBlock is the Porto orchestrator equivalent: facts, summary and
// recent turns, joined under the caller's character budget.
func (s *Store) ContextBlock() string {
	s.mu.RLock()
	lang := s.opts.Lang
	summary := s.data.Summary
	upTo := s.data.SummaryUpTo
	turns := append([]Turn(nil), s.data.Turns...)
	memoryTopK := s.opts.MemoryTopK
	s.mu.RUnlock()

	var parts []string
	if block := s.FactsBlock(); block != "" {
		parts = append(parts, block)
	}
	if summary != "" {
		parts = append(parts, lang.T("session.summary")+":\n"+summary)
	}

	start := upTo
	if start < 0 {
		start = 0
	}
	if start > len(turns) {
		start = len(turns)
	}
	recent := turns[start:]
	if memoryTopK <= 0 {
		recent = nil
	} else if len(recent) > memoryTopK {
		recent = recent[len(recent)-memoryTopK:]
	}
	if len(recent) > 0 {
		var b strings.Builder
		b.WriteString(lang.T("session.recent"))
		b.WriteString(":\n")
		for i, t := range recent {
			if i > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(t.Role)
			b.WriteString(": ")
			b.WriteString(trimRunes(t.Content, 300))
		}
		parts = append(parts, b.String())
	}
	return strings.Join(parts, "\n\n")
}
