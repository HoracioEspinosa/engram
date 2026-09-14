package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/shared"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	// paletteMinQuery is the shortest query worth running. One character
	// matches most of the workspace, which costs a full FTS5 scan to produce
	// a list nobody can use.
	paletteMinQuery = 2
	// paletteDebounce is how long the palette waits after a keystroke before
	// asking the store. Typing "preview" is seven keystrokes; without this it
	// is seven searches, six of which nobody ever reads.
	paletteDebounce = 100 * time.Millisecond
	// paletteLimitPerKind caps each group, with the rest reported as a count
	// rather than a scroll.
	paletteLimitPerKind = 5

	// SearchHistoryKey is where the last queries are remembered, and
	// paletteHistoryMax how many of them are kept.
	SearchHistoryKey  = "tui.search_history"
	paletteHistoryMax = 10
)

// paletteKeys are the bindings the overlay owns.
var paletteKeys = struct {
	Move, Group, Open, Close key.Binding
}{
	Move: key.NewBinding(
		key.WithKeys("up", "down", "ctrl+n", "ctrl+p"),
		key.WithHelp("↑/↓", "move"),
	),
	Group: key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "next group"),
	),
	Open: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "open"),
	),
	Close: key.NewBinding(
		key.WithKeys("esc", "ctrl+k"),
		key.WithHelp("esc", "close"),
	),
}

func paletteHelp() []key.Binding {
	return []key.Binding{paletteKeys.Move, paletteKeys.Group, paletteKeys.Open, paletteKeys.Close}
}

// paletteKind is one of the six things a workspace search can find, in the
// order the palette groups them: what holds the work, what is being worked on,
// what was written down, what proves it, what measures it, what documents it.
type paletteKind struct {
	prefix string
	kind   data.SearchKind
	label  string
	icon   theme.Icon
	target tabs.ID
}

var paletteKinds = []paletteKind{
	{prefix: "p", kind: data.SearchKindCard, label: "projects", icon: theme.IconProjectRepo, target: tabs.Home},
	{prefix: "t", kind: data.SearchKindTask, label: "tasks", icon: theme.IconTabTasks, target: tabs.Tasks},
	{prefix: "m", kind: data.SearchKindObservation, label: "memory", icon: theme.IconTabMemory, target: tabs.Memory},
	{prefix: "e", kind: data.SearchKindEvidence, label: "evidence", icon: theme.IconTabEvidence, target: tabs.Evidence},
	{prefix: "b", kind: data.SearchKindBenchmark, label: "benchmarks", icon: theme.IconTabBenchmarks, target: tabs.Benchmarks},
	{prefix: "r", kind: data.SearchKindRunbook, label: "runbooks", icon: theme.IconTabRunbooks, target: tabs.Runbooks},
}

// paletteKindByPrefix resolves a typed "t:" chip to the kind it narrows to.
func paletteKindByPrefix(prefix string) (paletteKind, bool) {
	for _, k := range paletteKinds {
		if k.prefix == prefix {
			return k, true
		}
	}
	return paletteKind{}, false
}

func paletteKindOf(kind data.SearchKind) (paletteKind, bool) {
	for _, k := range paletteKinds {
		if k.kind == kind {
			return k, true
		}
	}
	return paletteKind{}, false
}

// paletteRow is one line of the result list: either a group heading, a hit, or
// the count of what the per-kind cap left out.
type paletteRow struct {
	heading string
	icon    theme.Icon
	hit     *data.SearchHit
	matched []int
	more    int
}

// selectable reports whether the cursor may land on this row.
func (r paletteRow) selectable() bool { return r.hit != nil }

// paletteModel is the ctrl+k overlay: one query across everything the
// workspace holds.
type paletteModel struct {
	open   bool
	input  textinput.Model
	styles theme.Styles

	searcher data.GlobalSearcher
	settings data.SettingsWriter

	// gen counts the searches issued. Every tick and every result carries the
	// generation it was issued under, and anything older than the current one
	// is dropped: typing quickly enough leaves several searches in flight at
	// once, and the slowest is not the answer to what is on screen.
	gen int
	// pending is true while a search for the current generation is running.
	pending bool

	rows   []paletteRow
	cursor int

	notice string

	// history is the last queries, newest first, as remembered in settings.
	history []string
}

