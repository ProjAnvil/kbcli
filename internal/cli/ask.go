package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ProjAnvil/kbcli/internal/config"
)

// newAskCmd builds the headless "kbcli ask" command. It writes only the answer
// to stdout unless --json is set; diagnostics always go to stderr in text mode.
func newAskCmd(cfg *config.Config, g *globals) *cobra.Command {
	var jsonOut bool
	var explain bool
	cmd := &cobra.Command{
		Use:   "ask <question>",
		Short: "Answer a question from the index without the TUI",
		Long:  "Headless grounded answer. Use --json for a stable machine-readable envelope.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a, _, err := g.openExisting(cfg)
			if err != nil {
				return exitError(3, err)
			}
			question := strings.Join(args, " ")
			answer, hits, askErr := a.Ask(commandContext(cmd), nil, question)
			meta := a.LastMeta()
			if askErr != nil {
				return exitError(askExitCode(askErr), fmt.Errorf("ask: %w", askErr))
			}
			cfg := a.Config()
			payload := askResult{
				Answer:        answer,
				Question:      question,
				Profile:       cfg.Profile,
				Embedder:      cfg.Embedder,
				Rerank:        cfg.RerankType,
				RerankEnabled: cfg.RerankEnabled,
				Intent:        meta.Intent,
				IntentEngine:  meta.IntentEngine,
				IntentReason:  meta.IntentReason,
				Condensed:     meta.Condensed,
				Original:      meta.Original,
				Rewritten:     meta.Rewritten,
				SearchQuery:   meta.SearchQuery,
				Translated:    meta.Translated,
				From:          meta.QueryLang,
				To:            meta.KBLang,
				Degraded:      meta.Degraded,
				Reason:        meta.Reason,
				Hits:          make([]searchHit, 0, len(hits)),
			}
			for i, h := range hits {
				payload.Hits = append(payload.Hits, searchHit{
					Rank:    i + 1,
					Score:   h.Score,
					Source:  h.Chunk.Source,
					Heading: h.Chunk.Heading,
					Body:    h.Chunk.Body,
					ID:      h.Chunk.ID,
				})
			}
			out := cmd.OutOrStdout()
			if jsonOut {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(payload)
			}
			fmt.Fprintln(out, answer)
			if explain {
				errOut := cmd.ErrOrStderr()
				fmt.Fprintf(errOut, "profile=%s embedder=%s rerank=%s intent=%s translated=%t degraded=%t\n",
					payload.Profile, payload.Embedder, payload.Rerank, payload.Intent,
					payload.Translated, payload.Degraded)
				for _, h := range payload.Hits {
					fmt.Fprintf(errOut, "#%d score=%.4f %s %s\n", h.Rank, h.Score, h.Source, h.Heading)
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "print a stable JSON envelope for tool consumers")
	cmd.Flags().BoolVar(&explain, "explain", false, "print retrieval diagnostics to stderr")
	return cmd
}

func askExitCode(err error) int {
	if err == nil {
		return 0
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "retrieve evidence"), strings.Contains(msg, "reindex"):
		return 5
	case strings.Contains(msg, "completion"), strings.Contains(msg, "chat model"), strings.Contains(msg, "LLM"):
		return 4
	default:
		return 4
	}
}

type askResult struct {
	Answer        string      `json:"answer"`
	Question      string      `json:"question"`
	Profile       string      `json:"profile"`
	Embedder      string      `json:"embedder"`
	Rerank        string      `json:"rerank"`
	RerankEnabled bool        `json:"rerank_enabled"`
	Intent        string      `json:"intent"`
	IntentEngine  string      `json:"intent_engine"`
	IntentReason  string      `json:"intent_reason"`
	Condensed     bool        `json:"condensed"`
	Original      string      `json:"original"`
	Rewritten     string      `json:"rewritten"`
	SearchQuery   string      `json:"search_query"`
	Translated    bool        `json:"translation_applied"`
	From          string      `json:"query_language"`
	To            string      `json:"kb_language"`
	Degraded      bool        `json:"degraded"`
	Reason        string      `json:"reason"`
	Hits          []searchHit `json:"hits"`
}
