package tui

import (
	"github.com/charmbracelet/bubbles/key"

	"github.com/ProjAnvil/kbcli/internal/i18n"
)

// quitKey is the global quit binding. It works before a model exists, so
// settings and confirmation screens can share it without a keyMap.
var quitKey = key.NewBinding(key.WithKeys("ctrl+c"))

// maxWindows is the number of addressable windows. Each window gets a slot
// in the prefix digit bindings.
const maxWindows = 9

// keyMap holds every binding used by the chat screen. Prefix bindings carry
// only the second key; their help text shows the full sequence.
type keyMap struct {
	Quit     key.Binding
	QuitQ    key.Binding
	Send     key.Binding
	Focus    key.Binding
	Evidence key.Binding
	Up       key.Binding
	Down     key.Binding
	PageUp   key.Binding
	PageDown key.Binding

	Prefix      key.Binding
	NewWindow   key.Binding
	NextWindow  key.Binding
	PrevWindow  key.Binding
	CloseWindow key.Binding
	WindowList  key.Binding
	Windows     [maxWindows]key.Binding
}

// newKeyMap builds bindings with localized help text. key.Binding values are
// copied into the model, so each model owns its help strings.
func newKeyMap(lang i18n.Lang) keyMap {
	k := keyMap{
		Quit: key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("ctrl+c", lang.T("keys.quit")),
		),
		QuitQ: key.NewBinding(
			key.WithKeys("q"),
			key.WithHelp("q", lang.T("keys.quit")),
		),
		Send: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", lang.T("keys.send")),
		),
		Focus: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", lang.T("keys.focus")),
		),
		Evidence: key.NewBinding(
			key.WithKeys("ctrl+]"),
			key.WithHelp("ctrl+]", lang.T("keys.evidence")),
		),
		Up: key.NewBinding(
			key.WithKeys("up"),
			key.WithHelp("up", lang.T("keys.up")),
		),
		Down: key.NewBinding(
			key.WithKeys("down"),
			key.WithHelp("down", lang.T("keys.down")),
		),
		PageUp: key.NewBinding(
			key.WithKeys("pgup"),
			key.WithHelp("pgup", lang.T("keys.page_up")),
		),
		PageDown: key.NewBinding(
			key.WithKeys("pgdown"),
			key.WithHelp("pgdn", lang.T("keys.page_down")),
		),
		Prefix: key.NewBinding(
			key.WithKeys("ctrl+x"),
			key.WithHelp("ctrl+x", lang.T("keys.prefix")),
		),
		NewWindow: key.NewBinding(
			key.WithKeys("t"),
			key.WithHelp("ctrl+x t", lang.T("keys.new_window")),
		),
		NextWindow: key.NewBinding(
			key.WithKeys("n"),
			key.WithHelp("ctrl+x n", lang.T("keys.next_window")),
		),
		PrevWindow: key.NewBinding(
			key.WithKeys("p"),
			key.WithHelp("ctrl+x p", lang.T("keys.prev_window")),
		),
		CloseWindow: key.NewBinding(
			key.WithKeys("x"),
			key.WithHelp("ctrl+x x", lang.T("keys.close_window")),
		),
		WindowList: key.NewBinding(
			key.WithKeys("w"),
			key.WithHelp("ctrl+x w", lang.T("keys.window_list")),
		),
	}
	p := lang.T("keys.window")
	for i := range k.Windows {
		d := string(rune('0' + i))
		k.Windows[i] = key.NewBinding(
			key.WithKeys(d),
			key.WithHelp("ctrl+x "+d, p),
		)
	}
	return k
}

// ShortHelp implements help.KeyMap. It shows the keys used most often.
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Send, k.Focus, k.NewWindow, k.Quit}
}

// FullHelp implements help.KeyMap. Each inner slice renders as one column.
func (k keyMap) FullHelp() [][]key.Binding {
	win := make([]key.Binding, 0, maxWindows+1)
	win = append(win, k.NewWindow, k.NextWindow, k.PrevWindow, k.CloseWindow, k.WindowList)
	win = append(win, k.Windows[:]...)
	return [][]key.Binding{
		{k.Send, k.Focus, k.Evidence, k.Quit},
		{k.Up, k.Down, k.PageUp, k.PageDown},
		win,
	}
}
