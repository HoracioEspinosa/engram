package shared

import (
	"image"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestWheelDeltaReadsBothDirections(t *testing.T) {
	up, ok := WheelDelta(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp})
	if !ok || up != -WheelLines {
		t.Fatalf("wheel up = (%d, %v), want (%d, true)", up, ok, -WheelLines)
	}

	down, ok := WheelDelta(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	if !ok || down != WheelLines {
		t.Fatalf("wheel down = (%d, %v), want (%d, true)", down, ok, WheelLines)
	}
}

// TestWheelDeltaIgnoresEverythingButAWheelPress is what keeps one notch from
// moving a list twice: a terminal reports the notch as a press, and the
// release that may follow carries the same button.
func TestWheelDeltaIgnoresEverythingButAWheelPress(t *testing.T) {
	cases := []struct {
		name string
		msg  tea.MouseMsg
	}{
		{"left press", tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}},
		{"wheel release", tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonWheelDown}},
		{"wheel motion", tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonWheelUp}},
		{"horizontal wheel", tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelLeft}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if rows, ok := WheelDelta(tc.msg); ok || rows != 0 {
				t.Fatalf("WheelDelta = (%d, %v), want (0, false)", rows, ok)
			}
		})
	}
}

func TestPaneAtResolvesASplitLayout(t *testing.T) {
	r := Layout(160, 40)
	if !r.HasDetail() {
		t.Fatalf("Layout(160, 40) has no detail pane; the split breakpoint moved")
	}

	if pane, ok := PaneAt(r, image.Pt(1, 1)); !ok || pane != PaneMaster {
		t.Fatalf("point inside the master = (%v, %v), want (master, true)", pane, ok)
	}
	if pane, ok := PaneAt(r, image.Pt(r.Detail.Min.X+1, 1)); !ok || pane != PaneDetail {
		t.Fatalf("point inside the detail = (%v, %v), want (detail, true)", pane, ok)
	}
}

// TestPaneAtRejectsAPointOutsideBothPanes covers the gap between the panes
// and the rows past the layout: a gesture aimed at nothing must move nothing
// rather than defaulting to the master.
func TestPaneAtRejectsAPointOutsideBothPanes(t *testing.T) {
	r := Layout(160, 40)

	if _, ok := PaneAt(r, image.Pt(r.Master.Max.X, 1)); ok {
		t.Fatalf("the gap between the panes resolved to a pane")
	}
	if _, ok := PaneAt(r, image.Pt(1, r.Master.Max.Y)); ok {
		t.Fatalf("a row below the layout resolved to a pane")
	}
	if _, ok := PaneAt(r, image.Pt(-1, 1)); ok {
		t.Fatalf("a negative column resolved to a pane")
	}
}

// TestPaneAtBelowTheSplitHasOnlyAMaster is the single-column case: every point
// the layout covers belongs to the list, and there is no detail rectangle to
// fall into.
func TestPaneAtBelowTheSplitHasOnlyAMaster(t *testing.T) {
	r := Layout(90, 24)
	if r.HasDetail() {
		t.Fatalf("Layout(90, 24) grew a detail pane")
	}
	pane, ok := PaneAt(r, image.Pt(89, 23))
	if !ok || pane != PaneMaster {
		t.Fatalf("PaneAt = (%v, %v), want (master, true)", pane, ok)
	}
}
