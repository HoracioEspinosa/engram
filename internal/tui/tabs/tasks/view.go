package tasks

import (
	"fmt"
	"strings"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/shared"

	"github.com/HoracioEspinosa/engram/internal/tui/theme"
	"github.com/charmbracelet/bubbles/spinner"
)

// View renders the active screen followed by the transient banners the tab
// owns: the error line and the clipboard/save confirmation. The root wraps
// the result in the application frame.
func (m Model) View() string {
	var content string

	switch m.Screen {
	case ScreenDetail:
		content = m.viewDetail()
	case ScreenContextPack:
		content = m.viewContextPack()
	default:
		content = m.viewList()
	}

	if notice := shared.Error(m.ErrorMsg); !notice.Empty() {
		content += "\n" + notice.Render(m.styles)
	}
	if notice := shared.Info(m.CopyFeedback); !notice.Empty() {
		content += "\n" + notice.Render(m.styles)
	}
	return content
}

// ─── List (S3) ───────────────────────────────────────────────────────────────

func (m Model) viewList() string {
	var b strings.Builder

	b.WriteString(m.styles.SectionHeading.Render(fmt.Sprintf(
		"  Tasks (%d shown%sstate: %s%skind: %s)",
		len(m.Items), m.styles.Icons.Separator(), filterLabel(m.Filter.State, "active"),
		m.styles.Icons.Separator(), filterLabel(m.Filter.Kind, "all"))))
	b.WriteString("\n")

	switch {
	case m.Searching:
		b.WriteString(m.styles.SearchInput.Render(m.SearchInput.View()))
		b.WriteString("\n")
	case m.Filter.Query != "":
		b.WriteString(m.styles.Timestamp.Render("  search: " + m.Filter.Query))
		b.WriteString("\n")
	}

	if len(m.Items) == 0 {
		b.WriteString(m.styles.NoResults.Render("  No tasks match this filter."))
		b.WriteString("\n")
	} else {
		visible := shared.VisibleItems(m.Height, listChrome, taskItemLines, minVisibleItems)
		end := m.Scroll + visible
		if end > len(m.Items) {
			end = len(m.Items)
		}
		widths := taskColumns(m.masterWidth())
		for i := m.Scroll; i < end; i++ {
			b.WriteString(m.viewTaskRow(m.Items[i], i == m.Cursor, widths))
		}
		// The range is absolute: the page's own offset plus the window
		// scrolled inside it, against the total the store counted.
		b.WriteString(shared.RangeIndicator(m.styles, "tasks", m.Filter.Offset+m.Scroll+1, m.Filter.Offset+end, m.Total))
		b.WriteString("\n")
	}

	return shared.SplitPanes(m.styles, m.regions(), b.String(), m.viewRowDetail())
}

// viewRowDetail is the right-hand pane at the split breakpoint: what the list
// already knows about the row under the cursor, so the user can read a task
// without leaving the list.
//
// It reports what the row carries and no more. The linked observations and
// evidence live one query away, on the detail screen: issuing that query on
// every cursor move would put the store in the path of the arrow keys.
func (m Model) viewRowDetail() string {
	if m.Cursor < 0 || m.Cursor >= len(m.Items) {
		return ""
	}
	item := m.Items[m.Cursor]

	var b strings.Builder
	b.WriteString(m.styles.Title.Render(item.Key()))
	b.WriteString("\n")
	b.WriteString(m.styles.ListItem.Render(item.Title))
	b.WriteString("\n\n")

	row := func(label, value string) {
		if value == "" {
			return
		}
		b.WriteString(m.styles.DetailLabel.Render(label) + m.styles.DetailValue.Render(value) + "\n")
	}
	row("state    ", item.State)
	row("kind     ", item.Kind)
	row("branch   ", orEmpty(item.Branch))
	row("pr       ", orEmpty(item.PRUrl))
	row("memory   ", fmt.Sprintf("%d linked", item.Observations))
	row("evidence ", fmt.Sprintf("%d captured", item.Evidence))
	row("updated  ", shared.LocalTime(item.UpdatedAt))

	b.WriteString("\n")
	b.WriteString(m.styles.Help.Render("enter opens the full detail"))
	return b.String()
}

// filterLabel names an active filter value, or defaultLabel when it is unset.
func filterLabel(value, defaultLabel string) string {
	if value == "" {
		return defaultLabel
	}
	return value
}

// taskColumns solves the list row against the width it is drawn in: the key,
// the kind and the state never grow past their content, and the title takes
// whatever is left.
func taskColumns(width int) []int {
	return shared.SolveColumns(width-taskRowFixed, 1, []shared.Column{
		{Min: 8, Max: taskKeyCells},
		{Min: 4, Max: taskKindCells},
		{Min: 6, Max: taskStateCells},
		{Min: 16, Weight: 1},
	})
}

