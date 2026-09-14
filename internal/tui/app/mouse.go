package app

import (
	"image"
	"strings"

	"github.com/HoracioEspinosa/engram/internal/tui/shared"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// hitbox is one tab bar slot and the half-open span of terminal columns it
// occupies on the bar's row.
type hitbox struct {
	x0, x1 int
	target tabs.ID
	// dashboard marks slot "0", which opens a screen of the root's own rather
	// than a tab, and so cannot be named by a tabs.ID.
	dashboard bool
}

// holds reports whether a column falls inside the slot.
func (h hitbox) holds(x int) bool { return x >= h.x0 && x < h.x1 }

// updateMouse answers a mouse event.
//
// A click lands on the tab bar or it lands on nothing: the bar is the only
// chrome with targets of its own. The wheel goes to the active tab and to no
// other — a notch delivered to all five would scroll four lists nobody is
// looking at, and the tab that is showing would be the only one whose new
// position the reader could see.
func (m Model) updateMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.showHelp || m.themePicker.open {
		// An overlay is modal: the screen underneath is not answering keys
		// while one is open, so it must not answer the pointer either.
		return m, nil
	}

	if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
		return m.clickTabBar(msg)
	}

	if _, ok := shared.WheelDelta(msg); ok {
		return m.wheelToActiveTab(msg)
	}

	return m, nil
}

// clickTabBar activates whichever slot the pointer is on. A click anywhere
// else leaves the workspace exactly as it was: the body underneath draws rows
// the root cannot address, and guessing at one would move a cursor the user
// did not aim at.
func (m Model) clickTabBar(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	row, boxes := m.tabBarHitboxes()
	if boxes == nil || msg.Y != row {
		return m, nil
	}

	for _, box := range boxes {
		if !box.holds(msg.X) {
			continue
		}
		if box.dashboard {
			if m.project == "" {
				// Nothing to show a dashboard for, the same way the "0" key
				// is swallowed rather than landing somewhere else.
				return m, nil
			}
			m.screen = screenDashboard
			return m, loadDashboard(m.projects, m.project)
		}
		return m.activate(box.target)
	}
	return m, nil
}

// wheelToActiveTab hands a wheel notch to the tab on display, in the tab's own
// coordinates.
//
// The frame is the root's to measure: a tab lays its panes out from its own
// top-left corner, so the pointer has to be translated before the tab can say
// which pane it fell in. A tab that had to know how tall the bar above it is
// would be reimplementing the root's layout, and would be wrong the moment the
// frame changed.
func (m Model) wheelToActiveTab(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.screen != screenTab {
		// The dashboard and the selector are single blocks of chrome with no
		// scrolling body of their own.
		return m, nil
	}
	tab := m.tab(m.active)
	if tab == nil {
		return m, nil
	}

	origin := m.bodyOrigin()
	local := msg
	local.X -= origin.X
	local.Y -= origin.Y
	if local.X < 0 || local.Y < 0 {
		// The pointer is on the frame itself, not on the tab.
		return m, nil
	}

	updated, cmd := tab.Update(local)
	return m.withTab(m.active, updated), cmd
}

// bodyOrigin is where the active tab's own View() starts inside the terminal:
// the app frame's padding, plus the tab bar and the blank row under it.
//
// It is measured from the rendered bar rather than counted from constants, so
// a bar that grows a row keeps the arithmetic honest.
func (m Model) bodyOrigin() image.Point {
	return image.Pt(
		m.styles.App.GetPaddingLeft(),
		m.styles.App.GetPaddingTop()+lipgloss.Height(m.viewTabBar()),
	)
}

// tabBarHitboxes maps the rendered tab bar back to the slots it draws: the row
// it sits on, and the span of columns each slot occupies.
//
// The spans are measured from the bar the frame actually draws rather than
// re-derived from the entry list, because a hitbox is by definition where the
// text ended up. A second derivation would agree with the render until the
// day a label changed, and then quietly send clicks to the wrong tab.
//
// boxes is nil whenever the bar is not on screen or the render no longer
// matches the slots it is supposed to draw: a click that resolves to nothing
// is the only safe answer to a bar this cannot read.
func (m Model) tabBarHitboxes() (row int, boxes []hitbox) {
	if m.screen == screenSelector || m.showHelp || m.themePicker.open {
		return 0, nil
	}

	lines := strings.Split(ansi.Strip(m.viewTabBar()), "\n")
	index := -1
	for i, line := range lines {
		if strings.TrimSpace(line) != "" {
			index = i
			break
		}
	}
	if index < 0 {
		return 0, nil
	}

	spans := SlotSpans(lines[index])
	if len(spans) != len(tabBarEntries) {
		return 0, nil
	}

	left := m.styles.App.GetPaddingLeft()
	boxes = make([]hitbox, 0, len(spans))
	for i, span := range spans {
		boxes = append(boxes, hitbox{
			x0:        left + span[0],
			x1:        left + span[1],
			target:    tabBarEntries[i].tab,
			dashboard: tabBarEntries[i].isDashboard,
		})
	}
	return m.styles.App.GetPaddingTop() + index, boxes
}

// SlotSpans finds the runs of text on one rendered row, as half-open spans of
// terminal cells.
//
// Two or more blank cells separate one slot from the next; the single space
// inside "0 Dashboard" does not. That is the same rule the eye applies when
// reading the bar, and it is why the golden lint can assert the separation is
// still there: a bar whose slots have run together reads as one word and
// clicks as one target.
//
// It is exported for the golden lint, which checks the same separation on a
// frozen render.
func SlotSpans(line string) [][2]int {
	blank := make([]bool, 0, len(line))
	for _, r := range line {
		width := ansi.StringWidth(string(r))
		for i := 0; i < width; i++ {
			blank = append(blank, r == ' ')
		}
	}

	var spans [][2]int
	i := 0
	for i < len(blank) {
		if blank[i] {
			i++
			continue
		}
		start := i
		for i < len(blank) {
			if !blank[i] {
				i++
				continue
			}
			if i+1 < len(blank) && !blank[i+1] {
				// One blank cell with text behind it is a word break inside
				// the slot, not the end of it.
				i += 2
				continue
			}
			break
		}
		spans = append(spans, [2]int{start, i})
		for i < len(blank) && blank[i] {
			i++
		}
	}
	return spans
}
