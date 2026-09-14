package app

import (
	"strings"

	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/shared"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	tea "github.com/charmbracelet/bubbletea"
)

// Status bar priorities, lowest first. They are the give-ground order §6.9
// fixes: the hints go first because a screen advertises them and the "?"
// overlay repeats them; the theme name is decoration; the breadcrumb sheds
// its parents but keeps the project; the active task is the one thing the
// user is working on; and the sync state never gives, because a workspace
// quietly out of sync is exactly what a status bar exists to prevent.
const (
	statusPriorityHints = iota + 1
	statusPriorityTheme
	statusPriorityBreadcrumb
	statusPriorityTask
	statusPrioritySync
)

// statusMinWidths are how far each segment may be elided before it stops
// saying anything at all.
const (
	statusMinHints      = 8
	statusMinBreadcrumb = 6
	statusMinTask       = 6
)

// ancestorsLoadedMsg carries loadAncestors' result. slug guards the same way
// dashboardLoadedMsg does: a slow lookup for a project the user has since
// left must not relabel the breadcrumb of the one now on screen.
type ancestorsLoadedMsg struct {
	slug  string
	nodes []data.ProjectNode
	err   error
}

// loadAncestors returns the command that reads slug's ancestor chain for the
// breadcrumb. A workspace with no project tree reader wired, or no project
// active, has nothing to ask.
func loadAncestors(r data.ProjectTreeReader, slug string) tea.Cmd {
	if r == nil || slug == "" {
		return nil
	}
	return func() tea.Msg {
		nodes, err := r.Ancestors(slug)
		return ancestorsLoadedMsg{slug: slug, nodes: nodes, err: err}
	}
}

// viewStatusBar renders the frame's bottom line: where the user is, what
// they are working on, what the screen answers to, and whether the workspace
// is in sync.
//
// It is one line, not two. The hints used to be a footer of their own with a
// blank line above it, which spent three rows of a 24-row terminal on chrome.
func (m Model) viewStatusBar() string {
	width := m.width
	if width <= 0 {
		return ""
	}
	// The app frame pads two cells either side, and the bar is drawn inside
	// it.
	if width -= 4; width <= 0 {
		return ""
	}

	left := make([]shared.Segment, 0, 3)
	if seg, ok := m.breadcrumbSegment(); ok {
		left = append(left, seg)
	}
	if seg, ok := m.activeTaskSegment(); ok {
		left = append(left, seg)
	}
	if seg, ok := m.hintsSegment(); ok {
		left = append(left, seg)
	}

	right := make([]shared.Segment, 0, 2)
	if seg, ok := m.syncSegment(); ok {
		right = append(right, seg)
	}
	right = append(right, shared.Segment{
		Text:     m.styles.Palette.Name,
		Style:    m.styles.Timestamp,
		Priority: statusPriorityTheme,
		MinWidth: len(m.styles.Palette.Name),
	})

	return shared.StatusBar(m.styles, width, left, right)
}

// breadcrumbSegment is the active project's place in the forest: its
// ancestors root-first, then the project itself. Short keeps the project and
// drops the ancestors, which is the half the reader already knows.
func (m Model) breadcrumbSegment() (shared.Segment, bool) {
	slug := m.project
	if slug == "" {
		return shared.Segment{}, false
	}

	names := make([]string, 0, len(m.ancestors)+1)
	for _, node := range m.ancestors {
		names = append(names, projectLabel(node.DisplayName, node.Slug))
	}
	names = append(names, slug)

	// The separator is a glyph, so it comes from the icon vocabulary rather
	// than being spelled here.
	separator := " " + m.styles.Icons.Glyph(theme.IconChevronRight) + " "

	return shared.Segment{
		Icon:     m.styles.Icons.Glyph(theme.IconTree),
		Text:     strings.Join(names, separator),
		Short:    slug,
		Style:    m.styles.Project,
		Priority: statusPriorityBreadcrumb,
		MinWidth: statusMinBreadcrumb,
	}, true
}

// projectLabel prefers a card's display name and falls back to its slug,
// which is the only name every project is guaranteed to have.
func projectLabel(displayName, slug string) string {
	if name := strings.TrimSpace(displayName); name != "" {
		return name
	}
	return slug
}

// activeTaskSegment names the task the Tasks tab is sitting on, so the key
// stays on screen while the user is reading evidence or memory for it.
func (m Model) activeTaskSegment() (shared.Segment, bool) {
	key := m.tasks.SelectedKey()
	if key == "" {
		return shared.Segment{}, false
	}
	return shared.Segment{
		Icon:     m.styles.Icons.Glyph(theme.IconTabTasks),
		Text:     key,
		Style:    m.styles.ID,
		Priority: statusPriorityTask,
		MinWidth: statusMinTask,
	}, true
}

// hintsSegment carries whatever the active screen declares in Help(). It is
// the same derivation the footer used, moved into the bar.
func (m Model) hintsSegment() (shared.Segment, bool) {
	hints := shared.PlainHintsFrom(m.activeScreenHelp())
	if hints == "" {
		return shared.Segment{}, false
	}
	return shared.Segment{
		Text:     hints,
		Style:    m.styles.StatusBar,
		Priority: statusPriorityHints,
		MinWidth: statusMinHints,
	}, true
}

// syncSegment reports the active project's sync lifecycle, in the store's own
// vocabulary — the TUI renders what the store says and never recomputes it.
func (m Model) syncSegment() (shared.Segment, bool) {
	if m.dashboard.slug == "" && m.project == "" {
		return shared.Segment{}, false
	}
	sync := m.dashboard.health.Sync
	if !sync.Enrolled {
		return shared.Segment{}, false
	}
	text := "sync: " + sync.Lifecycle
	return shared.Segment{
		Icon:     m.styles.Icons.SyncState(sync.Lifecycle),
		Text:     text,
		Style:    m.styles.Timestamp,
		Priority: statusPrioritySync,
		MinWidth: len(text),
	}, true
}
