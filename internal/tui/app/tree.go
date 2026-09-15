package app

import (
	"fmt"
	"sort"
	"strings"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/shared"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// Caps and fixed costs of the project tree, in terminal cells. What each
// column actually gets is solved against the overlay's width — see
// treeColumns — so the header row and the data rows cannot drift apart.
const (
	treeSlugCells  = 24
	treeCountCells = 6

	// treeRowFixed is what a row spends outside its solved columns: the
	// cursor marker, the expander and the kind glyph, each with its space.
	treeRowFixed = 6

	// treeIndentCells is how far one level of nesting shifts a row.
	treeIndentCells = 2
)

// treeKeys are the bindings the overlay owns. Like the theme picker's, they
// live next to the screen that answers them rather than in the global keymap:
// the key that opens the overlay is a global, everything inside it is not.
var treeKeys = struct {
	Move, Fold, Open, Filter, Health, Reload, Close key.Binding
}{
	Move: key.NewBinding(
		key.WithKeys("up", "k", "down", "j"),
		key.WithHelp("j/k", "move"),
	),
	Fold: key.NewBinding(
		key.WithKeys(" "),
		key.WithHelp("space", "fold"),
	),
	Open: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "open"),
	),
	Filter: key.NewBinding(
		key.WithKeys("/"),
		key.WithHelp("/", "filter"),
	),
	Health: key.NewBinding(
		key.WithKeys("i"),
		key.WithHelp("i", "sort by health"),
	),
	Reload: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "refresh"),
	),
	Close: key.NewBinding(
		key.WithKeys("esc", "ctrl+p"),
		key.WithHelp("esc", "close"),
	),
}

// treeHelp lists the overlay's own bindings, in the order the status bar leads
// with them. The hints are derived from this declaration, so what the overlay
// answers to and what it advertises are one thing.
func treeHelp() []key.Binding {
	return []key.Binding{
		treeKeys.Move,
		key.NewBinding(key.WithKeys("g", "G"), key.WithHelp("g/G", "top/bottom")),
		treeKeys.Fold,
		treeKeys.Open,
		treeKeys.Filter,
		treeKeys.Health,
		treeKeys.Reload,
		treeKeys.Close,
	}
}

// treeRow is one rendered line of the flattened forest.
type treeRow struct {
	node  data.ProjectNode
	depth int
	// children is how many children the node has in the loaded forest, so a
	// leaf draws no expander at all.
	children int
	expanded bool
	// forced marks a row on screen only because a descendant matched the
	// filter. It is drawn de-emphasised: it is the path to a hit, not a hit.
	forced bool
	// slugMatched and nameMatched are the rune positions the filter matched in
	// each label, for the highlight.
	slugMatched []int
	nameMatched []int
}

// treeModel is the project tree overlay (ctrl+p), which replaced the flat
// project selector: a card list gave no hint that clarodrive's six instances
// were one family, so picking the right one meant reading six nearly identical
// slugs instead of opening a parent.
//
// It is a plain value the root holds directly, like the theme picker: the tree
// repaints nothing but itself and owns no tab.
type treeModel struct {
	reader data.ProjectTreeReader

	// open is what the root draws on: the tree is composited over whatever
	// screen is showing rather than replacing it.
	open bool

	roots []data.ProjectNode
	rows  []treeRow

	cursor int

	// collapsed holds the slugs whose children are hidden. It is rebuilt on
	// every write rather than mutated in place: the root is a value, so every
	// copy of this model would otherwise share one map and a fold applied to a
	// model the runtime discards would still be visible in the one it keeps.
	collapsed map[string]bool

	filterInput textinput.Model

	// healthSort ranks the cards that need attention first, within each level.
	healthSort bool

	// focus is the slug the cursor should land on once rows exist — the
	// project already active when the overlay opened. Without it, opening
	// ctrl+p on a workspace scoped to a leaf would drop the cursor on the
	// first root and make the reader walk back down.
	focus string

	loaded bool
	err    string
}

