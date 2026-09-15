package home

import (
	"github.com/HoracioEspinosa/engram/internal/tui/shared"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs"
)

// block names one of Home's four navigable blocks. The project card is not
// one of them: it is the header both columns sit under, and it opens nothing.
type block int

const (
	blockTasks block = iota
	blockEvidence
	blockGraph
	blockBenchmarks
)

// target is the tab enter opens for this block. Every block leads somewhere:
// a summary the reader cannot act on is a poster, not a workspace.
func (b block) target() tabs.ID {
	switch b {
	case blockTasks:
		return tabs.Tasks
	case blockEvidence:
		return tabs.Evidence
	case blockGraph:
		return tabs.Graph
	case blockBenchmarks:
		return tabs.Benchmarks
	}
	return tabs.Home
}

// heading is the block's own title.
func (b block) heading() string {
	switch b {
	case blockTasks:
		return "recent tasks"
	case blockEvidence:
		return "latest evidence"
	case blockGraph:
		return "code graph"
	case blockBenchmarks:
		return "benchmarks"
	}
	return ""
}

// columns is which blocks each pane holds. The master keeps what carries the
// longest text — task titles and evidence paths — because it is the wider of
// the two; the graph and the benchmarks are numbers and fit the narrow side.
var columns = [2][]block{
	{blockTasks, blockEvidence},
	{blockGraph, blockBenchmarks},
}

// bodyMargin is what the app frame spends either side of a tab's body,
// minBodyWidth the narrowest body worth laying out, and defaultBodyWidth what
// a tab with no size yet assumes.
const (
	bodyMargin       = 4
	minBodyWidth     = 24
	defaultBodyWidth = 80
)

// bodyWidth is how many cells the tab may actually draw in: the terminal less
// what the frame around it spends. A tab that laid out against the raw
// terminal width would overflow by exactly that frame.
func (m Model) bodyWidth() int {
	if m.width <= 0 {
		return defaultBodyWidth
	}
	if w := m.width - bodyMargin; w >= minBodyWidth {
		return w
	}
	return minBodyWidth
}

// regions solves the two panes against the width the tab may draw in.
func (m Model) regions() shared.Regions {
	return shared.Layout(m.bodyWidth(), m.height)
}

// pane returns the blocks in one column.
func pane(p shared.Pane) []block {
	if p == shared.PaneDetail {
		return columns[1]
	}
	return columns[0]
}

// selected returns the block under the cursor.
//
// Below the split breakpoint there is one column on screen holding every
// block in order, so the same cursor has to address a longer list: the panes
// are a layout, not a second set of state.
func (m Model) selected() block {
	blocks := m.visibleBlocks()
	i := m.cursor[m.focusIndex()]
	if i < 0 || i >= len(blocks) {
		return blocks[0]
	}
	return blocks[i]
}

// visibleBlocks is what the current width actually draws as one navigable
// list: both columns concatenated when there is only one column to draw them
// in, and the focused column's own blocks when there are two.
func (m Model) visibleBlocks() []block {
	if !m.regions().HasDetail() {
		return append(append([]block{}, columns[0]...), columns[1]...)
	}
	return pane(m.focus)
}

// focusIndex is the cursor slot the current focus uses. A single-column
// layout always uses the first: there is no second column to hold a place in.
func (m Model) focusIndex() int {
	if m.regions().HasDetail() && m.focus == shared.PaneDetail {
		return 1
	}
	return 0
}
