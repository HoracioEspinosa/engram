package benchmarks

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

func f64p(v float64) *float64 { return &v }

// fixture is one project's measurements: a latency that improved, a
// throughput that fell, and a metric with no baseline to compare against.
func fixture() *data.FakeBenchmark {
	return &data.FakeBenchmark{ByProject: map[string][]data.Benchmark{"clarodrive": {
		{BenchmarkDelta: store.BenchmarkDelta{
			Benchmark: store.Benchmark{
				Name: "cold start", Metric: "p95", Unit: "ms", Value: 812,
				Direction: store.BenchmarkDirectionLower, CapturedAt: "2026-01-15 09:30:00",
			},
			BaselineValue: f64p(940), DeltaPct: f64p(-13.6),
		}},
		{BenchmarkDelta: store.BenchmarkDelta{
			Benchmark: store.Benchmark{
				Name: "throughput", Metric: "rps", Unit: "ops", Value: 412,
				Direction: store.BenchmarkDirectionHigher, CapturedAt: "2026-01-15 09:31:00",
			},
			BaselineValue: f64p(500), DeltaPct: f64p(-17.6),
		}},
		{BenchmarkDelta: store.BenchmarkDelta{
			Benchmark: store.Benchmark{
				Name: "first paint", Metric: "fcp", Unit: "ms", Value: 210,
				Direction: store.BenchmarkDirectionLower, CapturedAt: "2026-01-15 09:32:00",
			},
		}},
	}}}
}

func loaded(t *testing.T, width int) (Model, *data.FakeBenchmark) {
	t.Helper()
	reader := fixture()
	m := New(reader).WithStyles(theme.New(theme.KoiPond())).WithProject("clarodrive")
	m = step(t, m, tea.WindowSizeMsg{Width: width, Height: 40})
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
		t.Fatalf("Update returned %T, want benchmarks.Model", updated)
	}
	return next
}

func stepCmd(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(msg)
	next, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want benchmarks.Model", updated)
	}
	return next, cmd
}

func press(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func TestTheTabIsScopedAndTitled(t *testing.T) {
	m := New(nil).WithProject("clarodrive")

	if m.Title() != "Benchmarks" {
		t.Errorf("Title() = %q", m.Title())
	}
	if m.Project() != "clarodrive" {
		t.Errorf("Project() = %q", m.Project())
	}
	if m.CapturingText() {
		t.Error("nothing is focused yet")
	}
	if New(nil).Refresh() != nil || New(nil).Init() != nil {
		t.Error("a tab with no project has nothing to load")
	}
}

func TestTheTableShowsFiveColumnsWideAndThreeNarrow(t *testing.T) {
	wide, _ := loaded(t, 140)
	if got := len(wide.columns()); got != 5 {
		t.Fatalf("a wide table has %d columns, want 5", got)
	}

	narrow, _ := loaded(t, 90)
	cols := narrow.columns()
	if len(cols) != 3 {
		t.Fatalf("a narrow table has %d columns, want metric · latest · Δ", len(cols))
	}
	titles := []string{cols[0].Title, cols[1].Title, cols[2].Title}
	if strings.Join(titles, ",") != "metric,latest,Δ" {
		t.Fatalf("narrow columns = %v", titles)
	}
	if got := len(narrow.rows()[0]); got != 3 {
		t.Fatalf("a narrow row carries %d cells, want 3", got)
	}
}

func TestEveryMeasurementIsOnScreen(t *testing.T) {
	m, _ := loaded(t, 140)

	out := m.View()
	for _, want := range []string{"p95", "rps", "fcp", "812", "940", "13.6"} {
		if !strings.Contains(out, want) {
			t.Errorf("the table omits %q, got:\n%s", want, out)
		}
	}
}

// TestADeltaIsReadForItsOwnDirection pins the one thing this table cannot get
// wrong: the same fall is an improvement for a latency and a regression for a
// throughput, and the arrow says which way the number moved either way.
func TestADeltaIsReadForItsOwnDirection(t *testing.T) {
	m, _ := loaded(t, 140)

	lower := m.delta(m.Items[0])
	higher := m.delta(m.Items[1])
	none := m.delta(m.Items[2])

	if lower == higher {
		t.Fatal("a latency and a throughput that both fell must not read the same")
	}
	for _, rendered := range []string{lower, higher} {
		if !strings.Contains(rendered, "%") {
			t.Errorf("a delta with a baseline should carry its percentage: %q", rendered)
		}
	}
	if strings.Contains(none, "%") {
		t.Errorf("a metric with no baseline has no percentage to show: %q", none)
	}
}

func TestAMetricWithNoBaselineSaysSoRatherThanShowingZero(t *testing.T) {
	m, _ := loaded(t, 140)

	if got := m.baseline(m.Items[2]); strings.Contains(got, "0") {
		t.Fatalf("baseline = %q, want a neutral marker rather than a number", got)
	}
	if got := m.baseline(m.Items[0]); got != "940" {
		t.Fatalf("baseline = %q, want the metric's own", got)
	}
}

func TestTFiltersByTaskAndMReloadsFromTheFirstPage(t *testing.T) {
	m, reader := loaded(t, 140)
	m.Filter.Offset = 50

	m, _ = stepCmd(t, m, press("t"))
	if !m.CapturingText() {
		t.Fatal("\"t\" should give the prompt the keyboard")
	}
	m, _ = stepCmd(t, m, press("task-1"))
	m, cmd := stepCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("applying a filter should reload")
	}
	step(t, m, cmd())

	got := reader.LastFilter()
	if got.Task != "task-1" {
		t.Errorf("filter task = %q, want task-1", got.Task)
	}
	if got.Offset != 0 {
		t.Errorf("offset = %d, want the filter to start at the first page", got.Offset)
	}
	if m.CapturingText() {
		t.Error("applying a filter should give the keyboard back")
	}
}

