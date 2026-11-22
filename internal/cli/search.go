package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ProjAnvil/kbcli/internal/config"
)

// newSearchCmd builds "kbcli search <query>".
func newSearchCmd(cfg *config.Config, g *globals) *cobra.Command {
	var topK int
	var jsonOut bool
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
				if jsonOut {
					return json.NewEncoder(w).Encode([]searchHit{})
				}
				fmt.Fprintln(w, "no matches")
				return nil
			}
			if jsonOut {
				out := make([]searchHit, 0, len(hits))
				for i, h := range hits {
					out = append(out, searchHit{
						Rank:    i + 1,
						Score:   h.Score,
						Source:  h.Chunk.Source,
						Heading: h.Chunk.Heading,
						Body:    h.Chunk.Body,
						ID:      h.Chunk.ID,
					})
				}
				enc := json.NewEncoder(w)
				enc.SetIndent("", "  ")
				return enc.Encode(out)
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
	cmd.Flags().BoolVar(&jsonOut, "json", false, "print ranked hits as JSON for tool consumers")
	return cmd
}

// searchHit is the stable JSON shape returned by "kbcli search --json".
type searchHit struct {
	Rank    int     `json:"rank"`
	Score   float64 `json:"score"`
	Source  string  `json:"source"`
	Heading string  `json:"heading"`
	Body    string  `json:"body"`
	ID      string  `json:"id"`
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
