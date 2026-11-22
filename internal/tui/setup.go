package tui

import (
	"errors"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/ProjAnvil/kbcli/internal/config"
	"github.com/ProjAnvil/kbcli/internal/i18n"
)

// setupStepCount is the number of wizard steps: provider, API key, base URL,
// model.
const setupStepCount = 4

// setupSkipKey skips the wizard on any step. It lives outside keyMap so the
// chat bindings and their help text stay untouched.
var setupSkipKey = key.NewBinding(key.WithKeys("ctrl+s"))

// setupState owns everything the first-run wizard needs. The wizard is modal:
// while setupActive is true no key reaches chat, settings, confirm or boot.
type setupState struct {
	step        int
	draft       *config.Config
	input       textinput.Model
	providerIdx int // 0 = openai, 1 = anthropic
	busy        bool
	err         error
}

// newSetupState seeds the wizard from the effective config. The draft is a
// clone, so editing never mutates the live config before SetupApply persists.
func newSetupState(opts Options, lang i18n.Lang) setupState {
	draft := opts.Config.Clone()
	idx := 0
	if draft.ChatProvider == config.ChatProviderAnthropic {
		idx = 1
	}
	ti := textinput.New()
	ti.Prompt = "> "
	ti.CharLimit = 512
	// Focus up front so key forwarding works even when Init is not called
	// (e.g. in tests) and stays valid across step transitions.
	ti.Focus()
	return setupState{
		step:        0,
		draft:       draft,
		input:       ti,
		providerIdx: idx,
	}
}

// setupFocus returns the command that focuses the wizard input. Step 0 has no
// text input, so it returns nil there.
func (m *model) setupFocus() tea.Cmd {
	if m.setup.step == 0 {
		return nil
	}
	return m.setup.input.Focus()
}

// setupProvider maps providerIdx to the config provider value.
func (m *model) setupProvider() string {
	if m.setup.providerIdx == 1 {
		return config.ChatProviderAnthropic
	}
	return config.ChatProviderOpenAI
}

// setupDefaultURL maps providerIdx to the provider's default endpoint.
func (m *model) setupDefaultURL() string {
	if m.setup.providerIdx == 1 {
		return config.DefaultAnthropicBaseURL
	}
	return config.DefaultOpenAIBaseURL
}

// setupEnterStep rebuilds the shared input for step. Step 1 always clears the
// value (a secret is never pre-filled) and masks it; steps 2 and 3 pre-fill
// from the draft so a plain enter keeps the current value.
func (m *model) setupEnterStep(step int) {
	m.setup.step = step
	m.setup.err = nil
	switch step {
	case 0:
		m.setup.input.Blur()
		m.setup.input.SetValue("")
	case 1:
		m.setup.input.EchoMode = textinput.EchoPassword
		m.setup.input.SetValue("")
		m.setup.input.Focus()
		m.setup.input.CursorEnd()
	case 2:
		m.setup.input.EchoMode = textinput.EchoNormal
		m.setup.input.SetValue(m.setup.draft.BaseURL)
		m.setup.input.Focus()
		m.setup.input.CursorEnd()
	case 3:
		m.setup.input.EchoMode = textinput.EchoNormal
		m.setup.input.SetValue(m.setup.draft.Model)
		m.setup.input.Focus()
		m.setup.input.CursorEnd()
	}
}

// setupStoreInput commits the current input value to the draft field owned by
// the active step.
func (m *model) setupStoreInput() {
	v := m.setup.input.Value()
	switch m.setup.step {
	case 1:
		m.setup.draft.APIKey = v
	case 2:
		m.setup.draft.BaseURL = v
	case 3:
		m.setup.draft.Model = v
	}
}

// setupToggleProvider flips the provider selection and resets the base URL to
// that provider's default endpoint.
func (m *model) setupToggleProvider() {
	m.setup.providerIdx = (m.setup.providerIdx + 1) % 2
	m.setup.draft.ChatProvider = m.setupProvider()
	m.setup.draft.BaseURL = m.setupDefaultURL()
}

// updateSetup owns every key while the wizard is active.
func (m *model) updateSetup(msg tea.KeyMsg) tea.Cmd {
	if key.Matches(msg, m.keys.Quit) {
		return tea.Quit
	}
	if m.setup.busy {
		return nil
	}
	if key.Matches(msg, setupSkipKey) {
		return m.setupSkip()
	}
	if m.setup.step == 0 {
		return m.updateSetupProvider(msg)
	}
	return m.updateSetupInput(msg)
}

// updateSetupProvider handles the provider picker (step 0), which has no text
// input. A bare s skips here; on later steps s must be typed into the field.
func (m *model) updateSetupProvider(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		return nil
	case "s":
		return m.setupSkip()
	case "enter":
		m.setup.draft.ChatProvider = m.setupProvider()
		m.setupEnterStep(1)
		return nil
	case "left", "right", "up", "down", "h", "l":
		m.setupToggleProvider()
		return nil
	}
	return nil
}

