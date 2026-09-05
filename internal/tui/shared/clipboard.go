// Package shared holds the widgets and helpers every workspace tab renders
// with: the two-line observation row, menus, range indicators, text helpers
// and the OSC 52 clipboard bridge.
//
// It knows about theme and about nothing else in the TUI, so a tab can use it
// without pulling in another tab.
package shared

import (
	"encoding/base64"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// CopiedMsg carries the OSC 52 escape sequence that the root emits so the
// terminal writes the payload to the system clipboard.
type CopiedMsg struct {
	Sequence string
}

// ClearFeedbackMsg fires when the copy confirmation has been on screen long
// enough to be read.
type ClearFeedbackMsg struct{}

// OSC52Sequence returns the terminal escape sequence that asks the terminal
// emulator to write content to the system clipboard.
//
// Format: ESC ] 52 ; c ; <base64> BEL
func OSC52Sequence(content string) string {
	b64 := base64.StdEncoding.EncodeToString([]byte(content))
	return fmt.Sprintf("\x1b]52;c;%s\x07", b64)
}

// Copy returns a Cmd that builds the OSC 52 sequence for content and wraps it
// in a CopiedMsg.
func Copy(content string) tea.Cmd {
	return func() tea.Msg {
		return CopiedMsg{Sequence: OSC52Sequence(content)}
	}
}

// ClearFeedbackAfter returns a Cmd that sends ClearFeedbackMsg after d.
func ClearFeedbackAfter(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(_ time.Time) tea.Msg {
		return ClearFeedbackMsg{}
	})
}
