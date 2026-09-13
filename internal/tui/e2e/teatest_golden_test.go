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

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/app"
	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/shared"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

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
			name: "s1-project-selector",
			// No project: the workspace opens on the Memory tab (see
			// internal/tui/tui.go's own doc comment on New) — this task's
			// report flags that as a discrepancy against rfc-tui.md
			// §10.1's smoke row ("engram tui sin proyecto resoluble abre
			// S1"). "p" is the global key that reaches the Selector
			// regardless of which tab is active.
			steps: []teatestStep{
				{key: keyRune("p"), waitFor: "Acme Corp"},
			},
		},
		{
			name:           "s2-project-dashboard",
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
				{key: teatestEnterKey, waitFor: "Root cause: preview worker pool exhausted"},
			},
		},
		{
			name:           "s5-context-pack",
			initialProject: "acme",
			steps: []teatestStep{
				{key: keyRune("2"), waitFor: "ACME-1"},
				{key: teatestEnterKey, waitFor: "Root cause: preview worker pool exhausted"},
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
			name:           "s8-runbooks-index",
			initialProject: "acme",
			steps: []teatestStep{
				{key: keyRune("4"), waitFor: "RB-900"},
			},
		},
		{
			name:           "s9-runbooks-markdown",
			initialProject: "acme",
			steps: []teatestStep{
				{key: keyRune("4"), waitFor: "RB-900"},
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
			name:           "s11-cloud",
			initialProject: "acme",
			steps: []teatestStep{
				{key: keyRune("5"), waitFor: "Configure server"},
			},
		},
	}
}

// teatestFixtures builds one deterministic "acme" project across every
// reader the workspace consumes — the same data.Fake* seam every tabs/*
// Update test already uses, just wired through the real root instead of a
// leaf tab.
func teatestFixtures(t *testing.T) (mem *data.FakeMemory, projects *data.FakeProject, task *data.FakeTask, ev *data.FakeEvidence, rb *data.FakeRunbook) {
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

	return mem, projects, task, ev, rb
}

// seedVaultFixture points ENGRAM_VAULT_ROOT at a temp directory holding
// RB-900's body, the same fixture pattern
// runbooks/update_test.go's TestEnterOpensTheMarkdownViewAndRendersAnExistingFile
// uses, so S9's golden shows real rendered Markdown instead of the "not
// cloned locally" instruction.
func seedVaultFixture(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(shared.VaultRootEnv, dir)

	rel := filepath.Join("Runbooks", "RB-900.md")
	if err := os.MkdirAll(filepath.Join(dir, "Runbooks"), 0o755); err != nil {
		t.Fatalf("mkdir vault fixture dir: %v", err)
	}
	content := "# Preview endpoint returns 503 under load\n\n" +
		"Restart the preview worker pool and watch memory.\n"
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

	mem, projects, task, ev, rb := teatestFixtures(t)
	m := app.New(mem, projects, task, ev, rb, e2eVersion, theme.Default(), screen.initialProject)

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
	return normalizeContextPackBuiltAt(out)
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
