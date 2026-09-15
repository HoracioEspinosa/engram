package tasks

import "github.com/charmbracelet/bubbles/key"

// Help lists whichever screen is on display's own bindings. It is the single
// declaration of what the screen answers to: the "?" overlay and the footer
// are both rendered from it, so a key that works and a key that is advertised
// cannot drift apart.
//
// The root's own bindings — tabs, refresh, the project selector, help, quit —
// are not repeated here; globalHelpBindings owns those.
func (m Model) Help() []key.Binding {
	switch m.Screen {
	case ScreenDetail:
		return []key.Binding{
			key.NewBinding(key.WithKeys("up", "k", "down", "j"), key.WithHelp("j/k", "move")),
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "observation")),
			key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "evidence")),
			key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "context pack")),
			key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "change state")),
			key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "link observation")),
			key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "copy key")),
			key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "jira")),
			key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "pr")),
			key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "copy branch")),
			key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
			key.NewBinding(key.WithKeys("esc", "q"), key.WithHelp("esc/q", "back")),
		}
	case ScreenContextPack:
		return []key.Binding{
			key.NewBinding(key.WithKeys("up", "k", "down", "j"), key.WithHelp("j/k", "scroll")),
			key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "copy to clipboard")),
			key.NewBinding(key.WithKeys("w"), key.WithHelp("w", "write context-pack.md")),
			key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "rebuild")),
			key.NewBinding(key.WithKeys("esc", "q"), key.WithHelp("esc/q", "back")),
		}
	default:
		return []key.Binding{
			key.NewBinding(key.WithKeys("up", "k", "down", "j"), key.WithHelp("j/k", "move")),
			key.NewBinding(key.WithKeys("g", "G"), key.WithHelp("g/G", "top/bottom")),
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "detail")),
			key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "copy key")),
			key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open jira")),
			key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
			key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "state filter")),
			key.NewBinding(key.WithKeys("K"), key.WithHelp("K", "kind filter")),
			key.NewBinding(key.WithKeys("p", "n"), key.WithHelp("p/n", "page")),
			key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
			key.NewBinding(key.WithKeys("esc", "q"), key.WithHelp("esc/q", "dashboard")),
		}
	}
}

// CapturingText reports whether Tasks currently owns a focused text input or
// a modal picker, which is what suspends the root's global keys: the search
// box, the "l" link-observation prompt, and the "s" state picker all take
// raw keys the root's global bindings must not steal.
func (m Model) CapturingText() bool {
	return (m.Searching && m.SearchInput.Focused()) || m.Linking || m.ChangingState
}
