package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ProjAnvil/kbcli/internal/agent"
	"github.com/ProjAnvil/kbcli/internal/index"
)

// blockingStreamAnswerer blocks until its context is cancelled so a test can
// press esc while the request is genuinely in flight.
type blockingStreamAnswerer struct {
	stubAnswerer
	started chan struct{}
	done    chan struct{}
	ctxCh   chan context.Context
}

func (b *blockingStreamAnswerer) AskStream(ctx context.Context, history []agent.Turn, question string, onDelta func(string)) (string, []index.Hit, error) {
	b.ctxCh <- ctx
	close(b.started)
	<-ctx.Done()
	return "", nil, ctx.Err()
}

func TestEscCancelsInFlightRequest(t *testing.T) {
	opts := baseOpts()
	sa := &blockingStreamAnswerer{
		started: make(chan struct{}),
		done:    make(chan struct{}),
		ctxCh:   make(chan context.Context, 1),
	}
	opts.Runner = sa
	m := testModel(t, opts)
	m.input.SetValue("question")
	got, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = got.(*model)
	if !m.busy {
		t.Fatal("busy was not set before asking")
	}

	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) < 2 {
		t.Fatalf("ask cmd = %T, want a batched command", cmd())
	}
	go func() {
		batch[1]() // producer blocks until esc cancels the context
		close(sa.done)
	}()
	<-sa.started
	ctx := <-sa.ctxCh
	oldGen := m.reqGen

	got, quit := pressKey(m, "esc")
	m = got.(*model)
	if quit != nil {
		if _, isQuit := quit().(tea.QuitMsg); isQuit {
			t.Fatal("esc while busy quit the app")
		}
	}
	if m.busy || m.streaming || m.streamText != "" {
		t.Fatalf("cancel did not clear busy/stream state: %+v", m)
	}
	if m.cancel != nil {
		t.Fatal("cancel func was not released")
	}
	if m.reqGen == oldGen {
		t.Fatal("request generation was not bumped on cancel")
	}
	if !chatHas(m, "generation cancelled") {
		t.Fatal("cancel status line missing from chat")
	}
	select {
	case <-sa.done:
	case <-time.After(2 * time.Second):
		t.Fatal("producer did not unblock after cancel")
	}
	if ctx.Err() == nil {
		t.Fatal("agent context was not cancelled")
	}

	// Late messages from the cancelled generation must be dropped.
	got, _ = m.Update(answerDeltaMsg{text: "stale delta", gen: oldGen})
	m = got.(*model)
	if m.streaming || m.streamText != "" {
		t.Fatal("stale delta was applied after cancel")
	}
	got, _ = m.Update(answerResetMsg{gen: oldGen})
	m = got.(*model)
	if m.busy || m.streaming {
		t.Fatal("stale reset changed state after cancel")
	}
	got, _ = m.Update(answerDoneMsg{answer: "stale answer", gen: oldGen})
	m = got.(*model)
	if chatHas(m, "stale answer") || len(m.history) != 1 {
		t.Fatal("stale done message was applied after cancel")
	}
	if m.busy {
		t.Fatal("stale done re-set busy")
	}
}

func TestEscIdleDoesNotQuit(t *testing.T) {
	m := testModel(t, baseOpts())
	got, cmd := pressKey(m, "esc")
	_ = got
	if cmd != nil {
		if _, isQuit := cmd().(tea.QuitMsg); isQuit {
			t.Fatal("esc while idle quit the app")
		}
	}
}

