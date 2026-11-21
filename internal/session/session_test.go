package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ProjAnvil/kbcli/internal/i18n"
)

func opts(dir string) Options {
	return Options{
		Dir:                 dir,
		Workspace:           "/kb/docs",
		Lang:                i18n.LangEn,
		Enabled:             true,
		FactsEnabled:        true,
		MaxFactsPerCategory: 3,
		SimilarityThreshold: 0.5,
		RecentContextTurns:  3,
		MemoryTopK:          5,
		CompactAfterTurns:   5,
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	o := opts(dir)
	s, err := Open(o)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(s.Path())); !os.IsNotExist(err) {
		t.Fatal("Open must not create the session directory")
	}
	s.AddTurn("user", "how does retry work")
	s.AddTurn("assistant", "it retries three times")
	if got := s.Title(); got != "how does retry work" {
		t.Fatalf("title = %q", got)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("mode = %o, want 600", perm)
	}

	reopened, err := Open(o)
	if err != nil {
		t.Fatal(err)
	}
	if len(reopened.Turns()) != 2 {
		t.Fatalf("reloaded turns = %d", len(reopened.Turns()))
	}
	if reopened.ID() != s.ID() {
		t.Fatal("session ID changed across reload")
	}
}

func TestDisabledDoesNotWrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	o := opts(dir)
	o.Enabled = false
	s, err := Open(o)
	if err != nil {
		t.Fatal(err)
	}
	s.AddTurn("user", "hello")
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.Path()); !os.IsNotExist(err) {
		t.Fatal("disabled session must not create a file")
	}
}

