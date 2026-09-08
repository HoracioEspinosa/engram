package tui_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/Gentleman-Programming/engram/internal/store"
	"github.com/Gentleman-Programming/engram/internal/tui"
	"github.com/Gentleman-Programming/engram/internal/tui/app"
	"github.com/Gentleman-Programming/engram/internal/tui/tabs"
	"github.com/Gentleman-Programming/engram/internal/tui/theme"

	tea "github.com/charmbracelet/bubbletea"
)

// The facade is the only thing cmd/engram depends on. These tests pin that
// contract: New returns a value usable as a tea.Model, Model names the root
// workspace model, and the store handed to New is the one every screen reads.

func TestNewSatisfiesTheBubbleteaModelContract(t *testing.T) {
	var m tea.Model = tui.New(nil, "1.0.0-test")

	if cmd := m.Init(); cmd == nil {
		t.Fatal("Init should return the startup batch")
	}

	sized, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	out := sized.View()
	if !strings.Contains(out, "engram 1.0.0-test") {
		t.Fatalf("the dashboard should render the version it was built with, got:\n%s", out)
	}
	if !strings.Contains(out, "Actions") {
		t.Fatal("the TUI should open on the memory dashboard")
	}
}

func TestModelIsTheRootWorkspaceModel(t *testing.T) {
	var m tui.Model = app.New(nil, nil, "", theme.Default(), "")

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
func run(t *testing.T, cmd tea.Cmd) (msg tea.Msg) {
	t.Helper()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("command panicked: %v", r)
		}
	}()
	return cmd()
}

func TestNewWiresTheStoreIntoTheMemoryTab(t *testing.T) {
	s := newTestStore(t)
	if err := s.CreateSession("session-1", "engram", "/tmp/engram"); err != nil {
		t.Fatalf("create session: %v", err)
	}

	var m tea.Model = tui.New(s, "1.0.0-test")
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
