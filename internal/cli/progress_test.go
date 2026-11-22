package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ProjAnvil/kbcli/internal/rerankmodel"
)

// resetModelProgress clears the package-level line width between tests.
func resetModelProgress() {
	var buf bytes.Buffer
	printModelProgress(&buf, rerankmodel.Progress{Phase: "done"})
}

func TestPrintModelProgressShowsPhasePercentAndFiles(t *testing.T) {
	resetModelProgress()
	var buf bytes.Buffer
	printModelProgress(&buf, rerankmodel.Progress{
		Phase:      "download",
		File:       "model.safetensors",
		Downloaded: 50 << 20,
		Total:      100 << 20,
		FilesDone:  1,
		FilesTotal: 5,
	})
	out := buf.String()
	if !strings.HasPrefix(out, "\r") {
		t.Fatalf("progress line does not overwrite: %q", out)
	}
	for _, want := range []string{"download", "model.safetensors", "50%", "1/5 files", "50.0/100.0 MB"} {
		if !strings.Contains(out, want) {
			t.Fatalf("progress output %q missing %q", out, want)
		}
	}
}

func TestPrintModelProgressDoneTerminatesLine(t *testing.T) {
	resetModelProgress()
	var buf bytes.Buffer
	printModelProgress(&buf, rerankmodel.Progress{Phase: "done"})
	if !strings.HasSuffix(buf.String(), "\n") {
		t.Fatalf("done phase did not terminate the line: %q", buf.String())
	}
	if modelProgressWidth != 0 {
		t.Fatalf("line width after done = %d, want 0", modelProgressWidth)
	}
}

func TestPrintModelProgressErasesLongerLine(t *testing.T) {
	resetModelProgress()
	var buf bytes.Buffer
	first := rerankmodel.Progress{
		Phase: "download", File: "a-very-long-file-name.safetensors",
		Downloaded: 1, Total: 2, FilesDone: 1, FilesTotal: 2,
	}
	printModelProgress(&buf, first)
	buf.Reset()
	printModelProgress(&buf, rerankmodel.Progress{Phase: "done"})
	raw := strings.TrimSuffix(strings.TrimPrefix(buf.String(), "\r"), "\n")
	pad := len([]rune(modelProgressLine(first))) - len([]rune(modelProgressLine(rerankmodel.Progress{Phase: "done"})))
	if pad > 0 && !strings.HasSuffix(raw, strings.Repeat(" ", pad)) {
		t.Fatalf("short line did not erase %d leftover columns: %q", pad, raw)
	}
}
