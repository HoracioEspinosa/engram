package evidence

import (
	"time"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/shared"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs"

	tea "github.com/charmbracelet/bubbletea"
)

// Update advances the Evidence tab. Key messages arrive only while the tab is
// active; every other message (window size, data loads, clipboard feedback)
// is broadcast and handled here regardless of which screen is showing.
func (m Model) Update(msg tea.Msg) (tabs.Tab, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch m.Screen {
		case ScreenDetail:
			return m.handleDetailKeys(msg.String())
		default:
			return m.handleListKeys(msg.String())
		}

	case evidenceLoadedMsg:
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
		m.Filter = msg.filter
		// The page echoes back the window the store applied, so the filter
		// carries the limit it defaulted to rather than the zero the caller
		// may have sent.
		m.Filter.Offset = msg.page.Offset
		m.Filter.Limit = msg.page.Limit
		m.Items = msg.page.Items
		m.Total = msg.page.Total
		m.TotalBytes = msg.page.TotalBytes
		if m.Cursor >= len(m.Items) {
			m.Cursor = 0
			m.Scroll = 0
		}
		return m, nil

	case manifestLoadedMsg:
		if m.Selected == nil || m.Selected.ID != msg.evidenceID {
			// The user moved to a different row (or back to the list)
			// before this read finished; do not resurrect a stale one.
			return m, nil
		}
		m.ManifestChecked = true
		m.ManifestExists = msg.exists
		if msg.err != nil {
			m.ManifestErr = msg.err.Error()
			m.Manifest = nil
			return m, nil
		}
		m.ManifestErr = ""
		m.Manifest = msg.entry
		return m, nil

	case shared.CopiedMsg:
		m.CopyFeedback = "Copied!"
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

// ─── List (S6) ───────────────────────────────────────────────────────────────

func (m Model) handleListKeys(key string) (tabs.Tab, tea.Cmd) {
	visible := shared.VisibleItems(m.Height, listChrome, evidenceItemLines, minVisibleItems)

	cursor := shared.ListCursor{Index: m.Cursor, Offset: m.Scroll}

	switch key {
	case "up", "k":
		m.Cursor, m.Scroll = cursor.Move(-1, len(m.Items), visible).Unpack()
	case "down", "j":
		m.Cursor, m.Scroll = cursor.Move(1, len(m.Items), visible).Unpack()
	case "h":
		// The list is always there; "h" brings the focus back to it.
		m.Focus = shared.FocusLeft()
	case "l":
		// Inert below the split breakpoint: there is no second pane to
		// move to, and a focus the reader cannot see is worse than none.
		m.Focus = shared.FocusRight(m.regions())
	case "g":
		m.Cursor, m.Scroll = cursor.Top().Unpack()
	case "G":
		m.Cursor, m.Scroll = cursor.Bottom(len(m.Items), visible).Unpack()
	case "enter":
		if len(m.Items) > 0 && m.Cursor < len(m.Items) {
			item := m.Items[m.Cursor]
			m.Selected = &item
			m.Screen = ScreenDetail
			m.Manifest = nil
			m.ManifestExists = false
			m.ManifestChecked = false
			m.ManifestErr = ""
			m.ErrorMsg = ""
			return m, loadManifest(item)
		}
	case "c":
		if len(m.Items) > 0 && m.Cursor < len(m.Items) {
			return m, shared.Copy(absolutePath(m.Items[m.Cursor].Path))
		}
	case "o":
		if len(m.Items) > 0 && m.Cursor < len(m.Items) {
			return m.openEvidenceFile(m.Items[m.Cursor])
		}
	case "t":
		return m.toggleTaskFilter()
	case "a":
		return m.toggleAttachedFilter()
	case "n":
		// Advance one page, and stop on the last one: the store's total says
		// where the list ends, so nothing is inferred from a short page.
		if !m.HasNextPage() {
			return m, nil
		}
		limit := m.pageLimit()
		m.Filter.Offset += limit
		m.Filter.Limit = limit
		return m, loadEvidence(m.reader, m.project, m.Filter)
	case "p":
		// Step one page back; the first page stays put.
		if !m.HasPrevPage() {
			return m, nil
		}
		limit := m.pageLimit()
		m.Filter.Offset -= limit
		if m.Filter.Offset < 0 {
			m.Filter.Offset = 0
		}
		m.Filter.Limit = limit
		return m, loadEvidence(m.reader, m.project, m.Filter)
	case "esc", "q":
		return m, tabs.Home()
	}
	return m, nil
}

// toggleTaskFilter is S6's "t" key (rfc-tui.md §7.2): with no task filter
// active, it scopes the list to the task under the cursor; with one active
// — set here or inherited from S4's "e" deep link — it clears it back to
// "all". A cursor with nothing under it (an empty list) leaves the filter
// untouched rather than clearing a filter the user did not ask to drop.
func (m Model) toggleTaskFilter() (tabs.Tab, tea.Cmd) {
	if m.Filter.TaskID != 0 {
		m.Filter.TaskID = 0
		m.Filter.Offset = 0
		return m, loadEvidence(m.reader, m.project, m.Filter)
	}
	if len(m.Items) == 0 || m.Cursor >= len(m.Items) {
		return m, nil
	}
	m.Filter.TaskID = m.Items[m.Cursor].TaskID
	m.Filter.Offset = 0
	return m, loadEvidence(m.reader, m.project, m.Filter)
}

// toggleAttachedFilter is S6's "a" key: a binary toggle between "all" and
// "attached to Jira only" (rfc-tui.md §7.2: "a alternar solo adjuntos").
func (m Model) toggleAttachedFilter() (tabs.Tab, tea.Cmd) {
	if m.Filter.AttachedJira != nil {
		m.Filter.AttachedJira = nil
	} else {
		yes := true
		m.Filter.AttachedJira = &yes
	}
	m.Filter.Offset = 0
	return m, loadEvidence(m.reader, m.project, m.Filter)
}

// openEvidenceFile opens item's captured file with the system viewer,
// falling back to reporting the path when the OS has no registered handler
// (rfc-tui.md §9.3, the same degradation tabs/tasks/update.go's openJira
// documents for "o").
func (m Model) openEvidenceFile(item store.EvidenceListItem) (tabs.Tab, tea.Cmd) {
	path := absolutePath(item.Path)
	if err := openFile(path); err != nil {
		m.ErrorMsg = path + " (could not open with the system viewer: " + err.Error() + ", copy it with c)"
	}
	return m, nil
}

// ─── Detail (S7) ─────────────────────────────────────────────────────────────

func (m Model) handleDetailKeys(key string) (tabs.Tab, tea.Cmd) {
	if m.Selected == nil {
		if key == "esc" || key == "q" {
			m.Screen = ScreenList
			return m, loadEvidence(m.reader, m.project, m.Filter)
		}
		return m, nil
	}
	item := *m.Selected

	switch key {
	case "o":
		return m.openEvidenceFile(item)
	case "c":
		// rfc-tui.md §7.2: S7 overrides the global "c" (path) to copy
		// sha256 instead — "p" takes over the path copy below.
		return m, shared.Copy(item.SHA256)
	case "p":
		return m, shared.Copy(absolutePath(item.Path))
	case "m":
		return m.openManifestFile(item)
	case "enter":
		return m, tabs.NavigateToTask(item.TaskID)
	case "esc", "q":
		m.Screen = ScreenList
		m.Selected = nil
		m.ErrorMsg = ""
		return m, loadEvidence(m.reader, m.project, m.Filter)
	}
	return m, nil
}

// openManifestFile opens item's sibling manifest.json with the system
// viewer (rfc-tui.md §3.1 S7's "m"). Most captures have none yet — every one
// of clarodrive's 10 registered evidence rows does not, see manifest.go's
// doc comment — so this reports that plainly instead of launching a viewer
// on a path that was never there.
func (m Model) openManifestFile(item store.EvidenceListItem) (tabs.Tab, tea.Cmd) {
	if !m.ManifestExists {
		m.ErrorMsg = "no manifest.json next to this file"
		return m, nil
	}
	path := manifestPath(absolutePath(item.Path))
	if err := openFile(path); err != nil {
		m.ErrorMsg = path + " (could not open with the system viewer: " + err.Error() + ")"
	}
	return m, nil
}
