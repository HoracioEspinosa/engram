package home

import (
	"errors"
	"fmt"
	"strings"

	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/shared"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"
)

var (
	errNoProjectReader = errors.New("no project reader is bound to this workspace")
	errNoGraphSyncer   = errors.New("no graph syncer is bound to this workspace")
)

// Widths of the task row's leading columns, in terminal cells. They are caps:
// what each one actually gets is solved against the column it is drawn in.
const (
	keyCells   = 14
	stateCells = 12
	metricMin  = 10
	metricMax  = 22
	valueCells = 14
	deltaCells = 8
)

// View renders the Home tab.
func (m Model) View() string {
	var b strings.Builder

	if !m.notice.Empty() {
		b.WriteString(m.notice.Render(m.styles))
		b.WriteString("\n")
	}

	if m.project == "" {
		b.WriteString(m.styles.NoResults.Render("No project is active. Press ctrl+p to pick one."))
		return b.String()
	}
	if !m.loaded {
		b.WriteString(shared.Loading(m.styles, spinner.Model{}, m.project))
		return b.String()
	}

	b.WriteString(m.viewCard())
	b.WriteString("\n")

	r := m.regions()
	if !r.HasDetail() {
		for i, blk := range m.visibleBlocks() {
			b.WriteString(m.viewBlock(blk, i == m.cursor[0], m.contentWidth()))
		}
		return b.String()
	}

	b.WriteString(shared.SplitPanes(m.styles, r, m.viewColumn(shared.PaneMaster, r.Master.Dx()), m.viewColumn(shared.PaneDetail, r.Detail.Dx()-4)))
	return b.String()
}

// contentWidth is how many cells a block may occupy when there is one column.
func (m Model) contentWidth() int { return m.bodyWidth() }

// viewColumn draws one pane's blocks, marking the cursor only in the pane that
// has the keyboard: two cursors on screen would say the reader is in two
// places at once.
func (m Model) viewColumn(p shared.Pane, width int) string {
	var b strings.Builder
	focused := m.focus == p
	for i, blk := range pane(p) {
		b.WriteString(m.viewBlock(blk, focused && i == m.cursor[paneSlot(p)], width))
	}
	return strings.TrimRight(b.String(), "\n")
}

func paneSlot(p shared.Pane) int {
	if p == shared.PaneDetail {
		return 1
	}
	return 0
}

// viewBlock wraps a block's rows in its heading, marking it with the cursor
// glyph when it is the one enter would open.
func (m Model) viewBlock(blk block, selected bool, width int) string {
	heading := "  " + blk.heading()
	if selected {
		heading = shared.RowCursor(m.styles, true) + blk.heading()
	}

	var body string
	switch blk {
	case blockTasks:
		body = m.viewTasks(width)
	case blockEvidence:
		body = m.viewEvidence(width)
	case blockGraph:
		body = m.viewGraph(width)
	case blockBenchmarks:
		body = m.viewBenchmarks(width)
	}
	return m.styles.SectionHeading.Render(heading) + "\n" + body
}

// viewCard is the project seen at a glance: what it is called, what kind of
// thing it is, what it is for, and where it sits in the forest.
//
// The card's own free-text icon field is not drawn. A glyph nobody validated
// can be two cells wide in one emulator and one in another, which would break
// every column beside it; the kind's own glyph says the same thing and is
// guaranteed one cell in all three icon modes.
func (m Model) viewCard() string {
	c := m.card
	accent := m.styles.Emphasis
	if colour, ok := theme.ResolveColor(m.styles.Palette, derefString(c.Color)); ok {
		accent = accent.Foreground(colour)
	}

	name := c.Slug
	if c.DisplayName != "" && c.DisplayName != c.Slug {
		name = c.DisplayName
	}

	title := m.styles.Icons.ProjectKind(c.Kind) + " " + accent.Render(name)
	if c.Kind != "" {
		title += "  " + m.styles.TypeBadge.Render(c.Kind)
	}

	lines := []string{title}
	if crumb := m.breadcrumb(); crumb != "" {
		lines = append(lines, m.styles.Timestamp.Render(crumb))
	}
	if desc := strings.TrimSpace(derefString(c.Description)); desc != "" {
		lines = append(lines, m.styles.DetailValue.Render(shared.Truncate(desc, m.contentWidth()-6)))
	}
	lines = append(lines, m.viewCounters())

	return m.styles.StatCard.Render(strings.Join(lines, "\n"))
}

