package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/projanvil/kbcli/internal/config"
)

// newSearchCmd builds "kbcli search <query>".
func newSearchCmd(cfg *config.Config, g *globals) *cobra.Command {
	var topK int
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search the index without an LLM",
		Long:  "Rank the indexed chunks by vector similarity. This does not call the LLM and needs no API key.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, _, err := g.openExisting(cfg)
			if err != nil {
				return err
			}
			k := topK
			if k <= 0 {
				k = cfg.TopK
			}
			hits, err := a.Search(commandContext(cmd), strings.Join(args, " "), k)
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			if len(hits) == 0 {
				fmt.Fprintln(w, "no matches")
				return nil
			}
			for i, h := range hits {
				fmt.Fprintf(w, "#%d  score=%.3f  %s  %s\n", i+1, h.Score, h.Chunk.Source, h.Chunk.Heading)
				for _, line := range snippet(h.Chunk.Body, 3) {
					fmt.Fprintf(w, "    %s\n", line)
				}
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&topK, "top-k", cfg.TopK, "number of results to print")
	return cmd
}

// snippet returns up to n non-empty lines from body.
func snippet(body string, n int) []string {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	out := make([]string, 0, n)
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		out = append(out, l)
		if len(out) == n {
			break
		}
	}
	return out
}
