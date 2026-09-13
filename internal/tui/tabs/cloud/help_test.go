package cloud

import "testing"

// TestHelpListsCloudsOwnBindings pins that the "?" overlay (rfc-tui.md §7.1)
// gets real content for Cloud, not an empty list — Cloud has one screen, so
// there is nothing to vary, unlike every other tab's Help.
func TestHelpListsCloudsOwnBindings(t *testing.T) {
	m := New()
	if len(m.Help()) == 0 {
		t.Fatal("Help() should list Cloud's own bindings")
	}
}

// TestCapturingTextIsAlwaysFalse pins that Cloud never suspends the root's
// global keys: it has no text input anywhere in its menu.
func TestCapturingTextIsAlwaysFalse(t *testing.T) {
	m := New()
	if m.CapturingText() {
		t.Fatal("Cloud has no text input; CapturingText() should be false")
	}
}