// newTreeModel creates the overlay bound to reader. The filter input starts
// blurred: "/" focuses it, the same way every other search box in the
// workspace works.
func newTreeModel(reader data.ProjectTreeReader) treeModel {
	ti := textinput.New()
	ti.Placeholder = "Filter by slug, name, tag or alias..."
	ti.CharLimit = 128
	ti.Width = 40

	return treeModel{reader: reader, filterInput: ti, collapsed: map[string]bool{}}
}

// WithProjectTree returns a copy of the root bound to the forest reader. It
// feeds both the ctrl+p overlay and the status bar's breadcrumb, which is why
// there is one reader and not two.
func (m Model) WithProjectTree(r data.ProjectTreeReader) Model {
	m.treeReader = r
	m.tree.reader = r
	return m
}

// treeLoadedMsg carries loadTree's result.
type treeLoadedMsg struct {
	roots []data.ProjectNode
	err   error
}

// loadTree returns the command that reads the whole forest.
//
// One call, not one per node: ProjectTreeReader.ProjectTree builds the forest
// from at most two store reads, which is the entire reason the overlay asks
// for a tree instead of walking parents itself.
func loadTree(r data.ProjectTreeReader) tea.Cmd {
	return func() tea.Msg {
		if r == nil {
			return treeLoadedMsg{err: errNoProjectTree}
		}
		roots, err := r.ProjectTree()
		return treeLoadedMsg{roots: roots, err: err}
	}
}

// errNoProjectTree is what a workspace built without a forest reader reports
// when ctrl+p is pressed: nothing to read, said out loud rather than an
// overlay that opens empty and looks broken.
var errNoProjectTree = fmt.Errorf("no project tree is bound to this workspace")

// applyLoaded stores the forest and re-flattens it, so reopening the overlay
// or reloading it keeps whatever filter and folds were in place.
func (t treeModel) applyLoaded(msg treeLoadedMsg) treeModel {
	if msg.err != nil {
		t.err = msg.err.Error()
		return t
	}
	t.err = ""
	t.roots = msg.roots
	t.loaded = true
	return t.flatten().focusOn(t.focus)
}

// focusOn puts the cursor on slug when that project is among the rows on
// screen, and leaves it alone when it is not — a filter that hides the active
// project must not drag the cursor somewhere arbitrary.
func (t treeModel) focusOn(slug string) treeModel {
	if slug == "" {
		return t
	}
	for i, row := range t.rows {
		if row.node.Slug == slug {
			t.cursor = i
			break
		}
	}
	return t
}

// flatten walks the forest in preorder and produces the rows on screen.
//
// Filtering and folding meet here rather than in two passes: a fold hides a
// subtree the reader chose to put away, while a filter hides a subtree that
// holds nothing the reader asked for — and a filter wins, because a match
// nobody can see is the same as no match at all.
func (t treeModel) flatten() treeModel {
	term := strings.TrimSpace(t.filterInput.Value())
	matched, slugHits, nameHits := t.matchNodes(term)

	rows := make([]treeRow, 0, len(t.roots))
	var walk func(nodes []data.ProjectNode, depth int)
	walk = func(nodes []data.ProjectNode, depth int) {
		for _, node := range t.sortLevel(nodes) {
			visible := term == "" || matched[node.Slug] || t.subtreeMatches(node, matched)
			if !visible {
				continue
			}
			// While filtering, a fold is ignored: the reader asked to see
			// what matches, and a parent they had put away would swallow the
			// hit underneath it.
			expanded := term != "" || !t.collapsed[node.Slug]
			rows = append(rows, treeRow{
				node:        node,
				depth:       depth,
				children:    len(node.Children),
				expanded:    expanded,
				forced:      term != "" && !matched[node.Slug],
				slugMatched: slugHits[node.Slug],
				nameMatched: nameHits[node.Slug],
			})
			if expanded {
				walk(node.Children, depth+1)
			}
		}
	}
	walk(t.roots, 0)

	t.rows = rows
	if t.cursor >= len(t.rows) {
		t.cursor = len(t.rows) - 1
	}
	if t.cursor < 0 {
		t.cursor = 0
	}
	return t
}