func TestThinkingSpinnerAdvances(t *testing.T) {
	m := testModel(t, baseOpts())
	m.input.SetValue("question")
	got, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = got.(*model)
	if cmd == nil {
		t.Fatal("ask returned no command")
	}
	if !m.busy {
		t.Fatal("model is not busy while thinking")
	}
	first := m.thinkingLine()
	if !strings.Contains(m.chatVP.View(), "esc") {
		t.Fatal("thinking line does not advertise esc to cancel")
	}

	got, tick := m.Update(thinkingTickMsg{})
	m = got.(*model)
	if tick == nil {
		t.Fatal("spinner tick did not re-arm")
	}
	if m.thinkingLine() == first {
		t.Fatal("spinner frame did not advance")
	}
	frame := spinnerFrames[m.spinFrame%len(spinnerFrames)]
	if !strings.Contains(m.chatVP.View(), frame) {
		t.Fatalf("spinner glyph %q not rendered in the chat viewport", frame)
	}

	// With no busy window left the shared chain stops.
	m.busy = false
	got, tick = m.Update(thinkingTickMsg{})
	m = got.(*model)
	if tick != nil {
		t.Fatal("spinner tick re-armed with no busy window")
	}
	if m.spinRunning {
		t.Fatal("spinRunning was not cleared when the chain stopped")
	}
}

func TestAnswerResetClearsLiveText(t *testing.T) {
	m := testModel(t, baseOpts())
	m.input.SetValue("question")
	got, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = got.(*model)
	gen := m.reqGen

	got, _ = m.Update(answerDeltaMsg{text: "preamble", gen: gen})
	m = got.(*model)
	if !m.streaming || m.streamText != "preamble" {
		t.Fatalf("live text state = %q/%v", m.streamText, m.streaming)
	}
	got, _ = m.Update(answerResetMsg{gen: gen})
	m = got.(*model)
	if m.streaming || m.streamText != "" {
		t.Fatal("reset did not clear tool-round preamble text")
	}
	if !m.busy {
		t.Fatal("reset must keep the request busy")
	}
	if !strings.Contains(m.chatVP.View(), "thinking") {
		t.Fatal("spinner was not restored after reset")
	}
}

// hookedStreamAnswerer emits a tool-round preamble, calls OnReset, then the
// final answer, mirroring a real ReAct stream.
type hookedStreamAnswerer struct {
	stubAnswerer
	reset bool
}

func (h *hookedStreamAnswerer) AskStream(ctx context.Context, history []agent.Turn, question string, onDelta func(string)) (string, []index.Hit, error) {
	return h.AskStreamHooked(ctx, history, question, agent.StreamHooks{OnDelta: onDelta})
}

func (h *hookedStreamAnswerer) AskStreamHooked(ctx context.Context, history []agent.Turn, question string, hooks agent.StreamHooks) (string, []index.Hit, error) {
	hooks.OnDelta("preamble")
	h.reset = true
	if hooks.OnReset != nil {
		hooks.OnReset()
	}
	hooks.OnDelta("final answer")
	return "final answer", nil, nil
}

func TestHookedStreamAnswererDeliversReset(t *testing.T) {
	opts := baseOpts()
	h := &hookedStreamAnswerer{}
	opts.Runner = h
	m := testModel(t, opts)
	m.input.SetValue("question")
	got, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = got.(*model)
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) < 2 {
		t.Fatalf("ask cmd = %T, want a batched command", cmd())
	}
	batch[1]() // producer fills the channel with delta, reset, delta, done
	if !h.reset {
		t.Fatal("AskStreamHooked was not used")
	}

	wait := batch[0]
	resetSeen := false
	for {
		msg := wait()
		if msg == nil {
			t.Fatal("stream ended without a done message")
		}
		if _, isReset := msg.(answerResetMsg); isReset {
			resetSeen = true
		}
		got, next := m.Update(msg)
		m = got.(*model)
		if _, done := msg.(answerDoneMsg); done {
			break
		}
		if next == nil {
			t.Fatalf("message %T did not re-arm the stream waiter", msg)
		}
		wait = next
	}
	if !resetSeen {
		t.Fatal("OnReset was not forwarded as answerResetMsg")
	}
	if m.streaming || m.streamText != "" {
		t.Fatal("live text was not cleared by the reset")
	}
	if !chatHas(m, "final answer") {
		t.Fatal("final answer missing from chat")
	}
	if len(m.history) != 2 {
		t.Fatalf("history length = %d, want user + assistant", len(m.history))
	}
}

