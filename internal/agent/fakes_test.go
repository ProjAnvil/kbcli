package agent

import (
	"context"
	"fmt"

	"github.com/ProjAnvil/kbcli/internal/config"
	"github.com/ProjAnvil/kbcli/internal/index"
	"github.com/ProjAnvil/kbcli/internal/retrieval"
	"github.com/projanvil/langchain-golang/core/messages"
)

// fakeRetriever is a minimal retrieval.Retriever for agent tests. It returns
// canned hits/errors and records the arguments of the last call so tests can
// assert how the runner and the tool drive the optimized pipeline.
type fakeRetriever struct {
	hits []index.Hit
	err  error

	searchCalls      int
	withHistoryCalls int
	gotQuery         string
	gotK             int
	gotHistory       []retrieval.Turn
}

func (f *fakeRetriever) Search(ctx context.Context, query string, k int) ([]index.Hit, error) {
	f.searchCalls++
	f.gotQuery, f.gotK = query, k
	return f.hits, f.err
}

func (f *fakeRetriever) SearchWithHistory(ctx context.Context, query string, k int, history []retrieval.Turn) ([]index.Hit, error) {
	f.withHistoryCalls++
	f.gotQuery, f.gotK, f.gotHistory = query, k, history
	return f.hits, f.err
}

// stubInvoker records the messages it received and returns canned output.
type stubInvoker struct {
	msgs []messages.Message
	err  error
	got  []messages.Message
}

func (s *stubInvoker) Invoke(ctx context.Context, msgs []messages.Message) ([]messages.Message, error) {
	s.got = msgs
	return s.msgs, s.err
}

// makeHits builds n predictable hits for assertions.
func makeHits(n int) []index.Hit {
	hits := make([]index.Hit, n)
	for i := range hits {
		hits[i] = index.Hit{
			Rank:  i + 1,
			Score: 0.9 - float64(i)*0.1,
			Dist:  float64(i) * 0.1,
			Chunk: index.Chunk{
				ID:       fmt.Sprintf("id-%d", i+1),
				Source:   fmt.Sprintf("doc%d.md", i+1),
				Body:     fmt.Sprintf("body %d", i+1),
				Text:     fmt.Sprintf("text %d", i+1),
				Vector:   []float32{float32(i)},
				Metadata: map[string]string{"k": "v"},
			},
		}
	}
	return hits
}

// testConfig returns a minimal, valid config for runner/tool tests. The
// context-budget fields mirror Default so TrimToBudget/FormatSources run
// without trimming the tiny test evidence.
func testConfig() *config.Config {
	return &config.Config{
		Model:                "test-model",
		BaseURL:              "http://localhost:1",
		Agent:                config.AgentReact,
		TopK:                 5,
		MaxIterations:        3,
		TimeoutSeconds:       5,
		ContextCharBudget:    16000,
		ContextMaxChunkChars: 800,
	}
}
