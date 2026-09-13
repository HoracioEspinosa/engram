package runbooks

import (
	"strings"
	"time"

	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/shared"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs"

	tea "github.com/charmbracelet/bubbletea"
)

// hubResolvedMsg carries openHub's result: the absolute path to open in
// $EDITOR, already resolved against shared.VaultRoot() (rfc-tui.md §9.4's
// "o abre el hub del servicio... en $EDITOR"), or the reason there is
// nothing to open.
type hubResolvedMsg struct {
	path string // "" means the project has no knowledge_hub_path configured
	err  error
}

// openHub returns the command that resolves project's knowledge_hub_path
// before handing off to execEditor — a project card lookup, not a plain
// filesystem read, so unlike loadMarkdown this goes through data.ProjectReader
// rather than a local helper.
func openHub(projects data.ProjectReader, project string) tea.Cmd {
	return func() tea.Msg {
		card, err := projects.Card(project)
		if err != nil {
			return hubResolvedMsg{err: err}
		}
		if card.KnowledgeHubPath == nil || strings.TrimSpace(*card.KnowledgeHubPath) == "" {
			return hubResolvedMsg{}
		}
		return hubResolvedMsg{path: absoluteVaultPath(*card.KnowledgeHubPath)}
	}
}

// Update advances the Runbooks tab. Key messages arrive only while the tab is
// active; every other message (window size, data loads, clipboard feedback)
// is broadcast and handled here regardless of which screen is showing.
func (m Model) Update(msg tea.Msg) (tabs.Tab, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		return m, nil

	case tea.KeyMsg:
		if m.Searching && m.SearchInput.Focused() {
			return m.handleSearchInputKeys(msg)
		}
		switch m.Screen {
		case ScreenView:
			return m.handleViewKeys(msg.String())
		default:
			return m.handleIndexKeys(msg.String())
		}

	case runbooksLoadedMsg:
		if msg.project != m.project {
			// A slow load for a project the user has since switched away
			// from (app.dashboardModel.applyLoaded's own guard): must never
			// clobber the list now on screen.
			return m, nil
		}
		if msg.err != nil {
			m.ErrorMsg = msg.err.Error()
			return m, nil
		}
		m.ErrorMsg = ""
		m.All = msg.all
		m.Query = msg.query
		m.Items = msg.items
		if m.Cursor >= len(m.Items) {
			m.Cursor = 0
			m.Scroll = 0
		}
		return m, nil

	case markdownLoadedMsg:
		if m.Selected == nil || m.Selected.ID != msg.id {
			// The user moved to a different row (or back to the index)
			// before this read finished; do not resurrect a stale one.
			return m, nil
		}
		if msg.err != nil {
			m.ErrorMsg = msg.err.Error()
			return m, nil
		}
		m.ErrorMsg = ""
		m.FileExists = msg.exists
		m.MarkdownRaw = msg.raw
		m.Rendered = msg.rendered
		m.MarkdownErr = msg.renderErr
		m.ViewScroll = 0
		return m, nil

	case hubResolvedMsg:
		if msg.err != nil {
			m.ErrorMsg = msg.err.Error()
			return m, nil
		}
		if msg.path == "" {
			m.ErrorMsg = "this project has no knowledge_hub_path configured"
			return m, nil
		}
		return m, execEditor(msg.path)

	case editorClosedMsg:
		if msg.err != nil {
			m.ErrorMsg = "$EDITOR exited with an error: " + msg.err.Error()
			return m, nil
		}
		m.ErrorMsg = ""
		return m, nil

	case shared.CopiedMsg:
		m.CopyFeedback = "✓ Copied!"
		return m, tea.Batch(
			tea.Println(msg.Sequence),
			shared.ClearFeedbackAfter(2*time.Second),
		)

	case shared.ClearFeedbackMsg:
		m.CopyFeedback = ""
		return m, nil
	}

	return m, nil
}

// ─── Index (S8) ──────────────────────────────────────────────────────────────

