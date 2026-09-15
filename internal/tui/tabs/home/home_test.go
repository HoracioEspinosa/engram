package home

import (
	"errors"
	"strings"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/shared"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	tea "github.com/charmbracelet/bubbletea"
)

func strp(s string) *string   { return &s }
func f64p(v float64) *float64 { return &v }

// fixtures builds one project across every reader Home consumes.
func fixtures() (*data.FakeProject, *data.FakeGraph, *data.FakeBenchmark) {
	card := store.ProjectCard{
		Slug: "clarodrive", DisplayName: "Claro Drive", Kind: "umbrella",
		Description: strp("The drive every instance hangs off"),
		Color:       strp("accent"),
	}
	projects := &data.FakeProject{
		CardBySlug: map[string]store.ProjectCard{"clarodrive": card},
		HealthBySlug: map[string]data.ProjectHealth{"clarodrive": {
			ProjectCardCounts: store.ProjectCardCounts{Observations: 120, TasksActive: 4, Evidence: 9, Runbooks: 3},
			Sync:              store.ProjectSyncSummary{Enrolled: true, Lifecycle: "healthy"},
		}},
		TasksBySlug: map[string][]store.TaskListItem{"clarodrive": {
			{Task: store.Task{ID: 1, JiraKey: strp("CDBS-1010"), Title: "Preview 503s", State: "in_progress"}},
		}},
		EvidenceBySlug: map[string][]store.EvidenceListItem{"clarodrive": {
			{Evidence: store.Evidence{ID: 1, Path: "CDBS-1010/cold-start.png", AttachedJira: true}},
		}},
	}

	graph := &data.FakeGraph{
		StateByProject: map[string]data.GraphState{"clarodrive": {
			Project: "clarodrive", Commit: strings.Repeat("c", 40), BuiltAt: "2026-01-14 08:00:00",
			Nodes: 1284, Edges: 3901, Communities: 17,
			Stale: true, StaleReason: "code_changed",
		}},
		SyncResult: map[string]data.GraphState{"clarodrive": {
			Project: "clarodrive", Commit: strings.Repeat("d", 40), Nodes: 1290, Edges: 3999, Communities: 18,
		}},
	}

	bench := &data.FakeBenchmark{ByProject: map[string][]data.Benchmark{"clarodrive": {
		{BenchmarkDelta: store.BenchmarkDelta{
			Benchmark:     store.Benchmark{Name: "cold start", Metric: "p95", Unit: "ms", Value: 812, Direction: store.BenchmarkDirectionLower},
			BaselineValue: f64p(940), DeltaPct: f64p(-13.6),
		}},
		{BenchmarkDelta: store.BenchmarkDelta{
			Benchmark: store.Benchmark{Name: "no baseline yet", Metric: "rps", Unit: "ops", Value: 400, Direction: store.BenchmarkDirectionHigher},
		}},
	}}}

	return projects, graph, bench
}

// loaded returns a tab that has already read everything, at a width wide
// enough for two columns.
func loaded(t *testing.T, width int) Model {
	t.Helper()
	projects, graph, bench := fixtures()
	m := New(projects).
		WithStyles(theme.New(theme.KoiPond())).
		WithGraph(graph, graph).
		WithBenchmarks(bench).
		WithProject("clarodrive")

	m = step(t, m, tea.WindowSizeMsg{Width: width, Height: 40})
	cmd := m.Refresh()
	if cmd == nil {
		t.Fatal("a scoped tab should have something to load")
	}
	return step(t, m, cmd())
}

func step(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	updated, _ := m.Update(msg)
	next, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want home.Model", updated)
	}
	return next
}

func stepCmd(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(msg)
	next, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want home.Model", updated)
	}
	return next, cmd
}

func press(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func TestTheTabIsScopedAndTitled(t *testing.T) {
	m := New(nil).WithProject("clarodrive")

	if m.Title() != "Home" {
		t.Errorf("Title() = %q", m.Title())
	}
	if m.Project() != "clarodrive" {
		t.Errorf("Project() = %q", m.Project())
	}
	if m.CapturingText() {
		t.Error("Home has no text input to capture with")
	}
	if New(nil).Refresh() != nil {
		t.Error("a tab with no project has nothing to reload")
	}
	if New(nil).Init() != nil {
		t.Error("a tab with no project has nothing to load")
	}
}

func TestEveryBlockRendersWhatItRead(t *testing.T) {
	m := loaded(t, 140)

	out := m.View()
	for _, want := range []string{
		"Claro Drive",                        // the card
		"umbrella",                           // its kind
		"The drive every instance hangs off", // its description
		"120",                                // the counters
		"CDBS-1010",                          // recent tasks, by the task's display key
		"cold-start.png",                     // latest evidence
		"1284",                               // the graph's nodes
		"code_changed",                       // the store's own staleness reason
		"p95",                                // a benchmark metric
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Home does not show %q, got:\n%s", want, out)
		}
	}
}

