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
	// help footer and any banner above the list/body.
	indexChrome = 5
	viewChrome  = 8
)
