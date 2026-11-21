package session

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/ProjAnvil/kbcli/internal/i18n"
)

// tokenSet is a lowercase bag of word tokens plus, for CJK runs, single
// characters and adjacent bigrams. It drives Porto's Jaccard deduplication.
func tokenSet(text string) map[string]bool {
	out := make(map[string]bool)
	runes := []rune(strings.ToLower(text))
	start := -1
	flush := func(end int) {
		if start < 0 {
			return
		}
		word := string(runes[start:end])
		start = -1
		if word == "" {
			return
		}
		out[word] = true
		var cjk []rune
		for _, r := range []rune(word) {
			if r >= 0x4E00 && r <= 0x9FFF {
				cjk = append(cjk, r)
			}
		}
		for _, r := range cjk {
			out[string(r)] = true
		}
		for i := 0; i+1 < len(cjk); i++ {
			out[string(cjk[i:i+2])] = true
		}
	}
	for i, r := range runes {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if start < 0 {
				start = i
			}
			continue
		}
		flush(i)
	}
	flush(len(runes))
	return out
}

func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	inter := 0
	for k := range a {
		if b[k] {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// upsert merges a fact into the same category when a near-duplicate is
// active, otherwise inserts it and enforces the per-category cap.
func (s *Store) upsert(category, content string) {
	now := time.Now().UTC()
	newTokens := tokenSet(content)
	for i := range s.data.Facts {
		f := &s.data.Facts[i]
		if f.Category != category || f.Status != StatusActive {
			continue
		}
		if jaccard(newTokens, tokenSet(f.Content)) >= s.opts.SimilarityThreshold {
			f.Content = content
			f.UpdatedAt = now
			return
		}
	}
	s.data.Facts = append(s.data.Facts, Fact{
		ID:        newID(),
		Category:  category,
		Content:   content,
		Status:    StatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	})
	s.enforceCap(category)
}

func (s *Store) enforceCap(category string) {
	type row struct {
		idx int
		at  time.Time
	}
	var active []row
	for i, f := range s.data.Facts {
		if f.Category == category && f.Status == StatusActive {
			active = append(active, row{i, f.UpdatedAt})
		}
	}
	if len(active) <= s.opts.MaxFactsPerCategory {
		return
	}
	sort.SliceStable(active, func(i, j int) bool { return active[i].at.Before(active[j].at) })
	drop := make(map[int]bool)
	for _, r := range active[:len(active)-s.opts.MaxFactsPerCategory] {
		drop[r.idx] = true
	}
	out := make([]Fact, 0, len(s.data.Facts)-len(drop))
	for i, f := range s.data.Facts {
		if !drop[i] {
			out = append(out, f)
		}
	}
	s.data.Facts = out
}

// retractByMatch soft-deletes the best-matching active fact in a category.
func (s *Store) retractByMatch(category, content string, threshold float64) bool {
	target := tokenSet(content)
	bestIdx, best := -1, threshold
	for i, f := range s.data.Facts {
		if f.Category != category || f.Status != StatusActive {
			continue
		}
		if score := jaccard(target, tokenSet(f.Content)); score >= best {
			bestIdx, best = i, score
		}
	}
	if bestIdx < 0 {
		return false
	}
	s.data.Facts[bestIdx].Status = StatusRetracted
	s.data.Facts[bestIdx].UpdatedAt = time.Now().UTC()
	return true
}

func validCategory(c string) bool {
	_, ok := categoryPriority[c]
	return ok
}

// ActiveFacts returns active facts ordered by category priority, then by
// most recently updated.
func (s *Store) ActiveFacts() []Fact {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Fact, 0, len(s.data.Facts))
	for _, f := range s.data.Facts {
		if f.Status == StatusActive {
			out = append(out, f)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		pi, pj := categoryPriority[out[i].Category], categoryPriority[out[j].Category]
		if pi != pj {
			return pi < pj
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out
}

// FactsByCategory groups active facts. The returned map is a defensive copy.
func (s *Store) FactsByCategory() map[string][]Fact {
	grouped := make(map[string][]Fact)
	for _, f := range s.ActiveFacts() {
		grouped[f.Category] = append(grouped[f.Category], f)
	}
	return grouped
}

// FactsBlock renders the localized Porto-style facts prompt. It returns ""
// when there are no active facts.
func (s *Store) FactsBlock() string {
	grouped := s.FactsByCategory()
	if len(grouped) == 0 {
		return ""
	}
	s.mu.RLock()
	lang := s.opts.Lang
	s.mu.RUnlock()
	cats := make([]string, 0, len(grouped))
	for c := range grouped {
		cats = append(cats, c)
	}
	sort.Slice(cats, func(i, j int) bool {
		return categoryPriority[cats[i]] < categoryPriority[cats[j]]
	})
	var b strings.Builder
	b.WriteString(lang.T("session.facts_header"))
	for _, c := range cats {
		b.WriteByte('\n')
		b.WriteString(lang.T(categoryKey[c]))
		for _, f := range grouped[c] {
			b.WriteString("\n- ")
			b.WriteString(f.Content)
		}
	}
	return b.String()
}

type extractedFact struct {
	Category string `json:"category"`
	Content  string `json:"content"`
	Action   string `json:"action"`
}

// ExtractFacts runs Porto's fire-and-forget structured extraction. Every
// failure falls back to offline rule markers and never returns an error.
func (s *Store) ExtractFacts(ctx context.Context, message string) int {
	s.mu.RLock()
	enabled, factsEnabled := s.opts.Enabled, s.opts.FactsEnabled
	complete, lang := s.opts.Complete, s.opts.Lang
	s.mu.RUnlock()
	if !enabled || !factsEnabled {
		return 0
	}

	if complete != nil {
		system := lang.T("facts.system_en")
		if lang == i18n.LangZh {
			system = lang.T("facts.system_zh")
		}
		recent := s.Recent(0)
		var h strings.Builder
		for _, t := range recent {
			h.WriteString(t.Role)
			h.WriteString(": ")
			h.WriteString(trimRunes(t.Content, 200))
			h.WriteByte('\n')
		}
		user := "Latest user message:\n" + message + "\n\nRecent context:\n" + h.String()
		out, err := complete(ctx, system, user)
		if err == nil {
			if facts, ok := parseFactsJSON(out); ok {
				return s.applyExtracted(facts)
			}
		}
	}
	return s.ruleExtract(message)
}

func parseFactsJSON(raw string) ([]extractedFact, bool) {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return nil, false
	}
	cleaned := raw[start : end+1]
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	var parsed struct {
		Facts []extractedFact `json:"facts"`
	}
	if err := json.Unmarshal([]byte(cleaned), &parsed); err != nil {
		return nil, false
	}
	return parsed.Facts, true
}

func (s *Store) applyExtracted(facts []extractedFact) int {
	written := 0
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, f := range facts {
		content := strings.TrimSpace(f.Content)
		if !validCategory(f.Category) || content == "" {
			continue
		}
		content = trimRunes(content, 200)
		if strings.EqualFold(f.Action, "retract") {
			if s.retractByMatch(f.Category, content, s.opts.SimilarityThreshold) {
				written++
			}
			continue
		}
		s.upsert(f.Category, content)
		written++
	}
	return written
}

// ruleExtract is the offline fallback. It recognizes Chinese and English
// markers and stores one atomic fact per matching message.
func (s *Store) ruleExtract(message string) int {
	msg := strings.TrimSpace(message)
	if runeLen(msg) < 4 {
		return 0
	}
	type rule struct {
		markers  []string
		category string
		retract  bool
	}
	rules := []rule{
		{[]string{"不用了", "取消了", "不再用", "no longer", "don't use", "do not use", "revert"}, "", true},
		{[]string{"我们决定", "决定用", "决定采用", "就用", "we decided", "let's use", "we will use", "we'll use"}, CategoryDecision, false},
		{[]string{"我偏好", "我更喜欢", "我喜欢", "我倾向", "i prefer", "i'd rather", "i would rather"}, CategoryPreference, false},
		{[]string{"项目背景", "项目约束", "我们的项目", "业务约束", "our project", "constraint", "background:"}, CategoryContext, false},
		{[]string{"待确认", "待澄清", "还没定", "open question", "todo:", "to be decided"}, CategoryQuestion, false},
	}
	for _, r := range rules {
		lower := strings.ToLower(msg)
		matched := false
		for _, marker := range r.markers {
			if strings.Contains(lower, marker) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		content := trimRunes(msg, 200)
		s.mu.Lock()
		if r.retract {
			written := 0
			// Negation messages share few tokens with the original statement
			// ("no longer use gRPC" vs "decided to use gRPC"), so the offline
			// retract fallback uses a looser Jaccard threshold than structured
			// extraction.
			const looseThreshold = 0.2
			for _, category := range []string{CategoryDecision, CategoryPreference, CategoryContext, CategoryQuestion} {
				if s.retractByMatch(category, content, looseThreshold) {
					written = 1
					break
				}
			}
			s.mu.Unlock()
			return written
		}
		s.upsert(r.category, content)
		s.mu.Unlock()
		return 1
	}
	return 0
}

// String is used in diagnostics only.
func (f Fact) String() string {
	return fmt.Sprintf("%s/%s: %s", f.Category, f.Status, f.Content)
}
