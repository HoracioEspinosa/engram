package tasks

import (
	"testing"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/shared"

	tea "github.com/charmbracelet/bubbletea"
)

// wheelRows is a list long enough that one notch cannot run off the end of it.
func wheelRows(n int) []store.TaskListItem {
	items := make([]store.TaskListItem, 0, n)
	for i := 1; i <= n; i++ {
		items = append(items, store.TaskListItem{Task: store.Task{ID: int64(i), Title: "task", Kind: "bugfix", State: "in_progress"}})
	}
	return items
}

func wheelModel(t *testing.T, width, height int) Model {
	t.Helper()

	m := New(&data.FakeTask{}).WithProject("acme")
	sized, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	model := sized.(Model)
	model.Items = wheelRows(40)
	return model
}

func notch(x, y int, button tea.MouseButton) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: button}
}

// TestTheWheelMovesTheListCursorOverTheMaster is the master-pane rule: a notch
// on the list moves the cursor exactly as far as the same number of "j"
// presses, so the pointer and the keyboard stay one behaviour.
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

// TestTheWheelOverTheDetailPaneMovesNothing: the panel beside the list shows
// what the row already carries, so there is nothing there to scroll and a
// notch over it must not scroll the list the pointer is not on.
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

// TestTheWheelScrollsTheContextPack: the pack is one long body, so it scrolls
// wherever the pointer is rather than only over a pane.
func TestTheWheelScrollsTheContextPack(t *testing.T) {
	m := wheelModel(t, 160, 40)
	m.Screen = ScreenContextPack
	m.ContextPack = "# pack"

	next, _ := m.Update(notch(1, 1, tea.MouseButtonWheelDown))
	if got := next.(Model).ContextPackScroll; got != shared.WheelLines {
		t.Fatalf("context pack scroll = %d, want %d", got, shared.WheelLines)
	}
}

// TestTheWheelMovesTheDetailObservationCursor: the detail screen's own list of
// linked observations answers the wheel the same way the list screen does.
func TestTheWheelMovesTheDetailObservationCursor(t *testing.T) {
	m := wheelModel(t, 160, 40)
	m.Screen = ScreenDetail
	m.Detail = &data.TaskDetail{
		Task:         store.Task{ID: 1, Title: "task"},
		Observations: make([]store.TaskObservationDetail, 10),
	}

	next, _ := m.Update(notch(1, 1, tea.MouseButtonWheelDown))
	if got := next.(Model).DetailCursor; got != shared.WheelLines {
		t.Fatalf("detail cursor = %d, want %d", got, shared.WheelLines)
	}
}

// TestTheWheelIsInertWhileAPromptIsUp: the state picker and the link prompt
// are modal, so the list behind them is not what is being navigated.
func TestTheWheelIsInertWhileAPromptIsUp(t *testing.T) {
	base := wheelModel(t, 160, 40)
	base.Screen = ScreenDetail
	base.Detail = &data.TaskDetail{Task: store.Task{ID: 1}, Observations: make([]store.TaskObservationDetail, 10)}

	changing := base
	changing.ChangingState = true
	if next, _ := changing.Update(notch(1, 1, tea.MouseButtonWheelDown)); next.(Model).DetailCursor != 0 {
		t.Fatalf("the wheel moved the cursor while the state picker was open")
	}

	linking := base
	linking.Linking = true
	if next, _ := linking.Update(notch(1, 1, tea.MouseButtonWheelDown)); next.(Model).DetailCursor != 0 {
		t.Fatalf("the wheel moved the cursor while the link prompt was open")
	}
}

// TestOnlyAWheelPressMovesTheList: a release carries the same button, and
// answering both would move the list twice per notch.
func TestOnlyAWheelPressMovesTheList(t *testing.T) {
	m := wheelModel(t, 160, 40)

	for _, msg := range []tea.MouseMsg{
		{X: 1, Y: 1, Action: tea.MouseActionRelease, Button: tea.MouseButtonWheelDown},
		{X: 1, Y: 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft},
		{X: 1, Y: 1, Action: tea.MouseActionMotion, Button: tea.MouseButtonNone},
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
