package app

import (
	"strings"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/tui/tabs"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	tea "github.com/charmbracelet/bubbletea"
)

// TestTabBarShowsFullLabelsAtOrAboveTheBreakpoint pins rfc-tui.md §5's
// wireframes: at 100 columns or wider the persistent tab bar shows every
// tab's digit and label, and brackets the active one.
func TestTabBarShowsFullLabelsAtOrAboveTheBreakpoint(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.active = tabs.Tasks
	m.screen = screenTab
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})

	out := m.View()
	if !strings.Contains(out, "[2 Tasks]") {
		t.Fatalf("expected the active tab bracketed with its label, got:\n%s", out)
	}
	if !strings.Contains(out, "1 Memory") {
		t.Fatalf("expected every tab's label at %d columns, got:\n%s", tabBarBreakpoint, out)
	}
	if !strings.Contains(out, "5 Cloud") {
		t.Fatalf("expected the last tab's label at %d columns, got:\n%s", tabBarBreakpoint, out)
	}
}

// TestTabBarCollapsesBelowTheBreakpoint pins the same wireframes' narrow
// behaviour: below 100 columns the bar drops every label down to bare
// digits, keeping only the active one marked.
func TestTabBarCollapsesBelowTheBreakpoint(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.active = tabs.Tasks
	m.screen = screenTab
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})

	out := m.View()
	if strings.Contains(out, "Memory") || strings.Contains(out, "Dashboard") {
		t.Fatalf("expected no tab labels below %d columns, got:\n%s", tabBarBreakpoint, out)
	}
	if !strings.Contains(out, "[2]") {
		t.Fatalf("expected the active tab's bare digit bracketed, got:\n%s", out)
	}
}

// TestTabBarDefaultsToFullFormWhenWidthIsUnknown pins the choice for a Model
// that never received a tea.WindowSizeMsg (width stays the zero value): the
// bar renders full rather than guessing narrow.
func TestTabBarDefaultsToFullFormWhenWidthIsUnknown(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")

	if !strings.Contains(m.View(), "1 Memory") {
		t.Fatalf("expected the full form when width is unknown, got:\n%s", m.View())
	}
}

// TestTabBarMarksTheDashboardAsTheActiveEntry pins that the bar also shows
// on the Project Dashboard (S2), bracketing its own "0" slot the same way
// every tab screen brackets its own.
func TestTabBarMarksTheDashboardAsTheActiveEntry(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.project = "nextcloud"
	m.screen = screenDashboard
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})

	if !strings.Contains(m.View(), "[0 Dashboard]") {
		t.Fatalf("expected the Dashboard slot bracketed while it is active, got:\n%s", m.View())
	}
}

// TestTabBarIsHiddenOnTheSelector pins that S1 keeps its own distinct
// header (rfc-tui.md §5's S1 wireframe) instead of the tab bar: there is no
// project yet, so there is nothing to number.
func TestTabBarIsHiddenOnTheSelector(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.screen = screenSelector
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})

	if strings.Contains(m.View(), "1 Memory") || strings.Contains(m.View(), "0 Dashboard") {
		t.Fatalf("the selector should not show the persistent tab bar, got:\n%s", m.View())
	}
}
