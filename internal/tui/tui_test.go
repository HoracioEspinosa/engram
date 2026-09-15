package tui_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui"
	"github.com/HoracioEspinosa/engram/internal/tui/app"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs/memory"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs/settings"
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
	if !strings.Contains(out, "space fold") {
		t.Fatalf("without a resolvable project the TUI should open on the project tree, got:\n%s", out)
	}

	// The tree is composited over the workspace rather than replacing it, but
	// the panel is centred on the terminal and covers the middle of the body,
	// so it is closed before reading what is underneath: this case is about
	// the version and the store New was given reaching the tab, not about how
	// much of a covered screen shows around a panel.
	closed, _ := sized.Update(tea.KeyMsg{Type: tea.KeyEsc})
	tabbed, _ := closed.Update(tabs.NavigateMsg{Target: tabs.Memory})
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
	// No project resolves from an empty store, so the workspace opens behind
	// the project tree; esc puts it away and leaves the tab underneath.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})

	// Leaving and re-entering the Memory tab reloads the dashboard counters
	// through whichever reader the tab was built with. A tab bound to a nil
	// store panics inside the command exactly as it would at startup.
	m, _ = m.Update(tabs.NavigateMsg{Target: tabs.Settings})
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

// TestNewOpensTheDashboardForAnExplicitProject pins the --project semantics:
// engram tui --project <slug> opens straight on that project's Home tab with
// its real counters, not on the Memory tab and not on the project tree.
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

// openWorkspace opens the facade on a seeded project, sized, with the project
// tree the workspace raises on an unresolvable project already out of the way.
func openWorkspace(t *testing.T, s *store.Store) tea.Model {
	t.Helper()

	if _, _, err := s.UpsertProjectCard(store.UpsertProjectCardParams{Slug: "koi"}); err != nil {
		t.Fatalf("seed project card: %v", err)
	}

	var m tea.Model = tui.New(s, "1.0.0-test", "koi", theme.CatppuccinMocha())
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return m
}

// TestTheIconsRowRemembersTheVocabularyItChose: the Settings tab writes the
// chosen icon vocabulary to the store the facade opened.
//
// The tab has always known how to write it. What it lacked was the store: the
// facade wired the theme picker's writer and nothing else, so every row that
// remembers something wrote into a nil and reported that it could not. The
// Doctor row said so in as many words, and the only place that showed was a
// screen capture nobody reads as a defect.
func TestTheIconsRowRemembersTheVocabularyItChose(t *testing.T) {
	s := newTestStore(t)
	m := openWorkspace(t, s)

	m, _ = m.Update(tabs.NavigateMsg{Target: tabs.Settings})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("cycling the icon vocabulary should remember the choice")
	}
	m, _ = m.Update(run(t, cmd))

	value, ok, err := s.Setting(settings.IconSettingKey)
	if err != nil {
		t.Fatalf("read %s: %v", settings.IconSettingKey, err)
	}
	if !ok || value == "" {
		t.Fatalf("%s was never written", settings.IconSettingKey)
	}

	if out := m.View(); strings.Contains(out, "no settings store bound") {
		t.Fatalf("the Doctor row reports no store on a workspace built from one:\n%s", out)
	}
}

// TestMemoryRemembersTheScopeItWasCycledTo is the same wiring for the other
// row that writes one: "a" narrows Memory, and the choice has to outlive the
// session or a reader working inside one project re-narrows it every start.
func TestMemoryRemembersTheScopeItWasCycledTo(t *testing.T) {
	s := newTestStore(t)
	m := openWorkspace(t, s)

	m, _ = m.Update(tabs.NavigateMsg{Target: tabs.Memory})
	// The dashboard's second entry is the recent list, which is one of the
	// screens "a" narrows.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if cmd == nil {
		t.Fatal("narrowing Memory should remember the choice and reload")
	}
	m, _ = m.Update(run(t, cmd))

	value, ok, err := s.Setting(memory.ScopeSettingKey)
	if err != nil {
		t.Fatalf("read %s: %v", memory.ScopeSettingKey, err)
	}
	if !ok || value != memory.ScopeProject.String() {
		t.Fatalf("%s holds %q (written: %v), want the width that was chosen",
			memory.ScopeSettingKey, value, ok)
	}
}

// TestMemoryOpensAtTheScopeItRemembers closes the loop: a remembered width is
// read back when the workspace opens, and the tab bar says which one it is.
func TestMemoryOpensAtTheScopeItRemembers(t *testing.T) {
	s := newTestStore(t)
	if err := s.SetSetting(memory.ScopeSettingKey, memory.ScopeSubtree.String()); err != nil {
		t.Fatalf("set %s: %v", memory.ScopeSettingKey, err)
	}

	out := openWorkspace(t, s).View()
	if !strings.Contains(out, "Memory (subtree)") {
		t.Fatalf("the workspace opened workspace-wide with a narrower width remembered:\n%s", out)
	}
}
