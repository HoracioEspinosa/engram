package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	"github.com/charmbracelet/x/ansi"
)

// runCmd executes cmd the way the Bubble Tea runtime would.
func runCmd(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("runCmd: nil command")
	}
	return cmd()
}

// statusBarModel is a workspace on a project deep enough in the forest to
// have a breadcrumb worth drawing, enrolled in sync, with a task selected.
func statusBarModel(t *testing.T) Model {
	t.Helper()

	tree := &data.FakeProjectTree{AncestorsBySlug: map[string][]data.ProjectNode{
		"previews": {
			{Slug: "clarodrive", DisplayName: "ClaroDrive"},
			{Slug: "nextcloud"},
		},
	}}
	tasks := &data.FakeTask{ItemsByProject: map[string][]store.TaskListItem{
		"previews": {{Task: store.Task{ID: 1, JiraKey: strp("CDBS-1010"), Title: "Preview 503s"}}},
	}}

	m := New(nil, nil, tasks, nil, nil, "", theme.New(theme.KoiPond()), "previews").WithProjectTree(tree)
	m.width, m.height = 120, 40
	m.screen, m.tree.open = screenTab, false
	m.active = tabs.Tasks

	msg := loadAncestors(tree, "previews")()
	updated, _ := m.Update(msg)
	m = updated.(Model)

	m.dashboard = m.dashboard.applyLoaded(dashboardLoadedMsg{
		slug:   "previews",
		card:   store.ProjectCard{Slug: "previews"},
		health: data.ProjectHealth{Sync: store.ProjectSyncSummary{Enrolled: true, Lifecycle: "healthy"}},
	})
	// Drive the tab's own load so the list has a row under the cursor: the
	// status bar reads the selection, not the fixture.
	loaded, _ := m.tasks.Update(runCmd(t, m.tasks.Init()))
	m = m.withTab(tabs.Tasks, loaded)
	return m
}

// TestStatusBarShowsBreadcrumbAndSync pins what the bar is for: where the
// user is in the forest, what they are working on, and whether the workspace
// is in sync — all on the one line the frame spends on chrome.
func TestStatusBarShowsBreadcrumbAndSync(t *testing.T) {
	m := statusBarModel(t)

	bar := ansi.Strip(m.viewStatusBar())
	if strings.Contains(bar, "\n") {
		t.Fatalf("the status bar took more than one row:\n%s", bar)
	}
	for _, want := range []string{"ClaroDrive", "nextcloud", "previews", "CDBS-1010", "sync: healthy", "koi-pond"} {
		if !strings.Contains(bar, want) {
			t.Fatalf("the status bar omits %q:\n%s", want, bar)
		}
	}

	// The whole screen carries it exactly once, at the bottom.
	view := ansi.Strip(m.View())
	if strings.Count(view, "sync: healthy") != 1 {
		t.Fatalf("the sync state is rendered %d times:\n%s", strings.Count(view, "sync: healthy"), view)
	}
	// The frame's own bottom padding follows it, so the bar is the last row
	// with anything on it.
	lines := strings.Split(view, "\n")
	last := ""
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			last = lines[i]
			break
		}
	}
	if !strings.Contains(last, "sync: healthy") {
		t.Fatalf("the status bar is not the frame's bottom row:\n%s", view)
	}
}

// TestStatusBarFallsBackToTheProjectWithoutATree covers the workspace built
// with no ProjectTreeReader: the breadcrumb is the project on its own rather
// than an empty segment or a crash.
func TestStatusBarFallsBackToTheProjectWithoutATree(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.KoiPond()), "previews")
	m.width, m.height = 120, 40

	seg, ok := m.breadcrumbSegment()
	if !ok {
		t.Fatal("an active project renders no breadcrumb at all")
	}
	if seg.Text != "previews" {
		t.Fatalf("breadcrumb = %q, want the project on its own", seg.Text)
	}
	if cmd := loadAncestors(m.treeReader, "previews"); cmd != nil {
		t.Fatal("a workspace with no tree reader still issued an ancestors query")
	}
}

// TestAStaleAncestorChainIsIgnored covers the guard every load in this root
// carries: a chain that arrives after the user switched projects must not
// relabel the breadcrumb of the one now on screen.
func TestAStaleAncestorChainIsIgnored(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.KoiPond()), "previews")

	updated, _ := m.Update(ancestorsLoadedMsg{slug: "somewhere-else", nodes: []data.ProjectNode{{Slug: "ghost"}}})
	if got := updated.(Model).ancestors; len(got) != 0 {
		t.Fatalf("a chain for another project was applied: %+v", got)
	}
}
