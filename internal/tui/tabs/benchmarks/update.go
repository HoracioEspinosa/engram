package benchmarks

import (
	"errors"

	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/shared"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs"

	tea "github.com/charmbracelet/bubbletea"
)

// benchmarksLoadedMsg carries one page of the table. project and filter are
// threaded through so a slow load issued before a project switch or a filter
// change never clobbers what is on screen now.
type benchmarksLoadedMsg struct {
	project string
	filter  data.BenchmarkFilter
	page    data.Page[data.Benchmark]
	err     error
}

// historyLoadedMsg carries one metric's series.
type historyLoadedMsg struct {
	project string
	metric  string
	items   []data.Benchmark
	err     error
}

// baselineOpenedMsg reports what happened to the "b" key's viewer: a terminal
// with no desktop behind it has neither `open` nor `xdg-open`, and saying so
// is better than a keypress that does nothing.
type baselineOpenedMsg struct {
	path string
	err  error
}

// The messages this tab issues belong to it alone: the root delivers each to
// its owner rather than broadcasting it to every tab.
func (benchmarksLoadedMsg) TabOwner() tabs.ID { return tabs.Benchmarks }
func (historyLoadedMsg) TabOwner() tabs.ID    { return tabs.Benchmarks }
func (baselineOpenedMsg) TabOwner() tabs.ID   { return tabs.Benchmarks }

var (
	errNoBenchmarkReader = errors.New("no benchmark reader is bound to this workspace")
	// errNoVaultRoot is what "b" reports with no ENGRAM_VAULT_ROOT set: the
	// baseline document lives in the vault, and there is no sensible guess
	// for where that is.
	errNoVaultRoot = errors.New("no vault root is configured")
)

// loadBenchmarks reads one page of the project's measurements.
func loadBenchmarks(reader data.BenchmarkReader, project string, filter data.BenchmarkFilter) tea.Cmd {
	return func() tea.Msg {
		if reader == nil {
			return benchmarksLoadedMsg{project: project, filter: filter, err: errNoBenchmarkReader}
		}
		if filter.Limit <= 0 {
			filter.Limit = pageSize
		}
		page, err := reader.ListBenchmarks(project, filter)
		return benchmarksLoadedMsg{project: project, filter: filter, page: page, err: err}
	}
}

// loadHistory reads one metric's series across the project, oldest first.
func loadHistory(reader data.BenchmarkReader, project, metric string) tea.Cmd {
	return func() tea.Msg {
		if reader == nil {
			return historyLoadedMsg{project: project, metric: metric, err: errNoBenchmarkReader}
		}
		items, err := reader.MetricHistory(project, metric, historyLimit)
		return historyLoadedMsg{project: project, metric: metric, items: items, err: err}
	}
}

// Update advances the Benchmarks tab.
func (m Model) Update(msg tea.Msg) (tabs.Tab, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width, m.Height = msg.Width, msg.Height
		return m.resize(), nil

	case benchmarksLoadedMsg:
		return m.applyLoaded(msg), nil

	case historyLoadedMsg:
		return m.applyHistory(msg), nil

	case baselineOpenedMsg:
		if msg.err != nil {
			// The path is part of the notice on purpose: with no viewer to
			// hand it to, seeing it is what lets the reader open it
			// themselves.
			m.Notice = shared.Error("could not open " + msg.path + ": " + msg.err.Error())
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) applyLoaded(msg benchmarksLoadedMsg) Model {
	if msg.project != m.project {
		return m
	}
	if msg.err != nil {
		m.Notice = shared.Error(msg.err.Error())
		return m
	}
	m.Notice = shared.Notice{}
	m.Filter = msg.filter
	// The page echoes back the window the store applied, so the filter
	// carries the limit it defaulted to rather than the zero the caller sent.
	m.Filter.Offset = msg.page.Offset
	m.Filter.Limit = msg.page.Limit
	m.Items = msg.page.Items
	m.Total = msg.page.Total
	m.loaded = true
	return m.resize()
}

func (m Model) applyHistory(msg historyLoadedMsg) Model {
	if msg.project != m.project || msg.metric != m.Metric {
		return m
	}
	if msg.err != nil {
		m.Notice = shared.Error(msg.err.Error())
		return m
	}
	m.Notice = shared.Notice{}
	m.History = msg.items
	return m
}

func (m Model) handleKey(msg tea.KeyMsg) (tabs.Tab, tea.Cmd) {
	if m.prompt.Focused() {
		return m.handlePromptKey(msg)
	}

	if m.Screen == ScreenHistory {
		switch msg.String() {
		case "esc", "q":
			m.Screen = ScreenTable
			m.History = nil
			m.Metric = ""
			return m, nil
		}
		return m, nil
	}

	switch msg.String() {
	case "t":
		m.promptFor = promptTask
		m.prompt.SetValue(m.Filter.Task)
		m.prompt.Focus()
		return m, nil
	case "m":
		m.promptFor = promptMetric
		m.prompt.SetValue(m.Filter.Metric)
		m.prompt.Focus()
		return m, nil
	case "enter":
		selected, ok := m.Selected()
		if !ok {
			return m, nil
		}
		m.Screen = ScreenHistory
		m.Metric = selected.Metric
		m.History = nil
		return m, loadHistory(m.reader, m.project, m.Metric)
	case "b":
		return m, openBaseline(m.project)
	case "p":
		if !m.HasPrevPage() {
			return m, nil
		}
		filter := m.Filter
		filter.Offset -= m.pageLimit()
		if filter.Offset < 0 {
			filter.Offset = 0
		}
		return m, loadBenchmarks(m.reader, m.project, filter)
	case "n":
		if !m.HasNextPage() {
			return m, nil
		}
		filter := m.Filter
		filter.Offset += m.pageLimit()
		return m, loadBenchmarks(m.reader, m.project, filter)
	}

	updated, cmd := m.table.Update(msg)
	m.table = updated
	return m, cmd
}

// handlePromptKey collects a filter. Enter applies it and reloads from the
// first page: a filter that kept the old offset would open on page three of a
// list that now has one.
func (m Model) handlePromptKey(msg tea.KeyMsg) (tabs.Tab, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		value := m.prompt.Value()
		filter := m.Filter
		filter.Offset = 0
		switch m.promptFor {
		case promptTask:
			filter.Task = value
		case promptMetric:
			filter.Metric = value
		}
		m.prompt.Blur()
		m.promptFor = promptNone
		return m, loadBenchmarks(m.reader, m.project, filter)
	case tea.KeyEsc:
		m.prompt.Blur()
		m.prompt.SetValue("")
		m.promptFor = promptNone
		return m, nil
	}

	updated, cmd := m.prompt.Update(msg)
	m.prompt = updated
	return m, cmd
}

// resize rebuilds the table against the width and height the tab now has.
//
// The cursor is put back on the first row whenever it is out of range.
// bubbles/table clamps its cursor to len(rows)-1 on SetRows, so emptying the
// table — which scoping to another project does — leaves it at -1, and nothing
// in the component moves it back when rows arrive. A table whose first row
// cannot be selected looks like a table whose "enter" is broken.
func (m Model) resize() Model {
	m.table.SetColumns(m.columns())
	m.table.SetRows(m.rows())
	m.table.SetWidth(m.bodyWidth())
	m.table.SetHeight(m.tableHeight())
	if m.table.Cursor() < 0 && len(m.Items) > 0 {
		m.table.SetCursor(0)
	}
	return m
}
