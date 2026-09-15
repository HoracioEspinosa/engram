package graph

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/project"
	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	tea "github.com/charmbracelet/bubbletea"
)

// fixture is one project whose graph the store has judged stale, with the
// reason it gave and the observations written against its nodes.
func fixture() *data.FakeGraph {
	refs := make([]data.ObservationRef, 0, refPageSize+5)
	for i := 1; i <= refPageSize+5; i++ {
		refs = append(refs, data.ObservationRef{
			ObservationID: int64(i), RefKind: "graph",
			Ref: fmt.Sprintf("internal/store/store.go:Store%d", i), GraphCommit: strings.Repeat("c", 40),
		})
	}

	return &data.FakeGraph{
		StateByProject: map[string]data.GraphState{"clarodrive": {
			Project: "clarodrive", Commit: strings.Repeat("c", 40),
			BuiltAt: "2026-01-14 08:00:00", CheckedAt: "2026-01-15 09:00:00",
			Nodes: 1284, Edges: 3901, Communities: 17,
			Stale: true, StaleReason: "code_changed", ChangedFiles: 6,
			GodNodes: []project.GodNode{
				{Label: "Store", Edges: 412, File: "internal/store/store.go"},
				{Label: "Model", Edges: 208, File: "internal/tui/app/model.go"},
			},
		}},
		SyncResult: map[string]data.GraphState{"clarodrive": {
			Project: "clarodrive", Commit: strings.Repeat("d", 40),
			Nodes: 1290, Edges: 3999, Communities: 18,
		}},
		RefsByProject: map[string][]data.ObservationRef{"clarodrive": refs},
	}
}

func loaded(t *testing.T) (Model, *data.FakeGraph) {
	t.Helper()
	reader := fixture()
	m := New(reader, reader).WithStyles(theme.New(theme.KoiPond())).WithProject("clarodrive")
	m = step(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})
	cmd := m.Refresh()
	if cmd == nil {
		t.Fatal("a scoped tab should have something to load")
	}
	return step(t, m, cmd()), reader
}

func step(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	updated, _ := m.Update(msg)
	next, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want graph.Model", updated)
	}
	return next
}

func stepCmd(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(msg)
	next, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want graph.Model", updated)
	}
	return next, cmd
}

func press(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func TestTheTabIsScopedAndTitled(t *testing.T) {
	m := New(nil, nil).WithProject("clarodrive")

	if m.Title() != "Graph" {
		t.Errorf("Title() = %q", m.Title())
	}
	if m.Project() != "clarodrive" {
		t.Errorf("Project() = %q", m.Project())
	}
	if m.CapturingText() {
		t.Error("the Graph tab has no text input")
	}
	if New(nil, nil).Refresh() != nil || New(nil, nil).Init() != nil {
		t.Error("a tab with no project has nothing to load")
	}
}

func TestTheSummaryShowsWhatTheStorePersisted(t *testing.T) {
	m, _ := loaded(t)

	out := m.View()
	for _, want := range []string{"1284", "3901", "17", "code_changed", "6 changed files", "Store", "412"} {
		if !strings.Contains(out, want) {
			t.Errorf("the summary omits %q, got:\n%s", want, out)
		}
	}
}

// TestTheStalenessReasonIsPrintedLiterally pins the rule: the TUI renders
// the store's verdict and never forms its own, so a reason this build has
// never heard of still reaches the reader.
func TestTheStalenessReasonIsPrintedLiterally(t *testing.T) {
	reader := fixture()
	reader.StateByProject["clarodrive"] = data.GraphState{
		Nodes: 1, Stale: true, StaleReason: "a_reason_from_a_newer_store",
	}
	m := New(reader, reader).WithStyles(theme.New(theme.KoiPond())).WithProject("clarodrive")
	m = step(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})
	m = step(t, m, m.Refresh()())

	if !strings.Contains(m.View(), "a_reason_from_a_newer_store") {
		t.Fatalf("the reason was rewritten instead of printed, got:\n%s", m.View())
	}
}

func TestAStaleGraphWithNoReasonStillSaysItIsStale(t *testing.T) {
	reader := fixture()
	reader.StateByProject["clarodrive"] = data.GraphState{Nodes: 1, Stale: true}
	m := New(reader, reader).WithStyles(theme.New(theme.KoiPond())).WithProject("clarodrive")
	m = step(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})
	m = step(t, m, m.Refresh()())

	if !strings.Contains(m.View(), "stale") {
		t.Fatalf("a stale graph with no reason should still read as stale, got:\n%s", m.View())
	}
}

