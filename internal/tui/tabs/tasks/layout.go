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

// Caps and fixed costs of the tab's rows, in terminal cells. What a column
// actually gets is solved from the screen's width — see taskColumns.
const (
	taskKeyCells         = 12
	taskKindCells        = 9
	taskStateCells       = 18
	observationTypeCells = 10

	// taskRowFixed is what a list row spends outside its solved columns:
	// the cursor marker and the brackets around the kind badge.
	taskRowFixed = 4
	// detailObservationFixed and detailEvidenceFixed are the same reckoning
	// for the two lists inside the detail screen.
	detailObservationFixed = 32
	detailEvidenceFixed    = 4
	// detailBadgeCells is the width of the attached/unattached badge.
	detailBadgeCells = 11
	// minTitleCells is the narrowest title worth drawing.
	minTitleCells = 12

	// bodyMargin is what the app frame spends either side of a tab's body,
	// minBodyWidth the narrowest body worth laying out, and
	// defaultBodyWidth what a screen with no size yet assumes.
	bodyMargin       = 4
	minBodyWidth     = 24
	defaultBodyWidth = 80
)
