// Package e2e is T-10.07's own golden suite: teatest drives the TUI
// workspace through github.com/charmbracelet/bubbletea's real Program (a
// real event loop, real tea.Cmd goroutines, real tea.KeyMsg values), one
// screen per rfc-tui.md §5's S1-S11, at the same two geometries
// internal/tui/app/golden_test.go already freezes S10/S11 at.
//
// It lives in its own package, a sibling of app/tabs/theme/data/shared
// rather than inside internal/tui/app, for one purely mechanical reason:
// github.com/charmbracelet/x/exp/teatest imports
// github.com/charmbracelet/x/exp/golden, which registers its own
// flag.Bool("update", ...) at package init. internal/tui/app/golden_test.go
// already registers a flag under that same name for its own (teatest-free)
// mechanism; both live in the same test binary only when their _test.go
// files share one directory, so importing teatest from inside internal/tui/app
// panics at test-binary startup with "flag redefined: update" — confirmed
// by running it. A distinct package is a real fix, not a workaround: each
// Go package still compiles into its own test binary with its own
// flag.CommandLine, so the two golden mechanisms' -update flags never
// collide, and each remains driveable with its own `go test ./internal/tui/... -update`.
//
// See this task's report for the fuller "coexist vs replace" reasoning:
// golden_test.go's direct field-assignment scenes stay the only mechanism
// for the Memory tab's many fine-grained sub-states (loading, empty,
// scrolled, error, ...) that would be expensive to reach one real key press
// at a time; this suite adds the complementary guarantee that the real
// wiring — Update, tea.Cmd, tea.KeyMsg routing — actually reaches each
// screen, which direct field assignment cannot exercise at all.
package e2e

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/HoracioEspinosa/engram/internal/project"
	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/app"
	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/shared"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"
	"github.com/HoracioEspinosa/engram/internal/version"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
)

// updateGolden rewrites testdata/*.golden instead of comparing against them:
// go test ./internal/tui/e2e/... -run TestTeatestGoldenScreens -e2e-update
//
// Named "-e2e-update", not the "-update" app/golden_test.go's own mechanism
// answers to: github.com/charmbracelet/x/exp/teatest imports
// github.com/charmbracelet/x/exp/golden, which registers its own
// flag.Bool("update", ...) at package init purely by being imported (this
// package never calls anything in it). A second flag.Bool("update", ...)
// declared here would collide with that one in this same test binary —
// confirmed by running it — so this flag needs its own name regardless of
// which directory the file lives in.
var updateGolden = flag.Bool("e2e-update", false, "update this package's .golden files")

// e2eVersion is the version string every scene's Model reports; frozen so a
// real release bump never perturbs a golden file that carries it in its
// frame.
const e2eVersion = "1.20.1-golden"

// goldenSize is one terminal geometry rfc-tui.md §10.1 requires golden
// coverage at.
type goldenSize struct {
	name          string
	width, height int
}

var goldenSizes = []goldenSize{
	{name: "120x40", width: 120, height: 40},
	{name: "80x24", width: 80, height: 24},
}

// teatestStep is one key press in a scene, and the text that step's own
// screen must show once the (possibly asynchronous) command it triggers has
// resolved. The substring must be exclusive to the state reached AFTER the
// key, never something the previous screen already displayed — otherwise a
// wait could report readiness from a stale frame still sitting in the
// output buffer.
type teatestStep struct {
	key     tea.KeyMsg
	waitFor string
}

// teatestScreen describes how to drive the real program from a fresh
// app.New(...) to one of rfc-tui.md's S1-S11.
type teatestScreen struct {
	name           string
	initialProject string
	// initialWaitFor is required once Init's own command(s) resolve, before
	// any key is sent. Empty means the screen needs no such wait (its first
	// step's key is a global binding that does not depend on Init having
	// finished).
	initialWaitFor string
	steps          []teatestStep
}

func keyRune(r string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(r)}
}

