package app

import (
	"fmt"
	"strings"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/shared"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs"

	tea "github.com/charmbracelet/bubbletea"
)

// dashBlock names one of the Dashboard's three navigable blocks — the ones
// rfc-tui.md §5.S2 wires to "Enter opens the block under the cursor". The
// counters and card blocks carry no cursor: neither maps to a single tab.
type dashBlock int

const (
	dashBlockTasks dashBlock = iota
	dashBlockRunbooks
	dashBlockEvidence
	dashBlockCount // sentinel: number of navigable blocks
)

// target is the tab Enter opens for this block.
func (b dashBlock) target() tabs.ID {
	switch b {
	case dashBlockTasks:
		return tabs.Tasks
	case dashBlockRunbooks:
		return tabs.Runbooks
	case dashBlockEvidence:
		return tabs.Evidence
	}
	return tabs.Memory
}

// dashboardModel is the Project Dashboard (S2, rfc-tui.md §5.S2): the active
// project's card, its RFC §3.1 counters, its 5 most recent tasks, its stale
// runbooks and its latest evidence.
//
// Like selectorModel, it is a plain value the root holds directly rather
// than a tabs.Tab: it is the project's root screen, not a tab in the bar.
type dashboardModel struct {
	reader data.ProjectReader
	slug   string

	card   store.ProjectCard
	health data.ProjectHealth

	tasks         []store.TaskListItem
	staleRunbooks []store.RunbookIndexRow
	evidence      []store.EvidenceListItem

	cursor dashBlock

	loaded bool
	err    string
}

// newDashboardModel creates the Dashboard for slug, bound to reader. The
// caller is responsible for issuing loadDashboard: newDashboardModel itself
// does no I/O, matching every other constructor in this package.
func newDashboardModel(reader data.ProjectReader, slug string) dashboardModel {
	return dashboardModel{reader: reader, slug: slug}
}

// dashboardLoadedMsg carries loadDashboard's result. Every field but slug
// and err is best-effort: a project missing its card is fatal (err is set
// and nothing else is trusted), but a project with no tasks, no stale
// runbooks or no evidence yet is a normal, empty answer, not a failure — see
// loadDashboard.
type dashboardLoadedMsg struct {
	slug string

	card   store.ProjectCard
	health data.ProjectHealth

	tasks         []store.TaskListItem
	staleRunbooks []store.RunbookIndexRow
	evidence      []store.EvidenceListItem

	err error
}

// dashboardBlockLimit caps each of the Dashboard's three lists, matching the
// "5" the S2 wireframe shows for recent tasks and applied uniformly to the
// other two blocks so one project with a long tail never dwarfs the screen.
const dashboardBlockLimit = 5

// loadDashboard returns the command that loads every block of the Dashboard
// for slug. The card fetch is load-bearing — without it there is no project
// to show, so its error short-circuits the rest — but the four queries after
// it are independent: one failing (a dropped connection mid-load, say)
// still lets the blocks that succeeded render, with the first error kept for
// the banner.
func loadDashboard(r data.ProjectReader, slug string) tea.Cmd {
	return func() tea.Msg {
		msg := dashboardLoadedMsg{slug: slug}

		card, err := r.Card(slug)
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

		health, err := r.Health(slug)
		keepFirstErr(err)
		if err == nil {
			msg.health = health
		}

		tasksList, err := r.RecentTasks(slug, dashboardBlockLimit)
		keepFirstErr(err)
		if err == nil {
			msg.tasks = tasksList
		}

		staleRunbooks, err := r.StaleRunbooks(slug, dashboardBlockLimit)
		keepFirstErr(err)
		if err == nil {
			msg.staleRunbooks = staleRunbooks
		}

		evidence, err := r.LatestEvidence(slug, dashboardBlockLimit)
		keepFirstErr(err)
		if err == nil {
			msg.evidence = evidence
		}

		return msg
	}
}

// applyLoaded stores a dashboardLoadedMsg's result. A response for a project
// the user has since navigated away from (msg.slug != m.slug) is dropped: a
// slow load for the project that used to be active must never clobber the
// one now on screen.
func (m dashboardModel) applyLoaded(msg dashboardLoadedMsg) dashboardModel {
	if msg.slug != m.slug {
		return m
	}
	if msg.err != nil {
		m.err = msg.err.Error()
	} else {
		m.err = ""
	}
	m.card = msg.card
	m.health = msg.health
	m.tasks = msg.tasks
	m.staleRunbooks = msg.staleRunbooks
	m.evidence = msg.evidence
	m.loaded = true
	return m
}

// moveCursor shifts the block cursor by delta, wrapping neither direction —
// it clamps, matching moveCursor in selectorModel.
func (m dashboardModel) moveCursor(delta int) dashboardModel {
	m.cursor += dashBlock(delta)
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= dashBlockCount {
		m.cursor = dashBlockCount - 1
	}
	return m
}

// ─── View ────────────────────────────────────────────────────────────────────

