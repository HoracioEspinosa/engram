package tasks

// Viewport arithmetic for the tab's scrolling lists, mirroring
// tabs/memory/layout.go: Update and View must window the same rows through
// shared.VisibleItems with the constants below, or the cursor drifts out of
// the rendered window.
const (
	// taskItemLines is the height of one two-line task row: identity line
	// plus its branch/PR/counters preview.
	taskItemLines = 2
	// observationItemLines is the height of one linked-observation row
	// inside the detail screen.
	observationItemLines = 1

	// minVisibleItems is the floor applied when the terminal is too short
	// for the arithmetic to yield a usable window.
	minVisibleItems = 3

	// listChrome, detailChrome and contextPackChrome are what each screen
	// spends on its header, any banner above the list/body, and the frame's
	// own rows: the tab bar above and the one-line status bar below.
	listChrome        = 5
	detailChrome      = 13
	contextPackChrome = 4
)

// Widths of the tab's fixed columns, in terminal cells.
const (
	taskKeyCells         = 12
	taskKindCells        = 9
	observationTypeCells = 10
)
