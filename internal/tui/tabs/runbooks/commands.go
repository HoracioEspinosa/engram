package runbooks

import (
	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/data"

	tea "github.com/charmbracelet/bubbletea"
)

// loadRunbookIndex returns the command that lists project's runbook index,
// or every project's when all is true (rfc-tui.md §9.2's "S8 Runbooks
// index" query and §3.1's "a" toggle).
func loadRunbookIndex(r data.RunbookReader, project string, all bool) tea.Cmd {
	return func() tea.Msg {
		items, err := r.ListRunbooks(project, all)
		return runbooksLoadedMsg{project: project, all: all, items: items, err: err}
	}
}

// searchRunbooks returns the command that ranks the index by query over
// runbook_index_fts (rfc-tui.md §9.2's "S8 search by symptoms" query),
// scoped the same way loadRunbookIndex is.
func searchRunbooks(r data.RunbookReader, project string, all bool, query string, limit int) tea.Cmd {
	return func() tea.Msg {
		items, err := r.SearchRunbooks(project, all, query, limit)
		return runbooksLoadedMsg{project: project, all: all, query: query, items: items, err: err}
	}
}

// loadMarkdown returns the command that reads item's Markdown file and
// renders it with glamour (S9, rfc-tui.md §9.4). width is captured at call
// time (the tab's current Width) so the render wraps to the terminal the
// request was issued from.
func loadMarkdown(item store.RunbookIndexRow, width int) tea.Cmd {
	return func() tea.Msg {
		raw, exists, err := readRunbookMarkdown(item.VaultPath)
		if err != nil {
			return markdownLoadedMsg{id: item.ID, err: err}
		}
		if !exists {
			return markdownLoadedMsg{id: item.ID, exists: false}
		}
		rendered, renderErr := renderMarkdown(raw, width)
		msg := markdownLoadedMsg{id: item.ID, exists: true, raw: raw, rendered: rendered}
		if renderErr != nil {
			msg.renderErr = renderErr.Error()
		}
		return msg
	}
}
