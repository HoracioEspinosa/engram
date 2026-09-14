package memory

import (
	"errors"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs"
	tea "github.com/charmbracelet/bubbletea"
)

// sampleLinkTasks builds a small ItemsByProject fixture for a FakeTask.
func sampleLinkTasks() map[string][]store.TaskListItem {
	return map[string][]store.TaskListItem{
		"nextcloud": {
			{Task: store.Task{ID: 7, Title: "Previews return 503 under load", State: "in_progress"}},
			{Task: store.Task{ID: 9, Title: "Runbook stale flag is wrong", State: "review"}},
		},
	}
}

// TestLKeyOpensLinkPickerFromObservationDetail pins rfc-tui.md §5's only
// addition to the memory screens: "L" on Observation Detail opens the
// shared task selector for the observation currently on screen.
func TestLKeyOpensLinkPickerFromObservationDetail(t *testing.T) {
	m := New(nil, "").WithTasks(&data.FakeTask{}).WithProject("nextcloud")
	m.Screen = ScreenObservationDetail
	m.SelectedObservation = &store.Observation{ID: 99}

	updated, cmd := m.handleObservationDetailKeys("L")
	m = updated.(Model)

	if !m.Linking {
		t.Fatal("L should open the link-to-task picker")
	}
	if m.LinkObsID != 99 {
		t.Fatalf("LinkObsID = %d, want 99", m.LinkObsID)
	}
	if !m.LinkQuery.Focused() {
		t.Fatal("L should focus the task search box")
	}
	if cmd != nil {
		t.Fatal("opening the picker should not issue a command yet — nothing has been searched")
	}
}

// TestLKeyIsANoOpOnObservationDetailWithoutAnObservation guards the nil
// SelectedObservation case (a detail screen that has not finished loading).
func TestLKeyIsANoOpOnObservationDetailWithoutAnObservation(t *testing.T) {
	m := New(nil, "").WithTasks(&data.FakeTask{})
	m.Screen = ScreenObservationDetail

	updated, _ := m.handleObservationDetailKeys("L")
	m = updated.(Model)

	if m.Linking {
		t.Fatal("L without a loaded observation should not open the picker")
	}
}

// TestLKeyOpensLinkPickerFromSearchResults pins the second of the three
// screens rfc-tui.md §5 names: Search Results.
func TestLKeyOpensLinkPickerFromSearchResults(t *testing.T) {
	m := New(nil, "").WithTasks(&data.FakeTask{}).WithProject("nextcloud")
	m.Screen = ScreenSearchResults
	m.SearchResults = []store.SearchResult{
		{Observation: store.Observation{ID: 11}},
		{Observation: store.Observation{ID: 12}},
	}
	m.Cursor = 1

	updated, _ := m.handleSearchResultsKeys("L")
	m = updated.(Model)

	if !m.Linking || m.LinkObsID != 12 {
		t.Fatalf("Linking = %v, LinkObsID = %d, want true/12 (the highlighted result)", m.Linking, m.LinkObsID)
	}
}

// TestLKeyOpensLinkPickerFromRecent pins the third of the three screens:
// Recent.
func TestLKeyOpensLinkPickerFromRecent(t *testing.T) {
	m := New(nil, "").WithTasks(&data.FakeTask{}).WithProject("nextcloud")
	m.Screen = ScreenRecent
	m.RecentObservations = []store.Observation{{ID: 21}, {ID: 22}}
	m.Cursor = 0

	updated, _ := m.handleRecentKeys("L")
	m = updated.(Model)

	if !m.Linking || m.LinkObsID != 21 {
		t.Fatalf("Linking = %v, LinkObsID = %d, want true/21", m.Linking, m.LinkObsID)
	}
}