var teatestEnterKey = tea.KeyMsg{Type: tea.KeyEnter}

// teatestScreens lists all eleven rfc-tui.md screens in RFC order. Marker
// strings come from teatestFixtures below; each is picked to appear in
// exactly one screen's state, so a wait can never be satisfied by a frame
// left over from the screen before it.
func teatestScreens() []teatestScreen {
	return []teatestScreen{
		{
			name: "project-tree",
			// No project: app.New opens straight on the project tree
			// (rfc-tui.md §9.1: "sin proyecto resoluble se abre S1"), and
			// Init loads the forest without any key needed — no ctrl+p step
			// to reach it, unlike every other screen here.
			initialWaitFor: "Acme Corp",
		},
		{
			name:           "project-tree-filtered",
			initialWaitFor: "Acme Corp",
			steps: []teatestStep{
				{key: keyRune("/"), waitFor: "Filter by slug"},
				// "thunder" is one of acme-api's aliases and appears nowhere
				// else on screen: it proves the filter reaches past the two
				// visible labels, and it is the one string that can only be
				// there once the term has been typed — so the wait can never
				// be satisfied by the unfiltered frame before it. The hit is a
				// child, so its parent is dragged along de-emphasised.
				{key: keyRune("thunder"), waitFor: "thunder"},
			},
		},
		{
			name:           "home",
			initialProject: "acme",
			initialWaitFor: "Fix the preview timeout",
		},
		{
			name:           "s3-tasks-list",
			initialProject: "acme",
			steps: []teatestStep{
				{key: keyRune("2"), waitFor: "ACME-1"},
			},
		},
		{
			name:           "s4-tasks-detail",
			initialProject: "acme",
			steps: []teatestStep{
				{key: keyRune("2"), waitFor: "ACME-1"},
				{key: teatestEnterKey, waitFor: "Root cause: preview"},
			},
		},
		{
			name:           "s5-context-pack",
			initialProject: "acme",
			steps: []teatestStep{
				{key: keyRune("2"), waitFor: "ACME-1"},
				{key: teatestEnterKey, waitFor: "Root cause: preview"},
				{key: keyRune("x"), waitFor: "# Context pack: acme / ACME-1"},
			},
		},
		{
			name:           "s6-evidence-list",
			initialProject: "acme",
			steps: []teatestStep{
				{key: keyRune("3"), waitFor: "cold-start.png"},
			},
		},
		{
			name:           "s7-evidence-detail",
			initialProject: "acme",
			steps: []teatestStep{
				{key: keyRune("3"), waitFor: "cold-start.png"},
				// The sha256 label, not the 64-char value: at 80 columns the
				// box wraps the full hash across the screen edge, so the
				// contiguous 64-character string this fixture uses never
				// appears intact in the captured frame (confirmed by running
				// this at 80x24 with the full hash as the wait target, which
				// timed out even though the field was genuinely on screen).
				// The label itself sits at the start of the line, before any
				// wrapping, and appears only once the detail loads.
				{key: teatestEnterKey, waitFor: "sha256"},
			},
		},
		{
			name:           "benchmarks-table",
			initialProject: "acme",
			steps: []teatestStep{
				// The metric column is the one thing both the wide and the narrow
				// layouts draw.
				{key: keyRune("4"), waitFor: "p95"},
			},
		},
		{
			name:           "graph-stale",
			initialProject: "acme",
			steps: []teatestStep{
				// The staleness reason is the store's own string, and it
				// appears nowhere else in the workspace.
				{key: keyRune("6"), waitFor: "code_changed"},
			},
		},
		{
			name:           "s8-runbooks-index",
			initialProject: "acme",
			steps: []teatestStep{
				{key: keyRune("5"), waitFor: "RB-900"},
			},
		},
		{
			name:           "s9-runbooks-markdown",
			initialProject: "acme",
			steps: []teatestStep{
				{key: keyRune("5"), waitFor: "RB-900"},
				{key: teatestEnterKey, waitFor: "Restart the preview worker pool"},
			},
		},
		{
			name:           "s10-memory",
			initialProject: "acme",
			steps: []teatestStep{
				{key: keyRune("1"), waitFor: "348"},
			},
		},
		{
			name:           "palette-empty",
			initialProject: "acme",
			steps: []teatestStep{
				{key: tea.KeyMsg{Type: tea.KeyCtrlK}, waitFor: "Type at least"},
			},
		},
		{
			name:           "palette-results",
			initialProject: "acme",
			steps: []teatestStep{
				{key: tea.KeyMsg{Type: tea.KeyCtrlK}, waitFor: "Type at least"},
				// The store answers with one hit of every kind, so the last
				// group's heading only appears once the search has resolved —
				// which is what makes it a safe wait target across the
				// hundred-millisecond debounce.
				{key: keyRune("preview"), waitFor: "runbooks"},
			},
		},
		{
			name:           "palette-prefix",
			initialProject: "acme",
			steps: []teatestStep{
				{key: tea.KeyMsg{Type: tea.KeyCtrlK}, waitFor: "Type at least"},
				{key: keyRune("t:preview"), waitFor: "[tasks]"},
			},
		},
		{
			name:           "settings",
			initialProject: "acme",
			steps: []teatestStep{
				// The vault root's own presence marker: it appears only once
				// the settings list is drawn.
				{key: keyRune("7"), waitFor: "evidence dir"},
			},
		},
		{
			name:           "theme-picker",
			initialProject: "acme",
			steps: []teatestStep{
				{key: tea.KeyMsg{Type: tea.KeyCtrlT}, waitFor: "koi-day"},
			},
		},
		{
			name:           "theme-picker-invalid",
			initialProject: "acme",
			steps: []teatestStep{
				// A palette the store could not make sense of is listed, not
				// hidden: the reason it cannot be used is the wait target,
				// and it appears nowhere else.
				{key: tea.KeyMsg{Type: tea.KeyCtrlT}, waitFor: "palette has no color roles"},
			},
		},
	}
}

