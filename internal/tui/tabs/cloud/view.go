package cloud

import (
	"strings"

	"github.com/Gentleman-Programming/engram/internal/tui/shared"
)

// View renders the cloud sync menu. The root wraps it in the application
// frame.
func (m Model) View() string {
	var b strings.Builder

	b.WriteString(m.styles.Header.Render("  Cloud sync settings"))
	b.WriteString("\n\n")
	b.WriteString(shared.Menu(m.styles, menuItems, m.Cursor))
	b.WriteString(m.styles.Help.Render("\n  j/k navigate • enter select • esc/q back"))

	return b.String()
}
