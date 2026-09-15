package runbooks

import "github.com/charmbracelet/bubbles/key"

// Help lists whichever screen is on display's own bindings, the same ones
// each screen's view.go footer already prints, plus
// "g"/"G" — real bindings both handleIndexKeys and handleViewKeys already
// answer to, just not named in that footer's hand-written text.
func (m Model) Help() []key.Binding {
	switch m.Screen {
	case ScreenView:
		return []key.Binding{
			key.NewBinding(key.WithKeys("up", "k", "down", "j"), key.WithHelp("j/k", "scroll")),
			key.NewBinding(key.WithKeys("g", "G"), key.WithHelp("g/G", "top/bottom")),
			key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "open in $EDITOR")),
			key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "executions in Memory")),
			key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "copy vault path")),
			key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open hub")),
			key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "reload")),
			key.NewBinding(key.WithKeys("esc", "q"), key.WithHelp("esc/q", "back")),
		}
	default:
		return []key.Binding{
			key.NewBinding(key.WithKeys("up", "k", "down", "j"), key.WithHelp("j/k", "move")),
			key.NewBinding(key.WithKeys("g", "G"), key.WithHelp("g/G", "top/bottom")),
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "view")),
			key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "copy vault path")),
			key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "all/project")),
			key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
			key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "executions")),
			key.NewBinding(key.WithKeys("p", "n"), key.WithHelp("p/n", "page")),
			key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
			key.NewBinding(key.WithKeys("esc", "q"), key.WithHelp("esc/q", "back")),
		}
	}
}

// CapturingText reports whether the search box is focused, which suspends
// the root's own key handling: while true, digits typed into a runbook search
// must reach SearchInput, never the root's tab-switch keys.
func (m Model) CapturingText() bool {
	return m.Searching && m.SearchInput.Focused()
}