// teatestFixtures builds one deterministic "acme" project across every
// reader the workspace consumes — the same data.Fake* seam every tabs/*
// Update test already uses, just wired through the real root instead of a
// leaf tab.
func teatestFixtures(t *testing.T) (mem *data.FakeMemory, projects *data.FakeProject, task *data.FakeTask, ev *data.FakeEvidence, rb *data.FakeRunbook, tree *data.FakeProjectTree, graph *data.FakeGraph, bench *data.FakeBenchmark, search *data.FakeSearch, themes *data.FakeTheme) {
	t.Helper()
	seedVaultFixture(t)
	// Evidence's detail screen (S7) renders an absolute filesystem path
	// (shared.EvidenceRoot() joined with the row's own relative path). Left
	// at its default ${CD_EVIDENCE_DIR:-~/.clarodrive/evidence}, that path
	// carries the machine's real $HOME into the golden file — a source of
	// non-determinism this task's brief specifically asked to hunt for
	// (widths, map order, relative dates), found by generating the golden
	// once, noticing the running user's home directory baked into it, and
	// pinning the variable here instead.
	t.Setenv(shared.EvidenceDirEnv, "/fixtures/evidence")

	jiraKey := "ACME-1"
	acmeTask := store.Task{
		ID: 1, SyncID: "task-1", Project: "acme", JiraKey: &jiraKey,
		Title: "Fix the preview timeout", Kind: "bugfix", State: "review",
	}
	runbookRow := store.RunbookIndexRow{
		ID: "RB-900", Project: "acme", VaultPath: "Runbooks/RB-900.md",
		Title: "Preview endpoint returns 503 under load", Category: "performance",
		Status: "verified", Stale: true,
	}
	evidenceItem := store.EvidenceListItem{
		Evidence: store.Evidence{
			ID: 1, SyncID: "evd-1", Project: "acme", TaskID: 1, TaskSyncID: acmeTask.SyncID,
			Path: "ACME-1/cold-start.png", SHA256: strings.Repeat("a", 64),
			Kind: "png", Proves: "cold start exceeds 30s", AttachedJira: true,
		},
		JiraKey: &jiraKey,
	}

	mem = &data.FakeMemory{StatsResult: &store.Stats{
		TotalSessions: 12, TotalObservations: 348, TotalPrompts: 57,
		Projects: []string{"engram", "acme"},
	}}

	card := store.ProjectCard{Slug: "acme", DisplayName: "Acme Corp", DefaultBranch: "main", JiraProject: "ACME"}
	projects = &data.FakeProject{
		Cards: []store.ProjectCardListItem{{
			ProjectCard: card,
			Counts:      &store.ProjectCardCounts{Observations: 42, TasksActive: 1, Evidence: 1, Runbooks: 1},
		}},
		CardBySlug: map[string]store.ProjectCard{"acme": card},
		HealthBySlug: map[string]data.ProjectHealth{"acme": {
			ProjectCardCounts: store.ProjectCardCounts{Observations: 42, TasksActive: 1, Evidence: 1, Runbooks: 1},
		}},
		TasksBySlug:       map[string][]store.TaskListItem{"acme": {{Task: acmeTask, Observations: 1, Evidence: 1}}},
		StaleRunbooksSlug: map[string][]store.RunbookIndexRow{"acme": {runbookRow}},
		EvidenceBySlug:    map[string][]store.EvidenceListItem{"acme": {evidenceItem}},
	}

	task = &data.FakeTask{
		ItemsByProject: map[string][]store.TaskListItem{"acme": {{Task: acmeTask, Observations: 1, Evidence: 1}}},
		DetailByID: map[int64]data.TaskDetail{1: {
			Task: acmeTask,
			Observations: []store.TaskObservationDetail{{
				Role: "root_cause", LinkedAt: "2026-01-15 09:30:00",
				Observation: store.Observation{
					ID: 501, Type: "discovery", Title: "Root cause: preview worker pool exhausted",
					CreatedAt: "2026-01-15 09:30:00",
				},
			}},
			Evidence: []store.EvidenceListItem{evidenceItem},
		}},
		ContextPackByID: map[int64]string{
			1: "# Context pack: acme / ACME-1\n\n## Task\nACME-1 - bugfix - review\nFix the preview timeout\n",
		},
	}

	ev = &data.FakeEvidence{ItemsByProject: map[string][]store.EvidenceListItem{"acme": {evidenceItem}}}

	rb = &data.FakeRunbook{ItemsByProject: map[string][]store.RunbookIndexRow{"acme": {runbookRow}}}

	// One umbrella with two children: enough for the tree to show nesting, a
	// fold marker and — once filtered to a child — a parent kept on screen
	// only because of what is under it.
	acmeAPI := data.ProjectNode{
		Slug: "acme-api", ParentSlug: "acme", DisplayName: "Acme API", Kind: "service",
		Aliases: []string{"thunderbird"}, Depth: 1,
		Counts: store.ProjectCardCounts{Observations: 18, TasksActive: 1, Evidence: 4},
	}
	acmeWeb := data.ProjectNode{
		Slug: "acme-web", ParentSlug: "acme", DisplayName: "Acme Web", Kind: "repo", Depth: 1,
		Counts: store.ProjectCardCounts{Observations: 7, TasksActive: 0, Evidence: 2},
	}
	acmeRoot := data.ProjectNode{
		Slug: "acme", DisplayName: "Acme Corp", Kind: "umbrella",
		Tags: []string{"platform"}, Children: []data.ProjectNode{acmeAPI, acmeWeb},
		Counts: store.ProjectCardCounts{Observations: 42, TasksActive: 1, Evidence: 1},
	}
	tree = &data.FakeProjectTree{
		Tree:            []data.ProjectNode{acmeRoot},
		NodeBySlug:      map[string]data.ProjectNode{"acme": acmeRoot, "acme-api": acmeAPI, "acme-web": acmeWeb},
		AncestorsBySlug: map[string][]data.ProjectNode{"acme-api": {acmeRoot}, "acme-web": {acmeRoot}},
	}

	// A graph the store has already judged stale, with the reason it gave:
	// Home prints that string literally rather than forming a verdict of its
	// own, and the scene is where that is visible.
	graph = &data.FakeGraph{
		StateByProject: map[string]data.GraphState{"acme": {
			Project: "acme", Commit: strings.Repeat("c", 40), BuiltAt: "2026-01-14 08:00:00",
			CheckedAt: "2026-01-15 09:00:00",
			Nodes:     1284, Edges: 3901, Communities: 17,
			Stale: true, StaleReason: "code_changed", ChangedFiles: 6,
			GodNodes: []project.GodNode{
				{Label: "Store", Edges: 412, File: "internal/store/store.go"},
				{Label: "Model", Edges: 208, File: "internal/tui/app/model.go"},
			},
		}},
		RefsByProject: map[string][]data.ObservationRef{"acme": {
			{ObservationID: 501, RefKind: "graph", Ref: "internal/preview/pool.go:Pool", GraphCommit: strings.Repeat("c", 40)},
		}},
	}

	bench = &data.FakeBenchmark{ByProject: map[string][]data.Benchmark{"acme": {
		{BenchmarkDelta: store.BenchmarkDelta{
			Benchmark:     store.Benchmark{Name: "cold start", Metric: "p95", Unit: "ms", Value: 812, Direction: store.BenchmarkDirectionLower},
			BaselineValue: float64Ptr(940), DeltaPct: float64Ptr(-13.6),
		}},
		{BenchmarkDelta: store.BenchmarkDelta{
			Benchmark:     store.Benchmark{Name: "throughput", Metric: "rps", Unit: "ops", Value: 412, Direction: store.BenchmarkDirectionHigher},
			BaselineValue: float64Ptr(500), DeltaPct: float64Ptr(-17.6),
		}},
	}}}

	// One hit of every kind, so the palette's scene shows all six groups in
	// the order it declares them.
	search = &data.FakeSearch{Hits: []data.SearchHit{
		{Kind: data.SearchKindCard, Slug: "acme", Project: "acme", Title: "Acme Corp", Subtitle: "umbrella"},
		{Kind: data.SearchKindTask, ID: 1, Project: "acme", Title: "Fix the preview timeout", Subtitle: "ACME-1 · review"},
		{Kind: data.SearchKindObservation, ID: 501, Project: "acme", Title: "Root cause: preview worker pool exhausted"},
		{Kind: data.SearchKindEvidence, ID: 1, Project: "acme", Title: "ACME-1/cold-start.png"},
		{Kind: data.SearchKindBenchmark, ID: 7, Project: "acme", Title: "preview p95", Subtitle: "812 ms"},
		{Kind: data.SearchKindRunbook, Slug: "RB-900", Project: "acme", Title: "Preview endpoint returns 503 under load"},
	}}

	// Two builtin palettes and one the store could not make sense of: the
	// picker lists all three, because hiding the broken one would leave
	// somebody hunting for a theme that simply never appears.
	themes = &data.FakeTheme{Themes: []data.ThemeRecord{
		{ThemeRecord: store.ThemeRecord{
			Name: "koi-pond", Variant: "dark", Source: "builtin",
			Palette: mustMarshalTheme(t, "koi-pond", "dark", theme.KoiPond()),
		}},
		{ThemeRecord: store.ThemeRecord{
			Name: "koi-day", Variant: "light", Source: "builtin",
			Palette: mustMarshalTheme(t, "koi-day", "light", theme.KoiDay()),
		}},
		{
			ThemeRecord: store.ThemeRecord{Name: "half-written", Variant: "dark", Source: "sql", Palette: []byte("{}")},
			Invalid:     "palette has no color roles",
		},
	}}

	return mem, projects, task, ev, rb, tree, graph, bench, search, themes
}

