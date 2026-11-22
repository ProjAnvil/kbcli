package tui

import (
	"errors"
	"strings"
	"testing"
)

func TestReindexViewShowsLogoAndFileProgress(t *testing.T) {
	m := testModel(t, baseOpts())
	m.reindexing = true
	m.reindexProgress = IndexProgress{
		Phase: "indexing", FilesDone: 1, FilesTotal: 4,
		Chunks: 7, Current: "docs/a.md",
	}

	view := m.View()
	for _, want := range []string{
		"| |/ /",             // ASCII logo
		"kbcli · reindexing", // title
		"indexing",           // phase label
		"1/4 files",          // files ratio
		"7 chunks",           // chunks
		"docs/a.md",          // current file
		"wait for reindex to finish before chatting", // hint
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("reindex view missing %q: %q", want, view)
		}
	}

	bar := reindexBar(m.reindexProgress)
	if got := strings.Count(bar, "="); got != bootBarWidth/4 {
		t.Fatalf("bar filled = %d, want %d: %q", got, bootBarWidth/4, bar)
	}
	if got := strings.Count(bar, "-"); got != bootBarWidth-bootBarWidth/4 {
		t.Fatalf("bar empty = %d, want %d: %q", got, bootBarWidth-bootBarWidth/4, bar)
	}
}

func TestReindexBarEmptyWhenNoTotal(t *testing.T) {
	bar := reindexBar(IndexProgress{Phase: "loading_model", FilesDone: 3})
	if strings.Contains(bar, "=") {
		t.Fatalf("bar must be empty when FilesTotal <= 0: %q", bar)
	}
	if got := strings.Count(bar, "-"); got != bootBarWidth {
		t.Fatalf("bar width = %d, want %d: %q", got, bootBarWidth, bar)
	}
}

func TestReindexViewShowsError(t *testing.T) {
	m := testModel(t, baseOpts())
	m.reindexing = true
	m.reindexProgress = IndexProgress{Phase: "saving"}
	m.reindexErr = errors.New("disk full")

	view := m.View()
	if !strings.Contains(view, "reindex error: disk full") {
		t.Fatalf("reindex view missing error: %q", view)
	}
}
