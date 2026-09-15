package app

import (
	"strconv"
	"strings"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/tui/tabs"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// mouseWorkspace is a sized root with no overlay open, which is where the tab
// bar is drawn and where the wheel has somewhere to go.
func mouseWorkspace(t *testing.T, width, height int) Model {
	t.Helper()

	m := New(nil, nil, nil, nil, nil, "test", theme.New(theme.KoiPond()), "acme")
	sized, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return sized.(Model)
}

func leftClick(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
}

func wheel(x, y int, button tea.MouseButton) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: button}
}

// TestClickingEachTabBarSlotActivatesIt walks every slot of the bar at both
// geometries. The spans come from the bar the frame actually draws, so this
// also fails if the bar ever renders slots the hitboxes cannot read.
func TestClickingEachTabBarSlotActivatesIt(t *testing.T) {
	for _, width := range []int{80, 120} {
		m := mouseWorkspace(t, width, 40)
		row, boxes := m.tabBarHitboxes()
		if len(boxes) != len(tabBarEntries) {
			t.Fatalf("width %d: %d hitboxes, want %d", width, len(boxes), len(tabBarEntries))
		}

		for i, box := range boxes {
			entry := tabBarEntries[i]
			t.Run(entry.digit+"@"+strconv.Itoa(width), func(t *testing.T) {
				next, _ := m.Update(leftClick(box.x0, row))
				got := next.(Model)

				if got.active != entry.tab {
					t.Fatalf("clicking %q activated %v, want %v", entry.digit, got.active, entry.tab)
				}
			})
		}
	}
}

// TestTabBarHitboxesCoverTheirOwnSlotText is the alignment check: every slot's
// span must hold that slot's digit on the rendered row, so a hitbox can never
// point at the gap beside the text it is supposed to cover.
func TestTabBarHitboxesCoverTheirOwnSlotText(t *testing.T) {
	m := mouseWorkspace(t, 120, 40)
	row, boxes := m.tabBarHitboxes()

	frame := strings.Split(ansi.Strip(m.View()), "\n")
	if row >= len(frame) {
		t.Fatalf("the bar is on row %d but the frame has %d rows", row, len(frame))
	}
	cells := []rune(frame[row])

	for i, box := range boxes {
		digit := tabBarEntries[i].digit
		slot := strings.TrimSpace(string(cells[box.x0:box.x1]))
		if !strings.Contains(slot, digit) {
			t.Fatalf("slot %d spans %q, which does not carry its digit %q", i, slot, digit)
		}
	}
}

// TestClickingOutsideTheTabBarChangesNothing covers the two ways a click can
// miss: the wrong row, and the gap between two slots on the right row.
func TestClickingOutsideTheTabBarChangesNothing(t *testing.T) {
	m := mouseWorkspace(t, 120, 40)
	m.active = tabs.Evidence
	row, boxes := m.tabBarHitboxes()

	gap := boxes[0].x1 // the first blank cell after slot "0"
	cases := map[string]tea.MouseMsg{
		"a row below the bar":       leftClick(boxes[1].x0, row+4),
		"the gap between two slots": leftClick(gap, row),
		"past the last slot":        leftClick(boxes[len(boxes)-1].x1+2, row),
	}

	for name, msg := range cases {
		t.Run(name, func(t *testing.T) {
			next, cmd := m.Update(msg)
			got := next.(Model)
			if got.active != tabs.Evidence {
				t.Fatalf("a click on %s moved the workspace to %v", name, got.active)
			}
			if cmd != nil {
				t.Fatalf("a click on %s issued a command", name)
			}
		})
	}
}

// TestClickingTheHomeSlotWithoutAProjectOpensHome: slot "0" is a tab like any
// other, so it answers a click whether or not a project has been resolved.
// Home without a project is the empty workspace, not a slot that does nothing.
func TestClickingTheHomeSlotWithoutAProjectOpensHome(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "test", theme.New(theme.KoiPond()), "")
	// New opens the project tree when it cannot resolve one; this case's
	// premise is the bar answering a click, not the overlay swallowing it.
	m.tree.open = false
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = sized.(Model)
	m.active = tabs.Memory

	row, boxes := m.tabBarHitboxes()
	next, _ := m.Update(leftClick(boxes[0].x0, row))
	if got := next.(Model); got.active != tabs.Home {
		t.Fatalf("active = %v, want Home", got.active)
	}
}