func TestTurnsAreDefensiveCopies(t *testing.T) {
	s, err := Open(opts(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		s.AddTurn("user", "turn")
	}
	got := s.Turns()
	got[0].Content = " mutated "
	if s.Turns()[0].Content == " mutated " {
		t.Fatal("Turns returned a shared slice")
	}
	if len(s.Recent(3)) != 3 {
		t.Fatalf("Recent(3) = %d", len(s.Recent(3)))
	}
	if len(s.Recent(0)) != 3 {
		t.Fatalf("Recent(0) should use the configured 3, got %d", len(s.Recent(0)))
	}
}

func TestJaccardUpsertAndCap(t *testing.T) {
	s, err := Open(opts(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if n := s.ExtractFacts(context.Background(), "我们决定用 Postgres 作为主数据库"); n != 1 {
		t.Fatalf("rule extract wrote %d facts", n)
	}
	// Near-duplicate in the same category updates instead of inserting.
	s.mu.Lock()
	s.upsert(CategoryDecision, "我们决定用 Postgres 作为主数据库，确认了")
	s.mu.Unlock()
	if got := len(s.ActiveFacts()); got != 1 {
		t.Fatalf("dedupe failed, active facts = %d", got)
	}
	// Different category is never merged.
	s.mu.Lock()
	s.upsert(CategoryPreference, "我们决定用 Postgres 作为主数据库")
	s.mu.Unlock()
	if got := len(s.ActiveFacts()); got != 2 {
		t.Fatalf("cross-category upsert merged, got %d", got)
	}
	for _, text := range []string{
		"project uses a monorepo with bazel",
		"alpha team owns the billing service",
		"deploys happen every Thursday evening",
		"staging runs on a separate cluster",
		"the SLO target is 99.95 percent",
		"legacy perl scripts remain in cron",
	} {
		s.mu.Lock()
		s.upsert(CategoryContext, text)
		s.mu.Unlock()
	}
	byCat := s.FactsByCategory()
	if len(byCat[CategoryContext]) != 3 {
		t.Fatalf("cap not enforced: %d context facts", len(byCat[CategoryContext]))
	}
}

func TestRetractAndPriorityOrder(t *testing.T) {
	s, err := Open(opts(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.upsert(CategoryQuestion, "待确认：超时时间")
	s.upsert(CategoryDecision, "决定采用 gRPC")
	s.upsert(CategoryPreference, "偏好 Go")
	s.mu.Unlock()
	facts := s.ActiveFacts()
	if facts[0].Category != CategoryDecision || facts[1].Category != CategoryPreference || facts[2].Category != CategoryQuestion {
		t.Fatalf("priority order = %v", []string{facts[0].Category, facts[1].Category, facts[2].Category})
	}
	if n := s.ExtractFacts(context.Background(), "不用了，决定取消 gRPC"); n != 1 {
		t.Fatalf("retract wrote %d", n)
	}
	if len(s.ActiveFacts()) != 2 {
		t.Fatalf("retract did not remove a fact: %d left", len(s.ActiveFacts()))
	}
}

func TestFactsBlockAndContextBlockBilingual(t *testing.T) {
	s, err := Open(opts(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.upsert(CategoryDecision, "decided to use SQLite")
	s.mu.Unlock()
	block := s.FactsBlock()
	if !strings.Contains(block, "Key facts") || !strings.Contains(block, "[Decision]") {
		t.Fatalf("en facts block = %q", block)
	}
	if !strings.Contains(s.ContextBlock(), "decided to use SQLite") {
		t.Fatal("facts missing from ContextBlock")
	}
	s.SetLang(i18n.LangZh)
	zh := s.FactsBlock()
	if !strings.Contains(zh, "关键事实") || !strings.Contains(zh, "[决策]") {
		t.Fatalf("zh facts block = %q", zh)
	}
}

func TestExtractFactsStructuredJSON(t *testing.T) {
	o := opts(t.TempDir())
	o.Complete = func(_ context.Context, _, _ string) (string, error) {
		return `{"facts":[{"category":"user_preference","content":"prefers JSON logs","action":"add"}]}`, nil
	}
	s, err := Open(o)
	if err != nil {
		t.Fatal(err)
	}
	if n := s.ExtractFacts(context.Background(), "I prefer JSON logs"); n != 1 {
		t.Fatalf("structured extract wrote %d", n)
	}
	if got := s.ActiveFacts(); len(got) != 1 || got[0].Content != "prefers JSON logs" {
		t.Fatalf("structured fact = %+v", got)
	}
}

func TestExtractFactsFencedJSONAndGarbageFallback(t *testing.T) {
	o := opts(t.TempDir())
	o.Complete = func(_ context.Context, _, _ string) (string, error) {
		return "```json\n" + `{"facts":[{"category":"project_context","content":"project uses Go 1.27","action":"add"}]}` + "\n```", nil
	}
	s, err := Open(o)
	if err != nil {
		t.Fatal(err)
	}
	if n := s.ExtractFacts(context.Background(), "our project uses Go"); n != 1 {
		t.Fatalf("fenced extract wrote %d", n)
	}

	o.Complete = func(_ context.Context, _, _ string) (string, error) { return "not json at all", nil }
	s2, err := Open(o)
	if err != nil {
		t.Fatal(err)
	}
	if n := s2.ExtractFacts(context.Background(), "我们决定用 Redis 缓存"); n != 1 {
		t.Fatalf("garbage fallback wrote %d, want 1", n)
	}
}

func TestRuleExtractChineseAndEnglish(t *testing.T) {
	s, err := Open(opts(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	cases := []string{
		"我偏好用 YAML 配置",
		"we decided to keep the old API",
		"待确认：是否需要 TLS",
	}
	for _, msg := range cases {
		if n := s.ExtractFacts(context.Background(), msg); n != 1 {
			t.Fatalf("rule extract %q wrote %d", msg, n)
		}
	}
	if got := len(s.ActiveFacts()); got != 3 {
		t.Fatalf("active facts = %d, want 3", got)
	}
	if n := s.ExtractFacts(context.Background(), "hi"); n != 0 {
		t.Fatalf("greeting produced %d facts", n)
	}
}

func TestCompactOfflineAndLLM(t *testing.T) {
	o := opts(t.TempDir())
	s, err := Open(o)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 9; i++ {
		s.AddTurn("user", "older question about retries")
		s.AddTurn("assistant", "answer")
	}
	if !s.Compact(context.Background()) {
		t.Fatal("offline compact returned false")
	}
	if s.Summary() == "" {
		t.Fatal("offline summary is empty")
	}
	if s.Compact(context.Background()) {
		t.Fatal("second compact should be idempotent")
	}
	if !strings.Contains(s.ContextBlock(), s.Summary()) {
		t.Fatal("summary missing from ContextBlock")
	}

	o.Complete = func(_ context.Context, _, _ string) (string, error) {
		return "LLM compact summary", nil
	}
	s2, err := Open(o)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 9; i++ {
		s2.AddTurn("user", "old")
		s2.AddTurn("assistant", "reply")
	}
	if !s2.Compact(context.Background()) || s2.Summary() != "LLM compact summary" {
		t.Fatalf("LLM compact summary = %q", s2.Summary())
	}
}

func TestResetAndCorruptFile(t *testing.T) {
	dir := t.TempDir()
	o := opts(dir)
	s, err := Open(o)
	if err != nil {
		t.Fatal(err)
	}
	s.AddTurn("user", "hello")
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	if err := s.Reset(); err != nil {
		t.Fatal(err)
	}
	if len(s.Turns()) != 0 || s.Summary() != "" {
		t.Fatal("Reset did not clear the session")
	}
	if err := os.WriteFile(s.Path(), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(o); err == nil {
		t.Fatal("corrupt session file must be an error")
	}
}

func TestConcurrentUse(t *testing.T) {
	s, err := Open(opts(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			s.AddTurn("user", "message")
			_ = s.ActiveFacts()
			_ = s.ContextBlock()
			_ = s.Save()
		}(i)
	}
	wg.Wait()
	if len(s.Turns()) != 40 {
		t.Fatalf("turns = %d, want 40", len(s.Turns()))
	}
	data, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	var parsed Session
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("session file is corrupt: %v", err)
	}
}

func TestMemoryTopKControlsInjectedTurns(t *testing.T) {
	o := opts(t.TempDir())
	o.MemoryTopK = 2
	s, err := Open(o)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		s.AddTurn("user", "message "+string(rune('A'+i)))
	}
	block := s.ContextBlock()
	if strings.Contains(block, "message A") || !strings.Contains(block, "message F") {
		t.Fatalf("MemoryTopK=2 did not keep only the newest turns: %q", block)
	}

	o.MemoryTopK = 0
	s2, err := Open(o)
	if err != nil {
		t.Fatal(err)
	}
	s2.AddTurn("user", "visible turn")
	if strings.Contains(s2.ContextBlock(), "visible turn") {
		t.Fatal("MemoryTopK=0 must disable recent-turn injection")
	}
}
