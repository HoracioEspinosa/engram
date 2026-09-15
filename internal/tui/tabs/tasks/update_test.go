package tasks

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/shared"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs"

	tea "github.com/charmbracelet/bubbletea"
)

func strp(v string) *string { return &v }

// step feeds msg to m's Update and casts the result back to Model, the same
// idiom app_test.go and tabs/memory's tests use for a leaf model.
func step(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(msg)
	next, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want tasks.Model", updated)
	}
	return next, cmd
}

// run executes cmd the way the Bubble Tea runtime would, turning a panic
// inside it into a test failure instead of a crashed test binary.
func run(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("run: nil command")
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("command panicked: %v", r)
		}
	}()
	return cmd()
}

func sampleTask(id int64, jiraKey, state string) store.Task {
	return store.Task{
		ID: id, SyncID: fmt.Sprintf("task-%d", id), Project: "acme",
		JiraKey: strp(jiraKey), Title: "Previews 503 on cold generation", Kind: "bugfix", State: state,
		Branch: strp("fix/" + jiraKey), CreatedAt: "2026-08-17 00:00:00", UpdatedAt: "2026-08-24 00:00:00",
	}
}

func sampleDetail(task store.Task, observationIDs ...int64) data.TaskDetail {
	var obs []store.TaskObservationDetail
	for _, id := range observationIDs {
		obs = append(obs, store.TaskObservationDetail{
			Role: "root_cause", LinkedAt: "2026-08-17 00:00:00",
			Observation: store.Observation{ID: id, Type: "discovery", Title: "root cause", CreatedAt: "2026-08-17 00:00:00"},
		})
	}
	return data.TaskDetail{Task: task, Observations: obs}
}

func withHeight(m Model, h int) Model {
	m.Height = h
	m.Width = 120
	return m
}

// ─── List (S3) ───────────────────────────────────────────────────────────────

func TestNewStartsOnTheListScreenWithNoProject(t *testing.T) {
	m := New(&data.FakeTask{})
	if m.Screen != ScreenList {
		t.Fatalf("Screen = %v, want ScreenList", m.Screen)
	}
	if cmd := m.Init(); cmd != nil {
		t.Fatal("Init without a project should have nothing to load")
	}
}

func TestInitLoadsTasksWhenAProjectIsAlreadyActive(t *testing.T) {
	fake := &data.FakeTask{ItemsByProject: map[string][]store.TaskListItem{
		"acme": {{Task: sampleTask(1, "ACME-1", "open")}},
	}}
	m := New(fake).WithProject("acme")

	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init with an active project should load its tasks")
	}
	m, _ = step(t, m, run(t, cmd))
	if len(m.Items) != 1 {
		t.Fatalf("Items = %+v, want the one seeded task", m.Items)
	}
}

// TestOpenTaskLoadsTheGivenTasksDetail pins rfc-tui.md §3.1 S7's "Enter" on
// an evidence file: the root drives this the same way it drives
// memory.Model.OpenObservation for the Memory deep link, so a message from
// outside this package (tabs.NavigateMsg.TaskID) can open a task's detail
// without the Tasks tab importing tabs/evidence.
func TestOpenTaskLoadsTheGivenTasksDetail(t *testing.T) {
	fake := &data.FakeTask{DetailByID: map[int64]data.TaskDetail{
		9: sampleDetail(sampleTask(9, "ACME-9", "open")),
	}}
	m := New(fake).WithProject("acme")

	cmd := m.OpenTask(9)
	if cmd == nil {
		t.Fatal("OpenTask should return a non-nil command")
	}
	m, _ = step(t, m, run(t, cmd))
	if m.Screen != ScreenDetail {
		t.Fatalf("Screen = %v, want ScreenDetail", m.Screen)
	}
	if m.Detail == nil || m.Detail.Task.ID != 9 {
		t.Fatalf("Detail = %+v, want task 9's detail", m.Detail)
	}
}

func TestListCursorMovesAndClampsScroll(t *testing.T) {
	fake := &data.FakeTask{ItemsByProject: map[string][]store.TaskListItem{
		"acme": {
			{Task: sampleTask(1, "ACME-1", "open")},
			{Task: sampleTask(2, "ACME-2", "open")},
			{Task: sampleTask(3, "ACME-3", "open")},
		},
	}}
	m := withHeight(New(fake).WithProject("acme"), 20)
	m, cmd := step(t, m, run(t, m.Init()))
	_ = cmd

	updated, _ := m.handleListKeys("down")
	m = updated.(Model)
	if m.Cursor != 1 {
		t.Fatalf("Cursor = %d, want 1 after down", m.Cursor)
	}
	updated, _ = m.handleListKeys("G")
	m = updated.(Model)
	if m.Cursor != 2 {
		t.Fatalf("Cursor = %d, want 2 after G", m.Cursor)
	}
	updated, _ = m.handleListKeys("g")
	m = updated.(Model)
	if m.Cursor != 0 {
		t.Fatalf("Cursor = %d, want 0 after g", m.Cursor)
	}
	// Up at the top must not go negative.
	updated, _ = m.handleListKeys("up")
	m = updated.(Model)
	if m.Cursor != 0 {
		t.Fatalf("Cursor = %d, want clamped to 0", m.Cursor)
	}
}

func TestListEnterLoadsTheSelectedTaskDetail(t *testing.T) {
	task := sampleTask(1, "ACME-1", "open")
	fake := &data.FakeTask{
		ItemsByProject: map[string][]store.TaskListItem{"acme": {{Task: task}}},
		DetailByID:     map[int64]data.TaskDetail{1: sampleDetail(task)},
	}
	m := New(fake).WithProject("acme")
	m, _ = step(t, m, run(t, m.Init()))

	updated, cmd := m.handleListKeys("enter")
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("enter on a task should load its detail")
	}
	m, _ = step(t, m, run(t, cmd))
	if m.Screen != ScreenDetail {
		t.Fatalf("Screen = %v, want ScreenDetail", m.Screen)
	}
	if m.Detail == nil || m.Detail.Task.ID != 1 {
		t.Fatalf("Detail = %+v, want task 1 loaded", m.Detail)
	}
}

