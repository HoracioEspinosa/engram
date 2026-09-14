package runbooks

import (
	"fmt"
	"strings"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/shared"
)

// View renders the active screen followed by the transient banners the tab
// owns: the error line and the clipboard confirmation. The root wraps the
// result in the application frame.
func (m Model) View() string {
	var content string

	switch m.Screen {
	case ScreenView:
		content = m.viewMarkdown()
	default:
		content = m.viewIndex()
	}

	if m.ErrorMsg != "" {
		content += "\n" + m.styles.Error.Render("Error: "+m.ErrorMsg)
	}
	if m.CopyFeedback != "" {
		content += "\n" + m.styles.Notice.Render(m.CopyFeedback)
	}
	return content
}

// ─── Index (S8) ──────────────────────────────────────────────────────────────

func (m Model) viewIndex() string {
	var b strings.Builder

	scope := "this project"
	if m.All {
		scope = "all projects"
	}
	header := fmt.Sprintf("  Runbooks (%s · a toggle)", scope)
	if m.Query != "" {
		header += fmt.Sprintf(" · search: %q", m.Query)
	}
	b.WriteString(m.styles.SectionHeading.Render(header))
	b.WriteString("\n")

	if m.Searching {
		b.WriteString(m.styles.SearchInput.Render(m.SearchInput.View()))
		b.WriteString("\n")
	}

	if len(m.Items) == 0 {
		b.WriteString(m.styles.NoResults.Render("  No runbooks match this filter."))
		b.WriteString("\n")
	} else {
		visible := shared.VisibleItems(m.Height, indexChrome, runbookItemLines, minVisibleItems)
		end := m.Scroll + visible
		if end > len(m.Items) {
			end = len(m.Items)
		}
		for i := m.Scroll; i < end; i++ {
			b.WriteString(m.viewRunbookRow(m.Items[i], i == m.Cursor))
		}
		b.WriteString(shared.RangeIndicator(m.styles, "runbooks", m.Filter.Offset+m.Scroll+1, m.Filter.Offset+end, m.Total))
		b.WriteString("\n")

		if m.Cursor < len(m.Items) {
			b.WriteString(m.viewRunbookPreview(m.Items[m.Cursor]))
		}
	}

	return b.String()
}

func (m Model) viewRunbookRow(item store.RunbookIndexRow, selected bool) string {
	cursor := "  "
	titleStyle := m.styles.ListItem
	if selected {
		cursor = "▸ "
		titleStyle = m.styles.ListSelected
	}

	verified := "new"
	if item.AgeDays != nil {
		verified = fmt.Sprintf("%d d", *item.AgeDays)
	}
	verifiedText := m.styles.DetailValue.Render(verified)
	if item.Stale {
		verifiedText = m.styles.StaleBadge.Render(verified + " ⚠")
	}

	pattern := "—"
	if item.Pattern != nil && *item.Pattern != "" {
		pattern = *item.Pattern
	}

	return fmt.Sprintf("%s%s %s %s %s %s %s\n",
		cursor,
		m.styles.ID.Render(shared.PadCells(shared.CutCells(item.ID, runbookIDCells), runbookIDCells)),
		titleStyle.Render(shared.PadCells(shared.Truncate(item.Title, runbookTitleCells), runbookTitleCells)),
		m.styles.Project.Render(shared.PadCells(shared.CutCells(item.Project, runbookProjectCells), runbookProjectCells)),
		m.styles.TypeBadge.Render(shared.PadCells(item.Category, runbookCategoryCells)),
		m.styles.DetailValue.Render(shared.PadCells(shared.CutCells(pattern, runbookPatternCells), runbookPatternCells)),
		verifiedText)
}

// viewRunbookPreview renders the strip rfc-tui.md §5's S8 wireframe shows
// under the cursor: the fields the table's columns had no room for.
func (m Model) viewRunbookPreview(item store.RunbookIndexRow) string {
	var b strings.Builder
	b.WriteString("\n")

	lastExec := "never"
	if item.LastExecAt != nil {
		lastExec = shared.LocalTime(*item.LastExecAt)
	}
	meta := fmt.Sprintf("%s · status %s · exec_count %d · last_exec %s\nvault %s",
		item.ID, item.Status, item.ExecCount, lastExec, item.VaultPath)
	b.WriteString(m.styles.StatCard.Render(meta))
	b.WriteString("\n")

	if len(item.Symptoms) > 0 {
		b.WriteString(m.styles.DetailContent.Render(
			"symptoms: " + shared.Truncate(strings.Join(item.Symptoms, " · "), 100)))
		b.WriteString("\n")
	}
	return b.String()
}

// ─── Markdown view (S9) ──────────────────────────────────────────────────────

func (m Model) viewMarkdown() string {
	if m.Selected == nil {
		return m.styles.StatCard.Render("Loading runbook...")
	}
	item := *m.Selected
	var b strings.Builder

	title := m.styles.Title.Render(fmt.Sprintf("%s — %s", item.ID, item.Title))
	if item.Stale {
		age := "stale"
		if item.AgeDays != nil {
			age = fmt.Sprintf("stale %d d", *item.AgeDays)
		}
		title += "  " + m.styles.StaleBadge.Render(age+" ⚠")
	}
	b.WriteString(title)
	b.WriteString("\n")

	meta := fmt.Sprintf("service %s · severity %s · category %s · pattern %s\nlast_verified %s · automation_level %s · status %s",
		item.Project, orDash(item.Severity), item.Category, orDash(item.Pattern),
		orDefault(item.LastVerified, "never"), orDash(item.AutomationLevel), item.Status)
	b.WriteString(m.styles.DetailContent.Render(meta))
	b.WriteString("\n\n")

	switch {
	case !m.FileExists:
		if root, ok := shared.VaultRoot(); ok {
			b.WriteString(m.styles.NoResults.Render(fmt.Sprintf(
				"  %s is not cloned locally under %s — clone cd-knowledge-mcp there to read this runbook's body.",
				item.VaultPath, root)))
		} else {
			b.WriteString(m.styles.Error.Render(fmt.Sprintf(
				"  %s is not set — export it to your local checkout of cd-knowledge-mcp before reading a runbook's body.",
				shared.VaultRootEnv)))
		}
		b.WriteString("\n")
	default:
		if m.MarkdownErr != "" {
			b.WriteString(m.styles.Error.Render("  glamour could not style this file: " + m.MarkdownErr))
			b.WriteString("\n")
		}
		lines := strings.Split(m.Rendered, "\n")
		visible := shared.VisibleItems(m.Height, viewChrome, 1, minVisibleItems)
		start := m.ViewScroll
		if start > len(lines) {
			start = len(lines)
		}
		end := start + visible
		if end > len(lines) {
			end = len(lines)
		}
		for _, line := range lines[start:end] {
			// Glamour's own output already carries ANSI styling; wrapping it
			// in another lipgloss style here would clash instead of adding
			// to it, unlike every plain-text line elsewhere in this tab.
			b.WriteString(line)
			b.WriteString("\n")
		}
		b.WriteString(shared.RangeIndicator(m.styles, "lines", start+1, end, len(lines)))
		b.WriteString("\n")
	}

	return b.String()
}

func orDash(v *string) string {
	if v == nil || *v == "" {
		return "—"
	}
	return *v
}

func orDefault(v *string, fallback string) string {
	if v == nil || *v == "" {
		return fallback
	}
	return *v
}
