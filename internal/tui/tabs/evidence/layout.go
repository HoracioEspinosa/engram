package evidence

// Viewport arithmetic for the list's scrolling, mirroring
// tabs/tasks/layout.go: Update and View must window the same rows through
// shared.VisibleItems with the constants below, or the cursor drifts out of
// the rendered window.
const (
	// evidenceItemLines is the height of one list row (rfc-tui.md §5's S6
	// wireframe: one line per file, unlike Tasks' two-line rows).
	evidenceItemLines = 1

	// minVisibleItems is the floor applied when the terminal is too short
	// for the arithmetic to yield a usable window.
	minVisibleItems = 3

	// listChrome and detailChrome are what each screen spends on its
	// header, help footer and any banner above the list/body.
	listChrome   = 4
	detailChrome = 14
)