// TestTheStalenessReasonIsPrintedLiterally pins the rule: the TUI renders
// the store's verdict and never forms its own, so a reason this build has
// never heard of still reaches the reader.
func TestTheStalenessReasonIsPrintedLiterally(t *testing.T) {
	projects, graph, bench := fixtures()
	graph.StateByProject["clarodrive"] = data.GraphState{
		Nodes: 1, Stale: true, StaleReason: "a_reason_from_a_newer_store",
	}
	m := New(projects).WithStyles(theme.New(theme.KoiPond())).WithGraph(graph, graph).WithBenchmarks(bench).WithProject("clarodrive")
	m = step(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})
	m = step(t, m, m.Refresh()())

	if !strings.Contains(m.View(), "a_reason_from_a_newer_store") {
		t.Fatalf("the reason was rewritten instead of printed, got:\n%s", m.View())
	}
}

// TestADeltaIsColouredByItsOwnDirection pins the one thing a Δ column cannot
// get wrong: the same -13.6% is an improvement for a latency and a regression
// for a throughput.
func TestADeltaIsColouredByItsOwnDirection(t *testing.T) {
	lower := data.Benchmark{BenchmarkDelta: store.BenchmarkDelta{
		Benchmark: store.Benchmark{Direction: store.BenchmarkDirectionLower}, DeltaPct: f64p(-13.6),
	}}
	higher := data.Benchmark{BenchmarkDelta: store.BenchmarkDelta{
		Benchmark: store.Benchmark{Direction: store.BenchmarkDirectionHigher}, DeltaPct: f64p(-13.6),
	}}

	if improved := lower.Improved(); improved == nil || !*improved {
		t.Error("a latency that fell is an improvement")
	}
	if improved := higher.Improved(); improved == nil || *improved {
		t.Error("a throughput that fell is a regression")
	}

	m := loaded(t, 140)
	if !strings.Contains(m.View(), "13.6") {
		t.Errorf("the delta is missing from the benchmarks block, got:\n%s", m.View())
	}
}

func TestABlockWithNothingInItSaysSo(t *testing.T) {
	projects := &data.FakeProject{CardBySlug: map[string]store.ProjectCard{"empty": {Slug: "empty"}}}
	m := New(projects).WithStyles(theme.New(theme.KoiPond())).WithProject("empty")
	m = step(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})
	m = step(t, m, m.Refresh()())

	out := m.View()
	for _, want := range []string{
		"No open tasks.",
		"No evidence captured yet.",
		"No graph has been built for this project.",
		"No benchmarks recorded.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("an empty block should explain itself; %q is missing from:\n%s", want, out)
		}
	}
}

func TestATabWithNoProjectPointsAtTheProjectTree(t *testing.T) {
	m := New(nil).WithStyles(theme.New(theme.KoiPond()))

	if !strings.Contains(m.View(), "ctrl+p") {
		t.Fatalf("an unscoped Home should say how to pick a project, got:\n%s", m.View())
	}
}

func TestALoadThatFailedIsReported(t *testing.T) {
	projects := &data.FakeProject{Err: errors.New("database is locked")}
	m := New(projects).WithStyles(theme.New(theme.KoiPond())).WithProject("clarodrive")
	m = step(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})
	m = step(t, m, m.Refresh()())

	if !strings.Contains(m.View(), "database is locked") {
		t.Fatalf("a failed read should say so, got:\n%s", m.View())
	}
}

func TestATabWithNoReaderSaysSoRatherThanPanicking(t *testing.T) {
	m := New(nil).WithStyles(theme.New(theme.KoiPond())).WithProject("clarodrive")
	m = step(t, m, m.Refresh()())

	if !strings.Contains(m.View(), "no project reader") {
		t.Fatalf("a workspace with no reader should say so, got:\n%s", m.View())
	}
}

