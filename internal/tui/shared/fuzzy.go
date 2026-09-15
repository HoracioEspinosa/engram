package shared

import (
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/lipgloss"
)

// Match is one fuzzy hit: which target matched, and which of its runes the
// term matched against.
type Match struct {
	// Index is the position of the matching target inside the slice that was
	// filtered, so a caller can map a hit back to the value it came from.
	Index int
	// MatchedIndexes are the rune positions inside that target the term
	// matched, ascending. Highlight underlines exactly these.
	MatchedIndexes []int
}

// Fuzzy ranks targets against term.
//
// It wraps bubbles/list.DefaultFilter rather than reimplementing a matcher:
// the theme picker's "/" already filters through that function, so the tree
// overlay and the search palette rank the same way the picker does instead of
// each screen inventing its own notion of "close enough".
//
// An empty term matches everything, in the order it was given. DefaultFilter
// answers an empty term with no hits at all, which would make an unfiltered
// list look empty — the one behaviour a filter must never have.
func Fuzzy(term string, targets []string) []Match {
	if strings.TrimSpace(term) == "" {
		all := make([]Match, len(targets))
		for i := range targets {
			all[i] = Match{Index: i}
		}
		return all
	}

	ranks := list.DefaultFilter(term, targets)
	matches := make([]Match, 0, len(ranks))
	for _, rank := range ranks {
		matches = append(matches, Match{Index: rank.Index, MatchedIndexes: rank.MatchedIndexes})
	}
	return matches
}

// MatchedBy indexes a Fuzzy result by target position, so a caller that needs
// "did target i match, and where" does not scan the result once per row.
func MatchedBy(matches []Match) map[int][]int {
	by := make(map[int][]int, len(matches))
	for _, m := range matches {
		by[m.Index] = m.MatchedIndexes
	}
	return by
}

// Highlight renders s with the runes at matched drawn in hl and the rest in
// base.
//
// Runs of consecutive runes sharing a style are rendered together: one styled
// segment per run rather than one per rune, so a highlighted word costs two
// escape sequences instead of two per letter. With no matched indexes it is
// base.Render(s) and nothing else, which is what an unfiltered row draws.
//
// Both styles are stripped of their box model first. A style carrying padding,
// a margin or a fixed width is meant to frame a whole row, and applying it once
// per segment would pay that frame once per run — turning "clarodrive" into
// "cl  arod  rive" and blowing the cell budget a column was solved against.
// Highlight promises the opposite: the result occupies exactly the cells s did.
func Highlight(base, hl lipgloss.Style, s string, matched []int) string {
	base, hl = inline(base), inline(hl)
	if len(matched) == 0 {
		return base.Render(s)
	}

	hit := make(map[int]bool, len(matched))
	for _, i := range matched {
		hit[i] = true
	}

	var b strings.Builder
	runes := []rune(s)
	for start := 0; start < len(runes); {
		on := hit[start]
		end := start + 1
		for end < len(runes) && hit[end] == on {
			end++
		}
		segment := string(runes[start:end])
		if on {
			b.WriteString(hl.Render(segment))
		} else {
			b.WriteString(base.Render(segment))
		}
		start = end
	}
	return b.String()
}

// inline strips whatever would make a style occupy more cells than the text it
// wraps, leaving only the colour and weight that are safe to apply to a slice
// of a row.
func inline(s lipgloss.Style) lipgloss.Style {
	return s.UnsetPadding().UnsetMargins().UnsetWidth().UnsetHeight().UnsetAlign()
}
