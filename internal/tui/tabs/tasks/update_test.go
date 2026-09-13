package tasks

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	if fake.LastListFilter.Query != "api" {
		t.Fatalf("reader saw query %q, want %q", fake.LastListFilter.Query, "api")
	}
}

func TestListNextPageAdvancesOffsetThenWrapsOnAShortPage(t *testing.T) {
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
	m, _ = step(t, m, run(t, cmd))
	if m.Filter.Offset != 0 {
		t.Fatalf("offset after a short page = %d, want wrapped back to 0", m.Filter.Offset)
	}
}

func TestListEscGoesHome(t *testing.T) {
	m := New(&data.FakeTask{}).WithProject("acme")
	_, cmd := m.handleListKeys("esc")
	if cmd == nil {
		t.Fatal("esc from the list root should emit HomeMsg")
	}
	if _, ok := run(t, cmd).(tabs.HomeMsg); !ok {
		t.Fatalf("esc produced %T, want tabs.HomeMsg", run(t, cmd))
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
	if len(fake.UpdateStateCalls) != 1 || fake.UpdateStateCalls[0].ID != 1 || fake.UpdateStateCalls[0].State != stateOptions[1] {
		t.Fatalf("UpdateStateCalls = %+v, want one call for task 1 with state %q", fake.UpdateStateCalls, stateOptions[1])
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
	if len(fake.UpdateStateCalls) != 0 {
		t.Fatalf("UpdateStateCalls = %+v, want none", fake.UpdateStateCalls)
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
	if len(fake.LinkCalls) != 1 || fake.LinkCalls[0].TaskID != 1 || fake.LinkCalls[0].ObservationID != 42 {
		t.Fatalf("LinkCalls = %+v, want one call linking observation 42 to task 1", fake.LinkCalls)
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
	if len(fake.LinkCalls) != 0 {
		t.Fatalf("LinkCalls = %+v, want none", fake.LinkCalls)
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
