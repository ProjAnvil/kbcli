package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/ProjAnvil/kbcli/internal/rerankmodel"
)

// modelProgressWidth is the rune width of the last line written. A later,
// shorter line pads itself so characters from a longer line are erased.
var modelProgressWidth int

// printModelProgress writes one overwritable line for a reranker model
// download. Phase "done" terminates the line so later output starts cleanly.
func printModelProgress(w io.Writer, p rerankmodel.Progress) {
	line := modelProgressLine(p)
	if pad := modelProgressWidth - len([]rune(line)); pad > 0 {
		line += strings.Repeat(" ", pad)
	}
	fmt.Fprint(w, "\r"+line)
	modelProgressWidth = len([]rune(line))
	if p.Phase == "done" {
		fmt.Fprintln(w)
		modelProgressWidth = 0
	}
}

func modelProgressLine(p rerankmodel.Progress) string {
	var b strings.Builder
	b.WriteString(p.Phase)
	if p.FilesTotal > 0 {
		fmt.Fprintf(&b, " %d/%d files", p.FilesDone, p.FilesTotal)
	}
	if p.Total > 0 {
		fmt.Fprintf(&b, " %.0f%% (%.1f/%.1f MB)", float64(p.Downloaded)*100/float64(p.Total),
			float64(p.Downloaded)/(1<<20), float64(p.Total)/(1<<20))
	} else if p.Downloaded > 0 {
		fmt.Fprintf(&b, " %.1f MB", float64(p.Downloaded)/(1<<20))
	}
	if p.File != "" {
		fmt.Fprintf(&b, " %s", p.File)
	}
	return b.String()
}
