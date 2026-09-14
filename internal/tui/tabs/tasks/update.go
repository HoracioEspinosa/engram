package tasks

import (
	"strconv"
	"strings"
	"time"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/shared"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs"

	tea "github.com/charmbracelet/bubbletea"
)

// Update advances the Tasks tab. Key messages arrive only while the tab is
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
		if m.Linking {
			return m.handleLinkInputKeys(msg)
		}
		switch m.Screen {
		case ScreenDetail:
			if m.ChangingState {
				return m.handleStatePickerKeys(msg.String())
			}
			return m.handleDetailKeys(msg.String())
		case ScreenContextPack:
			return m.handleContextPackKeys(msg.String())
		default:
			return m.handleListKeys(msg.String())
		}

	case tasksLoadedMsg:
		if msg.err != nil {
			m.ErrorMsg = msg.err.Error()
			return m, nil
		}
		m.ErrorMsg = ""
		m.Items = msg.items
		if m.Cursor >= len(m.Items) {
			m.Cursor = 0
			m.Scroll = 0
		}
		return m, nil

	case taskDetailLoadedMsg:
		if msg.err != nil {
			m.ErrorMsg = msg.err.Error()
			return m, nil
		}
		m.ErrorMsg = ""
		detail := msg.detail
		m.Detail = &detail
		m.Screen = ScreenDetail
		if m.DetailCursor >= len(m.Detail.Observations) {
			m.DetailCursor = 0
			m.DetailScroll = 0
		}
		return m, nil

	case stateUpdatedMsg:
		m.ChangingState = false
		if msg.err != nil {
			m.ErrorMsg = msg.err.Error()
			return m, nil
		}
		m.ErrorMsg = ""
		if m.Detail != nil && m.Detail.Task.ID == msg.id {
			return m, loadTaskDetail(m.reader, msg.id)
		}
		return m, nil

	case observationLinkedMsg:
		m.Linking = false
		m.LinkInput.SetValue("")
		m.LinkInput.Blur()
		if msg.err != nil {
			m.ErrorMsg = msg.err.Error()
			return m, nil
		}
		m.ErrorMsg = ""
		if m.Detail != nil && m.Detail.Task.ID == msg.taskID {
			return m, loadTaskDetail(m.reader, msg.taskID)
		}
		return m, nil

	case contextPackLoadedMsg:
		if msg.err != nil {
			m.ErrorMsg = msg.err.Error()
			return m, nil
		}
		m.ErrorMsg = ""
		m.ContextPack = msg.pack
		m.ContextPackBuilt = time.Now()
		m.ContextPackScroll = 0
		m.Screen = ScreenContextPack
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

// ─── List (S3) ───────────────────────────────────────────────────────────────

func (m Model) handleListKeys(key string) (tabs.Tab, tea.Cmd) {
	visible := shared.VisibleItems(m.Height, listChrome, taskItemLines, minVisibleItems)

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
	case "enter":
		if len(m.Items) > 0 && m.Cursor < len(m.Items) {
			return m, loadTaskDetail(m.reader, m.Items[m.Cursor].ID)
		}
	case "c":
		if len(m.Items) > 0 && m.Cursor < len(m.Items) {
			return m, shared.Copy(data.TaskKey(m.Items[m.Cursor].Task))
		}
	case "o":
		if len(m.Items) > 0 && m.Cursor < len(m.Items) {
			return m.openJira(m.Items[m.Cursor].Task)
		}
	case "f":
		m.Filter.State = nextState(m.Filter.State)
		m.Filter.Offset = 0
		return m, loadTasks(m.reader, m.project, m.Filter)
	case "K":
		m.Filter.Kind = nextKind(m.Filter.Kind)
		m.Filter.Offset = 0
		return m, loadTasks(m.reader, m.project, m.Filter)
	case "/":
		m.Searching = true
		m.SearchInput.Focus()
		return m, nil
	case "n":
		// Advance one page; wrap back to the start once a page comes back
		// short, since that is the only "was this the last page" signal
		// store.ListTasks gives back through TaskReader (rfc-tui.md §9.2's
		// list query carries no total, only LIMIT/OFFSET).
		limit := m.pageLimit()
		if len(m.Items) < limit {
			m.Filter.Offset = 0
		} else {
			m.Filter.Offset += limit
		}
		m.Filter.Limit = limit
		return m, loadTasks(m.reader, m.project, m.Filter)
	case "p":
		// Step one page back. Unlike "n" this needs no total: the offset
		// alone says whether there is a page behind this one, and the first
		// page stays put rather than wrapping round to an end nobody can
		// locate without a count.
		limit := m.pageLimit()
		if m.Filter.Offset == 0 {
			return m, nil
		}
		m.Filter.Offset -= limit
		if m.Filter.Offset < 0 {
			m.Filter.Offset = 0
		}
		m.Filter.Limit = limit
		return m, loadTasks(m.reader, m.project, m.Filter)
	case "esc", "q":
		return m, tabs.Home()
	}
	return m, nil
}