func TestAFreshGraphSaysSo(t *testing.T) {
	reader := fixture()
	reader.StateByProject["clarodrive"] = data.GraphState{Nodes: 10, Edges: 20, Commit: "abc"}
	m := New(reader, reader).WithStyles(theme.New(theme.KoiPond())).WithProject("clarodrive")
	m = step(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})
	m = step(t, m, m.Refresh()())

	if !strings.Contains(m.View(), "fresh") {
		t.Fatalf("a graph the store did not flag should read as fresh, got:\n%s", m.View())
	}
}

func TestAProjectWithNoGraphSaysHowToBuildOne(t *testing.T) {
	reader := &data.FakeGraph{}
	m := New(reader, reader).WithStyles(theme.New(theme.KoiPond())).WithProject("clarodrive")
	m = step(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})
	m = step(t, m, m.Refresh()())

	if !strings.Contains(m.View(), "Press s to sync") {
		t.Fatalf("an ungraphed project should say what to do, got:\n%s", m.View())
	}
}

func TestSSyncsTheGraphAndShowsTheResult(t *testing.T) {
	m, reader := loaded(t)

	m, cmd := stepCmd(t, m, press("s"))
	if cmd == nil {
		t.Fatal("\"s\" should run the graph sync")
	}
	if !strings.Contains(m.View(), "the graph") {
		t.Errorf("the screen should say what it is doing, got:\n%s", m.View())
	}

	m = step(t, m, cmd())
	if len(reader.SyncCalls()) != 1 || reader.SyncCalls()[0] != "clarodrive" {
		t.Fatalf("sync calls = %v, want one for the active project", reader.SyncCalls())
	}
	if m.State.Nodes != 1290 {
		t.Errorf("nodes = %d, want the state the sync returned", m.State.Nodes)
	}
	if !strings.Contains(m.View(), "graph synced") {
		t.Errorf("a finished sync should say so, got:\n%s", m.View())
	}
}

func TestASyncThatFailedIsReported(t *testing.T) {
	m, _ := loaded(t)

	m = step(t, m, graphSyncedMsg{project: "clarodrive", err: errors.New("graph.json is missing")})

	if !strings.Contains(m.View(), "graph.json is missing") {
		t.Fatalf("a failed sync should say so, got:\n%s", m.View())
	}
}

func TestSWithoutASyncerSaysSo(t *testing.T) {
	reader := fixture()
	m := New(reader, nil).WithStyles(theme.New(theme.KoiPond())).WithProject("clarodrive")
	m = step(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})
	m = step(t, m, m.Refresh()())

	m, cmd := stepCmd(t, m, press("s"))
	m = step(t, m, cmd())

	if !strings.Contains(m.View(), "no graph syncer") {
		t.Fatalf("a workspace with no syncer should say so, got:\n%s", m.View())
	}
}

func TestSOnAnUnscopedTabDoesNothing(t *testing.T) {
	if _, cmd := stepCmd(t, New(nil, nil), press("s")); cmd != nil {
		t.Fatal("there is no project to sync a graph for")
	}
}

func TestRefsArePagedAgainstTheStoresOwnTotal(t *testing.T) {
	m, _ := loaded(t)

	if len(m.Refs) != refPageSize {
		t.Fatalf("the first page holds %d refs, want %d", len(m.Refs), refPageSize)
	}
	if m.HasPrevPage() {
		t.Error("the first page has nothing before it")
	}
	if !m.HasNextPage() {
		t.Fatal("a total past the page should offer the next one")
	}

	m, cmd := stepCmd(t, m, press("n"))
	if cmd == nil {
		t.Fatal("\"n\" should load the next page")
	}
	m = step(t, m, cmd())
	if !m.HasPrevPage() || m.HasNextPage() {
		t.Fatalf("the second page should be the last: prev=%v next=%v", m.HasPrevPage(), m.HasNextPage())
	}

	m, cmd = stepCmd(t, m, press("p"))
	if cmd == nil {
		t.Fatal("\"p\" should load the previous page")
	}
	m = step(t, m, cmd())
	if m.HasPrevPage() {
		t.Error("back at the first page there is nothing before it")
	}
	if _, cmd := stepCmd(t, m, press("p")); cmd != nil {
		t.Error("\"p\" on the first page reloaded it")
	}
}

// TestRefsThatCannotBeReadDoNotBlankTheSummary pins the split the store forces
// on this screen: there is no paged observation_refs query yet, and a reader
// that says so must not take the summary down with it.
func TestRefsThatCannotBeReadDoNotBlankTheSummary(t *testing.T) {
	m, _ := loaded(t)

	m = step(t, m, graphLoadedMsg{
		project: "clarodrive",
		state:   data.GraphState{Nodes: 7, Edges: 9, Commit: "abc"},
		refsErr: errors.New("not implemented"),
	})

	out := m.View()
	if !strings.Contains(out, "7") {
		t.Errorf("the summary was lost with the refs, got:\n%s", out)
	}
	if !strings.Contains(out, "linked observations: not implemented") {
		t.Errorf("the half that failed should say which half it was, got:\n%s", out)
	}
}

