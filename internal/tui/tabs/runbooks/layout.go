package runbooks

// Viewport arithmetic for the tab's scrolling screens, mirroring
// tabs/evidence/layout.go: Update and View must window the same rows through
// shared.VisibleItems with the constants below, or the cursor drifts out of
// the rendered window.
const (
	// runbookItemLines is the height of one index row (rfc-tui.md §5's S8
	// wireframe: one line per runbook).
	runbookItemLines = 1

	// minVisibleItems is the floor applied when the terminal is too short
	// for the arithmetic to yield a usable window.
	minVisibleItems = 3

	// indexChrome and viewChrome are what each screen spends on its header,
	// any banner above the list/body, and the frame's own rows: the tab bar
	// above and the one-line status bar below.
	indexChrome = 4
	viewChrome  = 7

	// pageSize is how many rows the page keys move by. store.ListRunbooks
	// defaults to 50 when Limit is unset, so paging by the same number keeps
	// one "page" meaning the same thing whether or not a limit was ever set.
	pageSize = 50
)

// Widths of the index row's fixed columns, in terminal cells.
const (
	runbookIDCells       = 8
	runbookTitleCells    = 40
	runbookProjectCells  = 12
	runbookCategoryCells = 14
	runbookPatternCells  = 12
)
