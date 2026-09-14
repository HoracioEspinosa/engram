package evidence

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
	case ScreenDetail:
		content = m.viewDetail()
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

// ─── List (S6) ───────────────────────────────────────────────────────────────

func (m Model) viewList() string {
	var b strings.Builder

	b.WriteString(m.styles.SectionHeading.Render(fmt.Sprintf(
		"  Evidence (%d shown · task: %s · %s)",
		len(m.Items), taskFilterLabel(m.Items, m.Filter), attachedFilterLabel(m.Filter))))
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
		for i := m.Scroll; i < end; i++ {
			b.WriteString(m.viewEvidenceRow(m.Items[i], i == m.Cursor))
		}
		b.WriteString(shared.RangeIndicator(m.styles, "files", m.Scroll+1, end, len(m.Items)))
		b.WriteString("\n")
	}

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

func (m Model) viewEvidenceRow(item store.EvidenceListItem, selected bool) string {
	cursor := "  "
	titleStyle := m.styles.ListItem
	if selected {
		cursor = "▸ "
		titleStyle = m.styles.ListSelected
	}

	task := item.TaskSyncID
	if item.JiraKey != nil {
		task = *item.JiraKey
	}

	attached := m.styles.DangerInline.Render("✗ jira")
	if item.AttachedJira {
		attached = m.styles.AttachedBadge.Render("✓ jira")
	}

	return fmt.Sprintf("%s%s %s %s %s %s %s\n",
		cursor,
		titleStyle.Render(shared.PadCells(shared.Truncate(filepathBase(item.Path), evidenceNameCells), evidenceNameCells)),
		m.styles.TypeBadge.Render("["+shared.PadCells(item.Kind, evidenceKindCells)+"]"),
		m.styles.ID.Render(shared.PadCells(shared.CutCells(task, evidenceTaskCells), evidenceTaskCells)),
		m.styles.DetailValue.Render(shared.Truncate(item.Proves, 40)),
		attached,
		m.styles.Timestamp.Render(shared.LocalTime(item.CapturedAt)))
}

// ─── Detail (S7) ─────────────────────────────────────────────────────────────

func (m Model) viewDetail() string {
	if m.Selected == nil {
		return m.styles.StatCard.Render("Loading evidence...")
	}
	item := *m.Selected
	var b strings.Builder

	b.WriteString(m.styles.Title.Render(filepathBase(item.Path)))
	b.WriteString("\n")

	detail := func(label, value string) string {
		if value == "" {
			return ""
		}
		return m.styles.DetailLabel.Render(label) + m.styles.DetailValue.Render(value) + "\n"
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

	attachedJira := m.styles.DangerInline.Render("✗ run capture-evidence --attach")
	if item.AttachedJira {
		attachedJira = m.styles.AttachedBadge.Render("✓ attached")
	}
	confluence := "not needed"
	if item.AttachedConfluenceURL != nil && *item.AttachedConfluenceURL != "" {
		confluence = *item.AttachedConfluenceURL
	}
	b.WriteString(m.styles.DetailLabel.Render("attached_jira        ") + attachedJira + "\n")
	b.WriteString(m.styles.DetailLabel.Render("attached_confluence  ") + m.styles.DetailValue.Render(confluence) + "\n")
	b.WriteString("\n")

	b.WriteString(m.viewManifestSection())

	return b.String()
}

// viewManifestSection renders whatever readManifestEntry found next to the
// selected file (rfc-tui.md §9.3): the positive/negative control pair when a
// manifest.json exists and names this file, a plain notice when it does not
// exist yet (today's real evidence, see manifest.go), and the parse error
// when it exists but is not valid JSON.
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
			orDash(entry.PositiveControl), orDash(entry.NegativeControl))))
	}
	b.WriteString("\n\n")
	return b.String()
}

func orDash(v string) string {
	if v == "" {
		return "—"
	}
	return v
}

func orEmptyStr(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

// formatBytes renders size_bytes the way the dashboard and S7's wireframe
// do: a human count, or "unknown" when the capture never recorded one.
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
