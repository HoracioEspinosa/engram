package app

import (
	"strings"

	"github.com/HoracioEspinosa/engram/internal/tui/tabs"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"
)

// tabBarBreakpoint is the width rfc-tui.md §5 fixes for the persistent tab
// bar: below it a slot shows its glyph and its digit and nothing else. At or
// above it, every slot shows its label too.
const tabBarBreakpoint = 100

// tabBarEntry names one slot of the bar, in the fixed order §6.9 draws it.
type tabBarEntry struct {
	digit string
	label string
	icon  theme.Icon
	tab   tabs.ID
}

// tabBarEntries is the bar's content. It never changes at runtime — unlike
// registered in model.go, which only lists the tabs this build implements,
// this always shows all eight slots: a build with a tab missing still owns a
// digit for it (§6.9 fixes "0"…"7" to specific tabs, not to whichever ones
// happen to be wired up).
//
// label is a fallback, not the label a registered tab actually shows:
// tabBarLabel prefers tabs.Tab.Title() (rfc-tui.md §4.3: "label shown in the
// tab bar") whenever this build has that tab wired up, so Title() stays the
// one place a tab's name is spelled. label only surfaces for a tab this build
// does not register at all.
var tabBarEntries = []tabBarEntry{
	{digit: "0", label: "Home", icon: theme.IconTabHome, tab: tabs.Home},
	{digit: "1", label: "Memory", icon: theme.IconTabMemory, tab: tabs.Memory},
	{digit: "2", label: "Tasks", icon: theme.IconTabTasks, tab: tabs.Tasks},
	{digit: "3", label: "Evidence", icon: theme.IconTabEvidence, tab: tabs.Evidence},
	{digit: "4", label: "Benchmarks", icon: theme.IconTabBenchmarks, tab: tabs.Benchmarks},
	{digit: "5", label: "Runbooks", icon: theme.IconTabRunbooks, tab: tabs.Runbooks},
	{digit: "6", label: "Graph", icon: theme.IconTabGraph, tab: tabs.Graph},
	{digit: "7", label: "Settings", icon: theme.IconTabSettings, tab: tabs.Settings},
}

// tabBarLabel resolves the text one bar slot shows: the registered tab's own
// Title(), and the entry's literal label as a last resort, for a build that
// does not implement that tab at all.
func (m Model) tabBarLabel(e tabBarEntry) string {
	if tab := m.tab(e.tab); tab != nil {
		return tab.Title()
	}
	return e.label
}

// activeTabBarDigit reports which slot is active, or "" for a tab that owns
// no slot of its own.
func (m Model) activeTabBarDigit() string {
	for _, e := range tabBarEntries {
		if e.tab == m.active {
			return e.digit
		}
	}
	return ""
}

// viewTabBar renders the persistent tab bar. A Model that never received a
// tea.WindowSizeMsg (width at its zero value, as in a test that never sends
// one) renders full rather than guessing narrow: most terminals are wider
// than the breakpoint, and a real program always sends its size before the
// first paint.
func (m Model) viewTabBar() string {
	active := m.activeTabBarDigit()
	compact := m.width > 0 && m.width < tabBarBreakpoint

	parts := make([]string, 0, len(tabBarEntries))
	for _, e := range tabBarEntries {
		text := m.styles.Icons.Glyph(e.icon) + e.digit
		if !compact {
			text += " " + m.tabBarLabel(e)
		}
		if e.digit == active {
			parts = append(parts, m.styles.TabActive.Render("["+text+"]"))
		} else {
			parts = append(parts, m.styles.TabInactive.Render(text))
		}
	}

	// The sync state used to be appended here; it lives in the status bar
	// now, where it is one segment among the rest of the frame's context
	// instead of an afterthought hanging off the last tab.
	return m.styles.TabBar.Render(strings.Join(parts, " "))
}
