package memory

// Viewport arithmetic for the tab's scrolling lists.
//
// Update and View must window the same rows, so both compute their row count
// through shared.VisibleItems with the constants below. Changing a screen's
// header or footer means changing its chrome value here, once.
const (
	// observationItemLines is the height of a two-line observation row:
	// identity line plus content preview.
	observationItemLines = 2
	// sessionItemLines is the height of a session row.
	sessionItemLines = 1

	// minVisibleItems and minVisibleSessions are the floors applied when the
	// terminal is too short for the arithmetic to yield a usable window.
	minVisibleItems    = 3
	minVisibleSessions = 5

	// chrome is the number of lines each screen spends on its header, help
	// footer and range indicator.
	searchResultsChrome = 10
	recentChrome        = 8
	sessionsChrome      = 8
	sessionDetailChrome = 12
	// detailChrome is what the observation detail spends on its metadata rows
	// before the content pane starts.
	detailChrome = 16
	// minDetailLines is the floor for the content pane.
	minDetailLines = 5
	// detailWrapMargin is the horizontal padding reserved around wrapped
	// content, and minDetailWrap the narrowest wrap width worth rendering.
	detailWrapMargin = 6
	minDetailWrap    = 20
)
