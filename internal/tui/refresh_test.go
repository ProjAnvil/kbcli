package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/ProjAnvil/kbcli/internal/agent"
	"github.com/ProjAnvil/kbcli/internal/index"
)

// A reindex or update command replaces the index object inside app.App. The
// TUI must rebind its answerer, or every later question would still hit the
// stale vectors.
func TestCommandResultRebindsAnswerer(t *testing.T) {
	old := &stubAnswerer{answer: "stale"}
	fresh := &stubAnswerer{answer: "fresh"}
	rebuilt := stubStore{root: "/rebuilt"}

	opts := baseOpts()
	opts.Runner = old
	opts.Refresh = func() (index.Store, agent.Answerer) { return rebuilt, fresh }

	m := testModel(t, opts)
	got, _ := m.Update(commandResultMsg{line: "reindex", status: "reindex done"})
	m = got.(*model)

	if m.opts.Runner != fresh {
		t.Fatal("answerer was not rebound after reindex")
	}
	if m.opts.Store == nil || m.opts.Store.Root() != "/rebuilt" {
		t.Fatalf("store was not rebound, root = %v", m.opts.Store)
	}
	if !chatHas(m, "reindex done") {
		t.Fatal("command status missing from chat")
	}

	msg := askCmd(m.opts, m.timeout, nil, "question")()
	if _, ok := msg.(answerMsg); !ok {
		t.Fatalf("askCmd returned %T, want answerMsg", msg)
	}
	if !fresh.called {
		t.Fatal("rebound answerer was not used")
	}
	if old.called {
		t.Fatal("stale answerer was still used")
	}
}

func TestCommandResultFailureDoesNotRebind(t *testing.T) {
	old := &stubAnswerer{answer: "current"}
	opts := baseOpts()
	opts.Runner = old
	called := false
	opts.Refresh = func() (index.Store, agent.Answerer) {
		called = true
		return nil, nil
	}

	m := testModel(t, opts)
	got, _ := m.Update(commandResultMsg{line: "reindex", err: errors.New("boom")})
	m = got.(*model)

	if called {
		t.Fatal("Refresh must not run after a failed command")
	}
	if m.opts.Runner != old {
		t.Fatal("answerer changed after a failed command")
	}
	if !chatHas(m, "command error:") {
		t.Fatal("command error missing from chat")
	}
}

func TestNoIndexGivesActionableMessage(t *testing.T) {
	opts := baseOpts()
	opts.Runner = nil
	msg := askCmd(opts, 0, nil, "question")()
	answer, ok := msg.(answerMsg)
	if !ok {
		t.Fatalf("askCmd returned %T, want answerMsg", msg)
	}
	if answer.err == nil {
		t.Fatal("expected an error without an index")
	}
	if !strings.Contains(answer.err.Error(), "kbcli index") {
		t.Fatalf("error %q does not tell the user to run kbcli index", answer.err)
	}
}