// matchNodes runs the filter over the whole forest and reports which slugs
// matched, plus where the term hit each node's two visible labels.
//
// Three passes, not one: the haystack decides membership (a project found by
// its alias is still found), while the slug and the display name are filtered
// on their own because those are the strings on screen — a highlight has to
// point at characters the reader can actually see.
func (t treeModel) matchNodes(term string) (matched map[string]bool, slugHits, nameHits map[string][]int) {
	slugs, names, haystacks, order := t.searchTargets()

	matched = make(map[string]bool, len(order))
	for _, m := range shared.Fuzzy(term, haystacks) {
		matched[order[m.Index]] = true
	}

	slugHits = make(map[string][]int, len(order))
	if term != "" {
		for _, m := range shared.Fuzzy(term, slugs) {
			slugHits[order[m.Index]] = m.MatchedIndexes
		}
	}

	nameHits = make(map[string][]int, len(order))
	if term != "" {
		for _, m := range shared.Fuzzy(term, names) {
			nameHits[order[m.Index]] = m.MatchedIndexes
		}
	}

	return matched, slugHits, nameHits
}

// searchTargets flattens the forest into the parallel slices the filter runs
// over: the slug, the display name, everything a project answers to, and the
// slug each position belongs to.
func (t treeModel) searchTargets() (slugs, names, haystacks, order []string) {
	var walk func(nodes []data.ProjectNode)
	walk = func(nodes []data.ProjectNode) {
		for _, node := range nodes {
			slugs = append(slugs, node.Slug)
			names = append(names, node.DisplayName)
			haystacks = append(haystacks, projectHaystack(node))
			order = append(order, node.Slug)
			walk(node.Children)
		}
	}
	walk(t.roots)
	return slugs, names, haystacks, order
}

// projectHaystack is everything a project can be found by: what it is called,
// what it is about, and every other name it answers to.
func projectHaystack(node data.ProjectNode) string {
	parts := make([]string, 0, 3+len(node.Tags)+len(node.Aliases))
	parts = append(parts, node.Slug, node.DisplayName)
	parts = append(parts, node.Tags...)
	parts = append(parts, node.Aliases...)
	return strings.Join(parts, " ")
}

// subtreeMatches reports whether anything under node matched, which is what
// keeps a parent on screen while its child is the hit.
func (t treeModel) subtreeMatches(node data.ProjectNode, matched map[string]bool) bool {
	for _, child := range node.Children {
		if matched[child.Slug] || t.subtreeMatches(child, matched) {
			return true
		}
	}
	return false
}

// sortLevel orders one level of siblings. The store's own order is kept unless
// "i" asked for the cards that need attention first, and the sort is stable so
// two equally healthy projects never swap places between renders.
func (t treeModel) sortLevel(nodes []data.ProjectNode) []data.ProjectNode {
	if !t.healthSort || len(nodes) < 2 {
		return nodes
	}
	sorted := append([]data.ProjectNode(nil), nodes...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return worseHealthThan(sorted[i].Counts, sorted[j].Counts)
	})
	return sorted
}

// worseHealthThan reports whether a needs attention before b: more stale
// runbooks first, then more open tasks, then more observations as a final
// deterministic tiebreak.
func worseHealthThan(a, b store.ProjectCardCounts) bool {
	if a.RunbooksStale != b.RunbooksStale {
		return a.RunbooksStale > b.RunbooksStale
	}
	if a.TasksActive != b.TasksActive {
		return a.TasksActive > b.TasksActive
	}
	return a.Observations > b.Observations
}