// TestALoadForAProjectNobodyIsLookingAtIsDropped pins the anti-race guard
// every load in this workspace carries: a slow read for the project that used
// to be active must never clobber the one on screen.
func TestALoadForAProjectNobodyIsLookingAtIsDropped(t *testing.T) {
	m := loaded(t, 140)

	stale := loadedMsg{slug: "somebody-else", card: store.ProjectCard{Slug: "somebody-else", DisplayName: "Someone Else"}}
	m = step(t, m, stale)

	if strings.Contains(m.View(), "Someone Else") {
		t.Fatal("a load for another project reached the screen")
	}
}

func TestEveryLoadMessageNamesItsOwner(t *testing.T) {
	if got := (loadedMsg{}).TabOwner(); got != tabs.Home {
		t.Errorf("loadedMsg.TabOwner() = %v, want Home", got)
	}
	if got := (syncedMsg{}).TabOwner(); got != tabs.Home {
		t.Errorf("syncedMsg.TabOwner() = %v, want Home", got)
	}
}

func TestSSyncsTheGraphAndShowsTheResult(t *testing.T) {
	m := loaded(t, 140)

	m, cmd := stepCmd(t, m, press("s"))
	if cmd == nil {
		t.Fatal("\"s\" should run the graph sync")
	}
	if !strings.Contains(m.View(), "syncing") && !strings.Contains(m.View(), "the graph") {
		t.Errorf("the graph block should say what it is doing, got:\n%s", m.View())
	}

	m = step(t, m, cmd())
	if m.GraphState().Nodes != 1290 {
		t.Errorf("graph nodes = %d, want the state the sync returned", m.GraphState().Nodes)
	}
	if !strings.Contains(m.View(), "graph synced") {
		t.Errorf("a finished sync should say so, got:\n%s", m.View())
	}
}

func TestASyncThatFailedIsReported(t *testing.T) {
	m := loaded(t, 140)

	m = step(t, m, syncedMsg{slug: "clarodrive", err: errors.New("graph.json is missing")})

	if !strings.Contains(m.View(), "graph.json is missing") {
		t.Fatalf("a failed sync should say so, got:\n%s", m.View())
	}
}

func TestSWithoutASyncerSaysSo(t *testing.T) {
	projects, _, _ := fixtures()
	m := New(projects).WithStyles(theme.New(theme.KoiPond())).WithProject("clarodrive")
	m = step(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})
	m = step(t, m, m.Refresh()())

	m, cmd := stepCmd(t, m, press("s"))
	if cmd == nil {
		t.Fatal("\"s\" should still answer")
	}
	m = step(t, m, cmd())
	if !strings.Contains(m.View(), "no graph syncer") {
		t.Fatalf("a workspace with no syncer should say so, got:\n%s", m.View())
	}
}

func TestSOnAnUnscopedTabDoesNothing(t *testing.T) {
	m := New(nil)
	_, cmd := stepCmd(t, m, press("s"))
	if cmd != nil {
		t.Fatal("there is no project to sync a graph for")
	}
}

func TestHLMoveBetweenColumnsAndJKInsideOne(t *testing.T) {
	m := loaded(t, 140)

	if got := m.selected(); got != blockTasks {
		t.Fatalf("the cursor opens on %v, want the tasks block", got)
	}

	m = step(t, m, press("j"))
	if got := m.selected(); got != blockEvidence {
		t.Fatalf("j moved to %v, want the evidence block", got)
	}

	m = step(t, m, press("l"))
	if got := m.selected(); got != blockGraph {
		t.Fatalf("l moved to %v, want the graph block, the first of the right column", got)
	}

	m = step(t, m, press("G"))
	if got := m.selected(); got != blockBenchmarks {
		t.Fatalf("G moved to %v, want the last block of the focused column", got)
	}

	m = step(t, m, press("h"))
	if got := m.selected(); got != blockEvidence {
		t.Fatalf("h returned to %v, want the place the left column was left on", got)
	}

	m = step(t, m, press("g"))
	if got := m.selected(); got != blockTasks {
		t.Fatalf("g moved to %v, want the first block", got)
	}
	m = step(t, m, press("k"))
	if got := m.selected(); got != blockTasks {
		t.Fatalf("k past the top moved to %v, want it clamped", got)
	}
}

// TestBelowTheSplitEveryBlockIsOneList pins what a narrow terminal does: the
// two columns become one, so the same cursor has to reach all four blocks and
// "l" has nowhere to go.
func TestBelowTheSplitEveryBlockIsOneList(t *testing.T) {
	m := loaded(t, 90)

	m = step(t, m, press("l"))
	if m.focus != shared.PaneMaster {
		t.Fatal("there is no second column to focus at this width")
	}

	m = step(t, m, press("G"))
	if got := m.selected(); got != blockBenchmarks {
		t.Fatalf("G reached %v, want the last of all four blocks", got)
	}
}

