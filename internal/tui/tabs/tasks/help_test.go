package tasks

import (
	"testing"

	"github.com/charmbracelet/bubbles/key"
)

// TestHelpVariesAcrossScreens pins that the "?" overlay tracks whichever
// screen is on display: the list's filters, the detail screen's task actions
// and the context pack's actions are not interchangeable.
func TestHelpVariesAcrossScreens(t *testing.T) {
	m := New(nil)

	list := m.Help()
	if len(list) == 0 {
		t.Fatal("the list screen should advertise its own bindings")
	}

	m.Screen = ScreenDetail
	detail := m.Help()
	if len(detail) == 0 || bindingKeys(detail) == bindingKeys(list) {
		t.Fatal("the detail screen should advertise its own, different bindings")
	}

	m.Screen = ScreenContextPack
	pack := m.Help()
	if len(pack) == 0 || bindingKeys(pack) == bindingKeys(detail) {
		t.Fatal("the context pack screen should advertise its own, different bindings")
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

// TestCapturingTextCoversSearchLinkAndStatePicker pins the global-key
// suspension rule across the three modal inputs Tasks can have focused: the
// root must not steal a digit meant for any of them.
func TestCapturingTextCoversSearchLinkAndStatePicker(t *testing.T) {
	base := New(nil)
	if base.CapturingText() {
		t.Fatal("CapturingText() should be false on the plain list")
	}

	searching := base
	searching.Searching = true
	searching.SearchInput.Focus()
	if !searching.CapturingText() {
		t.Fatal("CapturingText() should be true while the search box is focused")
	}

	linking := base
	linking.Linking = true
	if !linking.CapturingText() {
		t.Fatal("CapturingText() should be true while linking an observation")
	}

	changingState := base
	changingState.ChangingState = true
	if !changingState.CapturingText() {
		t.Fatal("CapturingText() should be true while the state picker is open")
	}
}
