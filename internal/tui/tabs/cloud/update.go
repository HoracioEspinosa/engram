package cloud

import (
	"github.com/Gentleman-Programming/engram/internal/tui/tabs"

	tea "github.com/charmbracelet/bubbletea"
)

// Update advances the Cloud tab. Key messages arrive only while the tab is
// active; every other message is broadcast and ignored here.
func (m Model) Update(msg tea.Msg) (tabs.Tab, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		return m.handleKeys(key.String())
	}
	return m, nil
}

func (m Model) handleKeys(key string) (tabs.Tab, tea.Cmd) {
	switch key {
	case "up", "k":
		if m.Cursor > 0 {
			m.Cursor--
		}
	case "down", "j":
		if m.Cursor < len(menuItems)-1 {
			m.Cursor++
		}
	case "enter", " ":
		if m.Cursor == len(menuItems)-1 { // Back
			return m.leave()
		}
	case "esc", "q":
		return m.leave()
	}
	return m, nil
}

// leave hands control back to the Memory tab and rewinds the cursor, so the
// menu opens on its first item the next time around.
func (m Model) leave() (tabs.Tab, tea.Cmd) {
	m.Cursor = 0
	// Home, not Memory: the root decides where home is. With a project active
	// that is its dashboard, and only without one does it fall back to Memory.
	return m, tabs.Home()
}
