package home

import "github.com/charmbracelet/bubbles/key"

// Help lists Home's own bindings. The status bar's hints are derived from
// it, so what the tab answers to and what it advertises are one declaration.
//
// "h/l" only appear once the width actually draws two columns: a key that
// moves the focus somewhere invisible should not be advertised as if it did
// something.
func (m Model) Help() []key.Binding {
	bindings := []key.Binding{
		key.NewBinding(key.WithKeys("up", "k", "down", "j"), key.WithHelp("j/k", "move block")),
	}
	if m.regions().HasDetail() {
		bindings = append(bindings, key.NewBinding(key.WithKeys("h", "l"), key.WithHelp("h/l", "column")))
	}
	return append(bindings,
		key.NewBinding(key.WithKeys("g", "G"), key.WithHelp("g/G", "top/bottom")),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open block")),
		key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sync graph")),
	)
}
