package shared

import (
	"strings"

	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/x/ansi"
)

// hintIndent aligns the footer with the two-space gutter every list row uses.
const hintIndent = "  "

// HintsFrom renders a screen's footer from the bindings it declares.
//
// A screen declares its keys once, in Help(), and the footer is derived from
// that declaration instead of being written out a second time by hand: a
// footer maintained separately drifts from the keys the screen answers to,
// and the reader trusts the footer.
//
// budget is the width in cells the footer may occupy. Hints are dropped from
// the end until the line fits, and an ellipsis says that more exist — the
// first hints are the ones a screen leads with, so they are the ones worth
// keeping. A budget of zero or less means no limit.
func HintsFrom(st theme.Styles, bindings []key.Binding, budget int) string {
	hints := make([]string, 0, len(bindings))
	for _, b := range bindings {
		h := b.Help()
		if !b.Enabled() || h.Key == "" || h.Desc == "" {
			continue
		}
		hints = append(hints, h.Key+" "+h.Desc)
	}
	if len(hints) == 0 {
		return ""
	}

	separator := st.Icons.HintSeparator()
	line := hintIndent + strings.Join(hints, separator)
	if budget > 0 && ansi.StringWidth(line) > budget {
		for len(hints) > 1 {
			hints = hints[:len(hints)-1]
			line = hintIndent + strings.Join(hints, separator) + separator + theme.Ellipsis
			if ansi.StringWidth(line) <= budget {
				break
			}
		}
		if len(hints) == 1 {
			line = Truncate(hintIndent+hints[0], budget)
		}
	}

	return st.Help.Render(line)
}

// PlainHintsFrom is HintsFrom's unstyled, unbudgeted half: the hints joined
// into one line, for a caller that does its own width arithmetic. The status
// bar needs the text before it can decide how much of it fits.
//
// It still takes the style set, because the separator between two hints is a
// glyph like any other and the terminal that cannot draw the rest cannot draw
// this one either.
func PlainHintsFrom(st theme.Styles, bindings []key.Binding) string {
	hints := make([]string, 0, len(bindings))
	for _, b := range bindings {
		h := b.Help()
		if !b.Enabled() || h.Key == "" || h.Desc == "" {
			continue
		}
		hints = append(hints, h.Key+" "+h.Desc)
	}
	return strings.Join(hints, st.Icons.HintSeparator())
}
