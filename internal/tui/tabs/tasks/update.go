package tasks

import (
	"image"
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

	case tea.MouseMsg:
		return m.handleWheel(msg)

	case tasksLoadedMsg:
		if msg.err != nil {
			m.ErrorMsg = msg.err.Error()
			return m, nil
		}
		m.ErrorMsg = ""
		m.Items = msg.page.Items
		m.Total = msg.page.Total
		// The page echoes back the window the store actually applied, so the
		// filter carries the limit it defaulted to rather than the zero the
		// caller may have sent — otherwise the page keys and the footer
		// would each be reasoning about a different page size.
		m.Filter.Offset = msg.page.Offset
		m.Filter.Limit = msg.page.Limit
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

// ─── Mouse ───────────────────────────────────────────────────────────────────

// handleWheel translates a wheel notch into the movement the arrow keys
// already make.
//
// Going through the key handlers rather than touching the cursor directly is
// what keeps the pointer and the keyboard from drifting apart: the window
// arithmetic, the clamps and the page bounds are declared once, for both.
//
// Which pane the pointer is over decides what moves. Over the list it is the
// cursor; over the panel beside it there is nothing to move, because that
// panel shows what the row already carries rather than a body of its own. The
// coordinates arrive in the tab's own space — the root translates them out of
// the frame before delivering.
func (m Model) handleWheel(msg tea.MouseMsg) (tabs.Tab, tea.Cmd) {
	rows, ok := shared.WheelDelta(msg)
	if !ok {
		return m, nil
	}
	key := "down"
	if rows < 0 {
		key, rows = "up", -rows
	}

	switch m.Screen {
	case ScreenContextPack:
		// One long body: the whole screen scrolls, wherever the pointer is.
		return repeatKey(m, rows, Model.handleContextPackKeys, key)
	case ScreenDetail:
		if m.ChangingState || m.Linking {
			// A prompt is up; the list behind it is not the thing being
			// navigated.
			return m, nil
		}
		return repeatKey(m, rows, Model.handleDetailKeys, key)
	}

	if m.Searching && m.SearchInput.Focused() {
		return m, nil
	}
	if pane, ok := shared.PaneAt(m.regions(), image.Pt(msg.X, msg.Y)); !ok || pane != shared.PaneMaster {
		return m, nil
	}
	return repeatKey(m, rows, Model.handleListKeys, key)
}

// repeatKey applies one of the tab's key handlers n times, threading the model
// through each step. A wheel notch is several rows, and the handlers move one.
func repeatKey(m Model, n int, handle func(Model, string) (tabs.Tab, tea.Cmd), key string) (tabs.Tab, tea.Cmd) {
	cmds := make([]tea.Cmd, 0, n)
	for i := 0; i < n; i++ {
		next, cmd := handle(m, key)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		updated, ok := next.(Model)
		if !ok {
			return next, tea.Batch(cmds...)
		}
		m = updated
	}
	return m, tea.Batch(cmds...)
}

// ─── List (S3) ───────────────────────────────────────────────────────────────

func (m Model) handleListKeys(key string) (tabs.Tab, tea.Cmd) {
	visible := shared.VisibleItems(m.Height, listChrome, taskItemLines, minVisibleItems)

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
		// Advance one page, and stop on the last one. The store's own total
		// says where the list ends, so there is nothing left to infer from a
		// short page and no reason to wrap round to the start.
		if !m.HasNextPage() {
			return m, nil
		}
		limit := m.pageLimit()
		m.Filter.Offset += limit
		m.Filter.Limit = limit
		return m, loadTasks(m.reader, m.project, m.Filter)
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
