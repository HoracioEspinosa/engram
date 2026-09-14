package tui_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui"
	"github.com/HoracioEspinosa/engram/internal/tui/app"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	tea "github.com/charmbracelet/bubbletea"
)

// The facade is the only thing cmd/engram depends on. These tests pin that
// contract: New returns a value usable as a tea.Model, Model names the root
// workspace model, and the store handed to New is the one every screen reads.

func TestNewSatisfiesTheBubbleteaModelContract(t *testing.T) {
	var m tea.Model = tui.New(nil, "1.0.0-test", "", theme.CatppuccinMocha())

	if cmd := m.Init(); cmd == nil {
		t.Fatal("Init should return the startup batch")
	}

	sized, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	out := sized.View()
	if !strings.Contains(out, "Select a project") {
		t.Fatalf("without a resolvable project the TUI should open on the selector (rfc-tui.md §9.1), got:\n%s", out)
	}

	// The selector carries no version banner of its own (rfc-tui.md §5's S1
	// wireframe), so reach a tab screen — via the same NavigateMsg every
	// cross-tab jump in the app uses — to confirm the version and the store
	// New was given still reach the workspace underneath it.
	tabbed, _ := sized.Update(tabs.NavigateMsg{Target: tabs.Memory})
	out = tabbed.View()
	if !strings.Contains(out, "engram 1.0.0-test") {
		t.Fatalf("the memory dashboard should render the version it was built with, got:\n%s", out)
	}
	if !strings.Contains(out, "Actions") {
		t.Fatal("navigating to memory should show its own dashboard")
	}
}

func TestModelIsTheRootWorkspaceModel(t *testing.T) {
	var m tui.Model = app.New(nil, nil, nil, nil, nil, "", theme.Default(), "")

	if _, ok := any(m).(tea.Model); !ok {
		t.Fatal("tui.Model must remain a tea.Model")
	}
}

// newTestStore opens a throwaway store so the facade can be exercised against
// the real adapters rather than the render-only nil readers.
func newTestStore(t *testing.T) *store.Store {
	t.Helper()

	cfg, err := store.DefaultConfig()
	if err != nil {
		t.Fatalf("DefaultConfig: %v", err)
	}
	cfg.DataDir = t.TempDir()

	s, err := store.New(cfg)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// run executes a command the way the Bubble Tea runtime would, turning a
// panic inside it into a test failure instead of a crashed test binary.
//
// A command that batches others is expanded, because the runtime runs each of
// them: a screen switch carries both its reload and whatever the workspace is
// recording about the switch, and the caller here wants the reload.
func run(t *testing.T, cmd tea.Cmd) (msg tea.Msg) {
	t.Helper()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("command panicked: %v", r)
		}
	}()

	msg = cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return msg
	}
	for _, inner := range batch {
		if produced := run(t, inner); produced != nil {
			return produced
		}
	}
	return nil
}

func TestNewWiresTheStoreIntoTheMemoryTab(t *testing.T) {
	s := newTestStore(t)
	if err := s.CreateSession("session-1", "engram", "/tmp/engram"); err != nil {
		t.Fatalf("create session: %v", err)
	}

	var m tea.Model = tui.New(s, "1.0.0-test", "", theme.CatppuccinMocha())
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	// Leaving and re-entering the Memory tab reloads the dashboard counters
	// through whichever reader the tab was built with. A tab bound to a nil
	// store panics inside the command exactly as it would at startup.
	m, _ = m.Update(tabs.NavigateMsg{Target: tabs.Cloud})
	m, cmd := m.Update(tabs.NavigateMsg{Target: tabs.Memory})
	if cmd == nil {
		t.Fatal("returning to the memory dashboard should reload its counters")
	}
	m, _ = m.Update(run(t, cmd))

	out := m.View()
	if !regexp.MustCompile(`\b1\s+sessions\b`).MatchString(out) {
		t.Fatalf("the dashboard should render the counters of the store New was given, got:\n%s", out)
	}
}

// TestNewOpensTheDashboardForAnExplicitProject pins rfc-tui.md §9.1's
// "Semántica de --project": engram tui --project <slug> must open straight
// on that project's Dashboard (S2) with its real counters, not on the Memory
// tab or an empty Selector. Before this test, New took no project argument at
// all, so `engram tui --project nextcloud` silently ignored the flag —
// exactly the closing criterion roadmap task T-10.02 fixes.
func TestNewOpensTheDashboardForAnExplicitProject(t *testing.T) {
	s := newTestStore(t)
	if _, _, err := s.UpsertProjectCard(store.UpsertProjectCardParams{Slug: "clarodrive"}); err != nil {
		t.Fatalf("seed project card: %v", err)
	}

	var m tea.Model = tui.New(s, "1.0.0-test", "clarodrive", theme.CatppuccinMocha())

	initCmd := m.Init()
	if initCmd == nil {
		t.Fatal("Init should return the startup batch")
	}

	// Init's startup commands arrive batched; unwrap tea.BatchMsg recursively
	// so this test does not depend on whether tea.Batch folds a single
	// remaining command instead of wrapping it.
	var apply func(tea.Msg)
	apply = func(msg tea.Msg) {
		if msg == nil {
			return
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				if c == nil {
					continue
				}
				apply(run(t, c))
			}
			return
		}
		m, _ = m.Update(msg)
	}
	apply(run(t, initCmd))

	out := m.View()
	if !strings.Contains(out, "clarodrive") {
		t.Fatalf("opening on an explicit project should render its dashboard, got:\n%s", out)
	}
	if strings.Contains(out, "Select a project") {
		t.Fatalf("an explicit project must open its dashboard, not the selector, got:\n%s", out)
	}
}
