package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ProjAnvil/kbcli/internal/config"
	"github.com/ProjAnvil/kbcli/internal/index"
)

const (
	// evalQueryRunes caps the generated query. A shorter query keeps the
	// self-retrieval test from becoming a literal full-text lookup.
	evalQueryRunes = 80
	// minEvalBodyRunes skips headings and stubs that cannot form a query.
	minEvalBodyRunes = 40
	// defaultEvalSamples is small enough for a first-run smoke test.
	defaultEvalSamples = 50
)

type evalCase struct {
	id    string
	query string
}

type evalReport struct {
	Queries      int     `json:"queries"`
	TopK         int     `json:"top_k"`
	Retrieval    string  `json:"retrieval"`
	RecallAt1    float64 `json:"recall_at_1"`
	RecallAtK    float64 `json:"recall_at_k"`
	MRR          float64 `json:"mrr"`
	LatencyP50MS float64 `json:"latency_p50_ms"`
	LatencyP95MS float64 `json:"latency_p95_ms"`
	Note         string  `json:"note"`
}

// newEvalCmd builds "kbcli eval". It measures first-stage retrieval on a
// deterministic self-retrieval set built from indexed chunk bodies. It needs
// no LLM, no labelled data and no model download, so it can run in CI.
//
// This is a smoke test, not a question-answering benchmark: the generated
// queries come from the chunks themselves and recall is therefore optimistic.
// Use it to detect regressions between configurations, not to claim absolute
// answer quality.
func newEvalCmd(cfg *config.Config, g *globals) *cobra.Command {
	var samples, topK int
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "eval",
		Short: "Measure retrieval recall on the current index",
		Long:  "Run a deterministic self-retrieval smoke test over indexed chunks. Reports Recall@1, Recall@K, MRR and query latency. Needs no API key.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, _, err := g.openExisting(cfg)
			if err != nil {
				return err
			}
			if samples <= 0 {
				return fmt.Errorf("eval: --samples must be positive")
			}
			if topK <= 0 {
				topK = cfg.TopK
			}
			cases := sampleEvalCases(a.Chunks(), samples)
			if len(cases) == 0 {
				return fmt.Errorf("eval: no chunk has enough body text to form a query")
			}

			ctx := commandContext(cmd)
			var foundAt1, foundAtK, reciprocal float64
			latencies := make([]time.Duration, 0, len(cases))
			for _, c := range cases {
				start := time.Now()
				hits, err := a.Search(ctx, c.query, topK)
				latencies = append(latencies, time.Since(start))
				if err != nil {
					return fmt.Errorf("eval: search %q: %w", c.query, err)
				}
				rank := 0
				for i, h := range hits {
					if h.Chunk.ID == c.id {
						rank = i + 1
						break
					}
				}
				if rank == 0 {
					continue
				}
				foundAtK++
				reciprocal += 1.0 / float64(rank)
				if rank == 1 {
					foundAt1++
				}
			}

			n := float64(len(cases))
			sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
			r := evalReport{
				Queries:      len(cases),
				TopK:         topK,
				Retrieval:    cfg.RetrievalMethod,
				RecallAt1:    foundAt1 / n,
				RecallAtK:    foundAtK / n,
				MRR:          reciprocal / n,
				LatencyP50MS: millis(latencies[len(latencies)/2]),
				LatencyP95MS: millis(latencies[(len(latencies)*95)/100]),
				Note:         "Self-retrieval smoke test. Queries come from chunk bodies; recall is optimistic.",
			}

			w := cmd.OutOrStdout()
			if jsonOut {
				enc := json.NewEncoder(w)
				enc.SetIndent("", "  ")
				return enc.Encode(r)
			}
			fmt.Fprintf(w, "queries=%d  top_k=%d  retrieval=%s\n", r.Queries, r.TopK, r.Retrieval)
			fmt.Fprintf(w, "recall@1=%.3f  recall@%d=%.3f  mrr=%.3f\n", r.RecallAt1, r.TopK, r.RecallAtK, r.MRR)
			fmt.Fprintf(w, "latency p50=%.1fms  p95=%.1fms\n", r.LatencyP50MS, r.LatencyP95MS)
			fmt.Fprintf(w, "note: %s\n", r.Note)
			return nil
		},
	}
	cmd.Flags().IntVar(&samples, "samples", defaultEvalSamples, "number of sampled chunks used as queries")
	cmd.Flags().IntVar(&topK, "top-k", cfg.TopK, "number of results fetched per query")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "print the report as JSON")
	return cmd
}

// sampleEvalCases selects chunks at a fixed stride so repeated runs use the
// same queries. Each query is a trimmed prefix of the chunk body.
func sampleEvalCases(chunks []index.Chunk, n int) []evalCase {
	if len(chunks) == 0 || n <= 0 {
		return nil
	}
	step := len(chunks) / n
	if step < 1 {
		step = 1
	}
	out := make([]evalCase, 0, n)
	for i := 0; i < len(chunks) && len(out) < n; i += step {
		runes := []rune(chunks[i].Body)
		if len(runes) < minEvalBodyRunes {
			continue
		}
		end := min(evalQueryRunes, len(runes))
		query := strings.TrimSpace(string(runes[:end]))
		if len([]rune(query)) < minEvalBodyRunes/2 {
			continue
		}
		out = append(out, evalCase{id: chunks[i].ID, query: query})
	}
	return out
}

func millis(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}