// nextState cycles S3's state filter: the active-tasks default ("", which
// store.TaskListFilter treats as "every state but done/cancelled" — see its
// doc comment), then each concrete value in stateOptions, then back to "".
// The store exposes no single value meaning "every state including done and
// cancelled at once", so this filter's default is honestly labelled "active"
// in the footer rather than the wireframe's "all" (rfc-tui.md §5 S3 also
// shows "(7 open ...)" for that same default, which only an active-only
// count explains).
func nextState(current string) string {
	if current == "" {
		return stateOptions[0]
	}
	for i, s := range stateOptions {
		if s == current {
			if i+1 < len(stateOptions) {
				return stateOptions[i+1]
			}
			return ""
		}
	}
	return ""
}

// nextKind cycles S3's kind filter through kindOptions, wrapping back to ""
// (every kind) — unlike state, "" genuinely means "no filter" here, since
// store.ListTasks only applies a kind clause when f.Kind is non-empty.
func nextKind(current string) string {
	for i, k := range kindOptions {
		if k == current {
			return kindOptions[(i+1)%len(kindOptions)]
		}
	}
	return ""
}

func (m Model) handleSearchInputKeys(msg tea.KeyMsg) (tabs.Tab, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		m.Searching = false
		m.SearchInput.Blur()
		m.Filter.Query = m.SearchInput.Value()
		m.Filter.Offset = 0
		return m, loadTasks(m.reader, m.project, m.Filter)
	case tea.KeyEsc:
		m.Searching = false
		m.SearchInput.Blur()
		m.SearchInput.SetValue("")
		return m, nil
	}
	updated, cmd := m.SearchInput.Update(msg)
	m.SearchInput = updated
	return m, cmd
}

// ─── Detail (S4) ─────────────────────────────────────────────────────────────

