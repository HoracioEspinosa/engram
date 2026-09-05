package tui_test

import (
	"strings"
	"testing"

	"github.com/Gentleman-Programming/engram/internal/tui"
	"github.com/Gentleman-Programming/engram/internal/tui/app"

	tea "github.com/charmbracelet/bubbletea"
)

// The facade is the only thing cmd/engram depends on. These tests pin that
// contract: New returns a value usable as a tea.Model, and Model names the
// root workspace model.

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
	var m tui.Model = app.New(nil, "")

	if _, ok := any(m).(tea.Model); !ok {
		t.Fatal("tui.Model must remain a tea.Model")
	}
}
