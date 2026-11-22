package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ProjAnvil/kbcli/internal/config"
	"github.com/ProjAnvil/kbcli/internal/i18n"
)

// wizardApplyStub records the draft and flags passed to SetupApply.
type wizardApplyStub struct {
	draft     *config.Config
	skipCheck bool
	status    string
	err       error
	calls     int
}

func (s *wizardApplyStub) apply(draft *config.Config, skipCheck bool) (string, error) {
	s.calls++
	s.draft = draft
	s.skipCheck = skipCheck
	return s.status, s.err
}

// newWizardModel builds the first-run model and applies a terminal size so
// View() renders the full screen instead of the "starting" placeholder.
func newWizardModel(t *testing.T, stub *wizardApplyStub, mutate func(*Options)) *model {
	t.Helper()
	opts := Options{
		Config:      config.Default(),
		SetupNeeded: true,
		SetupApply:  stub.apply,
	}
	if mutate != nil {
		mutate(&opts)
	}
	m := newModel(opts)
	got, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return got.(*model)
}

func wizardKey(k tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: k} }

func wizardRunes(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func wizardUpdate(m *model, msg tea.Msg) (*model, tea.Cmd) {
	got, cmd := m.Update(msg)
	return got.(*model), cmd
}

func TestSetupWizardBlocksInputUntilSaved(t *testing.T) {
	stub := &wizardApplyStub{status: "saved"}
	m := newWizardModel(t, stub, nil)

	if !m.setupActive {
		t.Fatal("wizard must be active on first run")
	}
	if view := m.View(); !strings.Contains(view, i18n.LangEn.T("setup.title")) {
		t.Fatalf("wizard view missing title: %q", view)
	}

	m, _ = wizardUpdate(m, wizardRunes("hello"))
	if strings.Contains(m.input.Value(), "hello") {
		t.Fatalf("typing leaked into the chat input while wizard active: %q", m.input.Value())
	}
}

func TestSetupWizardSaveAppliesDraft(t *testing.T) {
	stub := &wizardApplyStub{status: "key saved"}
	m := newWizardModel(t, stub, nil)

	m, _ = wizardUpdate(m, wizardKey(tea.KeyEnter))         // step 0 -> 1
	m, _ = wizardUpdate(m, wizardRunes("sk-test"))          // step 1 API key
	m, _ = wizardUpdate(m, wizardKey(tea.KeyEnter))         // step 1 -> 2
	m, _ = wizardUpdate(m, wizardKey(tea.KeyEnter))         // step 2 -> 3 (keep default base URL)
	m, applyCmd := wizardUpdate(m, wizardKey(tea.KeyEnter)) // step 3 -> save

	if applyCmd == nil {
		t.Fatal("save did not start the apply command")
	}
	done := applyCmd()
	m, _ = wizardUpdate(m, done)

	if m.setupActive {
		t.Fatal("wizard did not close after a successful save")
	}
	if stub.calls != 1 {
		t.Fatalf("SetupApply called %d times, want 1", stub.calls)
	}
	if stub.draft == nil {
		t.Fatal("SetupApply received a nil draft")
	}
	if !stub.draft.SetupDone {
		t.Fatal("saved draft must have SetupDone=true")
	}
	if stub.draft.APIKey != "sk-test" {
		t.Fatalf("saved APIKey = %q, want sk-test", stub.draft.APIKey)
	}
	switch stub.draft.ChatProvider {
	case config.ChatProviderOpenAI, config.ChatProviderAnthropic:
	default:
		t.Fatalf("saved provider is invalid: %q", stub.draft.ChatProvider)
	}
	if stub.skipCheck {
		t.Fatal("save must not skip the endpoint check")
	}
	if !chatHas(m, "key saved") {
		t.Fatalf("status line was not appended to chat: %v", m.chatLines)
	}
}

func TestSetupWizardSkipSetsSetupDone(t *testing.T) {
	stub := &wizardApplyStub{}
	m := newWizardModel(t, stub, nil)

	m, applyCmd := wizardUpdate(m, wizardKey(tea.KeyCtrlS))
	if applyCmd == nil {
		t.Fatal("ctrl+s did not start the skip apply command")
	}
	done := applyCmd()
	m, _ = wizardUpdate(m, done)

	if stub.calls != 1 {
		t.Fatalf("SetupApply called %d times, want 1", stub.calls)
	}
	if !stub.skipCheck {
		t.Fatal("skip must bypass the endpoint check")
	}
	if stub.draft == nil || !stub.draft.SetupDone {
		t.Fatal("skip draft must have SetupDone=true")
	}
	if stub.draft.APIKey != "" {
		t.Fatalf("skip must leave the API key empty, got %q", stub.draft.APIKey)
	}
	if m.setupActive {
		t.Fatal("wizard did not close after skip")
	}
	if !chatHas(m, i18n.LangEn.T("setup.skipped")) {
		t.Fatalf("setup.skipped text missing from chat: %v", m.chatLines)
	}
}

func TestSetupWizardStaysOpenOnApplyError(t *testing.T) {
	stub := &wizardApplyStub{err: errors.New("disk full")}
	m := newWizardModel(t, stub, nil)

	m, applyCmd := wizardUpdate(m, wizardKey(tea.KeyCtrlS))
	if applyCmd == nil {
		t.Fatal("ctrl+s did not start the apply command")
	}
	done := applyCmd()
	m, _ = wizardUpdate(m, done)

	if !m.setupActive {
		t.Fatal("wizard must stay open when SetupApply fails")
	}
	want := i18n.LangEn.Tf("setup.failed", "disk full")
	if view := m.View(); !strings.Contains(view, want) {
		t.Fatalf("wizard view missing failure text %q: %q", want, view)
	}
}

func TestSetupWizardNotShownWhenNotNeeded(t *testing.T) {
	stub := &wizardApplyStub{status: "saved"}
	m := newWizardModel(t, stub, func(o *Options) { o.SetupNeeded = false })

	if m.setupActive {
		t.Fatal("wizard must not be active when SetupNeeded is false")
	}
	view := m.View()
	if strings.Contains(view, i18n.LangEn.T("setup.title")) {
		t.Fatalf("normal view must not render the wizard: %q", view)
	}
	if !strings.Contains(view, "enter send") {
		t.Fatalf("normal chat view missing help line: %q", view)
	}
}

func TestSetupWizardStartsBootAfterClose(t *testing.T) {
	stub := &wizardApplyStub{status: "saved"}
	m := newWizardModel(t, stub, func(o *Options) {
		o.EnsureModel = func(func(ModelProgress)) error { return nil }
	})
	if !m.booting {
		t.Fatal("EnsureModel must open the boot modal")
	}

	m, _ = wizardUpdate(m, wizardKey(tea.KeyEnter))
	m, _ = wizardUpdate(m, wizardRunes("sk-test"))
	m, _ = wizardUpdate(m, wizardKey(tea.KeyEnter))
	m, _ = wizardUpdate(m, wizardKey(tea.KeyEnter))
	m, applyCmd := wizardUpdate(m, wizardKey(tea.KeyEnter))
	if applyCmd == nil {
		t.Fatal("save did not start the apply command")
	}
	done := applyCmd()
	m, bootCmd := wizardUpdate(m, done)

	if !m.booting {
		t.Fatal("boot must still be pending after the wizard closes")
	}
	if bootCmd == nil {
		t.Fatal("closing the wizard must return the boot start command while booting")
	}
}

func TestSetupWizardChinese(t *testing.T) {
	stub := &wizardApplyStub{status: "saved"}
	m := newWizardModel(t, stub, func(o *Options) {
		o.Config.Language = config.LangZh
	})
	if view := m.View(); !strings.Contains(view, i18n.LangZh.T("setup.title")) {
		t.Fatalf("Chinese wizard view missing title: %q", view)
	}
}

func TestSetupWizardBareSIsTypedIntoKeyField(t *testing.T) {
	stub := &wizardApplyStub{status: "saved"}
	m := newWizardModel(t, stub, nil)

	m, _ = wizardUpdate(m, wizardKey(tea.KeyEnter)) // step 0 -> 1 (API key)
	m, _ = wizardUpdate(m, wizardRunes("s"))

	if !m.setupActive {
		t.Fatal("a bare s must not skip the wizard on step 1")
	}
	if m.setup.step != 1 {
		t.Fatalf("a bare s must not change the step, got %d", m.setup.step)
	}
	if stub.calls != 0 {
		t.Fatal("a bare s must not invoke SetupApply")
	}
	if m.setup.input.Value() != "s" {
		t.Fatalf("bare s was not typed into the key field, value = %q", m.setup.input.Value())
	}
}

func TestSetupWizardSkipReportsOnceWithRealStatus(t *testing.T) {
	// SetupApply returns the localized skip note as its status while
	// setupApplyDoneMsg also carries skipped=true. The wizard must report
	// that note exactly once, not append both copies.
	note := i18n.LangEn.T("setup.skipped")
	stub := &wizardApplyStub{status: note}
	m := newWizardModel(t, stub, nil)

	m, applyCmd := wizardUpdate(m, wizardKey(tea.KeyCtrlS))
	if applyCmd == nil {
		t.Fatal("ctrl+s did not start the apply command")
	}
	m, _ = wizardUpdate(m, applyCmd())

	count := 0
	for _, line := range m.chatLines {
		count += strings.Count(line.text, note)
	}
	if count != 1 {
		t.Fatalf("skip note appeared %d times, want exactly 1: %v", count, m.chatLines)
	}
}