func TestListCopyKeyEmitsTheTasksJiraKeyNotItsSyncID(t *testing.T) {
	task := sampleTask(1, "ACME-1", "open")
	fake := &data.FakeTask{ItemsByProject: map[string][]store.TaskListItem{"acme": {{Task: task}}}}
	m := New(fake).WithProject("acme")
	m, _ = step(t, m, run(t, m.Init()))

	_, cmd := m.handleListKeys("c")
	if cmd == nil {
		t.Fatal("c should copy the selected task's key")
	}
	msg, ok := run(t, cmd).(shared.CopiedMsg)
	if !ok {
		t.Fatalf("cmd produced %T, want shared.CopiedMsg", run(t, cmd))
	}
	want := shared.OSC52Sequence("ACME-1")
	if msg.Sequence != want {
		t.Fatalf("copied sequence = %q, want the one for ACME-1 (%q)", msg.Sequence, want)
	}
}

func TestListStateFilterCyclesThroughEveryConcreteStateThenBackToActive(t *testing.T) {
	fake := &data.FakeTask{}
	m := New(fake).WithProject("acme")

	got := []string{m.Filter.State}
	for i := 0; i < len(stateOptions); i++ {
		updated, _ := m.handleListKeys("f")
		m = updated.(Model)
		got = append(got, m.Filter.State)
	}
	// One more press should wrap back to "" (active).
	updated, _ := m.handleListKeys("f")
	m = updated.(Model)
	if m.Filter.State != "" {
		t.Fatalf("state filter after a full cycle = %q, want back to \"\" (active)", m.Filter.State)
	}
	if got[1] != stateOptions[0] {
		t.Fatalf("first press = %q, want the first concrete state %q", got[1], stateOptions[0])
	}
}

func TestListKindFilterCyclesAndWraps(t *testing.T) {
	fake := &data.FakeTask{}
	m := New(fake).WithProject("acme")

	if m.Filter.Kind != "" {
		t.Fatalf("initial kind filter = %q, want \"\" (all)", m.Filter.Kind)
	}
	for range kindOptions {
		updated, _ := m.handleListKeys("K")
		m = updated.(Model)
	}
	if m.Filter.Kind != "" {
		t.Fatalf("kind filter after a full cycle = %q, want wrapped back to \"\"", m.Filter.Kind)
	}
}

func TestListSearchEnterAppliesTheQuery(t *testing.T) {
	fake := &data.FakeTask{ItemsByProject: map[string][]store.TaskListItem{
		"acme": {{Task: sampleTask(1, "ACME-1", "open")}},
	}}
	m := New(fake).WithProject("acme")

	updated, _ := m.handleListKeys("/")
	m = updated.(Model)
	if !m.Searching || !m.SearchInput.Focused() {
		t.Fatal("/ should focus the search input")
	}
	m.SearchInput.SetValue("api")

	m, cmd := step(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on the search input should reload with the query")
	}
	if m.Searching {
		t.Fatal("search input should blur once submitted")
	}
	m, _ = step(t, m, run(t, cmd))
	if fake.LastListFilter().Query != "api" {
		t.Fatalf("reader saw query %q, want %q", fake.LastListFilter().Query, "api")
	}
}

// TestNextPageStopsAtTheLastPage: with the store's own total in hand there
// is nothing left to infer from a short page, so the last page stays put
// instead of wrapping round to the first.
func TestNextPageStopsAtTheLastPage(t *testing.T) {
	items := make([]store.TaskListItem, pageSize+5)
	for i := range items {
		items[i] = store.TaskListItem{Task: sampleTask(int64(i+1), fmt.Sprintf("ACME-%d", i+1), "open")}
	}
	fake := &data.FakeTask{ItemsByProject: map[string][]store.TaskListItem{"acme": items}}
	m := New(fake).WithProject("acme")
	m, _ = step(t, m, run(t, m.Init()))
	if len(m.Items) != pageSize {
		t.Fatalf("first page = %d items, want the default limit %d", len(m.Items), pageSize)
	}

	updated, cmd := m.handleListKeys("n")
	m = updated.(Model)
	m, _ = step(t, m, run(t, cmd))
	if m.Filter.Offset != pageSize {
		t.Fatalf("offset after one full page = %d, want %d", m.Filter.Offset, pageSize)
	}
	if len(m.Items) != 5 {
		t.Fatalf("second page = %d items, want the remaining 5", len(m.Items))
	}

	updated, cmd = m.handleListKeys("n")
	m = updated.(Model)
	if cmd != nil {
		t.Fatal("n on the last page should not re-query the store")
	}
	if m.Filter.Offset != pageSize {
		t.Fatalf("offset after n on the last page = %d, want it to stay at %d", m.Filter.Offset, pageSize)
	}
}

// TestPrevPageIsDisabledOnTheFirstPage covers the other half of the page
// pair: the offset alone says whether there is a page behind this one, so
// the first page stays put and issues no query.
func TestPrevPageIsDisabledOnTheFirstPage(t *testing.T) {
	items := make([]store.TaskListItem, pageSize+5)
	for i := range items {
		items[i] = store.TaskListItem{Task: sampleTask(int64(i+1), fmt.Sprintf("ACME-%d", i+1), "open")}
	}
	fake := &data.FakeTask{ItemsByProject: map[string][]store.TaskListItem{"acme": items}}
	m := New(fake).WithProject("acme")
	m, _ = step(t, m, run(t, m.Init()))

	updated, cmd := m.handleListKeys("n")
	m = updated.(Model)
	m, _ = step(t, m, run(t, cmd))
	if m.Filter.Offset != pageSize {
		t.Fatalf("offset after n = %d, want %d", m.Filter.Offset, pageSize)
	}

	updated, cmd = m.handleListKeys("p")
	m = updated.(Model)
	m, _ = step(t, m, run(t, cmd))
	if m.Filter.Offset != 0 {
		t.Fatalf("offset after p = %d, want back on the first page", m.Filter.Offset)
	}
	if len(m.Items) != pageSize {
		t.Fatalf("first page = %d items, want the full %d back", len(m.Items), pageSize)
	}

	updated, cmd = m.handleListKeys("p")
	m = updated.(Model)
	if cmd != nil {
		t.Fatal("p on the first page should not re-query the store")
	}
	if m.Filter.Offset != 0 {
		t.Fatalf("offset = %d, want the first page left alone", m.Filter.Offset)
	}
}

