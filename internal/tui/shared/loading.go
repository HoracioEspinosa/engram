package shared

import (
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
)

// Loading renders the one loading state every screen shows while a query is
// in flight.
//
// Each screen used to invent its own — "Loading evidence...", a bare
// spinner, an empty body that looked like a project with no data — so the
// same wait read as three different things depending on where the user was.
// what names the thing being fetched, in the screen's own words.
func Loading(st theme.Styles, sp spinner.Model, what string) string {
	mark := sp.View()
	if len(sp.Spinner.Frames) == 0 {
		// A screen with no throbber of its own still says the same thing;
		// the animation is the only part it lacks.
		mark = st.Icons.Glyph(theme.IconRefresh)
	}
	return st.NoResults.Render(mark + " loading " + what + theme.Ellipsis)
}

// Viewport renders content through bubbles/viewport at a given scroll
// offset.
//
// The three long-form bodies this workspace shows — a runbook's Markdown, a
// context pack, an observation's content — each sliced their own lines and
// each got the bounds subtly different. The viewport owns the windowing now;
// the screen keeps only the offset, which is what its keys already moved.
//
// The offset stays the caller's own int rather than viewport.Model's, so a
// screen's existing scroll keys keep working unchanged and the component can
// take over the key handling later without a flag day.
func Viewport(content string, width, height, offset int) string {
	if width < 1 || height < 1 {
		return content
	}
	vp := viewport.New(width, height)
	vp.SetContent(content)
	vp.SetYOffset(offset)
	return vp.View()
}