// newPaletteModel builds the overlay's own state.
func newPaletteModel(styles theme.Styles) paletteModel {
	ti := textinput.New()
	ti.Placeholder = "Search the workspace (t: e: m: r: p: b:)"
	ti.CharLimit = 200
	ti.Width = 48
	return paletteModel{input: ti, styles: styles}
}

// WithSearch binds the palette to the searcher it queries and the settings it
// remembers a query in. A root built without them still opens the overlay; it
// then says there is nothing to search rather than returning silence.
func (m Model) WithSearch(searcher data.GlobalSearcher, settings data.SettingsWriter) Model {
	m.palette.searcher = searcher
	m.palette.settings = settings
	return m
}

// WithSearchHistory seeds the palette with the queries the workspace last
// remembered. The root reads settings once at startup rather than the palette
// reading them every time it opens.
func (m Model) WithSearchHistory(history []string) Model {
	m.palette.history = history
	return m
}

// searchTickMsg fires once the debounce has elapsed for one generation.
type searchTickMsg struct{ gen int }

// searchDoneMsg carries one search's result, under the generation it was
// issued for.
type searchDoneMsg struct {
	gen   int
	query string
	hits  []data.SearchHit
	err   error
}

// searchHistorySavedMsg reports the history write, so a settings store that
// will not answer says so instead of failing silently.
type searchHistorySavedMsg struct{ err error }

// debounceSearch waits out the pause before a query is worth running.
func debounceSearch(gen int) tea.Cmd {
	return tea.Tick(paletteDebounce, func(time.Time) tea.Msg { return searchTickMsg{gen: gen} })
}

// runSearch queries every kind the query names, scoped to the active project's
// subtree so a search from inside a project answers about that project's
// family first.
func runSearch(searcher data.GlobalSearcher, gen int, query string, kinds []data.SearchKind, scope data.ProjectScope) tea.Cmd {
	return func() tea.Msg {
		if searcher == nil {
			return searchDoneMsg{gen: gen, query: query, err: errNoSearcher}
		}
		hits, err := searcher.SearchWorkspace(data.SearchQuery{
			Text:         query,
			Kinds:        kinds,
			Scope:        scope,
			LimitPerKind: paletteLimitPerKind,
		})
		return searchDoneMsg{gen: gen, query: query, hits: hits, err: err}
	}
}

var errNoSearcher = errors.New("no workspace search is bound to this workspace")

// saveSearchHistory remembers a query, newest first, without duplicates.
func saveSearchHistory(settings data.SettingsWriter, history []string) tea.Cmd {
	return func() tea.Msg {
		if settings == nil {
			return searchHistorySavedMsg{err: errors.New("no settings store is bound to this workspace")}
		}
		encoded, err := json.Marshal(history)
		if err != nil {
			return searchHistorySavedMsg{err: err}
		}
		return searchHistorySavedMsg{err: settings.SetSetting(SearchHistoryKey, string(encoded))}
	}
}

// DecodeSearchHistory reads the remembered queries out of a settings value.
// A value that is not a list of strings is treated as no history at all: the
// palette works without one, and refusing to open over a malformed setting
// would be the wrong trade.
func DecodeSearchHistory(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var history []string
	if err := json.Unmarshal([]byte(raw), &history); err != nil {
		return nil
	}
	return history
}

// rememberQuery puts query at the head of the history, dropping a previous
// copy of it and anything past the cap.
func rememberQuery(history []string, query string) []string {
	query = strings.TrimSpace(query)
	if query == "" {
		return history
	}
	next := make([]string, 0, len(history)+1)
	next = append(next, query)
	for _, old := range history {
		if old != query {
			next = append(next, old)
		}
	}
	if len(next) > paletteHistoryMax {
		next = next[:paletteHistoryMax]
	}
	return next
}

// splitPalettePrefix separates a leading kind chip from the query behind it.
// A prefix nobody declared is not a prefix: "http://" stays part of the query
// rather than becoming an "h:" chip and a broken search.
func splitPalettePrefix(raw string) (chip paletteKind, hasChip bool, query string) {
	trimmed := strings.TrimLeft(raw, " ")
	colon := strings.Index(trimmed, ":")
	if colon <= 0 {
		return paletteKind{}, false, strings.TrimSpace(raw)
	}
	kind, ok := paletteKindByPrefix(strings.ToLower(trimmed[:colon]))
	if !ok {
		return paletteKind{}, false, strings.TrimSpace(raw)
	}
	return kind, true, strings.TrimSpace(trimmed[colon+1:])
}