func TestListEscGoesHome(t *testing.T) {
	m := New(&data.FakeTask{}).WithProject("acme")
	_, cmd := m.handleListKeys("esc")
	if cmd == nil {
		t.Fatal("esc from the list root should navigate to Home")
	}
	if _, ok := run(t, cmd).(tabs.NavigateMsg); !ok {
		t.Fatalf("esc produced %T, want tabs.NavigateMsg{Target: tabs.Home}", run(t, cmd))
	}
}

// ─── Detail (S4) ─────────────────────────────────────────────────────────────

func TestDetailEnterOnAnObservationNavigatesToMemory(t *testing.T) {
	task := sampleTask(1, "ACME-1", "open")
	m := New(&data.FakeTask{}).WithProject("acme")
	detail := sampleDetail(task, 1460, 1466)
	m.Detail = &detail
	m.Screen = ScreenDetail

	_, cmd := m.handleDetailKeys("enter")
	if cmd == nil {
		t.Fatal("enter on a linked observation should navigate")
	}
	msg, ok := run(t, cmd).(tabs.NavigateMsg)
	if !ok {
		t.Fatalf("cmd produced %T, want tabs.NavigateMsg", run(t, cmd))
	}
	if msg.Target != tabs.Memory || msg.ObservationID != 1460 {
		t.Fatalf("NavigateMsg = %+v, want Memory/1460", msg)
	}
}

// TestDetailEvidenceKeyNavigatesToEvidenceFilteredByTheTask pins T-10.04's
// dependency: rfc-tui.md §3.1 S4's "e" must filter the Evidence tab down to
// the task under view (S6's task_id filter), which needs TaskID on
// tabs.NavigateMsg — until T-10.04 the key only switched tabs with no filter.
func TestDetailEvidenceKeyNavigatesToEvidenceFilteredByTheTask(t *testing.T) {
	task := sampleTask(9, "ACME-9", "open")
	m := New(&data.FakeTask{}).WithProject("acme")
	detail := sampleDetail(task)
	m.Detail = &detail
	m.Screen = ScreenDetail

	_, cmd := m.handleDetailKeys("e")
	if cmd == nil {
		t.Fatal("e should navigate to Evidence")
	}
	msg, ok := run(t, cmd).(tabs.NavigateMsg)
	if !ok {
		t.Fatalf("cmd produced %T, want tabs.NavigateMsg", run(t, cmd))
	}
	if msg.Target != tabs.Evidence || msg.TaskID != 9 {
		t.Fatalf("NavigateMsg = %+v, want Evidence/9", msg)
	}
}

func TestDetailObservationsScrollWhenTheListOverflowsTheWindow(t *testing.T) {
	task := sampleTask(1, "ACME-1", "open")
	var ids []int64
	for i := int64(1); i <= 10; i++ {
		ids = append(ids, i)
	}
	m := withHeight(New(&data.FakeTask{}).WithProject("acme"), 20)
	detail := sampleDetail(task, ids...)
	m.Detail = &detail
	m.Screen = ScreenDetail

	visible := shared.VisibleItems(m.Height, detailChrome, observationItemLines, minVisibleItems)
	// visible-1 presses land the cursor on the last row the first window
	// still shows (index visible-1); one more press below moves it past
	// that window and is what should trigger the scroll.
	for i := 0; i < visible-1; i++ {
		updated, _ := m.handleDetailKeys("down")
		m = updated.(Model)
	}
	if m.DetailScroll != 0 {
		t.Fatalf("scroll = %d, want 0 while the cursor is still inside the first window", m.DetailScroll)
	}
	updated, _ := m.handleDetailKeys("down")
	m = updated.(Model)
	if m.DetailScroll == 0 {
		t.Fatal("moving past the visible window should scroll the observation list")
	}
}

func TestDetailStateChangeWritesThroughTheReaderAndReloads(t *testing.T) {
	task := sampleTask(1, "ACME-1", "open")
	fake := &data.FakeTask{DetailByID: map[int64]data.TaskDetail{1: sampleDetail(task)}}
	m := New(fake).WithProject("acme")
	detail := sampleDetail(task)
	m.Detail = &detail
	m.Screen = ScreenDetail

	updated, _ := m.handleDetailKeys("s")
	m = updated.(Model)
	if !m.ChangingState {
		t.Fatal("s should open the state picker")
	}
	if stateOptions[m.StateCursor] != "open" {
		t.Fatalf("state picker started on %q, want the task's current state open", stateOptions[m.StateCursor])
	}

	updated, _ = m.handleStatePickerKeys("down")
	m = updated.(Model)
	updated, cmd := m.handleStatePickerKeys("enter")
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("enter in the state picker should write the new state")
	}
	m, cmd = step(t, m, run(t, cmd))
	if len(fake.UpdateStateCalls()) != 1 || fake.UpdateStateCalls()[0].ID != 1 || fake.UpdateStateCalls()[0].State != stateOptions[1] {
		t.Fatalf("UpdateStateCalls = %+v, want one call for task 1 with state %q", fake.UpdateStateCalls(), stateOptions[1])
	}
	if m.ChangingState {
		t.Fatal("the picker should close once the write completes")
	}
	if cmd == nil {
		t.Fatal("a successful write should reload the detail")
	}
}

func TestDetailStateChangeEscCancelsWithoutWriting(t *testing.T) {
	task := sampleTask(1, "ACME-1", "open")
	fake := &data.FakeTask{}
	m := New(fake).WithProject("acme")
	detail := sampleDetail(task)
	m.Detail = &detail
	m.Screen = ScreenDetail

	updated, _ := m.handleDetailKeys("s")
	m = updated.(Model)
	updated, cmd := m.handleStatePickerKeys("esc")
	m = updated.(Model)
	if cmd != nil {
		t.Fatal("esc should not issue a write")
	}
	if m.ChangingState {
		t.Fatal("esc should close the picker")
	}
	if len(fake.UpdateStateCalls()) != 0 {
		t.Fatalf("UpdateStateCalls = %+v, want none", fake.UpdateStateCalls())
	}
}

