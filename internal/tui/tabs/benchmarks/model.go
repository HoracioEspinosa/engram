// Package benchmarks is the Benchmarks tab: every measurement a project has
// recorded, next to the baseline of its own metric.
//
// It is the one screen in the workspace built on bubbles/table. A benchmark
// list is a true table — the same five fields for every row, read down a
// column rather than across a card — which is exactly what that component is
// for and what the two-line list rows elsewhere in the TUI are not.
package benchmarks

import (
	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/shared"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// Screen is the tab's own screen enum: the table, or one metric's series.
type Screen int

const (
	// ScreenTable is the project's measurements.
	ScreenTable Screen = iota
	// ScreenHistory is one metric's readings over time.
	ScreenHistory
)

// pageSize is how many measurements one page of the table holds.
const pageSize = 50

// historyLimit caps one metric's series. A chart nobody can read past is not
// more history, it is more scrolling.
const historyLimit = 30

// Model is the Benchmarks tab's state.
type Model struct {
	reader data.BenchmarkReader
	styles theme.Styles

	project string

	Screen Screen
	Width  int
	Height int

	// table is the component the list screen is drawn with. It holds the
	// cursor, so the tab's own state carries the rows it was built from
	// rather than a second cursor that could disagree with it.
	table table.Model

	Items  []data.Benchmark
	Total  int
	Filter data.BenchmarkFilter

	// History is one metric's series, oldest first, and Metric is which
	// metric it belongs to.
	History []data.Benchmark
	Metric  string

	// prompt is the "t" (task) and "m" (metric) filter box. Which of the two
	// it is collecting is what promptFor says.
	prompt    textinput.Model
	promptFor promptKind

	Notice shared.Notice
	loaded bool
}

// promptKind is which filter the prompt is collecting, or none.
type promptKind int

const (
	promptNone promptKind = iota
	promptTask
	promptMetric
)

// New creates the Benchmarks tab bound to its reader.
func New(reader data.BenchmarkReader) Model {
	ti := textinput.New()
	ti.CharLimit = 120
	ti.Width = 32

	return Model{
		reader: reader,
		styles: theme.Default(),
		table:  newTable(theme.Default()),
		prompt: ti,
	}
}

// newTable builds the component with the tab's own styling: a cursor drawn in
// weight and colour, never as an inverted block, so the workspace stays
// readable on a translucent terminal.
func newTable(st theme.Styles) table.Model {
	t := table.New(table.WithFocused(true))
	t.SetStyles(tableStyles(st))
	return t
}

func tableStyles(st theme.Styles) table.Styles {
	s := table.DefaultStyles()
	s.Header = st.Help.UnsetMargins().Bold(true)
	s.Cell = st.DetailValue
	s.Selected = st.ListSelected.UnsetPadding()
	return s
}

// WithStyles returns a copy of m painted with styles instead of the default.
func (m Model) WithStyles(styles theme.Styles) Model {
	m.styles = styles
	m.table.SetStyles(tableStyles(styles))
	return m
}

// Styles exposes the tab's current style set for app-level tests that assert
// every tab paints with the same resolved palette.
func (m Model) Styles() theme.Styles { return m.styles }

// WithProject returns a copy of m scoped to project, with everything the
// previous one loaded dropped.
func (m Model) WithProject(project string) Model {
	m.project = project
	m.Screen = ScreenTable
	m.Items = nil
	m.Total = 0
	m.History = nil
	m.Metric = ""
	m.Filter = data.BenchmarkFilter{}
	m.promptFor = promptNone
	m.prompt.Blur()
	m.prompt.SetValue("")
	m.Notice = shared.Notice{}
	m.loaded = false
	m.table.SetRows(nil)
	return m
}

// Project reports which project the tab is scoped to.
func (m Model) Project() string { return m.project }

// Title is the label the tab bar shows for this tab.
func (Model) Title() string { return "Benchmarks" }

// Init loads the tab's first screen.
func (m Model) Init() tea.Cmd { return m.Refresh() }

// Refresh reloads whatever is on display: the table, or the metric series.
func (m Model) Refresh() tea.Cmd {
	if m.project == "" {
		return nil
	}
	if m.Screen == ScreenHistory && m.Metric != "" {
		return loadHistory(m.reader, m.project, m.Metric)
	}
	return loadBenchmarks(m.reader, m.project, m.Filter)
}

// CapturingText reports whether the filter prompt has the keyboard.
func (m Model) CapturingText() bool { return m.prompt.Focused() }

// HasPrevPage and HasNextPage read the store's own total rather than guessing
// from a short page.
func (m Model) HasPrevPage() bool { return m.Filter.Offset > 0 }
func (m Model) HasNextPage() bool { return m.Filter.Offset+len(m.Items) < m.Total }

func (m Model) pageLimit() int {
	if m.Filter.Limit > 0 {
		return m.Filter.Limit
	}
	return pageSize
}

// Selected returns the measurement under the cursor.
func (m Model) Selected() (data.Benchmark, bool) {
	i := m.table.Cursor()
	if i < 0 || i >= len(m.Items) {
		return data.Benchmark{}, false
	}
	return m.Items[i], true
}