// mustMarshalTheme encodes a palette the way `engram theme export` does, so
// the picker decodes exactly what the store would have handed it.
func mustMarshalTheme(t *testing.T, name, variant string, p theme.Palette) []byte {
	t.Helper()
	encoded, err := theme.MarshalTheme(name, variant, p)
	if err != nil {
		t.Fatalf("marshal %s: %v", name, err)
	}
	return encoded
}

func float64Ptr(v float64) *float64 { return &v }

// seedVaultFixture points ENGRAM_VAULT_ROOT at a temp directory holding
// RB-900's body, the same fixture pattern
// runbooks/update_test.go's TestEnterOpensTheMarkdownViewAndRendersAnExistingFile
// uses, so S9's golden shows real rendered Markdown instead of the "not
// cloned locally" instruction.
func seedVaultFixture(t *testing.T) {
	t.Helper()
	// A fixed directory, not t.TempDir(): the Settings scene prints the
	// configured vault root, and lipgloss pads its block to the widest line
	// in it — so a path whose length changes from run to run changes the
	// trailing whitespace of every line around it. t.TempDir() names carry
	// the test's own name and a counter, which differ between the update run
	// and the comparison run. Found by rendering the scene twice and getting
	// two frames that differed only in padding.
	dir := filepath.Join(os.TempDir(), "engram-tui-e2e-vault")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create vault fixture dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	t.Setenv(shared.VaultRootEnv, dir)

	rel := filepath.Join("Runbooks", "RB-900.md")
	if err := os.MkdirAll(filepath.Join(dir, "Runbooks"), 0o755); err != nil {
		t.Fatalf("mkdir vault fixture dir: %v", err)
	}
	// The fenced Go block is not decoration: it is the only thing in this
	// suite that drives glamour's chroma path, so the scene proves a code
	// block is styled from the palette rather than from glamour's own fixed
	// scheme.
	content := "# Preview endpoint returns 503 under load\n\n" +
		"Restart the preview worker pool and watch memory.\n\n" +
		"```go\n" +
		"func restart(pool *Pool) error {\n" +
		"\tif err := pool.Drain(); err != nil {\n" +
		"\t\treturn fmt.Errorf(\"drain: %w\", err)\n" +
		"\t}\n" +
		"\treturn pool.Start()\n" +
		"}\n" +
		"```\n"
	if err := os.WriteFile(filepath.Join(dir, rel), []byte(content), 0o644); err != nil {
		t.Fatalf("write vault fixture: %v", err)
	}
}

