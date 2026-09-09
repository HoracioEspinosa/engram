package app

import (
	"fmt"
	"strings"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/shared"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
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
		m.filtered = m.cards
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
	if m.cursor >= len(m.filtered) {
		m.cursor = len(m.filtered) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	return m
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
		b.WriteString(m.styles.StatCard.Render("Loading projects..."))
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

	b.WriteString(m.styles.Help.Render("  j/k move • enter open • / filter • r refresh • q quit"))
	return b.String()
}

func (m Model) selectorHeaderRow() string {
	line := fmt.Sprintf("  %-2s%-22s %-28s %8s %8s %10s",
		"", "slug", "display name", "obs", "open", "stale RB")
	return m.styles.Help.Render(line) + "\n"
}

func (m Model) selectorRow(c store.ProjectCardListItem, selected bool) string {
	cursor := "  "
	rowStyle := m.styles.ListItem
	if selected {
		cursor = "▸ "
		rowStyle = m.styles.ListSelected
	}

	var obs, open, stale string
	if c.Counts != nil {
		obs = fmt.Sprintf("%d", c.Counts.Observations)
		open = fmt.Sprintf("%d", c.Counts.TasksActive)
		stale = fmt.Sprintf("%d", c.Counts.RunbooksStale)
	}

	line := fmt.Sprintf("%s%-22s %-28s %8s %8s %10s",
		cursor,
		shared.Truncate(c.Slug, 22),
		shared.Truncate(c.DisplayName, 28),
		obs, open, stale)
	return rowStyle.Render(line) + "\n"
}