func (m Model) viewDashboard() string {
	var b strings.Builder

	if m.dashboard.err != "" {
		b.WriteString(m.styles.Error.Render("  " + m.dashboard.err))
		b.WriteString("\n\n")
	}
	if !m.dashboard.loaded {
		b.WriteString(m.styles.StatCard.Render("Loading " + m.project + "..."))
		b.WriteString("\n")
		b.WriteString(m.styles.Help.Render("  p project • q quit"))
		return b.String()
	}

	b.WriteString(m.viewDashboardCard())
	b.WriteString("\n")
	b.WriteString(m.viewDashboardBlock("recent tasks", dashBlockTasks, m.viewDashboardTasks()))
	b.WriteString(m.viewDashboardBlock("stale runbooks", dashBlockRunbooks, m.viewDashboardRunbooks()))
	b.WriteString(m.viewDashboardBlock("latest evidence", dashBlockEvidence, m.viewDashboardEvidence()))

	b.WriteString(m.styles.Help.Render("\n  1-5 tabs • enter open block • p project • r refresh • q quit"))
	return b.String()
}

// viewDashboardCard renders the counters and the project_cards fields the S2
// wireframe puts side by side. graph_commit and graph_built_at are omitted
// when the project has never been graphed (StampProjectGraph never ran),
// which is the normal state for a freshly enrolled card, not an error.
func (m Model) viewDashboardCard() string {
	h := m.dashboard.health
	counters := fmt.Sprintf(
		"%s %s\n%s %s\n%s %s\n%s %s",
		m.styles.StatNumber.Render(fmt.Sprintf("%d", h.Observations)), m.styles.StatLabel.Render("observations"),
		m.styles.StatNumber.Render(fmt.Sprintf("%d", h.TasksActive)), m.styles.StatLabel.Render("open tasks"),
		m.styles.StatNumber.Render(fmt.Sprintf("%d", h.Evidence)), m.styles.StatLabel.Render("evidence files"),
		m.styles.StatNumber.Render(fmt.Sprintf("%d", h.Runbooks)), m.styles.StatLabel.Render("runbooks"),
	)

	c := m.dashboard.card
	detail := func(label, value string) string {
		if value == "" {
			return ""
		}
		return m.styles.DetailLabel.Render(label) + m.styles.DetailValue.Render(value) + "\n"
	}
	// The slug leads because it is what identifies the project everywhere else
	// — the command the user typed, the key in every table — and the display
	// name follows only when it says something the slug does not.
	name := c.Slug
	if c.DisplayName != "" && c.DisplayName != c.Slug {
		name += " — " + c.DisplayName
	}

	card := detail("project", name) +
		detail("repo", orEmpty(c.RepoURL)) +
		detail("branch", c.DefaultBranch) +
		detail("jira", c.JiraProject) +
		detail("hub", orEmpty(c.KnowledgeHubPath)) +
		detail("graph", m.dashboardGraphLine(c))

	return m.styles.StatCard.Render(counters) + "\n" + m.styles.StatCard.Render(strings.TrimRight(card, "\n"))
}

func (m Model) dashboardGraphLine(c store.ProjectCard) string {
	if c.GraphCommit == nil {
		return "none"
	}
	line := *c.GraphCommit
	if c.GraphBuiltAt != nil {
		line += " built " + shared.LocalTime(*c.GraphBuiltAt)
	}
	return line
}

func orEmpty(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

// viewDashboardBlock wraps a block's rendered rows in its section heading,
// marking it with the cursor glyph when it is the block Enter would open.
func (m Model) viewDashboardBlock(title string, id dashBlock, body string) string {
	heading := "  " + title
	if m.dashboard.cursor == id {
		heading = "▸ " + title
	}
	return m.styles.SectionHeading.Render(heading) + "\n" + body
}

func (m Model) viewDashboardTasks() string {
	if len(m.dashboard.tasks) == 0 {
		return m.styles.NoResults.Render("  No open tasks.") + "\n"
	}
	var b strings.Builder
	for _, t := range m.dashboard.tasks {
		key := t.SyncID
		if t.JiraKey != nil {
			key = *t.JiraKey
		}
		b.WriteString(fmt.Sprintf("  %s %s %s %s\n",
			m.styles.ID.Render(fmt.Sprintf("%-12s", key)),
			m.styles.TypeBadge.Render(fmt.Sprintf("%-10s", t.Kind)),
			m.styles.DetailValue.Render(fmt.Sprintf("%-16s", t.State)),
			shared.Truncate(t.Title, 50)))
	}
	return b.String()
}

func (m Model) viewDashboardRunbooks() string {
	if len(m.dashboard.staleRunbooks) == 0 {
		return m.styles.NoResults.Render("  No stale runbooks.") + "\n"
	}
	var b strings.Builder
	for _, rb := range m.dashboard.staleRunbooks {
		age := ""
		if rb.AgeDays != nil {
			age = fmt.Sprintf("%d d", *rb.AgeDays)
		}
		b.WriteString(fmt.Sprintf("  %s %s %s\n",
			m.styles.ID.Render(rb.ID),
			shared.Truncate(rb.Title, 40),
			m.styles.StaleBadge.Render(age)))
	}
	return b.String()
}

func (m Model) viewDashboardEvidence() string {
	if len(m.dashboard.evidence) == 0 {
		return m.styles.NoResults.Render("  No evidence captured yet.") + "\n"
	}
	var b strings.Builder
	for _, e := range m.dashboard.evidence {
		badge := m.styles.StaleBadge.Render("unattached")
		if e.AttachedJira {
			badge = m.styles.AttachedBadge.Render("attached ✓")
		}
		b.WriteString(fmt.Sprintf("  %s %s\n", shared.Truncate(e.Path, 46), badge))
	}
	return b.String()
}