// waitFor polls tm's output, accumulating into buf, until substr appears or
// the timeout elapses. Accumulating into a scene-lived buffer (rather than
// letting each call start from an empty one, the way a bare
// teatest.WaitFor(t, tm.Output(), ...) would) means a wait late in a
// multi-step scene can still see content a frame emitted several steps ago:
// bubbletea's own line diffing may not re-emit a line that did not change
// between two frames. Every wait string this file uses is still chosen to
// be exclusive to the screen it gates, so accumulation never turns into a
// false-positive wait on a stale, unrelated frame.
func waitFor(t *testing.T, tm *teatest.TestModel, buf *strings.Builder, substr string) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for {
		var chunk [4096]byte
		n, _ := tm.Output().Read(chunk[:])
		if n > 0 {
			buf.Write(chunk[:n])
		}
		if strings.Contains(buf.String(), substr) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("waitFor %q timed out after 4s; accumulated output:\n%s", substr, buf.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// renderTeatestScene drives screen to completion at size through a real
// tea.Program and returns the ASCII snapshot of the model it landed on.
//
// It deliberately does not compare teatest's raw FinalOutput() byte-for-byte
// (see the package doc comment above for why: that stream carries
// frame-by-frame terminal control sequences whose exact bytes depend on
// bubbletea's line diffing and render timing, not only on what changed in
// the UI). Instead it renders a clean snapshot from FinalModel's own View(),
// the same mechanism app/golden_test.go uses, fed by a model that arrived at
// that state through the real Update loop instead of direct field
// assignment.
func renderTeatestScene(t *testing.T, screen teatestScreen, size goldenSize) string {
	t.Helper()
	t.Setenv("ENGRAM_TIMEZONE", "UTC")

	mem, projects, task, ev, rb, tree, graph, bench, search, themes := teatestFixtures(t)
	m := app.New(mem, projects, task, ev, rb, e2eVersion, theme.Default(), screen.initialProject).
		WithUpdateChecker(quietUpdateCheck).
		WithProjectTree(tree).
		WithGraph(graph, graph).
		WithBenchmarks(bench).
		WithSearch(search, &data.FakeSettings{}).
		WithSearchHistory([]string{"cold start", "preview 503"}).
		WithThemePicker(themes, &data.FakeSettings{}).
		WithSettingsStore(&data.FakeSettings{}, &data.FakeSettings{})

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(size.width, size.height))
	var buf strings.Builder

	if screen.initialWaitFor != "" {
		waitFor(t, tm, &buf, screen.initialWaitFor)
	}
	for _, step := range screen.steps {
		tm.Send(step.key)
		waitFor(t, tm, &buf, step.waitFor)
	}

	if err := tm.Quit(); err != nil {
		t.Fatalf("quit: %v", err)
	}
	fm := tm.FinalModel(t, teatest.WithFinalTimeout(5*time.Second))
	final, ok := fm.(app.Model)
	if !ok {
		t.Fatalf("FinalModel returned %T, want app.Model", fm)
	}

	out := final.View()
	if strings.ContainsRune(out, 0x1b) {
		t.Fatalf("scene %q rendered ANSI escapes: the color profile is not Ascii, so the golden would not be portable", screen.name)
	}
	return normalizeVaultRoot(normalizeContextPackBuiltAt(out))
}

// quietUpdateCheck stands in for version.CheckLatest, which reaches GitHub
// over the network. Left alone, the Memory dashboard renders a banner only
// when the HTTP response beats tea.Quit — so two runs of the same scene
// differ by a whole line, and the recorded golden depends on which side of
// that race the machine happened to land on. An up-to-date result carries no
// message, so no banner is drawn and every run renders the same frame.
func quietUpdateCheck(string) version.CheckResult {
	return version.CheckResult{Status: version.StatusUpToDate}
}

// normalizeVaultRoot masks the per-run temporary directory the vault fixture
// is seeded into. The Settings tab prints the configured vault root, and
// t.TempDir() hands out a different path on every run — a second real
// non-determinism of exactly the kind the context-pack clock was, found the
// same way: the scene rendered twice and differed by one line.
func normalizeVaultRoot(s string) string {
	root := os.Getenv(shared.VaultRootEnv)
	if root == "" {
		return s
	}
	return strings.ReplaceAll(s, root, "/fixtures/vault")
}

// contextPackBuiltAtPattern matches S5's "built HH:MM:SS" stamp
// (tabs/tasks/view.go's own "%d chars est. · built %s" line).
var contextPackBuiltAtPattern = regexp.MustCompile(`built \d{2}:\d{2}:\d{2}`)

// normalizeContextPackBuiltAt masks S5's context-pack "built at" clock
// reading. tabs/tasks/update.go sets Model.ContextPackBuilt from time.Now()
// with no injectable clock (unlike the fixture-driven CreatedAt/UpdatedAt
// timestamps every other screen shows) — driving S5 through the real Update
// loop, as this suite does, bakes the wall-clock second into the frame, so
// two otherwise-identical runs a second apart fail byte-for-byte (confirmed:
// running this suite right after -e2e-update failed on exactly this line).
// This is a real non-determinism this task's brief asked to hunt for
// (\"fechas relativas\"), found in production code (tabs/tasks/update.go:107)
// rather than in a fixture; masking it here — the same kind of fix
// golden_test.go's own ENGRAM_TIMEZONE=UTC applies to Memory's relative
// dates — avoids reaching into tasks.Model to add a seam nobody asked for
// in this task.
func normalizeContextPackBuiltAt(s string) string {
	return contextPackBuiltAtPattern.ReplaceAllString(s, "built 00:00:00")
}

func teatestGoldenPath(size goldenSize) string {
	return filepath.Join("testdata", "screens-"+size.name+".golden")
}

// TestTeatestGoldenScreens is this task's own golden suite: S1-S11 at
// 120x40 and 80x24, driven through the real bubbletea Program.
// Regenerate with: go test ./internal/tui/e2e/... -run TestTeatestGoldenScreens -e2e-update
func TestTeatestGoldenScreens(t *testing.T) {
	for _, size := range goldenSizes {
		t.Run(size.name, func(t *testing.T) {
			var b strings.Builder
			for _, screen := range teatestScreens() {
				t.Run(screen.name, func(t *testing.T) {
					got := renderTeatestScene(t, screen, size)
					fmt.Fprintf(&b, "═══ %s ═══\n", screen.name)
					b.WriteString(got)
					b.WriteString("\n")
				})
			}

			path := teatestGoldenPath(size)
			got := b.String()

			if *updateGolden {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatalf("create testdata dir: %v", err)
				}
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
				t.Logf("wrote %s (%d bytes)", path, len(got))
				return
			}

			wantBytes, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden (run with -update to create it): %v", err)
			}
			want := string(wantBytes)
			if got != want {
				t.Errorf("teatest rendering drifted from %s\n%s", path, firstDiff(want, got))
			}
		})
	}
}

