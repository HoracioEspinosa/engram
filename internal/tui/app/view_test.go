package app

import (
	"strconv"
	"strings"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs"
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

// graphRefs is a page of graph-linked observations, which is what the Graph
// tab draws one row each of.
func graphRefs(n int) []data.ObservationRef {
	refs := make([]data.ObservationRef, 0, n)
	for i := range n {
		refs = append(refs, data.ObservationRef{
			ObservationID: int64(i + 1),
			RefKind:       "file",
			Ref:           "internal/tui/app/view.go",
			GraphCommit:   "0123456789ab",
		})
	}
	return refs
}

// overflowingWorkspace is a sized workspace showing a screen taller than the
// terminal.
//
// The Graph tab is the one that draws every row it holds instead of windowing
// them, so it is what a frame that outgrows its terminal actually looks like
// in this build — no stub stands in for it.
func overflowingWorkspace(t *testing.T, width, height int) Model {
	t.Helper()

	graph := &data.FakeGraph{
		StateByProject: map[string]data.GraphState{"acme": {
			Nodes: 11482, Edges: 30671, Communities: 42,
			Commit: "0123456789abcdef", BuiltAt: "2026-01-15 09:30:00", CheckedAt: "2026-01-15 09:31:00",
		}},
		RefsByProject: map[string][]data.ObservationRef{"acme": graphRefs(60)},
	}

	m := New(&data.FakeMemory{}, &data.FakeProject{}, &data.FakeTask{}, &data.FakeEvidence{}, &data.FakeRunbook{},
		"test", theme.New(theme.KoiPond()), "acme").WithGraph(graph, graph)
	m.tree.open = false
	m.active = tabs.Graph

	sized, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	m = sized.(Model)

	loaded, _ := m.graph.Update(m.graph.Refresh()())
	return m.withTab(tabs.Graph, loaded)
}

// statusBarRow is the index of the frame's bottom line — the last row that
// carries anything, the app frame's own bottom padding excluded.
func statusBarRow(rows []string) int {
	for i := len(rows) - 1; i >= 0; i-- {
		if strings.TrimSpace(rows[i]) != "" {
			return i
		}
	}
	return -1
}

// TestTheBodyIsClippedToTheTerminalHeight: a screen taller than the terminal
// is cut at the bottom, by the frame, on purpose.
//
// Left alone it is cut at the TOP instead, by bubbletea's renderer, which
// discards the rows that do not fit in the order it writes them. The reader
// then loses the tab bar and the screen's own header and keeps the tail of a
// list, which reads as a different application rather than as a truncation.
// The frame is the only place that sees the bar, the body and the status bar
// together, so it is where the cut is made — and the cut says so, with the
// same mark every truncated field in this workspace carries.
func TestTheBodyIsClippedToTheTerminalHeight(t *testing.T) {
	const width, height = 80, 24

	m := overflowingWorkspace(t, width, height)
	frame := ansi.Strip(m.View())
	rows := strings.Split(frame, "\n")

	if len(rows) != height {
		t.Fatalf("a screen taller than the terminal renders %d rows, want the terminal's %d:\n%s",
			len(rows), height, frame)
	}

	bar := strings.TrimSpace(ansi.Strip(m.viewTabBar()))
	if !strings.Contains(rows[1], bar) {
		t.Fatalf("the second row carries no tab bar, so the renderer would drop it:\n%s", frame)
	}

	status := statusBarRow(rows)
	if status < 0 || !strings.HasSuffix(strings.TrimRight(rows[status], " "), m.styles.Palette.Name) {
		t.Fatalf("the bottom row carries no status bar:\n%s", frame)
	}

	last := strings.TrimRight(rows[status-1], " ")
	if !strings.HasSuffix(last, theme.Ellipsis) {
		t.Fatalf("the last row of the clipped body does not say it was cut:\n\t%q", last)
	}
}

// TestAnOverlayOverAClippedBodyKeepsItsOwnTop: the panel is centred on the
// terminal's extent, so a body that outgrew the screen must not push the top
// of it off the frame. What would go first is the panel's own border and
// title — the two things that say what the overlay is.
func TestAnOverlayOverAClippedBodyKeepsItsOwnTop(t *testing.T) {
	const width, height = 80, 24

	m := overflowingWorkspace(t, width, height)
	m.tree.open = true

	frame := strings.Split(ansi.Strip(m.View()), "\n")
	if len(frame) != height {
		t.Fatalf("an overlay over a clipped body renders %d rows, want the terminal's %d:\n%s",
			len(frame), height, strings.Join(frame, "\n"))
	}
	panelBounds(t, m, frame)
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