// breadcrumb is the ancestor chain the root handed down, the project itself
// last. A root project has no chain and draws none.
func (m Model) breadcrumb() string {
	if len(m.ancestors) == 0 {
		return ""
	}
	names := make([]string, 0, len(m.ancestors)+1)
	for _, node := range m.ancestors {
		names = append(names, projectLabel(node))
	}
	names = append(names, m.card.Slug)
	return strings.Join(names, " "+m.styles.Icons.Glyph(theme.IconChevronRight)+" ")
}

func projectLabel(node data.ProjectNode) string {
	if name := strings.TrimSpace(node.DisplayName); name != "" {
		return name
	}
	return node.Slug
}

// viewCounters is the health line: what the project holds, in one row rather
// than the four-row block the dashboard used, so the card leaves room for the
// blocks underneath it.
func (m Model) viewCounters() string {
	h := m.health
	pairs := [][2]string{
		{fmt.Sprintf("%d", h.Observations), "observations"},
		{fmt.Sprintf("%d", h.TasksActive), "open tasks"},
		{fmt.Sprintf("%d", h.Evidence), "evidence"},
		{fmt.Sprintf("%d", h.Runbooks), "runbooks"},
	}
	parts := make([]string, 0, len(pairs))
	for _, p := range pairs {
		parts = append(parts, m.styles.Emphasis.Render(p[0])+" "+m.styles.StatusBar.Render(p[1]))
	}
	return strings.Join(parts, "   ")
}

func (m Model) viewTasks(width int) string {
	if len(m.tasks) == 0 {
		return m.styles.NoResults.Render("No open tasks.") + "\n"
	}
	w := shared.SolveColumns(width-4, 1, []shared.Column{
		{Min: keyCells, Max: keyCells},
		{Min: stateCells, Max: stateCells},
		{Min: 10, Weight: 1},
	})
	var b strings.Builder
	for _, t := range m.tasks {
		b.WriteString(fmt.Sprintf("  %s %s %s\n",
			m.styles.ID.Render(shared.Field(t.Key(), w[0])),
			m.styles.Icons.TaskState(t.State)+" "+m.styles.DetailValue.Render(shared.Field(t.State, w[1]-2)),
			shared.Field(t.Title, w[2])))
	}
	return b.String()
}

func (m Model) viewEvidence(width int) string {
	if len(m.evidence) == 0 {
		return m.styles.NoResults.Render("No evidence captured yet.") + "\n"
	}
	var b strings.Builder
	for _, e := range m.evidence {
		badge := m.styles.StaleBadge.Render("unattached")
		if e.AttachedJira {
			badge = m.styles.AttachedBadge.Render("attached " + m.styles.Icons.Glyph(theme.IconFresh))
		}
		room := width - 4 - lipgloss.Width(badge) - 1
		if room < 8 {
			room = 8
		}
		b.WriteString(fmt.Sprintf("  %s %s\n", shared.Field(e.Path, room), badge))
	}
	return b.String()
}