func (m Model) handleDetailKeys(key string) (tabs.Tab, tea.Cmd) {
	if m.Detail == nil {
		if key == "esc" || key == "q" {
			m.Screen = ScreenList
			return m, loadTasks(m.reader, m.project, m.Filter)
		}
		return m, nil
	}
	task := m.Detail.Task

	visible := shared.VisibleItems(m.Height, detailChrome, observationItemLines, minVisibleItems)

	switch key {
	case "up", "k":
		if m.DetailCursor > 0 {
			m.DetailCursor--
			if m.DetailCursor < m.DetailScroll {
				m.DetailScroll = m.DetailCursor
			}
		}
	case "down", "j":
		if m.DetailCursor < len(m.Detail.Observations)-1 {
			m.DetailCursor++
			if m.DetailCursor >= m.DetailScroll+visible {
				m.DetailScroll = m.DetailCursor - visible + 1
			}
		}
	case "enter":
		if len(m.Detail.Observations) > 0 && m.DetailCursor < len(m.Detail.Observations) {
			obsID := m.Detail.Observations[m.DetailCursor].Observation.ID
			return m, tabs.NavigateToObservation(obsID)
		}
	case "e":
		// rfc-tui.md §3.1: S4's "e" opens Evidence filtered to this task
		// (S6's task_id filter), carried through tabs.NavigateMsg.TaskID.
		return m, tabs.NavigateToTaskEvidence(task.ID)
	case "x":
		return m, loadContextPack(m.reader, task.ID)
	case "s":
		m.ChangingState = true
		m.StateCursor = 0
		for i, s := range stateOptions {
			if s == task.State {
				m.StateCursor = i
				break
			}
		}
		return m, nil
	case "l":
		m.Linking = true
		m.LinkInput.Focus()
		return m, nil
	case "c":
		return m, shared.Copy(data.TaskKey(task))
	case "o":
		return m.openJira(task)
	case "u":
		if task.PRUrl == nil || strings.TrimSpace(*task.PRUrl) == "" {
			m.ErrorMsg = "this task has no pr_url"
			return m, nil
		}
		if err := openURL(*task.PRUrl); err != nil {
			m.ErrorMsg = *task.PRUrl + " (could not open a browser: " + err.Error() + ", copy it with c)"
		}
		return m, nil
	case "b":
		if task.Branch == nil || strings.TrimSpace(*task.Branch) == "" {
			m.ErrorMsg = "this task has no branch"
			return m, nil
		}
		return m, shared.Copy(*task.Branch)
	case "esc", "q":
		m.Screen = ScreenList
		return m, loadTasks(m.reader, m.project, m.Filter)
	}
	return m, nil
}

// openJira opens the task's Jira issue, or records why it could not when the
// task has no jira_key at all (an sdd-only task) or the OS has no registered
// URL handler (rfc-tui.md §10.2: "open/xdg-open ausentes").
func (m Model) openJira(task store.Task) (tabs.Tab, tea.Cmd) {
	if task.JiraKey == nil || strings.TrimSpace(*task.JiraKey) == "" {
		m.ErrorMsg = "this task has no jira_key"
		return m, nil
	}
	url := data.JiraURL(*task.JiraKey)
	if err := openURL(url); err != nil {
		m.ErrorMsg = url + " (could not open a browser: " + err.Error() + ", copy the key with c)"
	}
	return m, nil
}

func (m Model) handleStatePickerKeys(key string) (tabs.Tab, tea.Cmd) {
	switch key {
	case "up", "k":
		if m.StateCursor > 0 {
			m.StateCursor--
		}
	case "down", "j":
		if m.StateCursor < len(stateOptions)-1 {
			m.StateCursor++
		}
	case "enter":
		if m.Detail != nil {
			return m, updateTaskState(m.reader, m.Detail.Task.ID, stateOptions[m.StateCursor])
		}
		m.ChangingState = false
	case "esc":
		m.ChangingState = false
	}
	return m, nil
}

func (m Model) handleLinkInputKeys(msg tea.KeyMsg) (tabs.Tab, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		raw := strings.TrimSpace(m.LinkInput.Value())
		raw = strings.TrimPrefix(raw, "#")
		obsID, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			m.ErrorMsg = "invalid observation id: " + raw
			return m, nil
		}
		if m.Detail == nil {
			m.Linking = false
			return m, nil
		}
		return m, linkObservation(m.reader, m.Detail.Task.ID, obsID)
	case tea.KeyEsc:
		m.Linking = false
		m.LinkInput.SetValue("")
		m.LinkInput.Blur()
		return m, nil
	}
	updated, cmd := m.LinkInput.Update(msg)
	m.LinkInput = updated
	return m, cmd
}

// ─── Context pack (S5) ───────────────────────────────────────────────────────

func (m Model) handleContextPackKeys(key string) (tabs.Tab, tea.Cmd) {
	switch key {
	case "up", "k":
		if m.ContextPackScroll > 0 {
			m.ContextPackScroll--
		}
	case "down", "j":
		m.ContextPackScroll++
	case "c":
		return m, shared.Copy(m.ContextPack)
	case "w":
		return m.writeContextPack()
	case "esc", "q":
		m.Screen = ScreenDetail
	}
	return m, nil
}
