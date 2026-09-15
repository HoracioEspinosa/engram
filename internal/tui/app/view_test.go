package app

import (
	"strconv"
	"strings"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// overlayOpeners is every panel the root composes over the body, by the field
// that raises it. They are listed together because the frame owes all four the
// same treatment: an overlay is a panel on top of the workspace, never a
// screen that replaces it.
var overlayOpeners = map[string]func(Model) Model{
	"the theme picker": func(m Model) Model { m.themePicker.open = true; return m },
	"the palette":      func(m Model) Model { m.palette.open = true; return m },
	"the project tree": func(m Model) Model { m.tree.open = true; return m },
	"the help overlay": func(m Model) Model { m.showHelp = true; return m },
}

// overlayGeometries are the two terminals the golden scenes are frozen at.
var overlayGeometries = []struct{ width, height int }{{80, 24}, {120, 40}}

// overlayWorkspace is a sized workspace with no overlay raised yet.
func overlayWorkspace(t *testing.T, width, height int) Model {
	t.Helper()

	m := New(&data.FakeMemory{}, &data.FakeProject{}, &data.FakeTask{}, &data.FakeEvidence{}, &data.FakeRunbook{},
		"test", theme.New(theme.KoiPond()), "acme")
	// New raises the tree when it cannot resolve a project; these cases raise
	// the overlay they are about themselves.
	m.tree.open = false
	sized, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	return sized.(Model)
}

// lastInkedRow is the bottom row of the frame that carries anything, which is
// where the status bar lives.
func lastInkedRow(frame string) string {
	lines := strings.Split(frame, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return strings.TrimRight(lines[i], " ")
		}
	}
	return ""
}

// TestOverlayKeepsTheStatusBarVisible: the status bar belongs to the frame,
// not to the screen underneath. An overlay covers the body it describes; it
// does not take the frame's own context — the project, the task, the sync
// state — off the screen with it.
//
// The hints an overlay replaces are its own business: whatever holds the
// keyboard names its keys there. What the bar must never lose is the context
// around them, so the two segments checked are the ones that bracket it — the
// project on the left, the palette on the right.
func TestOverlayKeepsTheStatusBarVisible(t *testing.T) {
	for _, size := range overlayGeometries {
		for name, open := range overlayOpeners {
			t.Run(name, func(t *testing.T) {
				m := open(overlayWorkspace(t, size.width, size.height))
				row := lastInkedRow(ansi.Strip(m.View()))

				if !strings.HasSuffix(row, m.styles.Palette.Name) {
					t.Fatalf("%dx%d: with %s open the bottom row carries no status bar:\n\t%q",
						size.width, size.height, name, row)
				}
				if !strings.Contains(row, m.project) {
					t.Fatalf("%dx%d: with %s open the bottom row has lost the project:\n\t%q",
						size.width, size.height, name, row)
				}
			})
		}
	}
}

// TestOverlayIsCenteredOnTheTerminalExtent: a panel centred on whatever the
// tab underneath happened to draw drifts with every screen it opens over, and
// on a short one it lands on the tab bar and the status bar — the two rows the
// reader needs to keep seeing. The terminal is the extent it is centred on, so
// the gaps either side of the panel match, and so do the rows above and below.
func TestOverlayIsCenteredOnTheTerminalExtent(t *testing.T) {
	for _, size := range overlayGeometries {
		for name, open := range overlayOpeners {
			t.Run(name, func(t *testing.T) {
				m := open(overlayWorkspace(t, size.width, size.height))
				frame := strings.Split(ansi.Strip(m.View()), "\n")

				if len(frame) != size.height {
					t.Fatalf("%dx%d: %s renders %d rows, want the terminal's %d",
						size.width, size.height, name, len(frame), size.height)
				}

				top, bottom, left, right := panelBounds(t, m, frame)
				if gap := top - (len(frame) - 1 - bottom); gap < -1 || gap > 1 {
					t.Fatalf("%dx%d: %s sits %d rows from the top and %d from the bottom",
						size.width, size.height, name, top, len(frame)-1-bottom)
				}
				if gap := left - (size.width - 1 - right); gap < -1 || gap > 1 {
					t.Fatalf("%dx%d: %s starts at column %d and ends at %d, which is not centred",
						size.width, size.height, name, left, right)
				}
			})
		}
	}
}

// TestAnOverlayNeverGrowsPastTheTerminal: an overlay is a panel on top of the
// workspace, so a list inside one that drew every row it holds would push the
// frame past the bottom of the screen and take the status bar with it. The
// window it draws instead always holds the cursor.
func TestAnOverlayNeverGrowsPastTheTerminal(t *testing.T) {
	for _, size := range overlayGeometries {
		m := overlayWorkspace(t, size.width, size.height)
		m.palette.open = true
		m.palette.input.SetValue("hit")
		for i := range 60 {
			m.palette.rows = append(m.palette.rows, paletteRow{
				hit: &data.SearchHit{Title: "hit " + strconv.Itoa(i), Project: "acme"},
			})
		}
		m.palette.cursor = len(m.palette.rows) - 1

		frame := ansi.Strip(m.View())
		if got := len(strings.Split(frame, "\n")); got != size.height {
			t.Fatalf("%dx%d: a full palette renders %d rows, want the terminal's %d:\n%s",
				size.width, size.height, got, size.height, frame)
		}
		if !strings.Contains(frame, "hit "+strconv.Itoa(len(m.palette.rows)-1)) {
			t.Fatalf("%dx%d: the window dropped the row the cursor is on:\n%s", size.width, size.height, frame)
		}
		if !strings.HasSuffix(lastInkedRow(frame), m.styles.Palette.Name) {
			t.Fatalf("%dx%d: a full palette pushed the status bar off the frame:\n%s", size.width, size.height, frame)
		}
	}
}

// panelBounds finds the overlay panel inside a rendered frame by the corners
// its own border style draws, so no glyph is spelled out here.
func panelBounds(t *testing.T, m Model, frame []string) (top, bottom, left, right int) {
	t.Helper()

	border := m.styles.Panel.GetBorderStyle()
	top, bottom, left, right = -1, -1, -1, -1
	for row, line := range frame {
		start := strings.Index(line, border.TopLeft)
		if start >= 0 {
			top, left = row, ansi.StringWidth(line[:start])
		}
		if start := strings.Index(line, border.BottomLeft); start >= 0 {
			bottom = row
			right = ansi.StringWidth(line[:start]) + ansi.StringWidth(strings.TrimRight(line[start:], " ")) - 1
		}
	}
	if top < 0 || bottom < 0 {
		t.Fatalf("no panel border in the frame:\n%s", strings.Join(frame, "\n"))
	}
	return top, bottom, left, right
}