func (m Model) viewTaskRow(item store.TaskListItem, selected bool, widths []int) string {
	titleStyle := m.styles.ListItem
	if selected {
		titleStyle = m.styles.ListSelected
	}

	state := item.State
	style := m.styles.DetailValue
	if item.StateStale {
		state, style = item.State+" (stale)", m.styles.StateWarningBadge
	}

	line1 := fmt.Sprintf("%s%s %s %s  %s\n",
		shared.RowCursor(m.styles, selected),
		m.styles.ID.Render(shared.Cell(item.Key(), widths[0])),
		m.styles.TypeBadge.Render("["+shared.Cell(item.Kind, widths[1])+"]"),
		style.Render(shared.Cell(state, widths[2])),
		titleStyle.Render(shared.Field(item.Title, widths[3])))

	branch := "no branch"
	if item.Branch != nil {
		branch = *item.Branch
	}
	prStatus := "no PR"
	if item.PRUrl != nil {
		prStatus = "PR linked"
	}
	sep := m.styles.Icons.Separator()
	second := fmt.Sprintf("%s%s%s%s%d obs%s%d evidence%supdated %s",
		branch, sep, prStatus, sep, item.Observations, sep, item.Evidence, sep, shared.LocalTime(item.UpdatedAt))
	line2 := "    " + m.styles.Timestamp.Render(shared.Truncate(second, width(widths)-4)) + "\n"

	return line1 + line2
}

// width is the row's own extent: every solved column plus the fixed cells
// between them.
func width(widths []int) int {
	total := taskRowFixed + len(widths) - 1
	for _, w := range widths {
		total += w
	}
	return total
}

// ─── Detail (S4) ─────────────────────────────────────────────────────────────

func (m Model) viewDetail() string {
	if m.Detail == nil {
		return shared.Loading(m.styles, spinner.Model{}, "the task")
	}
	t := m.Detail.Task
	var b strings.Builder

	b.WriteString(m.styles.Title.Render(fmt.Sprintf("%s%s%s", data.TaskKey(t), m.styles.Icons.Dash(), t.Title)))
	b.WriteString("\n")

	detail := func(label, value string) string {
		if value == "" {
			return ""
		}
		return m.styles.DetailLabel.Render(label) + m.styles.DetailValue.Render(value) + "\n"
	}
	meta := detail("kind", t.Kind) +
		detail("state", t.State) +
		detail("branch", orEmpty(t.Branch)) +
		detail("pr", orEmpty(t.PRUrl)) +
		detail("sdd change", orEmpty(t.SDDChange)) +
		detail("jira", orEmpty(t.JiraKey)) +
		detail("created", shared.LocalTime(t.CreatedAt)) +
		detail("updated", shared.LocalTime(t.UpdatedAt))
	b.WriteString(m.styles.StatCard.Render(strings.TrimRight(meta, "\n")))
	b.WriteString("\n")

	if m.ChangingState {
		b.WriteString(m.viewStatePicker())
	}
	if m.Linking {
		b.WriteString(m.styles.SearchInput.Render(m.LinkInput.View()))
		b.WriteString("\n")
	}

	b.WriteString(m.styles.SectionHeading.Render(fmt.Sprintf("  observations (%d)%senter opens it in Memory", len(m.Detail.Observations), m.styles.Icons.Dash())))
	b.WriteString("\n")
	if len(m.Detail.Observations) == 0 {
		b.WriteString(m.styles.NoResults.Render("  No linked observations."))
		b.WriteString("\n")
	} else {
		visible := shared.VisibleItems(m.Height, detailChrome, observationItemLines, minVisibleItems)
		end := m.DetailScroll + visible
		if end > len(m.Detail.Observations) {
			end = len(m.Detail.Observations)
		}
		titleWidth := m.bodyWidth() - detailObservationFixed - observationTypeCells
		if titleWidth < minTitleCells {
			titleWidth = minTitleCells
		}
		for i := m.DetailScroll; i < end; i++ {
			o := m.Detail.Observations[i]
			style := m.styles.ListItem
			if i == m.DetailCursor {
				style = m.styles.ListSelected
			}
			b.WriteString(fmt.Sprintf("%s%s %s %s %s\n",
				shared.RowCursor(m.styles, i == m.DetailCursor),
				m.styles.ID.Render(fmt.Sprintf("#%-5d", o.Observation.ID)),
				m.styles.TypeBadge.Render("["+shared.PadCells(o.Observation.Type, observationTypeCells)+"]"),
				style.Render(shared.Field(o.Observation.Title, titleWidth)),
				m.styles.Timestamp.Render(shared.LocalTime(o.Observation.CreatedAt))))
		}
		if len(m.Detail.Observations) > visible {
			b.WriteString(shared.RangeIndicator(m.styles, "observations", m.DetailScroll+1, end, len(m.Detail.Observations)))
			b.WriteString("\n")
		}
	}

	b.WriteString(m.styles.SectionHeading.Render(fmt.Sprintf("  evidence (%d)%se opens the Evidence tab", len(m.Detail.Evidence), m.styles.Icons.Dash())))
	b.WriteString("\n")
	if len(m.Detail.Evidence) == 0 {
		b.WriteString(m.styles.NoResults.Render("  No evidence captured yet."))
		b.WriteString("\n")
	} else {
		evidenceWidths := shared.SolveColumns(m.bodyWidth()-detailEvidenceFixed, 1, []shared.Column{
			{Min: 12, Weight: 2},
			{Min: 10, Weight: 1},
			{Min: detailBadgeCells, Max: detailBadgeCells},
		})
		for _, e := range m.Detail.Evidence {
			badge := m.styles.StaleBadge.Render(shared.Cell("unattached", evidenceWidths[2]))
			if e.AttachedJira {
				badge = m.styles.AttachedBadge.Render(shared.Cell(m.styles.Icons.Glyph(theme.IconFresh)+" attached", evidenceWidths[2]))
			}
			row := fmt.Sprintf("  %s %s %s",
				shared.Field(e.Path, evidenceWidths[0]),
				m.styles.Timestamp.Render(shared.Field(e.Proves, evidenceWidths[1])),
				badge)
			b.WriteString(strings.TrimRight(row, " ") + "\n")
		}
	}

	return b.String()
}

