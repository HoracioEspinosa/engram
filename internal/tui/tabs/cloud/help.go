package cloud

import "github.com/charmbracelet/bubbles/key"

// Help lists S11's own bindings (rfc-tui.md §7.2), the same ones view.go's
// footer already prints. Cloud has a single screen, so unlike the other tabs
// this never varies by an internal Screen field.
func (m Model) Help() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("up", "k", "down", "j"), key.WithHelp("j/k", "navigate")),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
		key.NewBinding(key.WithKeys("esc", "q"), key.WithHelp("esc/q", "back")),
	}
}

// CapturingText is always false: Cloud has no text input anywhere in its menu.
func (m Model) CapturingText() bool { return false }
