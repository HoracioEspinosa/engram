package runbooks

import (
	"strings"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/shared"

	tea "github.com/charmbracelet/bubbletea"
)

func wheelModel(t *testing.T, width, height int) Model {
	t.Helper()

	m := New(&data.FakeRunbook{}, &data.FakeProject{}).WithProject("acme")
	sized, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	model := sized.(Model)
	for i := 1; i <= 40; i++ {
		model.Items = append(model.Items, store.RunbookIndexRow{ID: "RB-900", Title: "restore the pond"})
	}
	return model
}

func notch(x, y int, button tea.MouseButton) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: button}
}

// TestTheWheelMovesTheIndexCursorOverTheMaster is the master-pane rule: a
// notch on the list moves the cursor exactly as far as the same number of "j"
// presses.
func TestTheWheelMovesTheIndexCursorOverTheMaster(t *testing.T) {
	m := wheelModel(t, 160, 40)

	down, _ := m.Update(notch(1, 1, tea.MouseButtonWheelDown))
	if got := down.(Model).Cursor; got != shared.WheelLines {
		t.Fatalf("cursor = %d after one notch down, want %d", got, shared.WheelLines)
	}

	up, _ := down.(Model).Update(notch(1, 1, tea.MouseButtonWheelUp))
	if got := up.(Model).Cursor; got != 0 {
		t.Fatalf("cursor = %d after a notch back up, want 0", got)
	}
}

// TestTheWheelOverThePreviewPaneMovesNothing: the pane beside the index is the
// same preview that sits below the list at a narrower width, and it follows
// the cursor rather than scrolling on its own.
func TestTheWheelOverThePreviewPaneMovesNothing(t *testing.T) {
	m := wheelModel(t, 160, 40)
	regions := m.regions()
	if !regions.HasDetail() {
		t.Fatalf("a 160-cell terminal has no detail pane; the split breakpoint moved")
	}

	next, _ := m.Update(notch(regions.Detail.Min.X+1, 1, tea.MouseButtonWheelDown))
	if got := next.(Model).Cursor; got != 0 {
		t.Fatalf("a notch over the preview pane moved the index cursor to %d", got)
	}
}

// TestTheWheelScrollsTheMarkdownBody: the Markdown view is one long body, so
// it scrolls wherever the pointer is.
func TestTheWheelScrollsTheMarkdownBody(t *testing.T) {
	m := wheelModel(t, 160, 40)
	m.Screen = ScreenView
	row := m.Items[0]
	m.Selected = &row
	m.Rendered = strings.Repeat("line\n", 200)

	next, _ := m.Update(notch(1, 1, tea.MouseButtonWheelDown))
	if got := next.(Model).ViewScroll; got != shared.WheelLines {
		t.Fatalf("view scroll = %d, want %d", got, shared.WheelLines)
	}

	back, _ := next.(Model).Update(notch(1, 1, tea.MouseButtonWheelUp))
	if got := back.(Model).ViewScroll; got != 0 {
		t.Fatalf("view scroll = %d after a notch back up, want 0", got)
	}
}

// TestOnlyAWheelPressMovesTheIndex: a release carries the same button, and
// answering both would move the list twice per notch.
func TestOnlyAWheelPressMovesTheIndex(t *testing.T) {
	m := wheelModel(t, 160, 40)

	for _, msg := range []tea.MouseMsg{
		{X: 1, Y: 1, Action: tea.MouseActionRelease, Button: tea.MouseButtonWheelDown},
		{X: 1, Y: 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft},
	} {
		next, cmd := m.Update(msg)
		if next.(Model).Cursor != 0 {
			t.Fatalf("%v moved the index cursor", msg)
		}
		if cmd != nil {
			t.Fatalf("%v issued a command", msg)
		}
	}
}
