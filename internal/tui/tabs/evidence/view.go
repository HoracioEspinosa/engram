package evidence

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
	case ScreenDetail:
		content = m.viewDetail()
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

// ─── List ────────────────────────────────────────────────────────────────────

func (m Model) viewList() string {
	var b strings.Builder

	sep := m.styles.Icons.Separator()
	b.WriteString(m.styles.SectionHeading.Render(fmt.Sprintf(
		"  Evidence (%d shown%stask: %s%s%s)",
		len(m.Items), sep, taskFilterLabel(m.Items, m.Filter), sep, attachedFilterLabel(m.Filter))))
	b.WriteString("\n")

	if len(m.Items) == 0 {
		b.WriteString(m.styles.NoResults.Render("  No evidence matches this filter."))
		b.WriteString("\n")
	} else {
		visible := shared.VisibleItems(m.Height, listChrome, evidenceItemLines, minVisibleItems)
		end := m.Scroll + visible
		if end > len(m.Items) {
			end = len(m.Items)
		}
		widths := evidenceColumns(m.masterWidth())
		for i := m.Scroll; i < end; i++ {
			b.WriteString(m.viewEvidenceRow(m.Items[i], i == m.Cursor, widths))
		}
		b.WriteString(shared.RangeIndicator(m.styles, "files", m.Filter.Offset+m.Scroll+1, m.Filter.Offset+end, m.Total))
		b.WriteString("\n")
	}

	return shared.SplitPanes(m.styles, m.regions(), b.String(), m.viewRowDetail())
}

// viewRowDetail is the right-hand pane at the split breakpoint: the fields of
// the row under the cursor the list's columns had no room for.
func (m Model) viewRowDetail() string {
	if m.Cursor < 0 || m.Cursor >= len(m.Items) {
		return ""
	}
	item := m.Items[m.Cursor]

	var b strings.Builder
	b.WriteString(m.styles.Title.Render(filepathBase(item.Path)))
	b.WriteString("\n\n")

	row := func(label, value string) {
		if value == "" {
			return
		}
		b.WriteString(m.styles.DetailLabel.Render(label) + m.styles.DetailValue.Render(value) + "\n")
	}
	row("path   ", item.Path)
	row("sha256 ", item.SHA256)
	row("size   ", formatBytes(item.SizeBytes))
	row("kind   ", item.Kind)
	row("proves ", item.Proves)

	b.WriteString("\n")
	b.WriteString(m.styles.Help.Render("enter opens the file's detail, m its manifest"))
	return b.String()
}

// taskFilterLabel names the active task filter by the jira key (or sync id,
// for an sdd-only task) of any row carrying it — the filtered rows all share
// one task_id, so the first row found names it — or "all" when unset.
func taskFilterLabel(items []store.EvidenceListItem, f store.EvidenceListFilter) string {
	if f.TaskID == 0 {
		return "all"
	}
	for _, item := range items {
		if item.TaskID == f.TaskID {
			if item.JiraKey != nil {
				return *item.JiraKey
			}
			return item.TaskSyncID
		}
	}
	return fmt.Sprintf("#%d", f.TaskID)
}

func attachedFilterLabel(f store.EvidenceListFilter) string {
	if f.AttachedJira != nil && *f.AttachedJira {
		return "attached only"
	}
	return "all attached states"
}

// evidenceColumns solves the list row against the width it is drawn in.
// The name and what the capture proves are the two fields worth stretching;
// the kind, the task key and the timestamp never grow past their content.
func evidenceColumns(width int) []int {
	return shared.SolveColumns(width-evidenceRowFixed, 1, []shared.Column{
		{Min: 12, Weight: 3},
		{Min: 3, Max: evidenceKindCells},
		{Min: 8, Max: evidenceTaskCells},
		{Min: 8, Weight: 2},
		{Min: evidenceAttachedCells, Max: evidenceAttachedCells},
		{Min: 10, Max: 19},
	})
}

func (m Model) viewEvidenceRow(item store.EvidenceListItem, selected bool, widths []int) string {
	titleStyle := m.styles.ListItem
	if selected {
		titleStyle = m.styles.ListSelected
	}

	task := item.TaskSyncID
	if item.JiraKey != nil {
		task = *item.JiraKey
	}

	attached := m.styles.DangerInline.Render(shared.Cell(m.styles.Icons.Glyph(theme.IconTaskCancelled)+" jira", widths[4]))
	if item.AttachedJira {
		attached = m.styles.AttachedBadge.Render(shared.Cell(m.styles.Icons.Glyph(theme.IconFresh)+" jira", widths[4]))
	}

	row := fmt.Sprintf("%s%s %s %s %s %s %s",
		shared.RowCursor(m.styles, selected),
		titleStyle.Render(shared.Field(filepathBase(item.Path), widths[0])),
		m.styles.TypeBadge.Render("["+shared.Cell(item.Kind, widths[1])+"]"),
		m.styles.ID.Render(shared.Cell(task, widths[2])),
		m.styles.DetailValue.Render(shared.Field(item.Proves, widths[3])),
		attached,
		m.styles.Timestamp.Render(shared.Cell(shared.LocalTime(item.CapturedAt), widths[5])))

	return strings.TrimRight(row, " ") + "\n"
}