// TestTeatestGoldenScreensAreStable renders every screen twice through two
// independent real programs and requires byte-identical results — the
// teatest counterpart of golden_test.go's TestGoldenScreensAreStable, and
// the actual proof (not just an assertion) that driving the workspace
// through bubbletea's real event loop is as deterministic as calling
// View() directly.
func TestTeatestGoldenScreensAreStable(t *testing.T) {
	size := goldenSize{name: "120x40", width: 120, height: 40}
	for _, screen := range teatestScreens() {
		t.Run(screen.name, func(t *testing.T) {
			first := renderTeatestScene(t, screen, size)
			second := renderTeatestScene(t, screen, size)
			if first != second {
				t.Errorf("%s rendering is not deterministic across two runs\n%s", screen.name, firstDiff(first, second))
			}
		})
	}
}

// firstDiff reports the first line where want and got diverge, with the
// scene header that line belongs to, so a failure names the screen that
// drifted — the same helper app/golden_test.go carries, duplicated rather
// than exported: this package intentionally shares no symbol with app's
// golden mechanism, only the visual convention of a "═══ name ═══" header
// per scene.
func firstDiff(want, got string) string {
	wantLines := strings.Split(want, "\n")
	gotLines := strings.Split(got, "\n")

	scene := "(before the first scene header)"
	limit := len(wantLines)
	if len(gotLines) > limit {
		limit = len(gotLines)
	}

	for i := 0; i < limit; i++ {
		var w, g string
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if i < len(gotLines) {
			g = gotLines[i]
		}
		if strings.HasPrefix(w, "═══ ") {
			scene = strings.Trim(w, "═ ")
		}
		if w != g {
			return fmt.Sprintf("scene %q, line %d:\n  want: %q\n  got:  %q", scene, i+1, w, g)
		}
	}
	return "files differ but no differing line was found (trailing newline?)"
}
