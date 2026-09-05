package shared

import (
	"strings"
	"time"

	"github.com/Gentleman-Programming/engram/internal/timeutil"
)

// dateLayout is the display form of a review deadline: the day, without a
// clock nobody acts on.
const dateLayout = "2006-01-02"

// LocalTime converts a UTC timestamp string from SQLite to the configured
// display timezone.
func LocalTime(utc string) string {
	return timeutil.FormatLocal(utc)
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

// Truncate flattens s to a single line and caps it at max runes, appending an
// ellipsis when it had to cut. Counting runes, not bytes, keeps accented and
// emoji titles from being sliced mid-character.
func Truncate(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "..."
}
