package shared

import (
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/HoracioEspinosa/engram/internal/timeutil"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"
)

// dateLayout is the display form of a review deadline: the day, without a
// clock nobody acts on.
const dateLayout = "2006-01-02"

// LocalTime converts a UTC timestamp string from SQLite to the configured
// display timezone.
func LocalTime(utc string) string {
	return timeutil.FormatLocal(utc)
}

// LocalDate is LocalTime without the clock — the form a narrow row falls
// back to when the full timestamp no longer leaves the title room to say
// anything.
func LocalDate(utc string) string {
	return FormatReviewDate(LocalTime(utc))
}

// FormatReviewDate reduces a stored review deadline to its calendar day. Input
// that parses in none of the accepted layouts is returned truncated to its
// first ten characters, or unchanged when it is shorter than a date.
func FormatReviewDate(value string) string {
	trimmed := strings.TrimSpace(value)
	formats := []string{"2006-01-02 15:04:05", time.RFC3339, time.RFC3339Nano, dateLayout}
	for _, layout := range formats {
		if parsed, err := time.Parse(layout, trimmed); err == nil {
			return parsed.Format(dateLayout)
		}
	}
	if len(trimmed) >= len(dateLayout) {
		return trimmed[:len(dateLayout)]
	}
	return trimmed
}

// Truncate flattens s to a single line and caps it at max terminal cells,
// marking the cut with an ellipsis that is itself paid for out of the budget.
//
// Cells, not runes: an ideograph and an emoji each occupy two columns while an
// SGR sequence occupies none, so a rune count either overflows the column it
// was meant to fit or leaves a ragged edge. The result never exceeds max cells
// and never ends mid-character or mid-escape.
func Truncate(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	return ansi.Truncate(s, max, theme.Ellipsis)
}
