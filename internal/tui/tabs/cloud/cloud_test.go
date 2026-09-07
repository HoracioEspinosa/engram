package cloud

import (
	"strings"
	"testing"

	"github.com/Gentleman-Programming/engram/internal/tui/tabs"

	tea "github.com/charmbracelet/bubbletea"
)

func TestMenuNavigation(t *testing.T) {
	m := New()

	updatedModel, _ := m.handleKeys("down")
	updated := updatedModel.(Model)
	if updated.Cursor != 1 {
		t.Fatalf("down should move cursor to 1, got %d", updated.Cursor)
	}

	updatedModel, _ = updated.handleKeys("j")
	updated = updatedModel.(Model)
	if updated.Cursor != 2 {
		t.Fatalf("j should move cursor to 2, got %d", updated.Cursor)
	}

	updatedModel, _ = updated.handleKeys("down")
	updated = updatedModel.(Model)
	if updated.Cursor != 3 {
		t.Fatalf("down should move cursor to 3, got %d", updated.Cursor)
	}

	updatedModel, _ = updated.handleKeys("down")
	updated = updatedModel.(Model)
	if updated.Cursor != 3 {
		t.Fatalf("down at bottom should stay at 3, got %d", updated.Cursor)
	}

	updatedModel, _ = updated.handleKeys("up")
	updated = updatedModel.(Model)
	if updated.Cursor != 2 {
		t.Fatalf("up should move cursor to 2, got %d", updated.Cursor)
	}

	updatedModel, _ = updated.handleKeys("k")
	updated = updatedModel.(Model)
	if updated.Cursor != 1 {
		t.Fatalf("k should move cursor to 1, got %d", updated.Cursor)
	}

	updatedModel, _ = New().handleKeys("up")
	updated = updatedModel.(Model)
	if updated.Cursor != 0 {
		t.Fatalf("up at top should stay at 0, got %d", updated.Cursor)
	}
}

func TestBackItemLeavesTheTab(t *testing.T) {
	m := New()
	m.Cursor = len(menuItems) - 1 // Back

	updatedModel, cmd := m.handleKeys("enter")
	updated := updatedModel.(Model)
	if cmd == nil {
		t.Fatal("enter on Back should ask the root to go home")
	}
	// Back goes home, the same as esc: the root picks the destination.
	if _, ok := cmd().(tabs.HomeMsg); !ok {
		t.Fatalf("enter on Back should emit HomeMsg, got %#v", cmd())
	}
	if updated.Cursor != 0 {
		t.Fatalf("cursor should rewind to 0 on leaving, got %d", updated.Cursor)
	}
}

func TestEnterOnANonBackItemStays(t *testing.T) {
	m := New()
	m.Cursor = 0

	updatedModel, cmd := m.handleKeys("enter")
	updated := updatedModel.(Model)
	if cmd != nil {
		t.Fatal("enter on a menu item with no action yet should not return a command")
	}
	if updated.Cursor != 0 {
		t.Fatalf("cursor = %d, want 0", updated.Cursor)
	}
}

func TestEscAndQLeaveTheTab(t *testing.T) {
	for _, key := range []string{"esc", "q"} {
		t.Run(key, func(t *testing.T) {
			m := New()
			m.Cursor = 2

			updatedModel, cmd := m.handleKeys(key)
			updated := updatedModel.(Model)
			if cmd == nil {
				t.Fatalf("%q should ask the root to go home", key)
			}
			// Home rather than a fixed tab: the root sends the user to the
			// active project's dashboard, and only to Memory when none is.
			if _, ok := cmd().(tabs.HomeMsg); !ok {
				t.Fatalf("%q should emit HomeMsg, got %#v", key, cmd())
			}
			if updated.Cursor != 0 {
				t.Fatalf("cursor should rewind to 0 on leaving, got %d", updated.Cursor)
			}
		})
	}
}

func TestUpdateIgnoresNonKeyMessages(t *testing.T) {
	m := New()
	m.Cursor = 2

	updatedModel, cmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	updated := updatedModel.(Model)
	if cmd != nil {
		t.Fatal("a broadcast message should not produce a command")
	}
	if updated.Cursor != 2 {
		t.Fatalf("a broadcast message should not move the cursor, got %d", updated.Cursor)
	}
}

func TestUpdateRoutesKeyMessages(t *testing.T) {
	m := New()

	updatedModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated := updatedModel.(Model)
	if updated.Cursor != 1 {
		t.Fatalf("down key should move the cursor, got %d", updated.Cursor)
	}
}

func TestViewRendersTheMenu(t *testing.T) {
	m := New()
	m.Cursor = 1

	out := m.View()
	if !strings.Contains(out, "Cloud sync settings") {
		t.Fatal("view should render the header")
	}
	for _, item := range menuItems {
		if !strings.Contains(out, item) {
			t.Fatalf("view should render menu item %q", item)
		}
	}
	if !strings.Contains(out, "▸ View status") {
		t.Fatal("view should mark the item under the cursor")
	}
	if !strings.Contains(out, "esc/q back") {
		t.Fatal("view should render the help footer")
	}
}

func TestMenuOrderPutsBackLast(t *testing.T) {
	if got := menuItems[len(menuItems)-1]; got != "Back" {
		t.Fatalf("last menu item = %q, want %q", got, "Back")
	}
}

func TestTabContract(t *testing.T) {
	var tab tabs.Tab = New()

	if tab.Title() != "Cloud" {
		t.Fatalf("Title() = %q, want %q", tab.Title(), "Cloud")
	}
	if cmd := tab.Init(); cmd != nil {
		t.Fatal("Init should have nothing to load")
	}
	if cmd := tab.Refresh(); cmd != nil {
		t.Fatal("Refresh should have nothing to reload")
	}
}
