package ingest

import (
	"strings"

	"github.com/ProjAnvil/kbcli/internal/index"
)

// fixedStrategy reproduces the legacy dispatch switch exactly for every
// supported extension, so StrategyFixed is byte-for-byte the old behaviour.
type fixedStrategy struct{}

func (fixedStrategy) Name() string         { return "fixed" }
func (fixedStrategy) Extensions() []string { return supportedExtList() }

func (fixedStrategy) Chunk(src Source) ([]index.Chunk, error) {
	switch strings.ToLower(src.Ext) {
	case ".md", ".markdown":
		return chunkMarkdown(src.Text, src.Meta, src.Opts)
	case ".pdf":
		return chunkPDF(src.Data, src.Meta, src.Opts)
	case ".docx":
		return chunkDOCX(src.Data, src.Meta, src.Opts)
	default: // .txt .text .csv .mdx .html .htm
		return chunkText(src.Text, src.Ext, src.Meta, src.Opts)
	}
}