func TestBackgroundTabKeepsRunningAndSpinningAfterSwitch(t *testing.T) {
	opts := baseOpts()
	sa := &blockingStreamAnswerer{
		started: make(chan struct{}),
		done:    make(chan struct{}),
		ctxCh:   make(chan context.Context, 1),
	}
	opts.Runner = sa
	m := testModel(t, opts)
	m.input.SetValue("question")
	got, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = got.(*model)
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) < 2 {
		t.Fatalf("ask cmd = %T, want a batched command", cmd())
	}
	go func() {
		batch[1]()
		close(sa.done)
	}()
	<-sa.started
	ctx := <-sa.ctxCh

	// Open a second window while window 0 is still generating.
	got, _ = m.Update(keyPress("ctrl+x"))
	m = got.(*model)
	got, _ = m.Update(keyPress("t"))
	m = got.(*model)
	if m.active != 1 || m.busy {
		t.Fatalf("switch state = active %d / busy %v", m.active, m.busy)
	}
	if ctx.Err() != nil {
		t.Fatal("switching tabs cancelled the background request")
	}
	if !m.windows[0].busy || !m.anyWindowBusy() {
		t.Fatal("background window lost its busy state on switch")
	}
	if !m.windowBusy(0) || m.windowBusy(1) {
		t.Fatal("per-tab busy state is wrong after switch")
	}

	frame := spinnerFrames[m.spinFrame%len(spinnerFrames)]
	if !strings.Contains(m.tabBarView(), frame) {
		t.Fatal("busy background tab has no spinner in the tab bar")
	}
	before := m.spinFrame
	got, tick := m.Update(thinkingTickMsg{})
	m = got.(*model)
	if tick == nil {
		t.Fatal("shared spinner chain stopped after switching to an idle tab")
	}
	if m.spinFrame == before {
		t.Fatal("spinner frame froze while a background tab was running")
	}
	frame = spinnerFrames[m.spinFrame%len(spinnerFrames)]
	if !strings.Contains(m.tabBarView(), frame) {
		t.Fatal("background tab spinner did not advance")
	}

	// Switching back restores the cancel handle; esc can still stop it.
	got, _ = m.Update(keyPress("ctrl+x"))
	m = got.(*model)
	got, _ = m.Update(keyPress("0"))
	m = got.(*model)
	if m.active != 0 || !m.busy || m.cancel == nil {
		t.Fatalf("return state = active %d / busy %v / cancel nil %v", m.active, m.busy, m.cancel == nil)
	}
	got, _ = pressKey(m, "esc")
	m = got.(*model)
	select {
	case <-sa.done:
	case <-time.After(2 * time.Second):
		t.Fatal("background request did not unblock after esc")
	}
	if ctx.Err() == nil {
		t.Fatal("background request context was not cancelled")
	}
	if m.windowBusy(0) {
		t.Fatal("tab 0 still shows busy after cancel")
	}
}

func TestTabBarReservesSpinnerCell(t *testing.T) {
	m := testModel(t, baseOpts())
	m.width = 1 // disable right-padding so width reflects the tabs alone
	idle := lipgloss.Width(m.tabBarView())
	m.busy = true
	busy := lipgloss.Width(m.tabBarView())
	if busy != idle {
		t.Fatalf("tab width changed when the spinner appeared: idle=%d busy=%d", idle, busy)
	}
	frame := spinnerFrames[m.spinFrame%len(spinnerFrames)]
	if !strings.Contains(m.tabBarView(), frame) {
		t.Fatal("busy tab did not render the spinner in its reserved cell")
	}

	// A second window gets the same reserved cell, busy or not.
	m.windows = append(m.windows, windowState{})
	m.windowTitles = append(m.windowTitles, "2")
	idle2 := lipgloss.Width(m.tabBarView())
	m.windows[1] = windowState{busy: true}
	busy2 := lipgloss.Width(m.tabBarView())
	if busy2 != idle2 {
		t.Fatalf("two-tab width changed: idle=%d busy=%d", idle2, busy2)
	}
}
