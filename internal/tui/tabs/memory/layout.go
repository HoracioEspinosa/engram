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

	// chrome is the number of lines each screen spends on its header, its
	// range indicator, and the frame's own rows: the tab bar above and the
	// one-line status bar below.
	searchResultsChrome = 9
	recentChrome        = 7
	sessionsChrome      = 7
	sessionDetailChrome = 11
	// detailChrome is what the observation detail spends on its metadata rows
	// before the content pane starts.
	detailChrome = 15
	// minDetailLines is the floor for the content pane.
	minDetailLines = 5
	// detailWrapMargin is the horizontal padding reserved around wrapped
	// content, and minDetailWrap the narrowest wrap width worth rendering.
	detailWrapMargin = 6
	minDetailWrap    = 20

	// memoryPageSize is how many rows one page of search results or recent
	// observations holds — the same 50 both screens already asked for before
	// either of them could page.
	memoryPageSize = 50
	// sessionPageSize is how many sessions the list reads. Sessions are
	// coarser than observations, so fifty of them already spans weeks.
	sessionPageSize = 50
)

// Widths of the tab's fixed columns, in terminal cells.
const (
	timelineTypeCells   = 12
	sessionProjectCells = 20

	// timelineFocusFrame is what the focus card's border and padding spend
	// before its own text starts.
	timelineFocusFrame = 8
	// sessionRowFixed is what a session row spends outside its four solved
	// columns: the cursor marker and the three separators.
	sessionRowFixed = 8
	// timelineRowFixed is what a timeline row spends before its title: the
	// indent, the connector, the id and the spaces between them.
	timelineRowFixed = 14
	// minTimelineTitle is the narrowest title worth drawing on that row.
	minTimelineTitle = 12

	// bodyMargin is what the app frame spends either side of a tab's body:
	// two cells of padding on each edge.
	bodyMargin = 4
	// minBodyWidth is the narrowest body worth laying out.
	minBodyWidth = 24
	// defaultBodyWidth is what a screen that never learned its size assumes.
	defaultBodyWidth = 80
)
