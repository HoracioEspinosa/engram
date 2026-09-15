package shared

import "github.com/HoracioEspinosa/engram/internal/tui/theme"

// Severity ranks a notice by what it asks of the reader.
type Severity int

const (
	// SeverityInfo confirms something that worked: a path copied, a file
	// written.
	SeverityInfo Severity = iota
	// SeverityWarning reports a degraded result the screen still rendered.
	SeverityWarning
	// SeverityError reports a request that did not happen at all.
	SeverityError
)

// Notice is a transient line a screen shows under its body.
//
// Screens used to concatenate their own: an error prefixed by hand here, a
// confirmation styled differently there, and no way for the reader to tell a
// failed write from a stale value. A notice carries its severity, so what it
// looks like follows from what it means.
type Notice struct {
	Severity Severity
	Text     string
	// Dismissable says the screen answers a key that clears this notice, so
	// the line can say so. A notice the next reload replaces on its own does
	// not need the hint.
	Dismissable bool
}

// Errorf is the notice a failed command produces.
func Error(text string) Notice { return Notice{Severity: SeverityError, Text: text} }

// Info is the notice a completed action produces.
func Info(text string) Notice { return Notice{Severity: SeverityInfo, Text: text} }

// Warn is the notice a degraded result produces.
func Warn(text string) Notice { return Notice{Severity: SeverityWarning, Text: text} }

// Empty reports whether there is nothing to draw.
func (n Notice) Empty() bool { return n.Text == "" }

// Render draws the notice, prefixed by the glyph its severity maps to.
func (n Notice) Render(st theme.Styles) string {
	if n.Empty() {
		return ""
	}

	var (
		icon  theme.Icon
		style = st.Notice
	)
	switch n.Severity {
	case SeverityError:
		icon, style = theme.IconTaskCancelled, st.Error
	case SeverityWarning:
		icon, style = theme.IconStale, st.StateWarningBadge
	default:
		icon, style = theme.IconFresh, st.Notice
	}

	text := st.Icons.Glyph(icon) + " " + n.Text
	if n.Dismissable {
		text += "  (esc)"
	}
	return style.Render(text)
}
