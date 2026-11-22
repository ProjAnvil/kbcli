package agent

import (
	"sync"

	"github.com/ProjAnvil/kbcli/internal/index"
)

// citationLedger assigns every chunk discovered during one answer a stable
// 1-based [n] number. The grounded prompt and the kb_search tool share the
// ledger, so a follow-up lookup cannot silently renumber the evidence the
// model already cited. The TUI sink is published in ledger order, which keeps
// the visible evidence pane aligned with the numbers in the answer.
//
// The ledger is reset at the start of every ask: each turn builds a fresh
// prompt, so numbers must start at 1 again instead of growing forever.
type citationLedger struct {
	mu      sync.Mutex
	order   []index.Hit
	numbers map[string]int
}

func newCitationLedger() *citationLedger {
	return &citationLedger{numbers: make(map[string]int)}
}

// Reset starts a new answer.
func (l *citationLedger) Reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.order = nil
	l.numbers = make(map[string]int)
}

// Register assigns numbers to hits and returns the numbers in input order
// plus a defensive copy of the full ordered evidence list.
func (l *citationLedger) Register(hits []index.Hit) ([]int, []index.Hit) {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]int, len(hits))
	for i, h := range hits {
		id := h.Chunk.ID
		n, ok := l.numbers[id]
		if !ok {
			l.order = append(l.order, h)
			n = len(l.order)
			l.numbers[id] = n
		}
		out[i] = n
	}
	all := make([]index.Hit, len(l.order))
	copy(all, l.order)
	return out, all
}

// sequentialNumbers is the fallback when no ledger is attached (for example a
// runner built by hand in a unit test).
func sequentialNumbers(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i + 1
	}
	return out
}