// applyFilter re-reads the filter input and rebuilds the rows.
func (t treeModel) applyFilter() treeModel { return t.flatten() }

// toggleHealthSort flips the health ordering and re-flattens.
func (t treeModel) toggleHealthSort() treeModel {
	t.healthSort = !t.healthSort
	return t.flatten()
}

// toggleFold folds or unfolds the node under the cursor. A leaf has nothing to
// fold, so space on one is left alone rather than recording a fold nobody can
// see or undo.
func (t treeModel) toggleFold() treeModel {
	row, ok := t.selectedRow()
	if !ok || row.children == 0 {
		return t
	}
	collapsed := make(map[string]bool, len(t.collapsed)+1)
	for slug, v := range t.collapsed {
		collapsed[slug] = v
	}
	if collapsed[row.node.Slug] {
		delete(collapsed, row.node.Slug)
	} else {
		collapsed[row.node.Slug] = true
	}
	t.collapsed = collapsed
	return t.flatten()
}

// moveCursor shifts the cursor by delta, clamped to the rows on screen.
func (t treeModel) moveCursor(delta int) treeModel {
	if len(t.rows) == 0 {
		return t
	}
	t.cursor += delta
	if t.cursor < 0 {
		t.cursor = 0
	}
	if t.cursor >= len(t.rows) {
		t.cursor = len(t.rows) - 1
	}
	return t
}

func (t treeModel) moveCursorToStart() treeModel {
	t.cursor = 0
	return t
}

func (t treeModel) moveCursorToEnd() treeModel {
	if len(t.rows) > 0 {
		t.cursor = len(t.rows) - 1
	}
	return t
}

// selectedRow returns the row under the cursor.
func (t treeModel) selectedRow() (treeRow, bool) {
	if t.cursor < 0 || t.cursor >= len(t.rows) {
		return treeRow{}, false
	}
	return t.rows[t.cursor], true
}

// selected returns the project under the cursor, or nil when nothing is
// selectable (nothing loaded yet, or the filter matches nothing).
func (t treeModel) selected() *data.ProjectNode {
	row, ok := t.selectedRow()
	if !ok {
		return nil
	}
	return &row.node
}

// ─── Update ──────────────────────────────────────────────────────────────────

// updateProjectTree is the root's single entry point for the overlay. It
// reports whether it consumed the key, so Update can hand it every keystroke
// and let the overlay decide.
func (m Model) updateProjectTree(msg tea.KeyMsg) (bool, tea.Model, tea.Cmd) {
	if !m.tree.open {
		if !key.Matches(msg, globalKeys.ProjectSelector) || m.showHelp {
			return false, m, nil
		}
		// A focused text input owns the keyboard, the same rule the tabs play
		// by (rfc-tui.md §7.1).
		if tab := m.tab(m.active); tab != nil && tab.CapturingText() {
			return false, m, nil
		}
		return true, m.openProjectTree(), loadTree(m.tree.reader)
	}

	// While filtering, every key is a character: only the input may have them.
	if m.tree.filterInput.Focused() {
		switch msg.Type {
		case tea.KeyEnter, tea.KeyEsc:
			m.tree.filterInput.Blur()
			if msg.Type == tea.KeyEsc {
				m.tree.filterInput.SetValue("")
			}
			m.tree = m.tree.applyFilter()
			return true, m, nil
		}
		updated, cmd := m.tree.filterInput.Update(msg)
		m.tree.filterInput = updated
		m.tree = m.tree.applyFilter()
		return true, m, cmd
	}

	switch {
	case key.Matches(msg, treeKeys.Close):
		// It closes even with no project chosen. Memory is the workspace-wide
		// browser and works unscoped; the project-scoped tabs render their own
		// empty state. Trapping the reader inside the overlay until they pick
		// something would make "just let me look" impossible, and ctrl+p brings
		// it straight back.
		m.tree.open = false
		return true, m, nil

	case key.Matches(msg, treeKeys.Reload):
		return true, m, loadTree(m.tree.reader)

	case key.Matches(msg, treeKeys.Fold):
		m.tree = m.tree.toggleFold()
		return true, m, nil

	case key.Matches(msg, treeKeys.Health):
		m.tree = m.tree.toggleHealthSort()
		return true, m, nil

	case key.Matches(msg, treeKeys.Filter):
		m.tree.filterInput.Focus()
		return true, m, nil

	case key.Matches(msg, treeKeys.Open):
		selected := m.tree.selected()
		if selected == nil {
			return true, m, nil
		}
		model, cmd := m.openProject(selected.Slug)
		return true, model, cmd
	}

	switch msg.String() {
	case "j", "down":
		m.tree = m.tree.moveCursor(1)
	case "k", "up":
		m.tree = m.tree.moveCursor(-1)
	case "g":
		m.tree = m.tree.moveCursorToStart()
	case "G":
		m.tree = m.tree.moveCursorToEnd()
	}
	return true, m, nil
}