// ─── Detail ──────────────────────────────────────────────────────────────────

func (m Model) viewDetail() string {
	if m.Selected == nil {
		return shared.Loading(m.styles, spinner.Model{}, "the evidence")
	}
	item := *m.Selected
	var b strings.Builder

	b.WriteString(m.styles.Title.Render(filepathBase(item.Path)))
	b.WriteString("\n")

	// The card sits inside a border, so a value is cut to what is left of
	// the body once the frame and the label are paid — an absolute evidence
	// path is longer than any terminal and used to run straight off it.
	valueWidth := m.bodyWidth() - detailCardFrame - detailLabelCells
	detail := func(label, value string) string {
		if value == "" {
			return ""
		}
		return m.styles.DetailLabel.Render(label) + m.styles.DetailValue.Render(shared.Truncate(value, valueWidth)) + "\n"
	}

	task := item.TaskSyncID
	if item.JiraKey != nil {
		task = *item.JiraKey
	}

	meta := detail("path", absolutePath(item.Path)+"  (copy with p)") +
		detail("sha256", item.SHA256+"  (copy with c)") +
		detail("size", formatBytes(item.SizeBytes)) +
		detail("kind", item.Kind) +
		detail("proves", item.Proves) +
		detail("config_stamp", orEmptyStr(item.ConfigStamp)) +
		detail("captured", shared.LocalTime(item.CapturedAt)) +
		detail("task", task)
	b.WriteString(m.styles.StatCard.Render(strings.TrimRight(meta, "\n")))
	b.WriteString("\n")

	attachedJira := m.styles.DangerInline.Render(m.styles.Icons.Glyph(theme.IconTaskCancelled) + " run capture-evidence --attach")
	if item.AttachedJira {
		attachedJira = m.styles.AttachedBadge.Render(m.styles.Icons.Glyph(theme.IconFresh) + " attached")
	}
	confluence := "not needed"
	if item.AttachedConfluenceURL != nil && *item.AttachedConfluenceURL != "" {
		confluence = *item.AttachedConfluenceURL
	}
	b.WriteString(m.styles.DetailLabel.Render("attached_jira        ") + attachedJira + "\n")
	b.WriteString(m.styles.DetailLabel.Render("attached_confluence  ") + m.styles.DetailValue.Render(shared.Truncate(confluence, valueWidth)) + "\n")
	b.WriteString("\n")

	b.WriteString(m.viewManifestSection())

	return b.String()
}

// viewManifestSection renders whatever readManifestEntry found next to the
// selected file: the positive/negative control pair when a manifest.json
// exists and names this file, a plain notice when it does not exist yet
// (today's real evidence, see manifest.go), and the parse error when it
// exists but is not valid JSON.
func (m Model) viewManifestSection() string {
	if !m.ManifestChecked {
		return ""
	}
	var b strings.Builder
	b.WriteString(m.styles.SectionHeading.Render("  manifest.json (m)"))
	b.WriteString("\n")

	switch {
	case m.ManifestErr != "":
		b.WriteString(m.styles.Error.Render("  " + m.ManifestErr))
	case !m.ManifestExists:
		b.WriteString(m.styles.NoResults.Render("  no manifest.json next to this file"))
	case m.Manifest == nil:
		b.WriteString(m.styles.NoResults.Render("  manifest.json exists but has no entry for this file"))
	default:
		entry := m.Manifest
		b.WriteString(m.styles.DetailContent.Render(fmt.Sprintf(
			"  positive_control: %s\n  negative_control: %s",
			m.orDash(entry.PositiveControl), m.orDash(entry.NegativeControl))))
	}
	b.WriteString("\n\n")
	return b.String()
}

// orDash renders a field the capture never recorded. The mark comes from the
// icon vocabulary so it degrades with the resolved mode like every other
// glyph.
func (m Model) orDash(v string) string {
	if v == "" {
		return m.styles.Icons.Glyph(theme.IconUnknown)
	}
	return v
}

func orEmptyStr(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

// formatBytes renders size_bytes the way the dashboard does: a human count,
// or "unknown" when the capture never recorded one.
func formatBytes(v *int64) string {
	if v == nil {
		return "unknown"
	}
	n := *v
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

// filepathBase returns path's last segment without pulling in path/filepath
// for a single split — evidence.Path is always a "/"-joined relative path
// (D-06 rejects backslashes and absolute paths before it is ever stored),
// never an OS-native one, so a plain string split is exact here where
// filepath.Base's OS-dependent separator would not be on Windows.
func filepathBase(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[i+1:]
	}
	return path
}

// bodyWidth is how many cells this tab's rows may occupy: the terminal less
// what the app frame spends either side. A screen that has not received a
// tea.WindowSizeMsg yet falls back to a conventional 80-column terminal.
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
