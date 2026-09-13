package memory

import "github.com/charmbracelet/bubbles/key"

// Help lists whichever screen is on display's own bindings (rfc-tui.md
// §7.2), the same ones each screen's view.go footer already prints. Memory
// predates every other tab and keeps its own screen router (Screen, not a
// shared one), so this switches on it directly rather than delegating.
func (m Model) Help() []key.Binding {
	switch m.Screen {
	case ScreenSearch:
		return []key.Binding{
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "search")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		}
	case ScreenSearchResults:
		return []key.Binding{
			key.NewBinding(key.WithKeys("up", "k", "down", "j"), key.WithHelp("j/k", "navigate")),
			key.NewBinding(key.WithKeys("g", "G"), key.WithHelp("g/G", "top/bottom")),
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "detail")),
			key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "copy")),
			key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "timeline")),
			key.NewBinding(key.WithKeys("L"), key.WithHelp("L", "link to task")),
			key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search again")),
			key.NewBinding(key.WithKeys("esc", "q"), key.WithHelp("esc/q", "back")),
		}
	case ScreenRecent:
		return []key.Binding{
			key.NewBinding(key.WithKeys("up", "k", "down", "j"), key.WithHelp("j/k", "navigate")),
			key.NewBinding(key.WithKeys("g", "G"), key.WithHelp("g/G", "top/bottom")),
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "detail")),
			key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "copy")),
			key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "timeline")),
			key.NewBinding(key.WithKeys("L"), key.WithHelp("L", "link to task")),
			key.NewBinding(key.WithKeys("esc", "q"), key.WithHelp("esc/q", "back")),
		}
	case ScreenObservationDetail:
		return []key.Binding{
			key.NewBinding(key.WithKeys("up", "k", "down", "j"), key.WithHelp("j/k", "scroll")),
			key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "copy")),
			key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "timeline")),
			key.NewBinding(key.WithKeys("L"), key.WithHelp("L", "link to task")),
			key.NewBinding(key.WithKeys("esc", "q"), key.WithHelp("esc/q", "back")),
		}
	case ScreenTimeline:
		return []key.Binding{
			key.NewBinding(key.WithKeys("up", "k", "down", "j"), key.WithHelp("j/k", "scroll")),
			key.NewBinding(key.WithKeys("esc", "q"), key.WithHelp("esc/q", "back")),
		}
	case ScreenSessions:
		if m.SessionDeleteState == SessionDeleteStatePrompt {
			return []key.Binding{
				key.NewBinding(key.WithKeys("y", "Y"), key.WithHelp("y", "delete")),
				key.NewBinding(key.WithKeys("n", "N", "esc"), key.WithHelp("n/esc", "cancel")),
			}
		}
		return []key.Binding{
			key.NewBinding(key.WithKeys("up", "k", "down", "j"), key.WithHelp("j/k", "navigate")),
			key.NewBinding(key.WithKeys("g", "G"), key.WithHelp("g/G", "top/bottom")),
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "view session")),
			key.NewBinding(key.WithKeys("d", "D"), key.WithHelp("d", "delete")),
			key.NewBinding(key.WithKeys("esc", "q"), key.WithHelp("esc/q", "back")),
		}
	case ScreenSessionDetail:
		return []key.Binding{
			key.NewBinding(key.WithKeys("up", "k", "down", "j"), key.WithHelp("j/k", "navigate")),
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "detail")),
			key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "copy")),
			key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "timeline")),
			key.NewBinding(key.WithKeys("esc", "q"), key.WithHelp("esc/q", "back")),
		}
	case ScreenSetup:
		if m.SetupAllowlistPrompt {
			return []key.Binding{
				key.NewBinding(key.WithKeys("y", "Y"), key.WithHelp("y", "allowlist")),
				key.NewBinding(key.WithKeys("n", "N", "esc"), key.WithHelp("n/esc", "skip")),
			}
		}
		if m.SetupDone {
			return []key.Binding{
				key.NewBinding(key.WithKeys("esc", "q", "enter"), key.WithHelp("esc/q/enter", "back to dashboard")),
			}
		}
		return []key.Binding{
			key.NewBinding(key.WithKeys("up", "k", "down", "j"), key.WithHelp("j/k", "navigate")),
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "install")),
			key.NewBinding(key.WithKeys("esc", "q"), key.WithHelp("esc/q", "back")),
		}
	default: // ScreenDashboard
		return []key.Binding{
			key.NewBinding(key.WithKeys("up", "k", "down", "j"), key.WithHelp("j/k", "navigate")),
			key.NewBinding(key.WithKeys("enter", " "), key.WithHelp("enter", "select")),
			key.NewBinding(key.WithKeys("s", "/"), key.WithHelp("s", "search")),
			key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
		}
	}
}

// CapturingText reports whether the search box is focused, or the "L"
// link-to-task picker is open (rfc-tui.md §7.1's textinput suspension
// rule): while true, digits and letters typed into either must reach the
// tab, never the root's tab-switch keys. The picker counts for its whole
// lifetime, not only while its own query box has focus — browsing its
// results with j/k must not have "1" jump to the Tasks tab out from under
// the cursor either.
func (m Model) CapturingText() bool {
	return (m.Screen == ScreenSearch && m.SearchInput.Focused()) || m.Linking
}