// TestANarrowerTerminalClampsTheCursor pins the resize case: a cursor sitting
// on the fourth block of a single column has to survive the split into two
// columns of two.
func TestANarrowerTerminalClampsTheCursor(t *testing.T) {
	m := loaded(t, 90)
	m = step(t, m, press("G"))

	m = step(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})

	if m.cursor[0] >= len(columns[0]) {
		t.Fatalf("cursor = %d, want it clamped inside a column of %d", m.cursor[0], len(columns[0]))
	}
}

func TestEnterOpensTheBlockInItsOwnTab(t *testing.T) {
	cases := []struct {
		keys []string
		want tabs.ID
	}{
		{nil, tabs.Tasks},
		{[]string{"j"}, tabs.Evidence},
		{[]string{"l"}, tabs.Graph},
		{[]string{"l", "j"}, tabs.Benchmarks},
	}

	for _, tc := range cases {
		t.Run(tc.want.String(), func(t *testing.T) {
			m := loaded(t, 140)
			for _, k := range tc.keys {
				m = step(t, m, press(k))
			}

			_, cmd := stepCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
			if cmd == nil {
				t.Fatal("enter should navigate")
			}
			msg, ok := cmd().(tabs.NavigateMsg)
			if !ok {
				t.Fatalf("enter produced %T, want tabs.NavigateMsg", cmd())
			}
			if msg.Target != tc.want {
				t.Fatalf("enter navigated to %v, want %v", msg.Target, tc.want)
			}
		})
	}
}

// TestHelpOnlyAdvertisesColumnKeysWhenThereAreColumns keeps the hints honest:
// a key that moves the focus somewhere invisible must not be advertised.
func TestHelpOnlyAdvertisesColumnKeysWhenThereAreColumns(t *testing.T) {
	wide := loaded(t, 140)
	narrow := loaded(t, 90)

	has := func(m Model, k string) bool {
		for _, b := range m.Help() {
			if b.Help().Key == k {
				return true
			}
		}
		return false
	}

	if !has(wide, "h/l") {
		t.Error("two columns should advertise the key that moves between them")
	}
	if has(narrow, "h/l") {
		t.Error("one column should not advertise a key that does nothing")
	}
}

func TestTheBreadcrumbIsTheChainTheRootHandedDown(t *testing.T) {
	m := loaded(t, 140).WithBreadcrumb([]data.ProjectNode{
		{Slug: "nextcloud", DisplayName: "Nextcloud"},
		{Slug: "unnamed"},
	})

	out := m.View()
	for _, want := range []string{"Nextcloud", "unnamed", "clarodrive"} {
		if !strings.Contains(out, want) {
			t.Errorf("the breadcrumb is missing %q, got:\n%s", want, out)
		}
	}
}

// TestAProjectColourFollowsThePaletteOrThePinnedHex pins the rule for
// project_cards.color: a role token tracks the active theme, an #rrggbb
// triple is honoured as the user pinned it.
func TestAProjectColourFollowsThePaletteOrThePinnedHex(t *testing.T) {
	palette := theme.KoiPond()

	colour, ok := theme.ResolveColor(palette, "accent")
	if !ok || colour != palette.Accent {
		t.Errorf("a role token resolved to %q, want the palette's accent", colour)
	}
	if _, ok := theme.ResolveColor(palette, "#ff8a3d"); !ok {
		t.Error("a pinned hex should be honoured as given")
	}
	if _, ok := theme.ResolveColor(palette, "chartreuse"); ok {
		t.Error("a colour that is neither a role nor a hex should be refused")
	}
}

func TestScopingToAnotherProjectDropsWhatTheLastOneLoaded(t *testing.T) {
	m := loaded(t, 140).WithProject("another")

	if m.loaded {
		t.Error("the new project has not been read yet")
	}
	out := m.View()
	if strings.Contains(out, "Claro Drive") || strings.Contains(out, "CDBS-1010") {
		t.Fatalf("the previous project's data is still on screen:\n%s", out)
	}
}

func TestAnUnknownKeyIsLeftAlone(t *testing.T) {
	m := loaded(t, 140)
	before := m.selected()

	m, cmd := stepCmd(t, m, press("z"))
	if cmd != nil {
		t.Error("an unbound key should produce no command")
	}
	if m.selected() != before {
		t.Error("an unbound key moved the cursor")
	}
}
