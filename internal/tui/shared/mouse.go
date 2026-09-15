package shared

import (
	"image"

	tea "github.com/charmbracelet/bubbletea"
)

// WheelLines is how many rows one notch of the wheel moves. It is
// bubbles/viewport's own default, so a list and a long-form body travel the
// same distance under the same gesture instead of scrolling at two speeds
// depending on which pane the pointer happens to be over.
const WheelLines = 3

// WheelDelta reads a mouse event as a vertical scroll: how many rows to move,
// and whether the event was a wheel notch at all.
//
// A terminal reports one notch as a press of a wheel button and may report a
// release with the same button behind it. Answering the press alone is what
// keeps a single notch from moving a list twice.
func WheelDelta(msg tea.MouseMsg) (rows int, ok bool) {
	if msg.Action != tea.MouseActionPress {
		return 0, false
	}
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		return -WheelLines, true
	case tea.MouseButtonWheelDown:
		return WheelLines, true
	}
	return 0, false
}

// PaneAt reports which pane of a master-detail layout holds a point, in the
// screen's own coordinates.
//
// ok is false for a point in neither pane — the gap between them, or anything
// past the area the layout was given — so a gesture aimed at nothing moves
// nothing. Defaulting to the master instead would scroll a list the pointer
// was never over.
func PaneAt(r Regions, p image.Point) (pane Pane, ok bool) {
	if p.In(r.Master) {
		return PaneMaster, true
	}
	if r.HasDetail() && p.In(r.Detail) {
		return PaneDetail, true
	}
	return PaneMaster, false
}
