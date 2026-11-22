package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ProjAnvil/kbcli/internal/config"
	"github.com/ProjAnvil/kbcli/internal/i18n"
)

func TestBootViewShowsLogoAndByteProgress(t *testing.T) {
	opts := baseOpts()
	opts.Config = &config.Config{Language: "en"}
	opts.EnsureModel = func(func(ModelProgress)) error { return nil }
	m := testModel(t, opts)
	if !m.booting {
		t.Fatal("EnsureModel did not open the boot modal")
	}

	m.bootProgress = ModelProgress{
		Phase: "downloading", File: "model.safetensors",
		Downloaded: 50 << 20, Total: 100 << 20, FilesDone: 3, FilesTotal: 5,
	}
	view := m.View()
	for _, want := range []string{
		"| |/ /", "preparing local reranker", "downloading", "3/5 files", "50.0/100.0 MB",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("boot view missing %q: %q", want, view)
		}
	}
	bar := bootBar(m.bootProgress)
	if strings.Count(bar, "=") != bootBarWidth/2 {
		t.Fatalf("bar filled = %d, want %d: %q", strings.Count(bar, "="), bootBarWidth/2, bar)
	}
	if got := bootPercent(m.bootProgress); got != 50 {
		t.Fatalf("bootPercent = %d, want 50", got)
	}
}

func TestBootModalBlocksKeysUntilDone(t *testing.T) {
	called := 0
	opts := baseOpts()
	opts.Config = &config.Config{Language: "en"}
	opts.EnsureModel = func(func(ModelProgress)) error { called++; return nil }
	m := testModel(t, opts)
	m.applySize(120, 40)

	blocked := []tea.Msg{
		tea.KeyMsg{Type: tea.KeyEnter},
		tea.KeyMsg{Type: tea.KeyTab},
		tea.KeyMsg{Type: tea.KeyEsc},
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}},
	}
	for _, key := range blocked {
		got, c := m.Update(key)
		if c != nil {
			t.Fatalf("boot modal accepted key %v: cmd != nil", key)
		}
		if mm := got.(*model); !mm.booting {
			t.Fatalf("boot modal accepted key %v", key)
		}
	}

	// Deliver progress over the channel, then completion.
	cmd := m.startBoot()
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) != 3 {
		t.Fatalf("startBoot batch = %T len=%d", cmd(), len(batch))
	}
	// The last cmd is the one that actually runs EnsureModel.
	batch[len(batch)-1]()
	m.bootCh <- ModelProgress{Phase: "copying", Downloaded: 1, Total: 2}
	next, _ := m.Update(bootProgressMsg{p: <-m.bootCh})
	if next.(*model).bootProgress.Phase != "copying" {
		t.Fatal("progress message did not update boot state")
	}
	next, c := m.Update(bootDoneMsg{})
	if next.(*model).booting {
		t.Fatal("bootDoneMsg did not close boot modal")
	}
	if c == nil {
		t.Fatal("successful boot must refocus the chat input")
	}
	if called != 1 {
		t.Fatalf("EnsureModel called %d times, want 1", called)
	}
}

func TestBootFailureKeepsChatBlocked(t *testing.T) {
	opts := baseOpts()
	opts.Config = &config.Config{Language: "en"}
	opts.EnsureModel = func(func(ModelProgress)) error { return errors.New("offline") }
	m := testModel(t, opts)
	m.applySize(120, 40)

	next, _ := m.Update(bootDoneMsg{err: errors.New("offline")})
	m = next.(*model)
	if !m.booting {
		t.Fatal("failed boot must keep chat blocked")
	}
	view := m.View()
	if !strings.Contains(view, "model setup failed: offline") {
		t.Fatalf("boot failure view missing error: %q", view)
	}
	if got, c := m.Update(tea.KeyMsg{Type: tea.KeyEnter}); c != nil || !got.(*model).booting {
		t.Fatal("chat key accepted after failed boot")
	}
}

func TestBootPhaseLabelsAreBilingual(t *testing.T) {
	if got := bootPhaseLabel(i18n.LangEn, "downloading"); got != "downloading" {
		t.Fatalf("en label = %q", got)
	}
	if got := bootPhaseLabel(i18n.LangZh, "downloading"); got != "下载中" {
		t.Fatalf("zh label = %q", got)
	}
	if got := bootPhaseLabel(i18n.LangEn, "unknown"); got != "unknown" {
		t.Fatalf("fallback = %q", got)
	}
}
