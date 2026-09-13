package evidence

import "github.com/charmbracelet/bubbles/key"

// Help lists whichever screen is on display's own bindings (rfc-tui.md
// §7.2), the same ones each screen's view.go footer already prints, plus
// "g"/"G" on the list — real bindings handleListKeys already answers to,
// just not named in that footer's hand-written text.
func (m Model) Help() []key.Binding {
	switch m.Screen {
	case ScreenDetail:
		return []key.Binding{
			key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open with system viewer")),
			key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "copy sha256")),
			key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "copy path")),
			key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "manifest")),
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "task")),
			key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
			key.NewBinding(key.WithKeys("esc", "q"), key.WithHelp("esc/q", "back")),
		}
	default:
		return []key.Binding{
			key.NewBinding(key.WithKeys("up", "k", "down", "j"), key.WithHelp("j/k", "move")),
			key.NewBinding(key.WithKeys("g", "G"), key.WithHelp("g/G", "top/bottom")),
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "detail")),
			key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "copy path")),
			key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open file")),
			key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "filter task")),
			key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "toggle attached")),
			key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
			key.NewBinding(key.WithKeys("esc", "q"), key.WithHelp("esc/q", "dashboard")),
		}
	}
}

// CapturingText is always false: Evidence has no text input in either screen.
func (m Model) CapturingText() bool { return false }