// TestLinkQuerySubmitLoadsMatchingTasks exercises the search half of the
// picker: typing a query and pressing enter runs it against the project's
// tasks and shows the matches.
func TestLinkQuerySubmitLoadsMatchingTasks(t *testing.T) {
	fake := &data.FakeTask{ItemsByProject: sampleLinkTasks()}
	m := New(nil, "").WithTasks(fake).WithProject("nextcloud")
	m.Screen = ScreenObservationDetail
	m.SelectedObservation = &store.Observation{ID: 99}

	updated, _ := m.handleObservationDetailKeys("L")
	m = updated.(Model)
	m.LinkQuery.SetValue("previews")

	updatedModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updatedModel.(Model)
	if m.LinkQuery.Focused() {
		t.Fatal("enter should blur the search box")
	}
	if cmd == nil {
		t.Fatal("enter with a query should search for matching tasks")
	}

	updatedModel, _ = m.Update(cmd())
	m = updatedModel.(Model)
	if fake.LastListFilter().Query != "previews" || fake.LastListFilter().Limit != 20 {
		t.Fatalf("LastListFilter = %+v, want Query=previews Limit=20", fake.LastListFilter())
	}
	if len(m.LinkResults) != 1 || m.LinkResults[0].ID != 7 {
		t.Fatalf("LinkResults = %+v, want the one task matching \"previews\"", m.LinkResults)
	}
}

// TestLinkQuerySubmitReportsAnError pins the error path: a failed search
// surfaces in ErrorMsg instead of silently leaving the picker empty.
func TestLinkQuerySubmitReportsAnError(t *testing.T) {
	fake := &data.FakeTask{Err: errors.New("boom")}
	m := New(nil, "").WithTasks(fake).WithProject("nextcloud")
	m.Screen = ScreenObservationDetail
	m.SelectedObservation = &store.Observation{ID: 99}

	updated, _ := m.handleObservationDetailKeys("L")
	m = updated.(Model)

	updatedModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updatedModel.(Model)
	updatedModel, _ = m.Update(cmd())
	m = updatedModel.(Model)

	if m.ErrorMsg == "" {
		t.Fatal("a failed task search should set ErrorMsg")
	}
}

// TestLinkPickerEnterLinksTheObservationAndNavigatesToTheTask is the whole
// round trip rfc-tui.md §5 describes: pick a task from the shared selector,
// write the task_observations row (what mem_task_link does over MCP today),
// and land on that task's detail — the MEM -->|L link| TD edge in §7.3's
// navigation diagram.
func TestLinkPickerEnterLinksTheObservationAndNavigatesToTheTask(t *testing.T) {
	fake := &data.FakeTask{ItemsByProject: sampleLinkTasks()}
	m := New(nil, "").WithTasks(fake).WithProject("nextcloud")
	m.Screen = ScreenObservationDetail
	m.SelectedObservation = &store.Observation{ID: 99}

	updated, _ := m.handleObservationDetailKeys("L")
	m = updated.(Model)

	updatedModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // submit the (empty) query
	m = updatedModel.(Model)
	updatedModel, _ = m.Update(cmd())
	m = updatedModel.(Model)
	if len(m.LinkResults) != 2 {
		t.Fatalf("LinkResults = %+v, want both project tasks for an empty query", m.LinkResults)
	}
	m.LinkCursor = 1 // task id 9

	updatedModel, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updatedModel.(Model)
	if m.Linking {
		t.Fatal("selecting a task should close the picker")
	}
	if cmd == nil {
		t.Fatal("selecting a task should issue the link write")
	}

	linkedMsg := run(t, cmd)
	if len(fake.LinkCalls()) != 1 || fake.LinkCalls()[0].TaskID != 9 || fake.LinkCalls()[0].ObservationID != 99 {
		t.Fatalf("LinkCalls = %+v, want one call linking observation 99 to task 9", fake.LinkCalls())
	}

	_, cmd2 := m.Update(linkedMsg)
	if cmd2 == nil {
		t.Fatal("a successful link should navigate to the task's detail")
	}
	nav, ok := run(t, cmd2).(tabs.NavigateMsg)
	if !ok || nav.Target != tabs.Tasks || nav.TaskID != 9 {
		t.Fatalf("navigation = %+v, want NavigateMsg{Target: Tasks, TaskID: 9}", nav)
	}
}

