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
	m.screen, m.tree.open = screenTab, false
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
	m.screen, m.tree.open = screenTab, false
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
	// New now opens the project tree without a resolvable project (T-10.02);
	// this case's premise is the tab bar showing over a tab screen.
	m.screen, m.tree.open = screenTab, false

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
	m.screen, m.tree.open = screenDashboard, false
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})

	if !strings.Contains(m.View(), "[0 Dashboard]") {
		t.Fatalf("expected the Dashboard slot bracketed while it is active, got:\n%s", m.View())
	}
}

// TestTabBarSurvivesTheProjectTreeOverlay pins that the tree is composited
// over the workspace rather than replacing it: the bar is chrome, and an
// overlay centred inside the body leaves its edges showing.
func TestTabBarSurvivesTheProjectTreeOverlay(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.tree.open = true
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})

	if !strings.Contains(m.View(), "0 Dashboard") {
		t.Fatalf("the project tree replaced the workspace instead of being composited over it, got:\n%s", m.View())
	}
}

// TestTabBarLabelsComeFromTitleNotTheEntryTable proves the bar's per-tab
// labels are read from each tab's own Title() (rfc-tui.md §4.3: "label
// shown in the tab bar") rather than from tabBarEntries' hand-written
// label field. It corrupts every non-Dashboard entry's label — Dashboard
// has no Title() of its own to read, so it keeps its literal — and checks
// the render never shows the corruption: if the bar ever again renders
// straight from tabBarEntries.label instead of calling Title(), this test
// catches the two sources of truth diverging before a golden file would.
func TestTabBarLabelsComeFromTitleNotTheEntryTable(t *testing.T) {
	original := tabBarEntries
	defer func() { tabBarEntries = original }()

	corrupted := make([]tabBarEntry, len(original))
	copy(corrupted, original)
	for i := range corrupted {
		if !corrupted[i].isDashboard {
			corrupted[i].label = "WRONG-" + corrupted[i].label
		}
	}
	tabBarEntries = corrupted

	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.active = tabs.Tasks
	m.screen, m.tree.open = screenTab, false
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})

	out := m.View()
	if strings.Contains(out, "WRONG-") {
		t.Fatalf("tab bar rendered tabBarEntries' hand-written label instead of the tab's own Title(), got:\n%s", out)
	}
	if want := "1 " + m.tab(tabs.Memory).Title(); !strings.Contains(out, want) {
		t.Fatalf("expected the bar to read Memory's own Title() (%q), got:\n%s", want, out)
	}
}