// TestTheTabBarHasNoHitboxesWhereItIsNotDrawn: every overlay is modal, so
// while one is up the bar underneath answers nothing.
func TestTheTabBarHasNoHitboxesWhereItIsNotDrawn(t *testing.T) {
	base := mouseWorkspace(t, 120, 40)

	overlays := map[string]func(Model) Model{
		"the project tree": func(m Model) Model { m.tree.open = true; return m },
		"the palette":      func(m Model) Model { m.palette.open = true; return m },
		"the theme picker": func(m Model) Model { m.themePicker.open = true; return m },
		"the help overlay": func(m Model) Model { m.showHelp = true; return m },
	}

	for name, open := range overlays {
		t.Run(name, func(t *testing.T) {
			if _, boxes := open(base).tabBarHitboxes(); boxes != nil {
				t.Fatalf("%s reported %d tab bar hitboxes", name, len(boxes))
			}
		})
	}
}

// TestAClickIsSwallowedWhileAnOverlayIsOpen mirrors the key handling: with the
// "?" overlay up, the screen underneath is not answering.
func TestAClickIsSwallowedWhileAnOverlayIsOpen(t *testing.T) {
	m := mouseWorkspace(t, 120, 40)
	row, boxes := m.tabBarHitboxes()
	m.showHelp = true
	m.active = tabs.Memory

	next, cmd := m.Update(leftClick(boxes[3].x0, row))
	got := next.(Model)
	if got.active != tabs.Memory {
		t.Fatalf("a click behind the overlay activated %v", got.active)
	}
	if !got.showHelp {
		t.Fatalf("a click behind the overlay closed it")
	}
	if cmd != nil {
		t.Fatalf("a click behind the overlay issued a command")
	}
}

// TestTheWheelReachesOnlyTheActiveTab is the routing rule: four tabs nobody is
// looking at must not scroll. Memory is left on its menu, whose cursor a wheel
// notch over the Tasks tab would otherwise move.
func TestTheWheelReachesOnlyTheActiveTab(t *testing.T) {
	m := mouseWorkspace(t, 120, 40)
	m.active = tabs.Tasks
	before := m.memory

	next, _ := m.Update(wheel(10, 10, tea.MouseButtonWheelDown))
	got := next.(Model)

	if got.memory.Cursor != before.Cursor {
		t.Fatalf("the Memory tab's cursor moved to %d while Tasks was active", got.memory.Cursor)
	}
}

// TestTheWheelIsInertBehindAnOverlay: an overlay is modal, so the tab it
// covers must not scroll under a notch aimed at the panel on top of it.
func TestTheWheelIsInertBehindAnOverlay(t *testing.T) {
	overlays := map[string]func(Model) Model{
		"the project tree": func(m Model) Model { m.tree.open = true; return m },
		"the palette":      func(m Model) Model { m.palette.open = true; return m },
		"the theme picker": func(m Model) Model { m.themePicker.open = true; return m },
		"the help overlay": func(m Model) Model { m.showHelp = true; return m },
	}

	for name, open := range overlays {
		t.Run(name, func(t *testing.T) {
			m := open(mouseWorkspace(t, 120, 40))
			m.active = tabs.Tasks

			next, cmd := m.Update(wheel(10, 10, tea.MouseButtonWheelDown))
			if got := next.(Model); got.tasks.Cursor != m.tasks.Cursor {
				t.Fatalf("a notch behind %s moved the Tasks cursor to %d", name, got.tasks.Cursor)
			}
			if cmd != nil {
				t.Fatalf("a notch behind %s issued a command", name)
			}
		})
	}
}