func TestDetailLinkObservationFlow(t *testing.T) {
	task := sampleTask(1, "ACME-1", "open")
	fake := &data.FakeTask{DetailByID: map[int64]data.TaskDetail{1: sampleDetail(task, 42)}}
	m := New(fake).WithProject("acme")
	detail := sampleDetail(task)
	m.Detail = &detail
	m.Screen = ScreenDetail

	updated, _ := m.handleDetailKeys("l")
	m = updated.(Model)
	if !m.Linking || !m.LinkInput.Focused() {
		t.Fatal("l should focus the link input")
	}
	m.LinkInput.SetValue("42")

	m, cmd := step(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter in the link input should link the observation")
	}
	// linkObservation's own command yields observationLinkedMsg, whose
	// handler in turn returns a second command (the detail reload) — both
	// have to run for the tab to end up showing the newly linked
	// observation, the same two-hop chain TestDetailStateChangeWritesThrough
	// TheReaderAndReloads exercises for the state mirror write.
	m, cmd = step(t, m, run(t, cmd))
	if len(fake.LinkCalls()) != 1 || fake.LinkCalls()[0].TaskID != 1 || fake.LinkCalls()[0].ObservationID != 42 {
		t.Fatalf("LinkCalls = %+v, want one call linking observation 42 to task 1", fake.LinkCalls())
	}
	if m.Linking {
		t.Fatal("the link input should close once the write completes")
	}
	if cmd == nil {
		t.Fatal("a successful link should reload the detail")
	}
	m, _ = step(t, m, run(t, cmd))
	if m.Detail == nil || len(m.Detail.Observations) != 1 {
		t.Fatalf("Detail after link = %+v, want the reloaded observation", m.Detail)
	}
}

func TestDetailLinkObservationRejectsNonNumericInput(t *testing.T) {
	task := sampleTask(1, "ACME-1", "open")
	fake := &data.FakeTask{}
	m := New(fake).WithProject("acme")
	detail := sampleDetail(task)
	m.Detail = &detail
	m.Screen = ScreenDetail
	m.Linking = true
	m.LinkInput.Focus()
	m.LinkInput.SetValue("not-a-number")

	m, cmd := step(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("an invalid observation id must not issue a write")
	}
	if len(fake.LinkCalls()) != 0 {
		t.Fatalf("LinkCalls = %+v, want none", fake.LinkCalls())
	}
	if m.ErrorMsg == "" {
		t.Fatal("an invalid observation id should surface an error")
	}
	if !m.Linking {
		t.Fatal("the link input should stay open so the user can correct it")
	}
}

func TestDetailOpenJiraUsesTheInjectableOpener(t *testing.T) {
	task := sampleTask(1, "ACME-1", "open")
	m := New(&data.FakeTask{}).WithProject("acme")
	detail := sampleDetail(task)
	m.Detail = &detail
	m.Screen = ScreenDetail

	var gotURL string
	prev := openURL
	openURL = func(url string) error { gotURL = url; return nil }
	defer func() { openURL = prev }()

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	if gotURL != data.JiraURL("ACME-1") {
		t.Fatalf("opened URL = %q, want the ACME-1 Jira link", gotURL)
	}
	if m.ErrorMsg != "" {
		t.Fatalf("ErrorMsg = %q, want none on a successful open", m.ErrorMsg)
	}
}

func TestDetailOpenJiraWithoutAKeyReportsAnErrorInsteadOfOpeningNothing(t *testing.T) {
	task := sampleTask(1, "ACME-1", "open")
	task.JiraKey = nil
	m := New(&data.FakeTask{}).WithProject("acme")
	detail := sampleDetail(task)
	m.Detail = &detail
	m.Screen = ScreenDetail

	called := false
	prev := openURL
	openURL = func(string) error { called = true; return nil }
	defer func() { openURL = prev }()

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	if called {
		t.Fatal("a task with no jira_key must not attempt to open a URL")
	}
	if m.ErrorMsg == "" {
		t.Fatal("expected an error explaining there is no jira_key")
	}
}

func TestDetailOpenFailureFallsBackToShowingTheURL(t *testing.T) {
	task := sampleTask(1, "ACME-1", "open")
	m := New(&data.FakeTask{}).WithProject("acme")
	detail := sampleDetail(task)
	m.Detail = &detail
	m.Screen = ScreenDetail

	prev := openURL
	openURL = func(string) error { return errors.New("exec: \"xdg-open\": executable file not found in $PATH") }
	defer func() { openURL = prev }()

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	if m.ErrorMsg == "" {
		t.Fatal("a failed open should surface the URL so it can be copied instead")
	}
}

func TestDetailCopyKeyAndBranch(t *testing.T) {
	task := sampleTask(1, "ACME-1", "open")
	m := New(&data.FakeTask{}).WithProject("acme")
	detail := sampleDetail(task)
	m.Detail = &detail
	m.Screen = ScreenDetail

	_, cKeyCmd := m.handleDetailKeys("c")
	keyMsg := run(t, cKeyCmd).(shared.CopiedMsg)
	if keyMsg.Sequence != shared.OSC52Sequence("ACME-1") {
		t.Fatalf("c copied the wrong content: %q", keyMsg.Sequence)
	}

	_, bCmd := m.handleDetailKeys("b")
	branchMsg := run(t, bCmd).(shared.CopiedMsg)
	if branchMsg.Sequence != shared.OSC52Sequence("fix/ACME-1") {
		t.Fatalf("b copied the wrong content: %q", branchMsg.Sequence)
	}
}

func TestDetailEscReturnsToListAndReloadsIt(t *testing.T) {
	task := sampleTask(1, "ACME-1", "open")
	fake := &data.FakeTask{ItemsByProject: map[string][]store.TaskListItem{"acme": {{Task: task}}}}
	m := New(fake).WithProject("acme")
	detail := sampleDetail(task)
	m.Detail = &detail
	m.Screen = ScreenDetail

	updated, cmd := m.handleDetailKeys("esc")
	m = updated.(Model)
	if m.Screen != ScreenList {
		t.Fatalf("Screen = %v, want ScreenList", m.Screen)
	}
	if cmd == nil {
		t.Fatal("leaving the detail should reload the list behind it")
	}
}

// ─── Context pack (S5) ───────────────────────────────────────────────────────