// ─── Update ──────────────────────────────────────────────────────────────────

// updatePalette is the root's single entry point for the overlay.
func (m Model) updatePalette(msg tea.KeyMsg) (bool, tea.Model, tea.Cmd) {
	if !m.palette.open {
		if !m.opensPalette(msg) {
			return false, m, nil
		}
		m.palette.open = true
		m.palette.notice = ""
		m.palette.input.Focus()
		return true, m, nil
	}

	switch {
	case key.Matches(msg, paletteKeys.Close):
		m.palette.open = false
		m.palette.input.Blur()
		m.palette.input.SetValue("")
		m.palette.rows = nil
		m.palette.cursor = 0
		return true, m, nil

	case key.Matches(msg, paletteKeys.Group):
		m.palette = m.palette.nextGroup()
		return true, m, nil

	case key.Matches(msg, paletteKeys.Open):
		model, cmd := m.openPaletteHit()
		return true, model, cmd

	case key.Matches(msg, paletteKeys.Move):
		m.palette = m.palette.moveCursor(paletteDelta(msg))
		return true, m, nil
	}

	updated, cmd := m.palette.input.Update(msg)
	m.palette.input = updated
	m.palette, cmd = m.palette.queryChanged(cmd)
	return true, m, cmd
}

// opensPalette reports whether a key should open the overlay: ctrl+k anywhere,
// and "/" on a tab that has no search of its own — Home, today.
func (m Model) opensPalette(msg tea.KeyMsg) bool {
	if m.showHelp || m.tree.open || m.themePicker.open {
		return false
	}
	tab := m.tab(m.active)
	if tab != nil && tab.CapturingText() {
		return false
	}
	if key.Matches(msg, globalKeys.Search) {
		return true
	}
	return msg.String() == "/" && !tabOwnsSlash(tab)
}

// tabOwnsSlash reports whether the tab on screen declares "/" as one of its
// own keys. The palette borrows the key only where nothing else claims it,
// which is read off the tab's own Help() rather than from a list here that
// would drift.
func tabOwnsSlash(tab tabs.Tab) bool {
	if tab == nil {
		return false
	}
	for _, b := range tab.Help() {
		for _, k := range b.Keys() {
			if k == "/" {
				return true
			}
		}
	}
	return false
}

// paletteDelta turns a movement key into a direction.
func paletteDelta(msg tea.KeyMsg) int {
	switch msg.String() {
	case "up", "ctrl+p":
		return -1
	}
	return 1
}

// queryChanged issues a fresh generation whenever the text changed enough to
// matter, and clears the list when it no longer does.
func (p paletteModel) queryChanged(cmd tea.Cmd) (paletteModel, tea.Cmd) {
	_, _, query := splitPalettePrefix(p.input.Value())
	if len([]rune(query)) < paletteMinQuery {
		// Below the floor there is nothing to show, and the generation still
		// moves so a search already in flight cannot land on this state.
		p.gen++
		p.pending = false
		p.rows = nil
		p.cursor = 0
		return p, cmd
	}
	p.gen++
	p.pending = true
	return p, tea.Batch(cmd, debounceSearch(p.gen))
}

// updatePaletteMessage folds the overlay's own messages back into the root.
func (m Model) updatePaletteMessage(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case searchTickMsg:
		if msg.gen != m.palette.gen || !m.palette.pending {
			// The query moved on while this tick was waiting.
			return m, nil
		}
		chip, hasChip, query := splitPalettePrefix(m.palette.input.Value())
		var kinds []data.SearchKind
		if hasChip {
			kinds = []data.SearchKind{chip.kind}
		}
		return m, runSearch(m.palette.searcher, msg.gen, query, kinds, m.searchScope())

	case searchDoneMsg:
		if msg.gen != m.palette.gen {
			// A result for a query nobody is looking at any more.
			return m, nil
		}
		m.palette = m.palette.applyResults(msg)
		return m, nil

	case searchHistorySavedMsg:
		if msg.err != nil {
			m.palette.notice = "could not remember the search: " + msg.err.Error()
		}
		return m, nil
	}
	return m, nil
}

// searchScope narrows a search to the active project's subtree. Without a
// project the search is workspace-wide, which is the only thing it could be.
func (m Model) searchScope() data.ProjectScope {
	if m.project == "" {
		return data.ProjectScope{}
	}
	return data.ProjectScope{Project: m.project, Subtree: true}
}