// openProjectTree puts the overlay on screen with its cursor on the project
// already active, so ctrl+p opens on where the reader is rather than at the
// top of a forest they have to walk back down.
func (m Model) openProjectTree() Model {
	m.tree.open = true
	m.tree.err = ""
	m.tree.focus = m.project
	m.tree = m.tree.focusOn(m.project)
	return m
}

// openProject scopes the whole workspace to slug and closes the overlay.
//
// Every project-scoped tab is rebuilt here: neither Tasks', Evidence's nor
// Runbooks' Refresh() takes a project of its own (tabs.Tab is a
// project-agnostic contract), so each has to already know the new slug before
// it is ever activated.
func (m Model) openProject(slug string) (tea.Model, tea.Cmd) {
	m.project = slug
	m.tree.open = false
	m.active = tabs.Home
	m.home = m.home.WithProject(slug)
	m.tasks = m.tasks.WithProject(slug)
	m.evidence = m.evidence.WithProject(slug)
	m.benchmarks = m.benchmarks.WithProject(slug)
	m.runbooks = m.runbooks.WithProject(slug)
	m.graph = m.graph.WithProject(slug)
	m.memory = m.memory.WithProject(slug)
	m.ancestors = nil
	// Nothing any tab is holding belongs to the project now active.
	m.freshness = m.freshness.invalidateAll()
	m.freshness = m.freshness.loaded(tabs.Home)
	// The tree is the only way into a project, so it is also where the choice
	// is recorded: the next run reopens on the project the reader was last
	// actually working in.
	return m, tea.Batch(m.home.Refresh(), loadAncestors(m.treeReader, slug), m.rememberProject(slug))
}

// ─── View ────────────────────────────────────────────────────────────────────

// viewProjectTree draws the overlay's own panel: the forest, whatever the
// filter narrowed it to, and the hints for the keys it answers.
func (m Model) viewProjectTree() string {
	var b strings.Builder

	b.WriteString(m.styles.Title.Render("Projects"))
	b.WriteString("\n")

	if m.tree.filterInput.Focused() || m.tree.filterInput.Value() != "" {
		b.WriteString(m.styles.Icons.Glyph(theme.IconSearch) + " " + m.tree.filterInput.View())
		b.WriteString("\n")
	}

	switch {
	case m.tree.err != "":
		b.WriteString(m.styles.Error.Render(m.tree.err))
		b.WriteString("\n")
	case !m.tree.loaded:
		b.WriteString(shared.Loading(m.styles, spinner.Model{}, "the projects"))
		b.WriteString("\n")
	case len(m.tree.rows) == 0:
		b.WriteString(m.styles.NoResults.Render("No projects match the filter."))
		b.WriteString("\n")
	default:
		b.WriteString(m.treeHeaderRow())
		for i, row := range m.tree.rows {
			b.WriteString(m.treeRowLine(row, i == m.tree.cursor))
		}
	}

	if hints := shared.HintsFrom(m.styles, treeHelp(), m.treeWidth()); hints != "" {
		b.WriteString(hints)
	}

	return strings.TrimRight(b.String(), "\n")
}