func TestContextPackLoadsAndRendersFromDetail(t *testing.T) {
	task := sampleTask(1, "ACME-1", "open")
	fake := &data.FakeTask{ContextPackByID: map[int64]string{1: "# Context pack: acme / ACME-1\n\nbody"}}
	m := New(fake).WithProject("acme")
	detail := sampleDetail(task)
	m.Detail = &detail
	m.Screen = ScreenDetail

	updated, cmd := m.handleDetailKeys("x")
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("x should build the context pack")
	}
	m, _ = step(t, m, run(t, cmd))
	if m.Screen != ScreenContextPack {
		t.Fatalf("Screen = %v, want ScreenContextPack", m.Screen)
	}
	if m.ContextPack != "# Context pack: acme / ACME-1\n\nbody" {
		t.Fatalf("ContextPack = %q", m.ContextPack)
	}
}

func TestContextPackCopyEmitsTheWholePack(t *testing.T) {
	m := New(&data.FakeTask{}).WithProject("acme")
	m.Screen = ScreenContextPack
	m.ContextPack = "# pack"
	detail := sampleDetail(sampleTask(1, "ACME-1", "open"))
	m.Detail = &detail

	_, cmd := m.handleContextPackKeys("c")
	msg, ok := run(t, cmd).(shared.CopiedMsg)
	if !ok || msg.Sequence != shared.OSC52Sequence("# pack") {
		t.Fatalf("c did not copy the full pack, got %+v", msg)
	}
}

func TestContextPackWriteSavesUnderTheEvidenceRoot(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CD_EVIDENCE_DIR", dir)

	task := sampleTask(1, "ACME-1", "open")
	m := New(&data.FakeTask{}).WithProject("acme")
	m.Screen = ScreenContextPack
	m.ContextPack = "# Context pack: acme / ACME-1"
	detail := sampleDetail(task)
	m.Detail = &detail

	updated, cmd := m.handleContextPackKeys("w")
	m = updated.(Model)
	if m.ErrorMsg != "" {
		t.Fatalf("ErrorMsg = %q, want none", m.ErrorMsg)
	}
	if cmd == nil {
		t.Fatal("a successful write should schedule clearing the confirmation banner")
	}

	want := filepath.Join(dir, "acme", "ACME-1", "context-pack.md")
	got, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", want, err)
	}
	if string(got) != m.ContextPack {
		t.Fatalf("file content = %q, want %q", got, m.ContextPack)
	}
	if m.CopyFeedback == "" {
		t.Fatal("expected a save confirmation banner")
	}
}

func TestContextPackEscReturnsToDetail(t *testing.T) {
	m := New(&data.FakeTask{}).WithProject("acme")
	m.Screen = ScreenContextPack

	updated, _ := m.handleContextPackKeys("esc")
	m = updated.(Model)
	if m.Screen != ScreenDetail {
		t.Fatalf("Screen = %v, want ScreenDetail", m.Screen)
	}
}

// ─── Update dispatch ─────────────────────────────────────────────────────────