// applyResults groups one search's hits into the rows on screen.
func (p paletteModel) applyResults(msg searchDoneMsg) paletteModel {
	p.pending = false
	if msg.err != nil {
		p.notice = msg.err.Error()
		p.rows = nil
		p.cursor = 0
		return p
	}
	p.notice = ""
	p.rows = paletteRows(msg.hits, msg.query)
	p.cursor = 0
	if len(p.rows) > 0 && !p.rows[0].selectable() {
		p = p.moveCursor(1)
	}
	return p
}

// paletteRows groups hits by kind, in the palette's own order, and marks what
// each group's cap left out.
//
// The highlight is computed here rather than read off the hit: a workspace
// search is FTS5-ranked and has no notion of which characters matched, so the
// same fuzzy filter the tree and the theme picker use is run over the titles
// that are actually on screen.
func paletteRows(hits []data.SearchHit, query string) []paletteRow {
	byKind := make(map[data.SearchKind][]data.SearchHit, len(paletteKinds))
	for _, hit := range hits {
		byKind[hit.Kind] = append(byKind[hit.Kind], hit)
	}

	rows := make([]paletteRow, 0, len(hits)+len(paletteKinds))
	for _, kind := range paletteKinds {
		group := byKind[kind.kind]
		if len(group) == 0 {
			continue
		}
		rows = append(rows, paletteRow{heading: kind.label, icon: kind.icon})

		shown := group
		if len(shown) > paletteLimitPerKind {
			shown = shown[:paletteLimitPerKind]
		}
		titles := make([]string, 0, len(shown))
		for _, hit := range shown {
			titles = append(titles, hit.Title)
		}
		matched := shared.MatchedBy(shared.Fuzzy(query, titles))

		for i := range shown {
			hit := shown[i]
			rows = append(rows, paletteRow{hit: &hit, matched: matched[i]})
		}
		if extra := len(group) - len(shown); extra > 0 {
			rows = append(rows, paletteRow{more: extra})
		}
	}
	return rows
}

// moveCursor shifts the cursor to the next selectable row in delta's
// direction, stepping over headings and the "more" markers.
func (p paletteModel) moveCursor(delta int) paletteModel {
	for i := p.cursor + delta; i >= 0 && i < len(p.rows); i += delta {
		if p.rows[i].selectable() {
			p.cursor = i
			return p
		}
	}
	return p
}

// nextGroup jumps to the first hit of the group after the one the cursor is
// in, wrapping to the first: a reader scanning for "the runbook" should not
// have to walk every task to reach it.
func (p paletteModel) nextGroup() paletteModel {
	for i := p.cursor + 1; i < len(p.rows); i++ {
		if p.rows[i].heading != "" {
			return p.jumpAfterHeading(i)
		}
	}
	for i := 0; i < len(p.rows); i++ {
		if p.rows[i].heading != "" {
			return p.jumpAfterHeading(i)
		}
	}
	return p
}

func (p paletteModel) jumpAfterHeading(heading int) paletteModel {
	for i := heading + 1; i < len(p.rows); i++ {
		if p.rows[i].selectable() {
			p.cursor = i
			return p
		}
	}
	return p
}

// selected returns the hit under the cursor.
func (p paletteModel) selected() (data.SearchHit, bool) {
	if p.cursor < 0 || p.cursor >= len(p.rows) {
		return data.SearchHit{}, false
	}
	row := p.rows[p.cursor]
	if row.hit == nil {
		return data.SearchHit{}, false
	}
	return *row.hit, true
}

// openPaletteHit closes the overlay and asks the root to go where the hit
// lives, remembering the query on the way out.
func (m Model) openPaletteHit() (tea.Model, tea.Cmd) {
	hit, ok := m.palette.selected()
	if !ok {
		return m, nil
	}

	_, _, query := splitPalettePrefix(m.palette.input.Value())
	m.palette.history = rememberQuery(m.palette.history, m.palette.input.Value())
	remember := saveSearchHistory(m.palette.settings, m.palette.history)

	m.palette.open = false
	m.palette.input.Blur()

	nav := paletteNavigation(hit, query)
	return m, tea.Batch(remember, func() tea.Msg { return nav })
}

