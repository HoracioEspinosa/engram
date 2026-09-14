package tasks

import (
	"fmt"
	"strings"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/shared"
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

	if m.ErrorMsg != "" {
		content += "\n" + m.styles.Error.Render("Error: "+m.ErrorMsg)
	}
	if m.CopyFeedback != "" {
		content += "\n" + m.styles.Notice.Render(m.CopyFeedback)
	}
	return content
}

// ─── List (S3) ───────────────────────────────────────────────────────────────

func (m Model) viewList() string {
	var b strings.Builder

	b.WriteString(m.styles.SectionHeading.Render(fmt.Sprintf(
		"  Tasks (%d shown · state: %s · kind: %s)",
		len(m.Items), filterLabel(m.Filter.State, "active"), filterLabel(m.Filter.Kind, "all"))))
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
		for i := m.Scroll; i < end; i++ {
			b.WriteString(m.viewTaskRow(m.Items[i], i == m.Cursor))
		}
		b.WriteString(shared.RangeIndicator(m.styles, "tasks", m.Scroll+1, end, len(m.Items)))
		b.WriteString("\n")
	}

	return b.String()
}

// filterLabel names an active filter value, or defaultLabel when it is unset.
func filterLabel(value, defaultLabel string) string {
	if value == "" {
		return defaultLabel
	}
	return value
}

func (m Model) viewTaskRow(item store.TaskListItem, selected bool) string {
	cursor := "  "
	titleStyle := m.styles.ListItem
	if selected {
		cursor = "▸ "
		titleStyle = m.styles.ListSelected
	}

	stateText := m.styles.DetailValue.Render(item.State)
	if item.StateStale {
		stateText = m.styles.StateWarningBadge.Render(item.State + " (mirror stale)")
	}

	line1 := fmt.Sprintf("%s%s %s %s  %s\n",
		cursor,
		m.styles.ID.Render(shared.PadCells(shared.CutCells(data.TaskKey(item.Task), taskKeyCells), taskKeyCells)),
		m.styles.TypeBadge.Render("["+shared.PadCells(item.Kind, taskKindCells)+"]"),
		stateText,
		titleStyle.Render(shared.Truncate(item.Title, 60)))

	branch := "no branch"
	if item.Branch != nil {
		branch = *item.Branch
	}
	prStatus := "no PR"
	if item.PRUrl != nil {
		prStatus = "PR linked"
	}
	line2 := fmt.Sprintf("    %s · %s · %d obs · %d evidence · updated %s\n",
		m.styles.Timestamp.Render(branch), m.styles.Timestamp.Render(prStatus),
		item.Observations, item.Evidence, shared.LocalTime(item.UpdatedAt))

	return line1 + line2
}

// ─── Detail (S4) ─────────────────────────────────────────────────────────────

func (m Model) viewDetail() string {
	if m.Detail == nil {
		return m.styles.StatCard.Render("Loading task...")
	}
	t := m.Detail.Task
	var b strings.Builder

	b.WriteString(m.styles.Title.Render(fmt.Sprintf("%s — %s", data.TaskKey(t), t.Title)))
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

	b.WriteString(m.styles.SectionHeading.Render(fmt.Sprintf("  observations (%d) — enter opens it in Memory", len(m.Detail.Observations))))
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
		for i := m.DetailScroll; i < end; i++ {
			o := m.Detail.Observations[i]
			cursor := "  "
			style := m.styles.ListItem
			if i == m.DetailCursor {
				cursor = "▸ "
				style = m.styles.ListSelected
			}
			b.WriteString(fmt.Sprintf("%s%s %s %s %s\n",
				cursor,
				m.styles.ID.Render(fmt.Sprintf("#%-5d", o.Observation.ID)),
				m.styles.TypeBadge.Render("["+shared.PadCells(o.Observation.Type, observationTypeCells)+"]"),
				style.Render(shared.Truncate(o.Observation.Title, 50)),
				m.styles.Timestamp.Render(shared.LocalTime(o.Observation.CreatedAt))))
		}
		if len(m.Detail.Observations) > visible {
			b.WriteString(shared.RangeIndicator(m.styles, "observations", m.DetailScroll+1, end, len(m.Detail.Observations)))
			b.WriteString("\n")
		}
	}

	b.WriteString(m.styles.SectionHeading.Render(fmt.Sprintf("  evidence (%d) — e opens the Evidence tab", len(m.Detail.Evidence))))
	b.WriteString("\n")
	if len(m.Detail.Evidence) == 0 {
		b.WriteString(m.styles.NoResults.Render("  No evidence captured yet."))
		b.WriteString("\n")
	} else {
		for _, e := range m.Detail.Evidence {
			badge := m.styles.StaleBadge.Render("unattached")
			if e.AttachedJira {
				badge = m.styles.AttachedBadge.Render("attached ✓")
			}
			b.WriteString(fmt.Sprintf("  %s %s %s\n", shared.Truncate(e.Path, 40), m.styles.Timestamp.Render(e.Proves), badge))
		}
	}

	return b.String()
}

func (m Model) viewStatePicker() string {
	var b strings.Builder
	b.WriteString(m.styles.SectionHeading.Render("  change state (enter confirm · esc cancel)"))
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
		return m.styles.StatCard.Render("Building context pack...")
	}
	var b strings.Builder

	b.WriteString(m.styles.Title.Render(fmt.Sprintf("Context pack — %s", data.TaskKey(m.Detail.Task))))
	b.WriteString("  ")
	b.WriteString(m.styles.Timestamp.Render(fmt.Sprintf(
		"%d chars est. · built %s", len([]rune(m.ContextPack)), m.ContextPackBuilt.Format("15:04:05"))))
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
	for _, line := range lines[start:end] {
		b.WriteString(m.styles.DetailContent.Render(line))
		b.WriteString("\n")
	}
	b.WriteString(shared.RangeIndicator(m.styles, "lines", start+1, end, len(lines)))
	b.WriteString("\n")

	return b.String()
}