func TestUpdateAppliesWindowSize(t *testing.T) {
	m := New(&data.FakeTask{}).WithProject("acme")

	m, _ = step(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
	if m.Width != 100 || m.Height != 40 {
		t.Fatalf("Width/Height = %d/%d, want 100/40", m.Width, m.Height)
	}
}

// TestUpdateRoutesKeyMsgToTheStatePickerAndContextPackHandlers pins Update's
// own routing for two branches every other test reaches by calling the
// handler directly: ChangingState on the detail screen, and ScreenContextPack.
func TestUpdateRoutesKeyMsgToTheStatePickerAndContextPackHandlers(t *testing.T) {
	task := sampleTask(1, "ACME-1", "open")
	m := New(&data.FakeTask{}).WithProject("acme")
	detail := sampleDetail(task)
	m.Detail = &detail
	m.Screen = ScreenDetail
	m.ChangingState = true

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if m.StateCursor != 1 {
		t.Fatalf("Update did not route to handleStatePickerKeys: StateCursor = %d, want 1", m.StateCursor)
	}

	m.ChangingState = false
	m.Screen = ScreenContextPack
	m.ContextPack = "# pack"
	m, cmd := step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	if cmd == nil {
		t.Fatal("Update did not route to handleContextPackKeys")
	}
	if _, ok := run(t, cmd).(shared.CopiedMsg); !ok {
		t.Fatalf("routed command produced %T, want shared.CopiedMsg", run(t, cmd))
	}
}

func TestUpdateOnCopiedMsgSetsFeedbackAndSchedulesItsClear(t *testing.T) {
	m := New(&data.FakeTask{}).WithProject("acme")

	m, cmd := step(t, m, shared.CopiedMsg{Sequence: "seq"})
	if m.CopyFeedback == "" {
		t.Fatal("a CopiedMsg should set the clipboard confirmation banner")
	}
	if cmd == nil {
		t.Fatal("a CopiedMsg should schedule clearing the banner")
	}
}

func TestUpdateOnClearFeedbackMsgClearsTheBanner(t *testing.T) {
	m := New(&data.FakeTask{}).WithProject("acme")
	m.CopyFeedback = "Copied!"

	m, _ = step(t, m, shared.ClearFeedbackMsg{})
	if m.CopyFeedback != "" {
		t.Fatalf("CopyFeedback = %q, want cleared", m.CopyFeedback)
	}
}

func TestTasksLoadedClampsAnOutOfRangeCursor(t *testing.T) {
	m := New(&data.FakeTask{}).WithProject("acme")
	m.Cursor, m.Scroll = 5, 3

	m, _ = step(t, m, tasksLoadedMsg{page: data.Page[store.TaskListItem]{Items: []store.TaskListItem{{Task: sampleTask(1, "ACME-1", "open")}}, Total: 1, Limit: 20}})
	if m.Cursor != 0 || m.Scroll != 0 {
		t.Fatalf("Cursor/Scroll = %d/%d, want reset to 0/0 once the reload is shorter", m.Cursor, m.Scroll)
	}
}

func TestTaskDetailLoadedClampsAnOutOfRangeDetailCursor(t *testing.T) {
	task := sampleTask(1, "ACME-1", "open")
	m := New(&data.FakeTask{}).WithProject("acme")
	m.DetailCursor, m.DetailScroll = 5, 3

	m, _ = step(t, m, taskDetailLoadedMsg{detail: sampleDetail(task, 42)})
	if m.DetailCursor != 0 || m.DetailScroll != 0 {
		t.Fatalf("DetailCursor/DetailScroll = %d/%d, want reset to 0/0", m.DetailCursor, m.DetailScroll)
	}
}

func TestStateUpdatedSurfacesAnError(t *testing.T) {
	task := sampleTask(1, "ACME-1", "open")
	m := New(&data.FakeTask{}).WithProject("acme")
	detail := sampleDetail(task)
	m.Detail = &detail
	m.ChangingState = true

	m, cmd := step(t, m, stateUpdatedMsg{id: 1, err: errors.New("database is locked")})
	if m.ChangingState {
		t.Fatal("a failed state update should still close the picker")
	}
	if m.ErrorMsg == "" {
		t.Fatal("a failed state update should surface an error")
	}
	if cmd != nil {
		t.Fatal("a failed state update must not reload the detail")
	}
}

// TestStateUpdatedForATaskTheUserHasSinceLeftDoesNotReload pins the guard
// mirroring evidence's manifestLoadedMsg one: a write completing for a task
// that is no longer on screen (or for which Detail was never set) must not
// issue a reload the current screen has nothing to do with.
func TestStateUpdatedForATaskTheUserHasSinceLeftDoesNotReload(t *testing.T) {
	m := New(&data.FakeTask{}).WithProject("acme")

	m, cmd := step(t, m, stateUpdatedMsg{id: 1})
	if cmd != nil {
		t.Fatal("a state update with no matching Detail on screen must not reload anything")
	}
	if m.ErrorMsg != "" {
		t.Fatalf("ErrorMsg = %q, want none on a successful update", m.ErrorMsg)
	}
}

func TestObservationLinkedSurfacesAnError(t *testing.T) {
	task := sampleTask(1, "ACME-1", "open")
	m := New(&data.FakeTask{}).WithProject("acme")
	detail := sampleDetail(task)
	m.Detail = &detail
	m.Linking = true
	m.LinkInput.SetValue("42")
	m.LinkInput.Focus()

	m, cmd := step(t, m, observationLinkedMsg{taskID: 1, err: errors.New("observation not found")})
	if m.Linking {
		t.Fatal("a failed link should still close the input")
	}
	if m.ErrorMsg == "" {
		t.Fatal("a failed link should surface an error")
	}
	if cmd != nil {
		t.Fatal("a failed link must not reload the detail")
	}
}

func TestContextPackLoadedSurfacesAnError(t *testing.T) {
	m := New(&data.FakeTask{}).WithProject("acme")

	m, _ = step(t, m, contextPackLoadedMsg{taskID: 1, err: errors.New("task has no observations")})
	if m.ErrorMsg == "" {
		t.Fatal("a failed context pack build should surface an error")
	}
	if m.Screen == ScreenContextPack {
		t.Fatal("a failed build must not switch to the context pack screen")
	}
}

// ─── Search input (S3 overlay) ────────────────────────────────────────────────

func TestHandleSearchInputKeysEscBlursAndClearsTheQuery(t *testing.T) {
	m := New(&data.FakeTask{}).WithProject("acme")
	m.Searching = true
	m.SearchInput.Focus()
	m.SearchInput.SetValue("api")

	m, cmd := step(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Fatal("esc from the search input must not reload anything")
	}
	if m.Searching || m.SearchInput.Focused() {
		t.Fatal("esc should close and blur the search input")
	}
	if m.SearchInput.Value() != "" {
		t.Fatalf("SearchInput.Value() = %q, want cleared on esc", m.SearchInput.Value())
	}
}

// TestHandleSearchInputKeysTypingUpdatesTheValue pins the passthrough branch
// (any key that is neither enter nor esc) that hands the keystroke to the
// underlying textinput.Model.
func TestHandleSearchInputKeysTypingUpdatesTheValue(t *testing.T) {
	m := New(&data.FakeTask{}).WithProject("acme")
	m.Searching = true
	m.SearchInput.Focus()

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if m.SearchInput.Value() != "a" {
		t.Fatalf("SearchInput.Value() = %q, want the typed rune", m.SearchInput.Value())
	}
}

// ─── Link input (S4 overlay) ─────────────────────────────────────────────────

func TestHandleLinkInputKeysEscBlursAndClearsTheValue(t *testing.T) {
	task := sampleTask(1, "ACME-1", "open")
	m := New(&data.FakeTask{}).WithProject("acme")
	detail := sampleDetail(task)
	m.Detail = &detail
	m.Linking = true
	m.LinkInput.Focus()
	m.LinkInput.SetValue("42")

	m, cmd := step(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Fatal("esc from the link input must not link anything")
	}
	if m.Linking || m.LinkInput.Focused() {
		t.Fatal("esc should close and blur the link input")
	}
	if m.LinkInput.Value() != "" {
		t.Fatalf("LinkInput.Value() = %q, want cleared on esc", m.LinkInput.Value())
	}
}

func TestHandleLinkInputKeysTypingUpdatesTheValue(t *testing.T) {
	task := sampleTask(1, "ACME-1", "open")
	m := New(&data.FakeTask{}).WithProject("acme")
	detail := sampleDetail(task)
	m.Detail = &detail
	m.Linking = true
	m.LinkInput.Focus()

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("4")})
	if m.LinkInput.Value() != "4" {
		t.Fatalf("LinkInput.Value() = %q, want the typed rune", m.LinkInput.Value())
	}
}

// TestHandleLinkInputKeysEnterWithNoDetailClosesWithoutLinking pins the
// defensive branch in handleLinkInputKeys: the input closed underneath it
// (e.g. esc on the detail screen) before enter landed.
func TestHandleLinkInputKeysEnterWithNoDetailClosesWithoutLinking(t *testing.T) {
	fake := &data.FakeTask{}
	m := New(fake).WithProject("acme")
	m.Linking = true
	m.LinkInput.Focus()
	m.LinkInput.SetValue("42")

	m, cmd := step(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("enter with no Detail on screen must not attempt to link")
	}
	if m.Linking {
		t.Fatal("the input should still close")
	}
	if len(fake.LinkCalls()) != 0 {
		t.Fatalf("LinkCalls = %+v, want none", fake.LinkCalls())
	}
}

// ─── State picker (S4 overlay) ────────────────────────────────────────────────