// treeWidth is how many cells the overlay's own content may occupy: the body
// less what the panel and the frame around it spend.
func (m Model) treeWidth() int {
	width := m.bodyWidth() - overlayChromeCells
	if width < minBodyWidth {
		return minBodyWidth
	}
	return width
}

// treeColumns solves the row against the overlay's width. The slug and the
// display name stretch; the three counters are digits and never need more than
// their cap.
func (m Model) treeColumns() []int {
	return shared.SolveColumns(m.treeWidth()-treeRowFixed, 1, []shared.Column{
		{Min: 10, Max: treeSlugCells, Weight: 2},
		{Min: 10, Weight: 3},
		{Min: treeCountCells, Max: treeCountCells},
		{Min: treeCountCells, Max: treeCountCells},
		{Min: treeCountCells, Max: treeCountCells},
	})
}

func (m Model) treeHeaderRow() string {
	w := m.treeColumns()
	line := fmt.Sprintf("      %s %s %s %s %s",
		shared.Cell("slug", w[0]),
		shared.Cell("display name", w[1]),
		shared.Cell("obs", w[2]),
		shared.Cell("open", w[3]),
		shared.Cell("ev", w[4]))
	return m.styles.Help.Render(strings.TrimRight(line, " ")) + "\n"
}

// treeRowLine renders one project.
//
// The indent is paid out of the slug column rather than added to the row, so a
// deeply nested project shortens its own name instead of pushing the counters
// off the right edge.
func (m Model) treeRowLine(row treeRow, selected bool) string {
	w := m.treeColumns()

	indent := strings.Repeat(" ", row.depth*treeIndentCells)
	slugWidth := w[0] - len(indent)
	if slugWidth < 1 {
		slugWidth = 1
	}

	slug := shared.CutCells(row.node.Slug, slugWidth)
	name := shared.CutCells(row.node.DisplayName, w[1])

	base := m.styles.ListItem
	if selected {
		base = m.styles.ListSelected
	}
	if row.forced {
		// A row on screen only because a child matched is context, not a
		// result: it is drawn in the Subtext role so the hit underneath it
		// stays the thing the eye lands on.
		base = m.styles.Timestamp
	}

	slugCell := shared.PadCells(shared.Highlight(base, m.styles.SearchHighlight, slug, row.slugMatched), slugWidth)
	nameCell := shared.PadCells(shared.Highlight(base, m.styles.SearchHighlight, name, row.nameMatched), w[1])

	counts := row.node.Counts
	line := fmt.Sprintf("%s%s %s %s%s %s %s %s %s",
		shared.RowCursor(m.styles, selected),
		m.treeExpander(row),
		m.styles.Icons.ProjectKind(row.node.Kind),
		indent, slugCell,
		nameCell,
		m.styles.ID.Render(shared.Cell(fmt.Sprintf("%d", counts.Observations), w[2])),
		m.styles.TypeBadge.Render(shared.Cell(fmt.Sprintf("%d", counts.TasksActive), w[3])),
		m.styles.Timestamp.Render(shared.Cell(fmt.Sprintf("%d", counts.Evidence), w[4])))

	return strings.TrimRight(line, " ") + "\n"
}

// treeExpander is the fold marker: pointing right for a folded subtree, down
// for an open one, and a space for a leaf, which has nothing to fold.
func (m Model) treeExpander(row treeRow) string {
	if row.children == 0 {
		return " "
	}
	if row.expanded {
		return m.styles.Icons.Glyph(theme.IconChevronDown)
	}
	return m.styles.Icons.Glyph(theme.IconChevronRight)
}