// viewGraph renders the store's own verdict about the code graph. The reason
// is printed literally: the TUI never recomputes staleness, so a reason it
// does not recognise still reaches the reader instead of being flattened to
// "stale".
func (m Model) viewGraph(width int) string {
	g := m.graphState
	if m.syncing {
		return shared.Loading(m.styles, spinner.Model{}, "the graph") + "\n"
	}
	if g.Commit == "" && g.Nodes == 0 {
		return m.styles.NoResults.Render("No graph has been built for this project.") + "\n"
	}

	state := m.styles.SuccessInline.Render(m.styles.Icons.Glyph(theme.IconFresh) + " fresh")
	if g.Stale {
		reason := g.StaleReason
		if reason == "" {
			reason = "stale"
		}
		state = m.styles.StaleBadge.Render(m.styles.Icons.Glyph(theme.IconStale) + " " + reason)
	}

	lines := []string{
		"  " + state,
		fmt.Sprintf("  %s nodes  %s edges  %s communities",
			m.styles.Emphasis.Render(fmt.Sprintf("%d", g.Nodes)),
			m.styles.Emphasis.Render(fmt.Sprintf("%d", g.Edges)),
			m.styles.Emphasis.Render(fmt.Sprintf("%d", g.Communities))),
	}
	if g.BuiltAt != "" {
		lines = append(lines, "  "+m.styles.Timestamp.Render("built "+shared.LocalTime(g.BuiltAt)))
	}
	return strings.Join(truncateLines(lines, width), "\n") + "\n"
}

// viewBenchmarks is each metric's latest reading next to its own baseline,
// with the arrow that says which way the number moved for that metric's own
// direction — lower is better for a latency, higher for a throughput.
func (m Model) viewBenchmarks(width int) string {
	if len(m.bench) == 0 {
		return m.styles.NoResults.Render("No benchmarks recorded.") + "\n"
	}
	w := shared.SolveColumns(width-4, 1, []shared.Column{
		{Min: metricMin, Max: metricMax, Weight: 2},
		{Min: 8, Max: valueCells, Weight: 1},
		{Min: deltaCells, Max: deltaCells},
	})
	var b strings.Builder
	for _, bench := range m.bench {
		b.WriteString(fmt.Sprintf("  %s %s %s\n",
			shared.Field(bench.Metric, w[0]),
			m.styles.DetailValue.Render(shared.Field(formatValue(bench.Value, bench.Unit), w[1])),
			m.deltaCell(bench, w[2])))
	}
	return b.String()
}

// deltaCell renders one measurement's distance from its baseline. A metric
// with no baseline yet gets a neutral marker rather than a zero: "unchanged"
// and "nothing to change from" are different answers.
func (m Model) deltaCell(b data.Benchmark, width int) string {
	delta := b.Delta()
	if delta == nil {
		return m.styles.StatusBar.Render(shared.Cell(m.styles.Icons.Glyph(theme.IconTrendFlat), width))
	}

	icon := theme.IconTrendFlat
	style := m.styles.StatusBar
	if improved := b.Improved(); improved != nil {
		style = m.styles.DangerInline
		if *improved {
			style = m.styles.SuccessInline
		}
	}
	switch {
	case *delta > 0:
		icon = theme.IconTrendUp
	case *delta < 0:
		icon = theme.IconTrendDown
	}
	text := fmt.Sprintf("%s%+.1f%%", m.styles.Icons.Glyph(icon), *delta)
	return style.Render(shared.Cell(shared.CutCells(text, width), width))
}

// formatValue prints a measurement the way a person reads it: whole numbers
// without a trailing ".0", and the unit attached so a column of latencies and
// a column of counts cannot be confused.
func formatValue(value float64, unit string) string {
	text := formatNumber(value)
	if unit != "" {
		text += " " + unit
	}
	return text
}

func formatNumber(v float64) string {
	if v == float64(int64(v)) {
		return fmt.Sprintf("%d", int64(v))
	}
	return fmt.Sprintf("%.2f", v)
}

// truncateLines cuts every line to the width its column has, so a long
// staleness reason from the store shortens instead of wrapping into the pane
// beside it.
func truncateLines(lines []string, width int) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, shared.CutCells(line, width))
	}
	return out
}

func derefString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