func TestHandleStatePickerKeysUpClampsAtZero(t *testing.T) {
	task := sampleTask(1, "ACME-1", "open")
	m := New(&data.FakeTask{}).WithProject("acme")
	detail := sampleDetail(task)
	m.Detail = &detail
	m.ChangingState = true
	m.StateCursor = 0

	updated, _ := m.handleStatePickerKeys("up")
	m = updated.(Model)
	if m.StateCursor != 0 {
		t.Fatalf("StateCursor = %d, want clamped to 0", m.StateCursor)
	}

	m.StateCursor = 1
	updated, _ = m.handleStatePickerKeys("k")
	m = updated.(Model)
	if m.StateCursor != 0 {
		t.Fatalf("StateCursor = %d, want 0 after k", m.StateCursor)
	}
}

// TestHandleStatePickerKeysEnterWithNoDetailClosesWithoutWriting pins the
// defensive branch symmetrical to handleLinkInputKeys's: Detail went away
// underneath the picker before enter landed.
func TestHandleStatePickerKeysEnterWithNoDetailClosesWithoutWriting(t *testing.T) {
	fake := &data.FakeTask{}
	m := New(fake).WithProject("acme")
	m.ChangingState = true

	updated, cmd := m.handleStatePickerKeys("enter")
	m = updated.(Model)
	if cmd != nil {
		t.Fatal("enter with no Detail on screen must not write a state")
	}
	if m.ChangingState {
		t.Fatal("the picker should still close")
	}
}

// ─── Detail (S4) — remaining keys ─────────────────────────────────────────────

func TestDetailUKeyOpensThePR(t *testing.T) {
	task := sampleTask(1, "ACME-1", "open")
	task.PRUrl = strp("https://github.com/acme/repo/pull/1")
	m := New(&data.FakeTask{}).WithProject("acme")
	detail := sampleDetail(task)
	m.Detail = &detail
	m.Screen = ScreenDetail

	var gotURL string
	prev := openURL
	openURL = func(url string) error { gotURL = url; return nil }
	defer func() { openURL = prev }()

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	if gotURL != *task.PRUrl {
		t.Fatalf("opened URL = %q, want the pr_url %q", gotURL, *task.PRUrl)
	}
	if m.ErrorMsg != "" {
		t.Fatalf("ErrorMsg = %q, want none on a successful open", m.ErrorMsg)
	}
}

func TestDetailUKeyWithNoPRReportsAnErrorInsteadOfOpeningNothing(t *testing.T) {
	task := sampleTask(1, "ACME-1", "open")
	task.PRUrl = nil
	m := New(&data.FakeTask{}).WithProject("acme")
	detail := sampleDetail(task)
	m.Detail = &detail
	m.Screen = ScreenDetail

	called := false
	prev := openURL
	openURL = func(string) error { called = true; return nil }
	defer func() { openURL = prev }()

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	if called {
		t.Fatal("a task with no pr_url must not attempt to open a URL")
	}
	if m.ErrorMsg == "" {
		t.Fatal("expected an error explaining there is no pr_url")
	}
}

func TestDetailUKeyOpenFailureFallsBackToShowingTheURL(t *testing.T) {
	task := sampleTask(1, "ACME-1", "open")
	task.PRUrl = strp("https://github.com/acme/repo/pull/1")
	m := New(&data.FakeTask{}).WithProject("acme")
	detail := sampleDetail(task)
	m.Detail = &detail
	m.Screen = ScreenDetail

	prev := openURL
	openURL = func(string) error { return errors.New("exec: \"xdg-open\": executable file not found in $PATH") }
	defer func() { openURL = prev }()

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	if m.ErrorMsg == "" {
		t.Fatal("a failed open should surface the URL so it can be copied instead")
	}
}

func TestDetailRKeyReloadsTheTask(t *testing.T) {
	task := sampleTask(1, "ACME-1", "open")
	fake := &data.FakeTask{DetailByID: map[int64]data.TaskDetail{1: sampleDetail(task, 7)}}
	m := New(fake).WithProject("acme")
	detail := sampleDetail(task)
	m.Detail = &detail
	m.Screen = ScreenDetail

	cmd := m.Refresh()
	if cmd == nil {
		t.Fatal("Refresh on the detail should reload the task")
	}
	m, _ = step(t, m, run(t, cmd))
	if m.Detail == nil || len(m.Detail.Observations) != 1 {
		t.Fatalf("Detail after r = %+v, want the reloaded observation", m.Detail)
	}
}

// ─── Context pack (S5) — remaining keys ───────────────────────────────────────

func TestHandleContextPackKeysScrolls(t *testing.T) {
	m := New(&data.FakeTask{}).WithProject("acme")
	m.Screen = ScreenContextPack
	m.ContextPack = "# pack"
	m.ContextPackScroll = 0

	updated, _ := m.handleContextPackKeys("down")
	m = updated.(Model)
	if m.ContextPackScroll != 1 {
		t.Fatalf("ContextPackScroll = %d, want 1 after down", m.ContextPackScroll)
	}
	updated, _ = m.handleContextPackKeys("up")
	m = updated.(Model)
	if m.ContextPackScroll != 0 {
		t.Fatalf("ContextPackScroll = %d, want 0 after up", m.ContextPackScroll)
	}
	// Up at the top must not go negative.
	updated, _ = m.handleContextPackKeys("up")
	m = updated.(Model)
	if m.ContextPackScroll != 0 {
		t.Fatalf("ContextPackScroll = %d, want clamped to 0", m.ContextPackScroll)
	}
}

func TestHandleContextPackKeysRRebuildsWhenDetailIsPresent(t *testing.T) {
	task := sampleTask(1, "ACME-1", "open")
	fake := &data.FakeTask{ContextPackByID: map[int64]string{1: "# rebuilt"}}
	m := New(fake).WithProject("acme")
	detail := sampleDetail(task)
	m.Detail = &detail
	m.Screen = ScreenContextPack
	m.ContextPack = "# stale"

	cmd := m.Refresh()
	if cmd == nil {
		t.Fatal("Refresh with a task loaded should rebuild the context pack")
	}
	m, _ = step(t, m, run(t, cmd))
	if m.ContextPack != "# rebuilt" {
		t.Fatalf("ContextPack = %q, want the rebuilt pack", m.ContextPack)
	}
}

