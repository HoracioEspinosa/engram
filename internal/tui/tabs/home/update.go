package home

import (
	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/shared"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs"

	tea "github.com/charmbracelet/bubbletea"
)

// loadedMsg carries every block of one load.
//
// The card is load-bearing — without it there is no project to show, so its
// error short-circuits the rest — but the reads after it are independent: one
// failing (a dropped connection mid-load, say) still lets the blocks that
// succeeded render, with the first error kept for the notice.
type loadedMsg struct {
	slug string

	card       store.ProjectCard
	health     data.ProjectHealth
	graphState data.GraphState
	tasks      []store.TaskListItem
	bench      []data.Benchmark
	evidence   []store.EvidenceListItem

	err error
}

// TabOwner names the tab this message belongs to, so the root delivers it here
// instead of waking every other tab to ignore it.
func (loadedMsg) TabOwner() tabs.ID { return tabs.Home }

// syncedMsg carries the result of "s".
type syncedMsg struct {
	slug  string
	state data.GraphState
	err   error
}

// TabOwner names the tab this message belongs to.
func (syncedMsg) TabOwner() tabs.ID { return tabs.Home }

// loadHome reads every block for slug.
func loadHome(projects data.ProjectReader, graph data.GraphReader, benchmarks data.BenchmarkReader, slug string) tea.Cmd {
	return func() tea.Msg {
		msg := loadedMsg{slug: slug}
		if projects == nil {
			msg.err = errNoProjectReader
			return msg
		}

		card, err := projects.Card(slug)
		if err != nil {
			msg.err = err
			return msg
		}
		msg.card = card

		keepFirstErr := func(err error) {
			if err != nil && msg.err == nil {
				msg.err = err
			}
		}

		health, err := projects.Health(slug)
		keepFirstErr(err)
		if err == nil {
			msg.health = health
		}

		tasks, err := projects.RecentTasks(slug, blockLimit)
		keepFirstErr(err)
		if err == nil {
			msg.tasks = tasks
		}

		evidence, err := projects.LatestEvidence(slug, blockLimit)
		keepFirstErr(err)
		if err == nil {
			msg.evidence = evidence
		}

		if graph != nil {
			state, err := graph.GraphState(slug)
			keepFirstErr(err)
			if err == nil {
				msg.graphState = state
			}
		}

		if benchmarks != nil {
			page, err := benchmarks.ListBenchmarks(slug, data.BenchmarkFilter{Limit: blockLimit})
			keepFirstErr(err)
			if err == nil {
				msg.bench = page.Items
			}
		}

		return msg
	}
}

// syncGraph runs the graph sync for slug.
func syncGraph(syncer data.GraphSyncer, slug string) tea.Cmd {
	return func() tea.Msg {
		if syncer == nil {
			return syncedMsg{slug: slug, err: errNoGraphSyncer}
		}
		state, err := syncer.SyncGraph(slug)
		return syncedMsg{slug: slug, state: state, err: err}
	}
}

// Update advances the Home tab.
func (m Model) Update(msg tea.Msg) (tabs.Tab, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m = m.clampCursor()
		return m, nil

	case loadedMsg:
		return m.applyLoaded(msg), nil

	case syncedMsg:
		return m.applySynced(msg), nil

	case tea.KeyMsg:
		return m.handleKey(msg.String())
	}
	return m, nil
}

// applyLoaded stores a load's result. A response for a project the workspace
// has since left is dropped: a slow read for the project that used to be
// active must never clobber the one now on screen.
func (m Model) applyLoaded(msg loadedMsg) Model {
	if msg.slug != m.project {
		return m
	}
	m.notice = shared.Notice{}
	if msg.err != nil {
		m.notice = shared.Error(msg.err.Error())
	}
	m.card = msg.card
	m.health = msg.health
	m.graphState = msg.graphState
	m.tasks = msg.tasks
	m.bench = msg.bench
	m.evidence = msg.evidence
	m.loaded = true
	return m.clampCursor()
}

// applySynced folds a finished graph sync back in, guarded the same way.
func (m Model) applySynced(msg syncedMsg) Model {
	if msg.slug != m.project {
		return m
	}
	m.syncing = false
	if msg.err != nil {
		m.notice = shared.Error("graph sync: " + msg.err.Error())
		return m
	}
	m.graphState = msg.state
	m.notice = shared.Info("graph synced")
	return m
}

func (m Model) handleKey(key string) (tabs.Tab, tea.Cmd) {
	switch key {
	case "h", "left":
		m.focus = shared.FocusLeft()
		return m.clampCursor(), nil
	case "l", "right":
		m.focus = shared.FocusRight(m.regions())
		return m.clampCursor(), nil
	case "j", "down":
		return m.moveCursor(1), nil
	case "k", "up":
		return m.moveCursor(-1), nil
	case "g":
		return m.moveCursorTo(0), nil
	case "G":
		return m.moveCursorTo(len(m.visibleBlocks()) - 1), nil
	case "enter":
		// The root already knows which tabs this build registers, so route
		// through it: a block whose tab does not exist yet leaves Home
		// exactly as it was.
		return m, tabs.Navigate(m.selected().target())
	case "s":
		if m.project == "" {
			return m, nil
		}
		m.syncing = true
		return m, syncGraph(m.syncer, m.project)
	}
	return m, nil
}

// moveCursor shifts the cursor inside the focused column, clamped rather than
// wrapped: a block list four long is short enough that wrapping reads as a
// glitch.
func (m Model) moveCursor(delta int) Model {
	return m.moveCursorTo(m.cursor[m.focusIndex()] + delta)
}

func (m Model) moveCursorTo(index int) Model {
	slot := m.focusIndex()
	m.cursor[slot] = index
	return m.clampCursor()
}

// clampCursor keeps every column's cursor inside the blocks that column now
// holds, which is what a resize across the split breakpoint changes: one
// column of four blocks becomes two of two, and a cursor left at 3 would
// address a block that column does not have.
func (m Model) clampCursor() Model {
	split := m.regions().HasDetail()
	if !split {
		m.focus = shared.PaneMaster
	}
	for slot := range m.cursor {
		limit := len(columns[slot])
		if !split {
			// One column draws every block, and only the first cursor slot is
			// in use.
			limit = len(columns[0]) + len(columns[1])
		}
		if m.cursor[slot] >= limit {
			m.cursor[slot] = limit - 1
		}
		if m.cursor[slot] < 0 {
			m.cursor[slot] = 0
		}
	}
	return m
}
