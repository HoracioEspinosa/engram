package runbooks

import (
	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	tea "github.com/charmbracelet/bubbletea"
)

// loadRunbookIndex returns the command that lists project's runbook index,
// or every project's when the "a" toggle has set all to true.
//
// It asks for the page rather than the bare slice: the store counts the
// whole match in the same round trip, which is what the footer reports and
// what the page keys stop at.
func loadRunbookIndex(r data.RunbookSource, project string, all bool, f data.RunbookFilter) tea.Cmd {
	return func() tea.Msg {
		page, err := r.ListRunbooksPage(project, all, f)
		return runbooksLoadedMsg{project: project, all: all, page: page, err: err}
	}
}

// searchRunbooks returns the command that ranks the index by query over
// runbook_index_fts — the search-by-symptoms path — scoped the same way
// loadRunbookIndex is.
//
// A ranked search has no page of its own: SearchRunbooks takes a limit and
// no offset, so the total is the hit count it returned and the page keys
// have nowhere to step. Saying so explicitly here keeps the footer honest
// rather than inventing a count the query never produced.
func searchRunbooks(r data.RunbookSource, project string, all bool, query string, limit int) tea.Cmd {
	return func() tea.Msg {
		items, err := r.SearchRunbooks(project, all, query, limit)
		page := data.Page[store.RunbookIndexRow]{Items: items, Total: len(items), Limit: limit}
		return runbooksLoadedMsg{project: project, all: all, query: query, page: page, err: err}
	}
}

// loadMarkdown returns the command that reads item's Markdown file and
// renders it with glamour for the Markdown view. width is captured at call
// time (the tab's current Width) so the render wraps to the terminal the
// request was issued from; palette is the tab's active theme.Palette, so the
// rendered Markdown matches whatever --theme / ENGRAM_TUI_THEME / tui.theme
// resolved to.
func loadMarkdown(item store.RunbookIndexRow, width int, palette theme.Palette) tea.Cmd {
	return func() tea.Msg {
		raw, exists, err := readRunbookMarkdown(item.VaultPath)
		if err != nil {
			return markdownLoadedMsg{id: item.ID, err: err}
		}
		if !exists {
			return markdownLoadedMsg{id: item.ID, exists: false}
		}
		rendered, renderErr := renderMarkdown(raw, width, palette)
		msg := markdownLoadedMsg{id: item.ID, exists: true, raw: raw, rendered: rendered}
		if renderErr != nil {
			msg.renderErr = renderErr.Error()
		}
		return msg
	}
}
