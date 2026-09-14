package runbooks

import (
	"fmt"
	"strings"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/shared"

	"github.com/HoracioEspinosa/engram/internal/tui/theme"
	"github.com/charmbracelet/bubbles/spinner"
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
		widths := runbookColumns(m.masterWidth())
		for i := m.Scroll; i < end; i++ {
			b.WriteString(m.viewRunbookRow(m.Items[i], i == m.Cursor, widths))
		}
		b.WriteString(shared.RangeIndicator(m.styles, "runbooks", m.Filter.Offset+m.Scroll+1, m.Filter.Offset+end, m.Total))
		b.WriteString("\n")

		// Below the split breakpoint the preview is a strip under the list;
		// above it, it is the pane beside it.
		if m.Cursor < len(m.Items) && !m.regions().HasDetail() {
			b.WriteString(m.viewRunbookPreview(m.Items[m.Cursor]))
		}
	}

	return shared.SplitPanes(m.styles, m.regions(), b.String(), m.viewRowDetail())
}

// viewRowDetail is the right-hand pane at the split breakpoint: the same
// preview the narrow layout puts under the list, given a column of its own.
func (m Model) viewRowDetail() string {
	if m.Cursor < 0 || m.Cursor >= len(m.Items) {
		return ""
	}
	return strings.TrimSpace(m.viewRunbookPreview(m.Items[m.Cursor]))
}

// runbookColumns solves the index row against the width it is drawn in: the
// title is the only field worth stretching, the rest are capped identifiers.
func runbookColumns(width int) []int {
	return shared.SolveColumns(width-runbookRowFixed, 1, []shared.Column{
		{Min: 6, Max: runbookIDCells},
		{Min: 16, Weight: 1},
		{Min: 6, Max: runbookProjectCells},
		{Min: 6, Max: runbookCategoryCells},
		{Min: 4, Max: runbookPatternCells},
		{Min: runbookAgeCells, Max: runbookAgeCells},
	})
}

func (m Model) viewRunbookRow(item store.RunbookIndexRow, selected bool, widths []int) string {
	titleStyle := m.styles.ListItem
	if selected {
		titleStyle = m.styles.ListSelected
	}

	verified := "new"
	if item.AgeDays != nil {
		verified = fmt.Sprintf("%d d", *item.AgeDays)
	}
	verifiedStyle := m.styles.DetailValue
	if item.Stale {
		verified, verifiedStyle = verified+" "+m.styles.Icons.Glyph(theme.IconStale), m.styles.StaleBadge
	}

	pattern := ""
	if item.Pattern != nil && *item.Pattern != "" {
		pattern = *item.Pattern
	}

	row := fmt.Sprintf("%s%s %s %s %s %s %s",
		shared.RowCursor(m.styles, selected),
		m.styles.ID.Render(shared.Cell(item.ID, widths[0])),
		titleStyle.Render(shared.Field(item.Title, widths[1])),
		m.styles.Project.Render(shared.Cell(item.Project, widths[2])),
		m.styles.TypeBadge.Render(shared.Cell(item.Category, widths[3])),
		m.styles.DetailValue.Render(shared.Cell(pattern, widths[4])),
		verifiedStyle.Render(shared.Cell(verified, widths[5])))

	return strings.TrimRight(row, " ") + "\n"
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
			"symptoms: " + shared.Truncate(strings.Join(item.Symptoms, " · "), m.bodyWidth()-symptomsLabelCells)))
		b.WriteString("\n")
	}
	return b.String()
}

// ─── Markdown view (S9) ──────────────────────────────────────────────────────

func (m Model) viewMarkdown() string {
	if m.Selected == nil {
		return shared.Loading(m.styles, spinner.Model{}, "the runbook")
	}
	item := *m.Selected
	var b strings.Builder

	title := m.styles.Title.Render(fmt.Sprintf("%s — %s", item.ID, item.Title))
	if item.Stale {
		age := "stale"
		if item.AgeDays != nil {
			age = fmt.Sprintf("stale %d d", *item.AgeDays)
		}
		title += "  " + m.styles.StaleBadge.Render(age+" "+m.styles.Icons.Glyph(theme.IconStale))
	}
	b.WriteString(title)
	b.WriteString("\n")

	meta := fmt.Sprintf("service %s · severity %s · category %s · pattern %s\nlast_verified %s · automation_level %s · status %s",
		item.Project, m.orDash(item.Severity), item.Category, m.orDash(item.Pattern),
		orDefault(item.LastVerified, "never"), m.orDash(item.AutomationLevel), item.Status)
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
		// The body goes through bubbles/viewport rather than a hand-sliced
		// window: it owns the bounds, and glamour's own ANSI styling passes
		// through it untouched — wrapping it in a lipgloss style here would
		// clash instead of adding to it.
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
		b.WriteString(shared.Viewport(m.Rendered, m.bodyWidth(), end-start, start))
		b.WriteString("\n")
		b.WriteString(shared.RangeIndicator(m.styles, "lines", start+1, end, len(lines)))
		b.WriteString("\n")
	}

	return b.String()
}

// orDash renders a field the runbook never declared. The mark comes from the
// icon vocabulary so it degrades with the resolved mode like every other
// glyph.
func (m Model) orDash(v *string) string {
	if v == nil || *v == "" {
		return m.styles.Icons.Glyph(theme.IconUnknown)
	}
	return *v
}

func orDefault(v *string, fallback string) string {
	if v == nil || *v == "" {
		return fallback
	}
	return *v
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

// masterWidth is how wide the index itself is: the whole body below the
// split breakpoint, the left pane above it.
func (m Model) masterWidth() int { return m.regions().Master.Dx() }