func TestMFiltersByMetric(t *testing.T) {
	m, reader := loaded(t, 140)

	m, _ = stepCmd(t, m, press("m"))
	m, _ = stepCmd(t, m, press("p95"))
	m, cmd := stepCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	step(t, m, cmd())

	if got := reader.LastFilter(); got.Metric != "p95" {
		t.Fatalf("filter metric = %q, want p95", got.Metric)
	}
}

func TestEscAbandonsTheFilterPrompt(t *testing.T) {
	m, _ := loaded(t, 140)

	m, _ = stepCmd(t, m, press("m"))
	m, _ = stepCmd(t, m, press("p95"))
	m, cmd := stepCmd(t, m, tea.KeyMsg{Type: tea.KeyEsc})

	if cmd != nil {
		t.Error("abandoning a filter should not reload")
	}
	if m.CapturingText() || m.Filter.Metric != "" {
		t.Error("esc should drop the prompt and leave the filter alone")
	}
}

func TestEnterOpensTheMetricsOwnHistory(t *testing.T) {
	m, _ := loaded(t, 140)

	m, cmd := stepCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter should load the metric's series")
	}
	if m.Screen != ScreenHistory || m.Metric != "p95" {
		t.Fatalf("screen = %v metric = %q, want the history of p95", m.Screen, m.Metric)
	}

	m = step(t, m, cmd())
	out := m.View()
	if !strings.Contains(out, "p95 over time") || !strings.Contains(out, "812") {
		t.Fatalf("the history should show the metric's readings, got:\n%s", out)
	}

	m = step(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.Screen != ScreenTable || m.Metric != "" {
		t.Fatal("esc should come back to the table")
	}
}

func TestEnterWithNoRowUnderTheCursorDoesNothing(t *testing.T) {
	m := New(fixture()).WithStyles(theme.New(theme.KoiPond())).WithProject("empty")
	m = step(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})
	m = step(t, m, m.Refresh()())

	m, cmd := stepCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || m.Screen != ScreenTable {
		t.Fatal("there is no metric to open a history for")
	}
	if !strings.Contains(m.View(), "No benchmarks match this filter.") {
		t.Fatalf("an empty table should say so, got:\n%s", m.View())
	}
}

func TestBOpensTheProjectsBaselineDocument(t *testing.T) {
	t.Setenv(shared.VaultRootEnv, "/fixtures/vault")
	var opened string
	original := openFile
	openFile = func(path string) error {
		opened = path
		return nil
	}
	defer func() { openFile = original }()

	m, _ := loaded(t, 140)
	_, cmd := stepCmd(t, m, press("b"))
	if cmd == nil {
		t.Fatal("\"b\" should open the baseline document")
	}
	cmd()

	if !strings.HasSuffix(opened, "/clarodrive/BASELINE.md") {
		t.Fatalf("opened %q, want the project's own BASELINE.md", opened)
	}
}

func TestBWithoutAVaultRootSaysSo(t *testing.T) {
	t.Setenv(shared.VaultRootEnv, "")

	m, _ := loaded(t, 140)
	m, cmd := stepCmd(t, m, press("b"))
	m = step(t, m, cmd())

	if !strings.Contains(m.View(), "no vault root") {
		t.Fatalf("a workspace with no vault should say so, got:\n%s", m.View())
	}
}

func TestAViewerThatWillNotStartShowsThePath(t *testing.T) {
	t.Setenv(shared.VaultRootEnv, "/fixtures/vault")
	original := openFile
	openFile = func(string) error { return errors.New("exec: \"xdg-open\": not found") }
	defer func() { openFile = original }()

	m, _ := loaded(t, 140)
	m, cmd := stepCmd(t, m, press("b"))
	m = step(t, m, cmd())

	out := m.View()
	if !strings.Contains(out, "BASELINE.md") || !strings.Contains(out, "not found") {
		t.Fatalf("a failed open should show the path and the reason, got:\n%s", out)
	}
}

func TestTheOpenerIsTheOnesTheOSRegisters(t *testing.T) {
	if got := openerFor("darwin"); got != "open" {
		t.Errorf("openerFor(darwin) = %q", got)
	}
	if got := openerFor("linux"); got != "xdg-open" {
		t.Errorf("openerFor(linux) = %q", got)
	}
}

