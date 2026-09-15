package benchmarks

import "github.com/charmbracelet/bubbles/key"

// Help lists whichever screen is showing's own bindings. The status bar's
// hints are derived from it, so what the tab answers to and what it
// advertises are one declaration.
func (m Model) Help() []key.Binding {
	if m.prompt.Focused() {
		return []key.Binding{
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "apply")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
		}
	}

	if m.Screen == ScreenHistory {
		return []key.Binding{
			key.NewBinding(key.WithKeys("esc", "q"), key.WithHelp("esc/q", "back")),
		}
	}

	bindings := []key.Binding{
		key.NewBinding(key.WithKeys("up", "k", "down", "j"), key.WithHelp("j/k", "move")),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "metric history")),
		key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "filter task")),
		key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "filter metric")),
		key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "open BASELINE.md")),
	}
	// A page key is advertised only where there is a page to reach: an
	// unreachable hint is a hint that teaches the wrong thing.
	if m.HasPrevPage() {
		bindings = append(bindings, key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "previous page")))
	}
	if m.HasNextPage() {
		bindings = append(bindings, key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "next page")))
	}
	return bindings
}