// updateSetupInput handles the text-input steps (1..3).
func (m *model) updateSetupInput(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.setupEnterStep(m.setup.step - 1)
		return nil
	case "enter":
		m.setupStoreInput()
		if m.setup.step == setupStepCount-1 {
			return m.setupSave()
		}
		m.setupEnterStep(m.setup.step + 1)
		return nil
	}
	// Any other key (printable runes, backspace, cursor moves) belongs to the
	// text input; propagate its command so the cursor keeps blinking.
	var cmd tea.Cmd
	m.setup.input, cmd = m.setup.input.Update(msg)
	return cmd
}

// setupSave validates the API key, then persists the draft with a connectivity
// check. An empty key is a validation error, not a save.
func (m *model) setupSave() tea.Cmd {
	draft := m.setup.draft.Clone()
	draft.SetupDone = true
	draft.ChatProvider = m.setupProvider()
	if strings.TrimSpace(draft.APIKey) == "" {
		m.setup.err = errors.New(m.lang.T("setup.empty_key"))
		return nil
	}
	m.setup.busy = true
	m.setup.err = nil
	return setupApplyCmd(m.opts, draft, false, false)
}

// setupSkip persists the untouched config with SetupDone set, bypassing the
// endpoint check so an offline user is never blocked at first run.
func (m *model) setupSkip() tea.Cmd {
	draft := m.opts.Config.Clone()
	draft.SetupDone = true
	m.setup.busy = true
	m.setup.err = nil
	return setupApplyCmd(m.opts, draft, true, true)
}

// onSetupApplyDone closes the wizard on success or keeps it open on error. The
// callback's status is appended verbatim; a skip reports its own note instead
// so the same line is never written twice.
func (m *model) onSetupApplyDone(msg setupApplyDoneMsg) tea.Cmd {
	m.setup.busy = false
	if msg.err != nil {
		m.setup.err = msg.err
		return nil
	}
	m.setupActive = false
	if msg.skipped {
		m.appendChat(m.lang.T("setup.skipped"), lineStatus)
	} else if msg.status != "" {
		m.appendChat(msg.status, lineStatus)
	}
	if m.booting {
		return m.startBoot()
	}
	return m.input.Focus()
}

// setupView renders the full-screen wizard. Every potentially long line is
// truncated to the terminal width so the layout never overflows minWidth.
func (m *model) setupView() string {
	w := m.width
	if w <= 0 {
		w = minWidth
	}
	var b strings.Builder
	b.WriteString(bootLogoStyle.Render(bootLogo))
	b.WriteString("\n\n")
	b.WriteString(headerStyle.Render(truncate(m.lang.T("setup.title"), w)))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render(truncate(m.lang.T("setup.subtitle"), w)))
	b.WriteString("\n\n")
	b.WriteString(settingsStatusStyle.Render(m.lang.Tf("setup.progress", m.setup.step+1)))
	b.WriteString("\n")

	steps := []string{
		m.lang.T("setup.step_provider"),
		m.lang.T("setup.step_api_key"),
		m.lang.T("setup.step_base_url"),
		m.lang.T("setup.step_model"),
	}
	for i, label := range steps {
		prefix := "  "
		style := tabInactiveStyle
		if i == m.setup.step {
			prefix = "> "
			style = tabActiveStyle
		}
		b.WriteString(style.Render(truncate(prefix+label, w)))
		b.WriteString("\n")
	}

	if m.setup.step == 0 {
		openai := "[ openai ]"
		anthropic := "[ anthropic ]"
		if m.setup.providerIdx == 1 {
			b.WriteString(tabInactiveStyle.Render(openai) + "  " + tabActiveStyle.Render(anthropic))
		} else {
			b.WriteString(tabActiveStyle.Render(openai) + "  " + tabInactiveStyle.Render(anthropic))
		}
	} else {
		b.WriteString(truncate(m.setup.input.View(), w))
	}
	b.WriteString("\n")

	hints := []string{"setup.provider_hint", "setup.key_hint", "setup.url_hint", "setup.model_hint"}
	b.WriteString(helpStyle.Render(truncate(m.lang.T(hints[m.setup.step]), w)))
	b.WriteString("\n")

	if m.setup.busy {
		b.WriteString(settingsStatusStyle.Render(truncate(m.lang.T("setup.checking"), w)))
		b.WriteString("\n")
	} else if m.setup.err != nil {
		b.WriteString(errorStyle.Render(truncate(m.lang.Tf("setup.failed", m.setup.err.Error()), w)))
		b.WriteString("\n")
	}

	hint := m.lang.T("setup.hint")
	if m.setup.step == setupStepCount-1 {
		hint = m.lang.T("setup.hint_last")
	}
	b.WriteString(helpStyle.Render(truncate(hint, w)))
	return b.String()
}