// TestLinkPickerEscCancelsFromTheQueryBox pins that esc backs all the way
// out, matching the Tasks tab's own "l" input (handleLinkInputKeys).
func TestLinkPickerEscCancelsFromTheQueryBox(t *testing.T) {
	fake := &data.FakeTask{}
	m := New(nil, "").WithTasks(fake).WithProject("nextcloud")
	m.Screen = ScreenObservationDetail
	m.SelectedObservation = &store.Observation{ID: 99}

	updated, _ := m.handleObservationDetailKeys("L")
	m = updated.(Model)
	m.LinkQuery.SetValue("something")

	updatedModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updatedModel.(Model)
	if m.Linking || m.LinkQuery.Value() != "" {
		t.Fatalf("esc should cancel the picker entirely, got Linking=%v Query=%q", m.Linking, m.LinkQuery.Value())
	}
	if cmd != nil {
		t.Fatal("esc should not issue a write")
	}
	if len(fake.LinkCalls()) != 0 {
		t.Fatalf("LinkCalls = %+v, want none", fake.LinkCalls())
	}
}

// TestLinkPickerEscCancelsFromTheResultsList pins the same cancellation once
// results are already showing and the query box is no longer focused.
func TestLinkPickerEscCancelsFromTheResultsList(t *testing.T) {
	fake := &data.FakeTask{ItemsByProject: sampleLinkTasks()}
	m := New(nil, "").WithTasks(fake).WithProject("nextcloud")
	m.Screen = ScreenObservationDetail
	m.SelectedObservation = &store.Observation{ID: 99}

	updated, _ := m.handleObservationDetailKeys("L")
	m = updated.(Model)
	updatedModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updatedModel.(Model)
	updatedModel, _ = m.Update(cmd())
	m = updatedModel.(Model)

	updatedModel, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updatedModel.(Model)
	if m.Linking {
		t.Fatal("esc from the results list should cancel the picker")
	}
	if cmd != nil {
		t.Fatal("esc should not issue a write")
	}
}

// TestCapturingTextIncludesTheLinkPicker extends rfc-tui.md §7.1's
// suspension rule to "L": while the picker is open — typing a query or
// browsing its results — the root must not steal 0, p, a digit or Tab out
// from under it, the same guard T-10.09 added for the search box.
func TestCapturingTextIncludesTheLinkPicker(t *testing.T) {
	m := New(nil, "").WithTasks(&data.FakeTask{})
	m.Screen = ScreenObservationDetail
	m.SelectedObservation = &store.Observation{ID: 1}

	if m.CapturingText() {
		t.Fatal("CapturingText() should be false before L is pressed")
	}

	updated, _ := m.handleObservationDetailKeys("L")
	m = updated.(Model)
	if !m.CapturingText() {
		t.Fatal("CapturingText() should be true while the task query box is focused")
	}

	m.LinkQuery.Blur()
	m.LinkResults = []store.TaskListItem{{Task: store.Task{ID: 1}}}
	if !m.CapturingText() {
		t.Fatal("CapturingText() should stay true while browsing the picker's results, not just while typing")
	}
}

// TestHelpAdvertisesTheLinkToTaskKey pins rfc-tui.md §5's S10 wireframe,
// which lists "L link to task" alongside the screen's other keys. The root
// renders the footer from this declaration, so declaring it is what puts it
// on the always-on hint line as well as in the "?" overlay.
func TestHelpAdvertisesTheLinkToTaskKey(t *testing.T) {
	for _, screen := range []Screen{ScreenSearchResults, ScreenRecent, ScreenObservationDetail} {
		m := New(nil, "")
		m.Screen = screen

		found := false
		for _, b := range m.Help() {
			if b.Help().Key == "L" && b.Help().Desc == "link to task" {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("screen %v answers \"L\" but does not declare it", screen)
		}
	}
}

// run executes cmd and returns the tea.Msg it produces, failing the test if
// cmd is nil.
func run(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("run: nil command")
	}
	return cmd()
}
