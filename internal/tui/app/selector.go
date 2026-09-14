package app

import (
	"fmt"
	"sort"
	"strings"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/shared"

	"github.com/charmbracelet/bubbles/spinner"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// Caps and fixed costs of the selector table, in terminal cells. What each
// column actually gets is solved from the screen's width — see
// selectorColumns — so the header row and the data rows, which both call it,
// cannot drift apart.
const (
	selectorSlugCells  = 22
	selectorCountCells = 6
	selectorStaleCells = 8

	// selectorRowFixed is what a row spends outside its solved columns: the
	// cursor marker.
	selectorRowFixed = 2
)

// selectorModel is the Project Selector (S1, rfc-tui.md §5.S1): every live
// project card, filterable by slug or display name, each row showing the
// RFC §3.1 health counters the S1 wireframe puts in the list itself
// (store.ListProjectCards' includeCounts=true) so opening the Selector never
// costs one query per row.
//
// It is a plain value, like memory.Model and cloud.Model, even though it is
// not a tabs.Tab: the root shows it as a screen of its own (screenSelector),
// not as a tab in the bar.
type selectorModel struct {
	reader data.ProjectReader

	cards    []store.ProjectCardListItem
	filtered []store.ProjectCardListItem
	cursor   int

	filterInput textinput.Model

	// healthSort toggles rfc-tui.md §7.2's "i invertir orden (salud
	// primero)": false keeps the store's natural (updated_at) order, true
	// ranks the cards that need attention first.
	healthSort bool

	loaded bool
	err    string
}

// newSelectorModel creates the Selector bound to reader. The filter input
// starts blurred: "/" focuses it, same as Memory's search box.
func newSelectorModel(reader data.ProjectReader) selectorModel {
	ti := textinput.New()
	ti.Placeholder = "Filter by slug or name..."
	ti.CharLimit = 128
	ti.Width = 40

	return selectorModel{reader: reader, filterInput: ti}
}

// selectorLoadedMsg carries the result of loadSelector.
type selectorLoadedMsg struct {
	cards []store.ProjectCardListItem
	err   error
}

// loadSelector returns the command that lists every project card.
func loadSelector(r data.ProjectReader) tea.Cmd {
	return func() tea.Msg {
		cards, err := r.ListCards()
		return selectorLoadedMsg{cards: cards, err: err}
	}
}

// applyLoaded stores a selectorLoadedMsg's result and re-applies the current
// filter, so reopening the Selector ("p") or reloading it ("r") keeps
// whatever the user had typed.
func (m selectorModel) applyLoaded(msg selectorLoadedMsg) selectorModel {
	if msg.err != nil {
		m.err = msg.err.Error()
		return m
	}
	m.err = ""
	m.cards = msg.cards
	m.loaded = true
	return m.applyFilter()
}

// applyFilter recomputes the filtered list from the current query, matching
// a card whose slug or display name contains it, case-insensitively — the
// same "slug or name" contract rfc-tui.md §3.1 row S1 names. The cursor is
// clamped rather than reset, so narrowing a filter with the cursor already
// past the new end does not silently jump to the top.
func (m selectorModel) applyFilter() selectorModel {
	query := strings.ToLower(strings.TrimSpace(m.filterInput.Value()))
	if query == "" {
		m.filtered = append([]store.ProjectCardListItem{}, m.cards...)
	} else {
		filtered := make([]store.ProjectCardListItem, 0, len(m.cards))
		for _, c := range m.cards {
			if strings.Contains(strings.ToLower(c.Slug), query) ||
				strings.Contains(strings.ToLower(c.DisplayName), query) {
				filtered = append(filtered, c)
			}
		}
		m.filtered = filtered
	}
	if m.healthSort {
		sort.SliceStable(m.filtered, func(i, j int) bool {
			return worseHealthThan(m.filtered[i], m.filtered[j])
		})
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = len(m.filtered) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	return m
}

// toggleHealthSort flips healthSort and re-applies it, so "i" is a plain
// toggle rather than a one-way switch — rfc-tui.md §7.2 names it "invertir
// orden", not just "ordenar".
func (m selectorModel) toggleHealthSort() selectorModel {
	m.healthSort = !m.healthSort
	return m.applyFilter()
}

// worseHealthThan reports whether a needs attention before b: more stale
// runbooks first, then more open tasks, then more observations as a final
// deterministic tiebreak. A card with no counters loaded (Counts is nil,
// e.g. ListCards was called without includeCounts) ranks as perfectly
// healthy rather than panicking.
func worseHealthThan(a, b store.ProjectCardListItem) bool {
	ac, bc := cardCounts(a), cardCounts(b)
	if ac.RunbooksStale != bc.RunbooksStale {
		return ac.RunbooksStale > bc.RunbooksStale
	}
	if ac.TasksActive != bc.TasksActive {
		return ac.TasksActive > bc.TasksActive
	}
	return ac.Observations > bc.Observations
}

func cardCounts(c store.ProjectCardListItem) store.ProjectCardCounts {
	if c.Counts == nil {
		return store.ProjectCardCounts{}
	}
	return *c.Counts
}

// moveCursor shifts the cursor by delta, clamped to the filtered list.
func (m selectorModel) moveCursor(delta int) selectorModel {
	if len(m.filtered) == 0 {
		return m
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = len(m.filtered) - 1
	}
	return m
}

// moveCursorToStart and moveCursorToEnd are rfc-tui.md §7.1's "g/G van al
// inicio y al fin de cada lista", applied to S1's project list.
func (m selectorModel) moveCursorToStart() selectorModel {
	m.cursor = 0
	return m
}

func (m selectorModel) moveCursorToEnd() selectorModel {
	if len(m.filtered) > 0 {
		m.cursor = len(m.filtered) - 1
	}
	return m
}

// selected returns the card under the cursor, or nil when the filtered list
// is empty (nothing loaded yet, or the filter matches nothing).
func (m selectorModel) selected() *store.ProjectCardListItem {
	if m.cursor < 0 || m.cursor >= len(m.filtered) {
		return nil
	}
	return &m.filtered[m.cursor]
}

// ─── View ────────────────────────────────────────────────────────────────────

func (m Model) viewSelector() string {
	var b strings.Builder

	b.WriteString(m.styles.Title.Render("  Select a project"))
	b.WriteString("\n")

	if m.selector.filterInput.Focused() || m.selector.filterInput.Value() != "" {
		b.WriteString("  " + m.selector.filterInput.View())
		b.WriteString("\n")
	}
	b.WriteString("\n")

	switch {
	case m.selector.err != "":
		b.WriteString(m.styles.Error.Render("  " + m.selector.err))
		b.WriteString("\n")
	case !m.selector.loaded:
		b.WriteString(shared.Loading(m.styles, spinner.Model{}, "the projects"))
		b.WriteString("\n")
	case len(m.selector.filtered) == 0:
		b.WriteString(m.styles.NoResults.Render("  No projects match the filter."))
		b.WriteString("\n")
	default:
		b.WriteString(m.selectorHeaderRow())
		for i, c := range m.selector.filtered {
			b.WriteString(m.selectorRow(c, i == m.selector.cursor))
		}
	}

	return b.String()
}

// selectorColumns solves the project table against the width it is drawn in.
// The slug and the display name stretch; the three counters are digits and
// never need more than their cap.
func (m Model) selectorColumns() []int {
	return shared.SolveColumns(m.bodyWidth()-selectorRowFixed, 1, []shared.Column{
		{Min: 10, Max: selectorSlugCells, Weight: 2},
		{Min: 12, Weight: 3},
		{Min: selectorCountCells, Max: selectorCountCells},
		{Min: selectorCountCells, Max: selectorCountCells},
		{Min: selectorStaleCells, Max: selectorStaleCells},
	})
}

func (m Model) selectorHeaderRow() string {
	w := m.selectorColumns()
	line := fmt.Sprintf("  %s %s %s %s %s",
		shared.Cell("slug", w[0]),
		shared.Cell("display name", w[1]),
		shared.Cell("obs", w[2]),
		shared.Cell("open", w[3]),
		shared.Cell("stale RB", w[4]))
	return m.styles.Help.Render(strings.TrimRight(line, " ")) + "\n"
}

func (m Model) selectorRow(c store.ProjectCardListItem, selected bool) string {
	rowStyle := m.styles.ListItem
	if selected {
		rowStyle = m.styles.ListSelected
	}

	var obs, open, stale string
	if c.Counts != nil {
		obs = fmt.Sprintf("%d", c.Counts.Observations)
		open = fmt.Sprintf("%d", c.Counts.TasksActive)
		stale = fmt.Sprintf("%d", c.Counts.RunbooksStale)
	}

	w := m.selectorColumns()
	line := fmt.Sprintf("%s%s %s %s %s %s",
		shared.RowCursor(m.styles, selected),
		shared.Field(c.Slug, w[0]),
		shared.Field(c.DisplayName, w[1]),
		shared.Cell(obs, w[2]),
		shared.Cell(open, w[3]),
		shared.Cell(stale, w[4]))
	return rowStyle.Render(strings.TrimRight(line, " ")) + "\n"
}