func (m Model) handleIndexKeys(key string) (tabs.Tab, tea.Cmd) {
	visible := shared.VisibleItems(m.Height, indexChrome, runbookItemLines, minVisibleItems)

	switch key {
	case "up", "k":
		if m.Cursor > 0 {
			m.Cursor--
			if m.Cursor < m.Scroll {
				m.Scroll = m.Cursor
			}
		}
	case "down", "j":
		if m.Cursor < len(m.Items)-1 {
			m.Cursor++
			if m.Cursor >= m.Scroll+visible {
				m.Scroll = m.Cursor - visible + 1
			}
		}
	case "g":
		m.Cursor, m.Scroll = 0, 0
	case "G":
		if len(m.Items) > 0 {
			m.Cursor = len(m.Items) - 1
			if m.Cursor >= visible {
				m.Scroll = m.Cursor - visible + 1
			}
		}
	case "a":
		m.All = !m.All
		return m, m.reload()
	case "/":
		m.Searching = true
		m.SearchInput.SetValue(m.Query)
		m.SearchInput.Focus()
		return m, nil
	case "enter":
		if len(m.Items) > 0 && m.Cursor < len(m.Items) {
			item := m.Items[m.Cursor]
			m.Selected = &item
			m.Screen = ScreenView
			m.FileExists = false
			m.MarkdownRaw = ""
			m.Rendered = ""
			m.MarkdownErr = ""
			m.ErrorMsg = ""
			return m, loadMarkdown(item, m.Width, m.styles.Palette)
		}
	case "c":
		if len(m.Items) > 0 && m.Cursor < len(m.Items) {
			return m, shared.Copy(m.Items[m.Cursor].VaultPath)
		}
	case "t":
		if len(m.Items) > 0 && m.Cursor < len(m.Items) {
			return m, tabs.NavigateToMemorySearch("runbook/" + m.Items[m.Cursor].ID)
		}
	case "r":
		return m, m.reload()
	case "esc", "q":
		return m, tabs.Home()
	}
	return m, nil
}

// reload re-issues whichever data the index screen is currently showing:
// the active search when m.Query is set, the plain index otherwise. "a" and
// "r" both call this so toggling "all projects" mid-search re-ranks the same
// query instead of silently dropping it back to the unfiltered list.
func (m Model) reload() tea.Cmd {
	if m.Query != "" {
		return searchRunbooks(m.reader, m.project, m.All, m.Query, searchLimit)
	}
	return loadRunbookIndex(m.reader, m.project, m.All)
}

func (m Model) handleSearchInputKeys(msg tea.KeyMsg) (tabs.Tab, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		m.Searching = false
		m.SearchInput.Blur()
		query := strings.TrimSpace(m.SearchInput.Value())
		if query == "" {
			m.Query = ""
			return m, loadRunbookIndex(m.reader, m.project, m.All)
		}
		return m, searchRunbooks(m.reader, m.project, m.All, query, searchLimit)
	case tea.KeyEsc:
		m.Searching = false
		m.SearchInput.Blur()
		m.SearchInput.SetValue(m.Query)
		return m, nil
	}
	updated, cmd := m.SearchInput.Update(msg)
	m.SearchInput = updated
	return m, cmd
}

// ─── Markdown view (S9) ──────────────────────────────────────────────────────

func (m Model) handleViewKeys(key string) (tabs.Tab, tea.Cmd) {
	if m.Selected == nil {
		if key == "esc" || key == "q" {
			m.Screen = ScreenIndex
			return m, m.reload()
		}
		return m, nil
	}
	item := *m.Selected
	lines := strings.Split(m.Rendered, "\n")

	switch key {
	case "up", "k":
		if m.ViewScroll > 0 {
			m.ViewScroll--
		}
	case "down", "j":
		if m.ViewScroll < len(lines)-1 {
			m.ViewScroll++
		}
	case "g":
		m.ViewScroll = 0
	case "G":
		visible := shared.VisibleItems(m.Height, viewChrome, 1, minVisibleItems)
		m.ViewScroll = len(lines) - visible
		if m.ViewScroll < 0 {
			m.ViewScroll = 0
		}
	case "e":
		return m, execEditor(absoluteVaultPath(item.VaultPath))
	case "t":
		return m, tabs.NavigateToMemorySearch("runbook/" + item.ID)
	case "c":
		return m, shared.Copy(item.VaultPath)
	case "o":
		return m, openHub(m.projects, m.project)
	case "r":
		return m, loadMarkdown(item, m.Width, m.styles.Palette)
	case "esc", "q":
		m.Screen = ScreenIndex
		m.Selected = nil
		m.ErrorMsg = ""
		return m, m.reload()
	}
	return m, nil
}