// TestTheWheelArrivesInTheTabsOwnCoordinates: a notch on the master pane's
// first column must reach the tab as column zero, not as the frame's column
// two, or every pane boundary would be off by the frame's padding.
func TestTheWheelArrivesInTheTabsOwnCoordinates(t *testing.T) {
	m := mouseWorkspace(t, 120, 40)
	origin := m.bodyOrigin()

	if origin.X != m.styles.App.GetPaddingLeft() {
		t.Fatalf("body origin x = %d, want the frame's left padding %d", origin.X, m.styles.App.GetPaddingLeft())
	}

	frame := strings.Split(ansi.Strip(m.View()), "\n")
	row, _ := m.tabBarHitboxes()
	if origin.Y <= row {
		t.Fatalf("body origin y = %d, which is on or above the tab bar at row %d", origin.Y, row)
	}
	if origin.Y >= len(frame) {
		t.Fatalf("body origin y = %d is past the %d-row frame", origin.Y, len(frame))
	}
}

// TestAWheelNotchOnTheFrameItselfIsInert: the padding above and left of the
// body belongs to no pane.
func TestAWheelNotchOnTheFrameItselfIsInert(t *testing.T) {
	m := mouseWorkspace(t, 120, 40)
	m.active = tabs.Tasks

	next, cmd := m.Update(wheel(0, 0, tea.MouseButtonWheelDown))
	if next.(Model).tasks.Cursor != m.tasks.Cursor {
		t.Fatalf("a notch on the frame moved the Tasks cursor")
	}
	if cmd != nil {
		t.Fatalf("a notch on the frame issued a command")
	}
}

// TestSlotSpansKeepsAWordBreakInsideItsSlot: "0 Dashboard" is one slot, and
// the two blanks after it are the separation the bar declares.
func TestSlotSpansKeepsAWordBreakInsideItsSlot(t *testing.T) {
	spans := SlotSpans("0 Dashboard  [1] Memory  2 Tasks")
	if len(spans) != 3 {
		t.Fatalf("SlotSpans found %d slots, want 3: %v", len(spans), spans)
	}
	if got := "0 Dashboard"; spans[0][1]-spans[0][0] != len(got) {
		t.Fatalf("the first slot spans %d cells, want %d", spans[0][1]-spans[0][0], len(got))
	}
}

// TestTheHelpOverlayNamesTheMouse: enabling the mouse costs the terminal's own
// selection, so the gesture that gets it back has to be written down.
func TestTheHelpOverlayNamesTheMouse(t *testing.T) {
	m := mouseWorkspace(t, 120, 40)
	m.showHelp = true

	out := ansi.Strip(m.View())
	if !strings.Contains(out, nativeSelection.Help().Key) || !strings.Contains(out, nativeSelection.Help().Desc) {
		t.Fatalf("the help overlay does not name the native selection gesture:\n%s", out)
	}
	if got := nativeSelection.Keys(); len(got) != 1 || got[0] != "shift+drag" {
		t.Fatalf("the gesture is spelled %v, want shift+drag", got)
	}
}

// TestTheHelpGridDoesNotGrowForTheMouse: the overlay is composited over the
// screen it describes, so a row or a column it grows by is part of that screen
// the reader loses. The gesture has to fit the widths the grid already has.
func TestTheHelpGridDoesNotGrowForTheMouse(t *testing.T) {
	help := nativeSelection.Help()

	var widestKey, widestDesc int
	for _, b := range globalHelpBindings() {
		if b.Help().Key == help.Key && b.Help().Desc == help.Desc {
			continue
		}
		widestKey = max(widestKey, ansi.StringWidth(b.Help().Key))
		widestDesc = max(widestDesc, ansi.StringWidth(b.Help().Desc))
	}

	if w := ansi.StringWidth(help.Key); w > widestKey {
		t.Fatalf("the gesture's key is %d cells, wider than the %d the grid already spends", w, widestKey)
	}
	if w := ansi.StringWidth(help.Desc); w > widestDesc {
		t.Fatalf("the gesture's description is %d cells, wider than the %d the grid already spends", w, widestDesc)
	}
}