// TestContextPackWriteWithNothingLoadedIsANoOp pins writeContextPack's early
// return: "w" pressed before a pack (or a task) has actually loaded must not
// try to resolve a path or touch the filesystem at all.
func TestContextPackWriteWithNothingLoadedIsANoOp(t *testing.T) {
	m := New(&data.FakeTask{}).WithProject("acme")
	m.Screen = ScreenContextPack

	updated, cmd := m.handleContextPackKeys("w")
	m = updated.(Model)
	if cmd != nil {
		t.Fatal("w with no context pack loaded must not schedule anything")
	}
	if m.ErrorMsg != "" || m.CopyFeedback != "" {
		t.Fatalf("m = %+v, want no error and no feedback", m)
	}
}

// TestContextPackWriteReportsAMkdirAllFailure forces a real, non-mocked
// os.MkdirAll error: "acme" already exists as a plain file, so it cannot
// also be created as the directory writeContextPack needs on the path to
// context-pack.md.
func TestContextPackWriteReportsAMkdirAllFailure(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "acme"), []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("seeding the blocking file: %v", err)
	}
	t.Setenv("CD_EVIDENCE_DIR", root)

	task := sampleTask(1, "ACME-1", "open")
	m := New(&data.FakeTask{}).WithProject("acme")
	m.Screen = ScreenContextPack
	m.ContextPack = "# pack"
	detail := sampleDetail(task)
	m.Detail = &detail

	updated, cmd := m.handleContextPackKeys("w")
	m = updated.(Model)
	if cmd != nil {
		t.Fatal("a MkdirAll failure must not schedule clearing a save banner that never appeared")
	}
	if m.ErrorMsg == "" {
		t.Fatal("a MkdirAll failure should surface an error instead of silently doing nothing")
	}
	if m.CopyFeedback != "" {
		t.Fatalf("CopyFeedback = %q, want none on a failed write", m.CopyFeedback)
	}
}

// TestContextPackWriteReportsAWriteFileFailure forces a real os.WriteFile
// error the same way: context-pack.md already exists as a directory, so it
// cannot also be written as a regular file.
func TestContextPackWriteReportsAWriteFileFailure(t *testing.T) {
	root := t.TempDir()
	blocked := filepath.Join(root, "acme", "ACME-1", "context-pack.md")
	if err := os.MkdirAll(blocked, 0o755); err != nil {
		t.Fatalf("seeding the blocking directory: %v", err)
	}
	t.Setenv("CD_EVIDENCE_DIR", root)

	task := sampleTask(1, "ACME-1", "open")
	m := New(&data.FakeTask{}).WithProject("acme")
	m.Screen = ScreenContextPack
	m.ContextPack = "# pack"
	detail := sampleDetail(task)
	m.Detail = &detail

	updated, cmd := m.handleContextPackKeys("w")
	m = updated.(Model)
	if cmd != nil {
		t.Fatal("a WriteFile failure must not schedule clearing a save banner that never appeared")
	}
	if m.ErrorMsg == "" {
		t.Fatal("a WriteFile failure should surface an error instead of silently doing nothing")
	}
}

// ─── Pure helpers ────────────────────────────────────────────────────────────

// TestNextStateWithAnUnrecognizedCurrentValueDefaultsToActive pins
// nextState's fallback branch: a current value store.TaskListFilter never
// actually produces (not "" and not one of stateOptions) still returns to
// "" instead of getting stuck, the same fail-safe nextKind's wrap-around
// gives a value already in kindOptions.
func TestNextStateWithAnUnrecognizedCurrentValueDefaultsToActive(t *testing.T) {
	if got := nextState("not-a-real-state"); got != "" {
		t.Fatalf("nextState(garbage) = %q, want \"\" (active)", got)
	}
}

func TestNextKindWithAnUnrecognizedCurrentValueDefaultsToAll(t *testing.T) {
	if got := nextKind("not-a-real-kind"); got != "" {
		t.Fatalf("nextKind(garbage) = %q, want \"\" (all)", got)
	}
}

// ─── Project switching ────────────────────────────────────────────────────────

func TestWithProjectResetsListAndDetailState(t *testing.T) {
	m := New(&data.FakeTask{}).WithProject("acme")
	m.Items = []store.TaskListItem{{Task: sampleTask(1, "ACME-1", "open")}}
	m.Cursor = 1
	m.Filter.State = "done"
	detail := sampleDetail(sampleTask(1, "ACME-1", "open"))
	m.Detail = &detail
	m.Screen = ScreenDetail
	m.ErrorMsg = "boom"

	m = m.WithProject("nextcloud")

	if m.Screen != ScreenList || len(m.Items) != 0 || m.Cursor != 0 || m.Filter.State != "" || m.Detail != nil || m.ErrorMsg != "" {
		t.Fatalf("WithProject left stale state: %+v", m)
	}
}

// TestRangeIndicatorShowsStoreTotal: the footer counts what the filter
// matched in the store, not what fits on the page. Seeding more rows than
// one page holds is what makes the two numbers differ — an adapter that
// dropped the total would render "of 20" here.
func TestRangeIndicatorShowsStoreTotal(t *testing.T) {
	items := make([]store.TaskListItem, pageSize+7)
	for i := range items {
		items[i] = store.TaskListItem{Task: sampleTask(int64(i+1), fmt.Sprintf("ACME-%d", i+1), "open")}
	}
	fake := &data.FakeTask{ItemsByProject: map[string][]store.TaskListItem{"acme": items}}
	m := New(fake).WithProject("acme")
	m.Height = 40
	m, _ = step(t, m, run(t, m.Init()))

	if m.Total != len(items) {
		t.Fatalf("Total = %d, want the store's own %d", m.Total, len(items))
	}
	if got := m.View(); !strings.Contains(got, fmt.Sprintf("of %d", len(items))) {
		t.Fatalf("range indicator does not report the store total %d:\n%s", len(items), got)
	}

	// On the second page the range is absolute, not page-relative.
	updated, cmd := m.handleListKeys("n")
	m = updated.(Model)
	m, _ = step(t, m, run(t, cmd))
	if got := m.View(); !strings.Contains(got, fmt.Sprintf("tasks %d-%d of %d", pageSize+1, len(items), len(items))) {
		t.Fatalf("second page range is not absolute:\n%s", got)
	}
}