func (m Model) viewStatePicker() string {
	var b strings.Builder
	b.WriteString(m.styles.SectionHeading.Render("  change state (enter confirm" + m.styles.Icons.Separator() + "esc cancel)"))
	b.WriteString("\n")
	b.WriteString(shared.Menu(m.styles, stateOptions, m.StateCursor))
	return b.String()
}

func orEmpty(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

// ─── Context pack (S5) ───────────────────────────────────────────────────────

func (m Model) viewContextPack() string {
	if m.Detail == nil || m.ContextPack == "" {
		return shared.Loading(m.styles, spinner.Model{}, "the context pack")
	}
	var b strings.Builder

	b.WriteString(m.styles.Title.Render(fmt.Sprintf("Context pack%s%s", m.styles.Icons.Dash(), data.TaskKey(m.Detail.Task))))
	b.WriteString("  ")
	b.WriteString(m.styles.Timestamp.Render(fmt.Sprintf(
		"%d chars est.%sbuilt %s", len([]rune(m.ContextPack)), m.styles.Icons.Separator(), m.ContextPackBuilt.Format("15:04:05"))))
	b.WriteString("\n\n")

	lines := strings.Split(m.ContextPack, "\n")
	visible := shared.VisibleItems(m.Height, contextPackChrome, 1, minVisibleItems)
	start := m.ContextPackScroll
	if start > len(lines) {
		start = len(lines)
	}
	end := start + visible
	if end > len(lines) {
		end = len(lines)
	}
	// The pack goes through bubbles/viewport rather than a hand-sliced
	// window: it owns the bounds, and every screen that shows a long body
	// now windows it the same way.
	b.WriteString(m.styles.DetailContent.Render(shared.Viewport(m.ContextPack, m.bodyWidth(), end-start, start)))
	b.WriteString("\n")
	b.WriteString(shared.RangeIndicator(m.styles, "lines", start+1, end, len(lines)))
	b.WriteString("\n")

	return b.String()
}

// bodyWidth is how many cells this tab's rows may occupy: the terminal less
// what the app frame spends either side. A screen that has not received a
// tea.WindowSizeMsg yet assumes the width the wireframes were drawn at.
func (m Model) bodyWidth() int {
	if m.Width <= 0 {
		return defaultBodyWidth
	}
	if w := m.Width - bodyMargin; w >= minBodyWidth {
		return w
	}
	return minBodyWidth
}

// regions is the tab's master/detail split for its current width.
func (m Model) regions() shared.Regions {
	// The split depends on the width alone; a screen that has not learned
	// its height yet still has to know how wide its columns are.
	height := m.Height
	if height < 1 {
		height = 1
	}
	return shared.Layout(m.bodyWidth(), height)
}

// masterWidth is how wide the list itself is: the whole body below the split
// breakpoint, the left pane above it.
func (m Model) masterWidth() int { return m.regions().Master.Dx() }
