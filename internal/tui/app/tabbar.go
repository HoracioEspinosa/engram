package app

import (
	"strings"

	"github.com/HoracioEspinosa/engram/internal/tui/tabs"
)

// tabBarBreakpoint is the width rfc-tui.md §5 fixes for the persistent tab
// bar: "por debajo de 100 columnas... la barra de pestañas muestra solo
// 0 1 2 3 4 5". At or above it, every slot shows its digit and label.
const tabBarBreakpoint = 100

// tabBarEntry names one slot of the bar, in the fixed order rfc-tui.md §5's
// wireframes draw it: Dashboard first, then the five tabs.
type tabBarEntry struct {
	digit       string
	label       string
	isDashboard bool
	tab         tabs.ID
}

// tabBarEntries is the bar's content. It never changes at runtime — unlike
// registered in model.go, which only lists the tabs this build implements,
// this always shows all six slots: a build with a tab missing still owns a
// digit for it (rfc-tui.md §7.1 fixes "1"…"5" to specific tabs, not to
// whichever ones happen to be wired up).
//
// label is a fallback, not the label a registered tab actually shows:
// tabBarLabel prefers tabs.Tab.Title() (rfc-tui.md §4.3: "label shown in the
// tab bar") whenever this build has that tab wired up, so Title() stays the
// one place a tab's name is spelled. label only surfaces for the Dashboard
// slot, which has no Tab of its own, and for a tab this build does not
// register at all.
var tabBarEntries = []tabBarEntry{
	{digit: "0", label: "Dashboard", isDashboard: true},
	{digit: "1", label: "Memory", tab: tabs.Memory},
	{digit: "2", label: "Tasks", tab: tabs.Tasks},
	{digit: "3", label: "Evidence", tab: tabs.Evidence},
	{digit: "4", label: "Runbooks", tab: tabs.Runbooks},
	{digit: "5", label: "Cloud", tab: tabs.Cloud},
}

// tabBarLabel resolves the text one bar slot shows: the Dashboard's literal
// label, since it is not a tabs.Tab and so has no Title() to read; the
// registered tab's own Title() otherwise; and the entry's literal label as a
// last resort, for a build that does not implement that tab at all.
func (m Model) tabBarLabel(e tabBarEntry) string {
	if e.isDashboard {
		return e.label
	}
	if tab := m.tab(e.tab); tab != nil {
		return tab.Title()
	}
	return e.label
}

// activeTabBarDigit reports which slot is active: "0" while the Project
// Dashboard is showing, the active tab's digit otherwise. Every tab screen's
// wireframe (S3…S11) brackets its own slot; S2's happens to omit the
// bracket in the RFC's hand-drawn ASCII, which this treats as an oversight
// in the artwork rather than a rule, for the same reason every other slot
// gets one: the active entry is the active entry regardless of which one it
// is.
func (m Model) activeTabBarDigit() string {
	if m.screen == screenDashboard {
		return "0"
	}
	for _, e := range tabBarEntries {
		if !e.isDashboard && e.tab == m.active {
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
		text := e.digit
		if !compact {
			text = e.digit + " " + m.tabBarLabel(e)
		}
		if e.digit == active {
			parts = append(parts, m.styles.TabActive.Render("["+text+"]"))
		} else {
			parts = append(parts, m.styles.TabInactive.Render(text))
		}
	}

	bar := strings.Join(parts, "  ")
	if !compact {
		if status := m.statusText(); status != "" {
			bar += "  " + m.styles.Help.Render(status)
		}
	}
	return m.styles.TabBar.Render(bar)
}