func TestPagingReadsTheStoresOwnTotal(t *testing.T) {
	reader := &data.FakeBenchmark{ByProject: map[string][]data.Benchmark{"clarodrive": {}}}
	for i := 0; i < pageSize+10; i++ {
		reader.ByProject["clarodrive"] = append(reader.ByProject["clarodrive"], data.Benchmark{
			BenchmarkDelta: store.BenchmarkDelta{Benchmark: store.Benchmark{Metric: "m", Value: float64(i)}},
		})
	}

	m := New(reader).WithStyles(theme.New(theme.KoiPond())).WithProject("clarodrive")
	m = step(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})
	m = step(t, m, m.Refresh()())

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
	if !m.HasPrevPage() {
		t.Error("the second page has one before it")
	}

	m, cmd = stepCmd(t, m, press("p"))
	if cmd == nil {
		t.Fatal("\"p\" should load the previous page")
	}
	m = step(t, m, cmd())
	if m.HasPrevPage() {
		t.Error("back at the first page there is nothing before it")
	}

	// Past either end the key is inert rather than loading the same page.
	if _, cmd := stepCmd(t, m, press("p")); cmd != nil {
		t.Error("\"p\" on the first page reloaded it")
	}
}

func TestAPageKeyIsOnlyAdvertisedWhereThereIsAPage(t *testing.T) {
	m, _ := loaded(t, 140)

	for _, b := range m.Help() {
		if b.Help().Key == "n" || b.Help().Key == "p" {
			t.Fatalf("a single page should not advertise %q", b.Help().Key)
		}
	}
}

func TestTheHelpFollowsWhicheverScreenIsShowing(t *testing.T) {
	m, _ := loaded(t, 140)

	table := m.Help()
	if len(table) == 0 {
		t.Fatal("the table declares no bindings")
	}

	m, _ = stepCmd(t, m, press("t"))
	prompt := m.Help()
	if len(prompt) != 2 {
		t.Fatalf("the prompt declares %d bindings, want enter and esc", len(prompt))
	}

	m, _ = stepCmd(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	m, cmd := stepCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	step(t, m, cmd())
	if got := m.Help(); len(got) != 1 {
		t.Fatalf("the history declares %d bindings, want only the way back", len(got))
	}
}

func TestALoadThatFailedIsReported(t *testing.T) {
	reader := fixture()
	reader.SetErr(errors.New("database is locked"))
	m := New(reader).WithStyles(theme.New(theme.KoiPond())).WithProject("clarodrive")
	m = step(t, m, tea.WindowSizeMsg{Width: 140, Height: 40})
	m = step(t, m, m.Refresh()())

	if !strings.Contains(m.View(), "database is locked") {
		t.Fatalf("a failed read should say so, got:\n%s", m.View())
	}
}

func TestATabWithNoReaderSaysSo(t *testing.T) {
	m := New(nil).WithStyles(theme.New(theme.KoiPond())).WithProject("clarodrive")
	m = step(t, m, m.Refresh()())

	if !strings.Contains(m.View(), "no benchmark reader") {
		t.Fatalf("a workspace with no reader should say so, got:\n%s", m.View())
	}
}

func TestALoadForAnotherProjectIsDropped(t *testing.T) {
	m, _ := loaded(t, 140)

	m = step(t, m, benchmarksLoadedMsg{project: "somewhere-else", page: data.Page[data.Benchmark]{Total: 999}})

	if m.Total == 999 {
		t.Fatal("a load for another project reached the screen")
	}
}

func TestEveryLoadMessageNamesItsOwner(t *testing.T) {
	for _, msg := range []tabs.Targeted{benchmarksLoadedMsg{}, historyLoadedMsg{}, baselineOpenedMsg{}} {
		if got := msg.TabOwner(); got != tabs.Benchmarks {
			t.Errorf("%T.TabOwner() = %v, want Benchmarks", msg, got)
		}
	}
}

func TestAnUnscopedTabPointsAtTheProjectTree(t *testing.T) {
	m := New(nil).WithStyles(theme.New(theme.KoiPond()))

	if !strings.Contains(m.View(), "ctrl+p") {
		t.Fatalf("an unscoped tab should say how to pick a project, got:\n%s", m.View())
	}
}

func TestTheHeadingNamesTheFiltersInForce(t *testing.T) {
	m, _ := loaded(t, 140)
	m.Filter.Task = "task-1"
	m.Filter.Metric = "p95"

	if got := m.heading(); !strings.Contains(got, "task: task-1") || !strings.Contains(got, "metric: p95") {
		t.Fatalf("heading = %q, want it to say why the table is short", got)
	}
}

func TestAHistoryWithNoReadingsSaysSo(t *testing.T) {
	m, _ := loaded(t, 140)
	m.Screen = ScreenHistory
	m.Metric = "nothing"

	if !strings.Contains(m.View(), "No readings recorded") {
		t.Fatalf("an empty history should say so, got:\n%s", m.View())
	}
}

func TestScopingToAnotherProjectDropsWhatTheLastOneLoaded(t *testing.T) {
	m, _ := loaded(t, 140)
	m = m.WithProject("another")

	if len(m.Items) != 0 || m.Total != 0 || m.loaded {
		t.Fatal("the previous project's measurements survived the switch")
	}
}
