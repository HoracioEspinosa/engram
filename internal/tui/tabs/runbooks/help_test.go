package runbooks

import (
	"testing"

	"github.com/charmbracelet/bubbles/key"
)

// TestHelpVariesBetweenIndexAndView pins that the "?" overlay tracks
// whichever screen is on display: the index's "a"/"/" filters and the
// Markdown view's "e"/"o" editor/hub actions are not interchangeable.
func TestHelpVariesBetweenIndexAndView(t *testing.T) {
	m := New(nil, nil)

	index := m.Help()
	if len(index) == 0 {
		t.Fatal("the index screen should advertise its own bindings")
	}

	m.Screen = ScreenView
	view := m.Help()
	if len(view) == 0 {
		t.Fatal("the view screen should advertise its own bindings")
	}

	if bindingKeys(index) == bindingKeys(view) {
		t.Fatal("index and view should not share the exact same help, they answer to different keys")
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

// TestCapturingTextReflectsTheSearchBoxFocus pins the suspension rule: the
// root must not steal a digit typed into the runbook search box.
func TestCapturingTextReflectsTheSearchBoxFocus(t *testing.T) {
	m := New(nil, nil)
	if m.CapturingText() {
		t.Fatal("CapturingText() should be false before searching")
	}

	m.Searching = true
	m.SearchInput.Focus()
	if !m.CapturingText() {
		t.Fatal("CapturingText() should be true while the search box is focused")
	}
}