// paletteNavigation is where one hit lives, in the shape the root routes.
func paletteNavigation(hit data.SearchHit, query string) tabs.NavigateMsg {
	kind, ok := paletteKindOf(hit.Kind)
	if !ok {
		return tabs.NavigateMsg{Target: tabs.Home}
	}

	nav := tabs.NavigateMsg{Target: kind.target, Slug: hit.Project}
	switch hit.Kind {
	case data.SearchKindCard:
		// A project is opened, not navigated to: the whole workspace rescopes
		// and Home is what shows it.
		nav.Slug = hit.Slug
		if nav.Slug == "" {
			nav.Slug = hit.Project
		}
	case data.SearchKindTask:
		nav.TaskID = hit.ID
	case data.SearchKindObservation:
		nav.ObservationID = hit.ID
	case data.SearchKindEvidence:
		nav.EvidenceID = hit.ID
	case data.SearchKindBenchmark:
		nav.BenchmarkID = hit.ID
	case data.SearchKindRunbook:
		// Runbooks are addressed by their own id (RB-NNN), which the hit
		// carries as its slug; the query is the fallback for a store that
		// answered without one.
		nav.Query = hit.Slug
		if nav.Query == "" {
			nav.Query = query
		}
	}
	return nav
}

// ─── View ────────────────────────────────────────────────────────────────────

// viewPalette draws the overlay's panel: the query, whatever chip it carries,
// and the grouped results.
func (m Model) viewPalette() string {
	var b strings.Builder

	b.WriteString(m.styles.Title.Render("Search"))
	b.WriteString("\n")

	chip, hasChip, query := splitPalettePrefix(m.palette.input.Value())
	line := m.styles.Icons.Glyph(theme.IconSearch) + " " + m.palette.input.View()
	if hasChip {
		line += "  " + m.styles.TypeBadge.Render("["+chip.label+"]")
	}
	b.WriteString(line)
	b.WriteString("\n")

	if m.palette.notice != "" {
		b.WriteString(m.styles.Error.Render(m.palette.notice))
		b.WriteString("\n")
	}

	switch {
	case len([]rune(query)) < paletteMinQuery:
		b.WriteString(m.styles.NoResults.Render(fmt.Sprintf("Type at least %d characters.", paletteMinQuery)))
		b.WriteString("\n")
		b.WriteString(m.viewPaletteHistory())
	case m.palette.pending:
		b.WriteString(m.styles.Timestamp.Render("  searching…"))
		b.WriteString("\n")
	case len(m.palette.rows) == 0:
		b.WriteString(m.styles.NoResults.Render("Nothing matches " + query + "."))
		b.WriteString("\n")
	default:
		for i, row := range m.palette.rows {
			b.WriteString(m.viewPaletteRow(row, i == m.palette.cursor))
		}
	}

	if hints := shared.HintsFrom(m.styles, paletteHelp(), m.treeWidth()); hints != "" {
		b.WriteString(hints)
	}
	return strings.TrimRight(b.String(), "\n")
}

// viewPaletteHistory lists what was searched for before, so reopening the
// palette is a way back to the last query rather than a blank page.
func (m Model) viewPaletteHistory() string {
	if len(m.palette.history) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(m.styles.SectionHeading.Render("  recent searches"))
	b.WriteString("\n")
	for _, q := range m.palette.history {
		b.WriteString("  " + m.styles.Timestamp.Render(shared.CutCells(q, m.treeWidth()-4)) + "\n")
	}
	return b.String()
}

func (m Model) viewPaletteRow(row paletteRow, selected bool) string {
	width := m.treeWidth()

	if row.heading != "" {
		return m.styles.SectionHeading.Render("  "+m.styles.Icons.Glyph(row.icon)+" "+row.heading) + "\n"
	}
	if row.more > 0 {
		return "    " + m.styles.Timestamp.Render(fmt.Sprintf("+%d more", row.more)) + "\n"
	}

	base := m.styles.ListItem
	if selected {
		base = m.styles.ListSelected
	}

	title := shared.CutCells(row.hit.Title, width/2)
	subtitle := row.hit.Subtitle
	if subtitle == "" {
		subtitle = row.hit.Snippet
	}

	line := shared.RowCursor(m.styles, selected) +
		shared.Highlight(base, m.styles.SearchHighlight, title, row.matched)
	if row.hit.Project != "" {
		line += "  " + m.styles.Project.Render(row.hit.Project)
	}
	if subtitle != "" {
		room := width - lipgloss.Width(line) - 2
		if room > 8 {
			line += "  " + m.styles.Timestamp.Render(shared.CutCells(subtitle, room))
		}
	}
	return strings.TrimRight(line, " ") + "\n"
}
