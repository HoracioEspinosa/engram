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
	// header, any banner above the list/body, and the frame's own rows: the
	// tab bar above and the one-line status bar below.
	listChrome   = 3
	detailChrome = 13

	// pageSize is how many rows the page keys move by. store.ListEvidence
	// defaults to 50 when Limit is unset, so paging by the same number keeps
	// one "page" meaning the same thing whether or not a limit was ever set.
	pageSize = 50
)

// Caps and fixed costs of the list row, in terminal cells. The name and the
// "proves" note are solved from the screen's width instead — see
// evidenceColumns.
const (
	evidenceKindCells     = 4
	evidenceTaskCells     = 12
	evidenceAttachedCells = 6

	// evidenceRowFixed is what a row spends outside its solved columns: the
	// cursor marker and the brackets around the kind badge.
	evidenceRowFixed = 4
	// detailCardFrame is what the detail card's border, padding and indent
	// spend before its own text starts, and detailLabelCells what the label
	// column of each row inside it takes.
	detailCardFrame  = 10
	detailLabelCells = 22

	// bodyMargin is what the app frame spends either side of a tab's body.
	bodyMargin = 4
	// minBodyWidth is the narrowest body worth laying out, and
	// defaultBodyWidth what a screen with no size yet assumes.
	minBodyWidth     = 24
	defaultBodyWidth = 80
)
