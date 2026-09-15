package evidence

import (
	"testing"

	"github.com/charmbracelet/bubbles/key"
)

// TestHelpVariesBetweenListAndDetail pins that the "?" overlay tracks
// whichever screen is on display, not a fixed per-tab list: the list and the
// detail advertise different keys ("t"/"a" filters only make sense on the
// list; "p"/"m" only on the detail).
func TestHelpVariesBetweenListAndDetail(t *testing.T) {
	m := New(nil)

	list := m.Help()
	if len(list) == 0 {
		t.Fatal("the list screen should advertise its own bindings")
	}

	m.Screen = ScreenDetail
	detail := m.Help()
	if len(detail) == 0 {
		t.Fatal("the detail screen should advertise its own bindings")
	}

	if bindingKeys(list) == bindingKeys(detail) {
		t.Fatal("list and detail should not share the exact same help, they answer to different keys")
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

// TestCapturingTextIsAlwaysFalse pins that Evidence never suspends the
// root's global keys: neither the list nor the detail has a text input.
func TestCapturingTextIsAlwaysFalse(t *testing.T) {
	m := New(nil)
	if m.CapturingText() {
		t.Fatal("Evidence has no text input; CapturingText() should be false")
	}
}
