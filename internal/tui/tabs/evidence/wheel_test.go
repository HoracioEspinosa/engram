package evidence

import (
	"testing"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/shared"

	tea "github.com/charmbracelet/bubbletea"
)

func wheelModel(t *testing.T, width, height int) Model {
	t.Helper()

	m := New(&data.FakeEvidence{}).WithProject("acme")
	sized, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	model := sized.(Model)
	for i := 1; i <= 40; i++ {
		model.Items = append(model.Items, store.EvidenceListItem{
			Evidence: store.Evidence{ID: int64(i), TaskID: 1, Path: "ACME-1/shot.png", Kind: "png"},
		})
	}
	return model
}

func notch(x, y int, button tea.MouseButton) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: button}
}

// TestTheWheelMovesTheListCursorOverTheMaster is the master-pane rule: a notch
// on the list moves the cursor exactly as far as the same number of "j"
// presses.
func TestTheWheelMovesTheListCursorOverTheMaster(t *testing.T) {
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

// TestTheWheelOverTheDetailPaneMovesNothing: the panel beside the list is the
// same fixed summary the detail screen shows, so there is nothing to scroll.
func TestTheWheelOverTheDetailPaneMovesNothing(t *testing.T) {
	m := wheelModel(t, 160, 40)
	regions := m.regions()
	if !regions.HasDetail() {
		t.Fatalf("a 160-cell terminal has no detail pane; the split breakpoint moved")
	}

	next, _ := m.Update(notch(regions.Detail.Min.X+1, 1, tea.MouseButtonWheelDown))
	if got := next.(Model).Cursor; got != 0 {
		t.Fatalf("a notch over the detail pane moved the list cursor to %d", got)
	}
}

// TestTheWheelIsInertOnTheDetailScreen: every field of one capture fits, so
// the screen has no scroll of its own for a notch to move.
func TestTheWheelIsInertOnTheDetailScreen(t *testing.T) {
	m := wheelModel(t, 160, 40)
	m.Screen = ScreenDetail
	item := m.Items[0]
	m.Selected = &item

	next, cmd := m.Update(notch(1, 1, tea.MouseButtonWheelDown))
	if next.(Model).Cursor != 0 {
		t.Fatalf("a notch on the detail screen moved the list cursor")
	}
	if cmd != nil {
		t.Fatalf("a notch on the detail screen issued a command")
	}
}

// TestOnlyAWheelPressMovesTheList: a release carries the same button, and
// answering both would move the list twice per notch.
func TestOnlyAWheelPressMovesTheList(t *testing.T) {
	m := wheelModel(t, 160, 40)

	for _, msg := range []tea.MouseMsg{
		{X: 1, Y: 1, Action: tea.MouseActionRelease, Button: tea.MouseButtonWheelDown},
		{X: 1, Y: 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft},
	} {
		next, cmd := m.Update(msg)
		if next.(Model).Cursor != 0 {
			t.Fatalf("%v moved the list cursor", msg)
		}
		if cmd != nil {
			t.Fatalf("%v issued a command", msg)
		}
	}
}
