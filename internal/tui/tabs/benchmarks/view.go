package benchmarks

import (
	"fmt"
	"strings"

	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/shared"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
)

// Widths of the table's columns, in terminal cells. bubbles/table takes fixed
// widths by design — that is what makes a column a column — so these are the
// caps the narrow layout sheds rather than a solver's inputs.
const (
	metricCells   = 22
	unitCells     = 8
	baselineCells = 14
	latestCells   = 14
	deltaCells    = 10

	// narrowBreakpoint is where the table drops to metric · latest · Δ. Below
	// it the five columns each get so little that every value is an ellipsis.
	narrowBreakpoint = 100

	// bodyMargin is what the app frame spends either side of a tab's body,
	// minBodyWidth the narrowest body worth laying out, and defaultBodyWidth
	// what a tab with no size yet assumes.
	bodyMargin       = 4
	minBodyWidth     = 24
	defaultBodyWidth = 80

	// chrome is what the screen spends around the table: its own heading, the
	// filter line, the tab bar above and the status bar below.
	chrome = 7
	// minTableRows is the floor applied when the terminal is too short for
	// the arithmetic to leave a usable table.
	minTableRows = 3
)

func (m Model) bodyWidth() int {
	if m.Width <= 0 {
		return defaultBodyWidth
	}
	if w := m.Width - bodyMargin; w >= minBodyWidth {
		return w
	}
	return minBodyWidth
}

// narrow reports whether the table has to shed its two middle columns.
func (m Model) narrow() bool { return m.Width > 0 && m.Width < narrowBreakpoint }

func (m Model) tableHeight() int {
	h := m.Height - chrome
	if h < minTableRows {
		return minTableRows
	}
	return h
}

// columns is the table's shape at the current width: the full five, or the
// three a narrow terminal can still read.
func (m Model) columns() []table.Column {
	if m.narrow() {
		return []table.Column{
			{Title: "metric", Width: metricCells},
			{Title: "latest", Width: latestCells},
			{Title: "Δ", Width: deltaCells},
		}
	}
	return []table.Column{
		{Title: "metric", Width: metricCells},
		{Title: "unit", Width: unitCells},
		{Title: "baseline", Width: baselineCells},
		{Title: "latest", Width: latestCells},
		{Title: "Δ", Width: deltaCells},
	}
}

func (m Model) rows() []table.Row {
	rows := make([]table.Row, 0, len(m.Items))
	for _, b := range m.Items {
		if m.narrow() {
			rows = append(rows, table.Row{b.Metric, formatValue(b.Value, ""), m.delta(b)})
			continue
		}
		rows = append(rows, table.Row{b.Metric, b.Unit, m.baseline(b), formatValue(b.Value, ""), m.delta(b)})
	}
	return rows
}

// baseline is the metric's own baseline, or a neutral marker when it has none
// yet: "no baseline" and "a baseline of zero" are different answers.
func (m Model) baseline(b data.Benchmark) string {
	if b.BaselineValue == nil {
		return m.styles.Icons.Glyph(theme.IconUnknown)
	}
	return formatValue(*b.BaselineValue, "")
}

// delta is the measurement's distance from its baseline, with the arrow read
// for that metric's own direction: the same -13.6% is an improvement for a
// latency and a regression for a throughput.
//
// The arrow says which way the number moved and the colour says whether that
// is good, so the two readings stay separable — a reader who cannot tell the
// palette's green from its red still sees the direction.
func (m Model) delta(b data.Benchmark) string {
	pct := b.Delta()
	if pct == nil {
		return m.styles.Icons.Glyph(theme.IconTrendFlat)
	}

	icon := theme.IconTrendFlat
	switch {
	case *pct > 0:
		icon = theme.IconTrendUp
	case *pct < 0:
		icon = theme.IconTrendDown
	}

	text := fmt.Sprintf("%s%+.1f%%", m.styles.Icons.Glyph(icon), *pct)
	improved := b.Improved()
	if improved == nil {
		return m.styles.StatusBar.Render(text)
	}
	if *improved {
		return m.styles.SuccessInline.Render(text)
	}
	return m.styles.DangerInline.Render(text)
}

// formatValue prints a measurement the way a person reads it: whole numbers
// without a trailing ".0".
func formatValue(value float64, unit string) string {
	text := fmt.Sprintf("%.2f", value)
	if value == float64(int64(value)) {
		text = fmt.Sprintf("%d", int64(value))
	}
	if unit != "" {
		text += " " + unit
	}
	return text
}

// View renders the tab.
func (m Model) View() string {
	if m.Screen == ScreenHistory {
		return m.viewHistory()
	}
	return m.viewTable()
}

func (m Model) viewTable() string {
	var b strings.Builder

	b.WriteString(m.styles.Header.Render("  " + m.heading()))
	b.WriteString("\n")

	if line := m.viewFilterLine(); line != "" {
		b.WriteString(line)
		b.WriteString("\n")
	}
	if !m.Notice.Empty() {
		b.WriteString(m.Notice.Render(m.styles))
		b.WriteString("\n")
	}

	switch {
	case m.project == "":
		b.WriteString(m.styles.NoResults.Render("No project is active. Press ctrl+p to pick one."))
		return b.String()
	case !m.loaded:
		b.WriteString(shared.Loading(m.styles, spinner.Model{}, "the benchmarks"))
		return b.String()
	case len(m.Items) == 0:
		b.WriteString(m.styles.NoResults.Render("No benchmarks match this filter."))
		return b.String()
	}

	b.WriteString(m.table.View())
	b.WriteString("\n")
	b.WriteString(shared.RangeIndicator(m.styles, "benchmarks",
		m.Filter.Offset+1, m.Filter.Offset+len(m.Items), m.Total))
	return b.String()
}

// heading names what the table is showing, filters included, so the screen
// says why it is short instead of looking empty.
func (m Model) heading() string {
	parts := []string{fmt.Sprintf("Benchmarks (%d shown", m.Total)}
	if m.Filter.Task != "" {
		parts = append(parts, "task: "+m.Filter.Task)
	}
	if m.Filter.Metric != "" {
		parts = append(parts, "metric: "+m.Filter.Metric)
	}
	return strings.Join(parts, " · ") + ")"
}

// viewFilterLine draws the prompt while it is collecting a filter.
func (m Model) viewFilterLine() string {
	if !m.prompt.Focused() {
		return ""
	}
	label := "task"
	if m.promptFor == promptMetric {
		label = "metric"
	}
	return "  " + m.styles.Icons.Glyph(theme.IconFilter) + " " + label + ": " + m.prompt.View()
}

// viewHistory draws one metric's readings, oldest first — the series a chart
// would plot left to right, as a column so it stays exact.
func (m Model) viewHistory() string {
	var b strings.Builder

	b.WriteString(m.styles.Header.Render("  " + m.Metric + " over time"))
	b.WriteString("\n")

	if !m.Notice.Empty() {
		b.WriteString(m.Notice.Render(m.styles))
		b.WriteString("\n")
	}
	if len(m.History) == 0 {
		b.WriteString(m.styles.NoResults.Render("No readings recorded for this metric."))
		return b.String()
	}

	for _, reading := range m.History {
		b.WriteString(fmt.Sprintf("  %s  %s  %s\n",
			m.styles.Timestamp.Render(shared.Cell(shared.LocalTime(reading.CapturedAt), 20)),
			m.styles.DetailValue.Render(shared.Cell(formatValue(reading.Value, reading.Unit), 16)),
			m.delta(reading)))
	}
	return b.String()
}
