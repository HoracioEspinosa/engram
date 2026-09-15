package app

import (
	"strings"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/tui/tabs"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// TestTabBarShowsFullLabelsAtOrAboveTheBreakpoint pins §6.9's bar: at 100
// columns or wider every slot shows its glyph, its digit and its label, and
// the active one is bracketed.
func TestTabBarShowsFullLabelsAtOrAboveTheBreakpoint(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.active = tabs.Tasks
	m.tree.open = false
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})

	out := m.View()
	if !strings.Contains(out, "2 Tasks]") {
		t.Fatalf("expected the active tab bracketed with its label, got:\n%s", out)
	}
	for _, want := range []string{"0 Home", "1 Memory", "4 Benchmarks", "7 Settings"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected %q at %d columns, got:\n%s", want, tabBarBreakpoint, out)
		}
	}
}

// TestTabBarSlotsStayApartAtEveryWidth pins the separation the pointer and the
// golden lint both read the bar by.
//
// A single blank cell with text behind it is a word break inside one slot —
// which is exactly what "0 Home" is — so slots only one cell apart parse as a
// single run: the bar would read as one word and click as one target. The
// labels appear above the breakpoint and vanish below it, so the separation
// has to hold at both widths or the pointer works on one terminal and not the
// other.
func TestTabBarSlotsStayApartAtEveryWidth(t *testing.T) {
	for _, width := range []int{80, 120} {
		m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
		m.tree.open = false
		m, _ = step(t, m, tea.WindowSizeMsg{Width: width, Height: 40})

		row := strings.TrimSpace(ansi.Strip(m.viewTabBar()))
		if got := len(SlotSpans(row)); got != len(tabBarEntries) {
			t.Fatalf("%d columns: the bar parses as %d slots, want %d:\n%s",
				width, got, len(tabBarEntries), row)
		}
	}
}

// TestTabBarCollapsesBelowTheBreakpoint pins the narrow behaviour: below 100
// columns the bar drops every label down to a glyph and a digit, keeping only
// the active one marked.
func TestTabBarCollapsesBelowTheBreakpoint(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.active = tabs.Tasks
	m.tree.open = false
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})

	out := m.View()
	if strings.Contains(out, "Memory") || strings.Contains(out, "Home") {
		t.Fatalf("expected no tab labels below %d columns, got:\n%s", tabBarBreakpoint, out)
	}
	if !strings.Contains(out, "2]") {
		t.Fatalf("expected the active tab's bare digit bracketed, got:\n%s", out)
	}
}

// TestTabBarDefaultsToFullFormWhenWidthIsUnknown pins the choice for a Model
// that never received a tea.WindowSizeMsg (width stays the zero value): the
// bar renders full rather than guessing narrow.
func TestTabBarDefaultsToFullFormWhenWidthIsUnknown(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	// New opens the project tree without a resolvable project; this case's
	// premise is the tab bar showing over a tab screen.
	m.tree.open = false

	if !strings.Contains(m.View(), "1 Memory") {
		t.Fatalf("expected the full form when width is unknown, got:\n%s", m.View())
	}
}

// TestTabBarMarksHomeAsTheActiveEntry pins that Home brackets its own "0"
// slot the same way every other tab brackets its own: it is a tab now, not a
// screen the frame drew around.
func TestTabBarMarksHomeAsTheActiveEntry(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.project = "nextcloud"
	m.tree.open, m.active = false, tabs.Home
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})

	if !strings.Contains(m.View(), "0 Home]") {
		t.Fatalf("expected the Home slot bracketed while it is active, got:\n%s", m.View())
	}
}

// TestTabBarSurvivesTheProjectTreeOverlay pins that the tree is composited
// over the workspace rather than replacing it: the bar is chrome, and an
// overlay centred inside the body leaves its edges showing.
func TestTabBarSurvivesTheProjectTreeOverlay(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.tree.open = true
	// A tab with a body tall enough to have room around the panel: an overlay
	// centred over two rows of text has nothing left to leave showing.
	m.active = tabs.Memory
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})

	// The overlay is centred, so the bar's own row survives at both ends
	// around it: the first slot and the last.
	out := m.View()
	if !strings.Contains(out, "0 Home") || !strings.Contains(out, "7 Settings") {
		t.Fatalf("the project tree replaced the workspace instead of being composited over it, got:\n%s", out)
	}
}

// TestTabBarLabelsComeFromTitleNotTheEntryTable proves the bar's per-tab
// labels are read from each tab's own Title() (rfc-tui.md §4.3: "label shown
// in the tab bar") rather than from tabBarEntries' hand-written label field.
// It corrupts every entry's label and checks the render never shows the
// corruption for a tab this build implements: if the bar ever again renders
// straight from tabBarEntries.label instead of calling Title(), this test
// catches the two sources of truth diverging before a golden file would.
func TestTabBarLabelsComeFromTitleNotTheEntryTable(t *testing.T) {
	original := tabBarEntries
	defer func() { tabBarEntries = original }()

	corrupted := make([]tabBarEntry, len(original))
	copy(corrupted, original)
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	for i := range corrupted {
		if m.tab(corrupted[i].tab) != nil {
			corrupted[i].label = "WRONG-" + corrupted[i].label
		}
	}
	tabBarEntries = corrupted

	m.active = tabs.Tasks
	m.tree.open = false
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})

	out := m.View()
	if strings.Contains(out, "WRONG-") {
		t.Fatalf("tab bar rendered tabBarEntries' hand-written label instead of the tab's own Title(), got:\n%s", out)
	}
	if want := "1 " + m.tab(tabs.Memory).Title(); !strings.Contains(out, want) {
		t.Fatalf("expected the bar to read Memory's own Title() (%q), got:\n%s", want, out)
	}
}

// TestAnUnimplementedSlotKeepsItsDeclaredLabel is the other half: a slot the
// bar declares but this build has no tab for still names its destination, so
// the bar reads the same whether or not the tab behind it exists yet.
func TestAnUnimplementedSlotKeepsItsDeclaredLabel(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.tree.open = false
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})

	if !strings.Contains(m.View(), "6 Graph") {
		t.Fatalf("a slot with no tab behind it should still name its destination, got:\n%s", m.View())
	}
}
