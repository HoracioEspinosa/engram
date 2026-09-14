package memory

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/key"
)

// helpKeys renders a screen's declaration the way the footer does, so a test
// can assert on what the user is told in one string.
func helpKeys(bindings []key.Binding) string {
	hints := make([]string, 0, len(bindings))
	for _, b := range bindings {
		h := b.Help()
		hints = append(hints, h.Key+" "+h.Desc)
	}
	return strings.Join(hints, " • ")
}

// TestHelpVariesAcrossScreens pins that the "?" overlay (rfc-tui.md §7.1)
// tracks whichever of Memory's nine screens is on display, not a fixed list.
func TestHelpVariesAcrossScreens(t *testing.T) {
	m := New(nil, "1.0.0")

	dashboard := m.Help()
	if len(dashboard) == 0 {
		t.Fatal("the dashboard should advertise its own bindings")
	}

	m.Screen = ScreenTimeline
	timeline := m.Help()
	if len(timeline) == 0 || bindingKeys(timeline) == bindingKeys(dashboard) {
		t.Fatal("the timeline should advertise its own, different bindings")
	}
}

// TestHelpSwitchesWithTheSessionDeletePrompt pins that a modal sub-state
// (rfc-tui.md §3.1's "y/n" confirm) gets its own help too, not the list's.
func TestHelpSwitchesWithTheSessionDeletePrompt(t *testing.T) {
	m := New(nil, "1.0.0")
	m.Screen = ScreenSessions

	list := m.Help()

	m.SessionDeleteState = SessionDeleteStatePrompt
	prompt := m.Help()

	if bindingKeys(prompt) == bindingKeys(list) {
		t.Fatal("the delete prompt should advertise y/n, not the session list's bindings")
	}
}

func bindingKeys(bindings []key.Binding) string {
	var s string
	for _, b := range bindings {
		for _, k := range b.Keys() {
			s += k + ","
		}
	}
	return s
}

// TestCapturingTextReflectsTheSearchBoxFocus pins rfc-tui.md §7.1's
// suspension rule: the root must not steal a digit typed into a memory
// search.
func TestCapturingTextReflectsTheSearchBoxFocus(t *testing.T) {
	m := New(nil, "1.0.0")
	m.Screen = ScreenSearch
	if m.CapturingText() {
		t.Fatal("CapturingText() should be false before the search box is focused")
	}

	m.SearchInput.Focus()
	if !m.CapturingText() {
		t.Fatal("CapturingText() should be true while the search box is focused")
	}
}