func TestAProjectWithNoLinkedObservationsSaysSo(t *testing.T) {
	reader := fixture()
	reader.RefsByProject = nil
	m := New(reader, reader).WithStyles(theme.New(theme.KoiPond())).WithProject("clarodrive")
	m = step(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})
	m = step(t, m, m.Refresh()())

	if !strings.Contains(m.View(), "No observation names a node") {
		t.Fatalf("an empty ref list should say so, got:\n%s", m.View())
	}
}

func TestALoadThatFailedIsReported(t *testing.T) {
	reader := fixture()
	reader.SetErr(errors.New("database is locked"))
	m := New(reader, reader).WithStyles(theme.New(theme.KoiPond())).WithProject("clarodrive")
	m = step(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})
	m = step(t, m, m.Refresh()())

	if !strings.Contains(m.View(), "database is locked") {
		t.Fatalf("a failed read should say so, got:\n%s", m.View())
	}
}

func TestATabWithNoReaderSaysSo(t *testing.T) {
	m := New(nil, nil).WithStyles(theme.New(theme.KoiPond())).WithProject("clarodrive")
	m = step(t, m, m.Refresh()())

	if !strings.Contains(m.View(), "no graph reader") {
		t.Fatalf("a workspace with no reader should say so, got:\n%s", m.View())
	}
}

func TestALoadForAnotherProjectIsDropped(t *testing.T) {
	m, _ := loaded(t)

	m = step(t, m, graphLoadedMsg{project: "somewhere-else", state: data.GraphState{Nodes: 999}})
	if m.State.Nodes == 999 {
		t.Fatal("a load for another project reached the screen")
	}

	m = step(t, m, graphSyncedMsg{project: "somewhere-else", state: data.GraphState{Nodes: 888}})
	if m.State.Nodes == 888 {
		t.Fatal("a sync for another project reached the screen")
	}
}

func TestEveryLoadMessageNamesItsOwner(t *testing.T) {
	for _, msg := range []tabs.Targeted{graphLoadedMsg{}, graphSyncedMsg{}} {
		if got := msg.TabOwner(); got != tabs.Graph {
			t.Errorf("%T.TabOwner() = %v, want Graph", msg, got)
		}
	}
}

func TestAnUnscopedTabPointsAtTheProjectTree(t *testing.T) {
	m := New(nil, nil).WithStyles(theme.New(theme.KoiPond()))

	if !strings.Contains(m.View(), "ctrl+p") {
		t.Fatalf("an unscoped tab should say how to pick a project, got:\n%s", m.View())
	}
}

func TestAPageKeyIsOnlyAdvertisedWhereThereIsAPage(t *testing.T) {
	reader := fixture()
	reader.RefsByProject["clarodrive"] = reader.RefsByProject["clarodrive"][:2]
	m := New(reader, reader).WithStyles(theme.New(theme.KoiPond())).WithProject("clarodrive")
	m = step(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})
	m = step(t, m, m.Refresh()())

	for _, b := range m.Help() {
		if k := b.Help().Key; k == "n" || k == "p" {
			t.Fatalf("a single page should not advertise %q", k)
		}
	}

	full, _ := loaded(t)
	advertised := map[string]bool{}
	for _, b := range full.Help() {
		advertised[b.Help().Key] = true
	}
	if !advertised["n"] {
		t.Error("a list with a next page should advertise the key that reaches it")
	}
}

func TestScopingToAnotherProjectDropsWhatTheLastOneLoaded(t *testing.T) {
	m, _ := loaded(t)
	m = m.WithProject("another")

	if m.State.Nodes != 0 || len(m.Refs) != 0 || m.loaded {
		t.Fatal("the previous project's graph survived the switch")
	}
}

func TestANarrowTerminalDropsTheCommitRatherThanWrapping(t *testing.T) {
	if got := maxCommitCells(200); got != 12 {
		t.Errorf("a wide row shows %d cells of the commit, want 12", got)
	}
	if got := maxCommitCells(40); got != 0 {
		t.Errorf("a narrow row shows %d cells of the commit, want none", got)
	}
}

func TestAnUnknownKeyIsLeftAlone(t *testing.T) {
	m, _ := loaded(t)

	_, cmd := stepCmd(t, m, press("z"))
	if cmd != nil {
		t.Error("an unbound key should produce no command")
	}
}
