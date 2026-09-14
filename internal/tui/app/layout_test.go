package app

import (
	"strings"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// TestNoScreenExceedsItsWidth is the layout's own gate: every screen this
// build registers, at both geometries the wireframes are drawn at, fits the
// terminal it was given.
//
// A row that overflows does not wrap harmlessly — it pushes the rest of the
// frame sideways and the reader loses the column they were scanning. Fixed
// column constants passed this test at exactly one width and failed at every
// other, which is why the widths are solved now.
func TestNoScreenExceedsItsWidth(t *testing.T) {
	for _, size := range goldenSizes {
		for _, scene := range goldenScenes() {
			t.Run(size.name+"/"+scene.name, func(t *testing.T) {
				assertFitsWidth(t, renderScene(t, scene, size), size.width)
			})
		}

		for _, id := range registered {
			t.Run(size.name+"/tab-"+tabTitle(t, id), func(t *testing.T) {
				assertFitsWidth(t, renderTab(t, id, size), size.width)
			})
		}

		t.Run(size.name+"/selector", func(t *testing.T) {
			m := populatedWorkspace(t, size)
			m.screen = screenSelector
			m.selector = m.selector.applyLoaded(selectorLoadedMsg{cards: []store.ProjectCardListItem{
				{
					ProjectCard: store.ProjectCard{Slug: "a-very-long-project-slug-nobody-would-type", DisplayName: "A display name long enough to need cutting"},
					Counts:      &store.ProjectCardCounts{Observations: 1204, TasksActive: 17, RunbooksStale: 3},
				},
			}})
			assertFitsWidth(t, m.View(), size.width)
		})
	}
}

// tabTitle names a registered tab for the subtest, from the tab's own
// Title() rather than from a second list this test would have to maintain.
func tabTitle(t *testing.T, id tabs.ID) string {
	t.Helper()
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.KoiPond()), "")
	if tab := m.tab(id); tab != nil {
		return tab.Title()
	}
	return "unregistered"
}

// assertFitsWidth fails when any row of out is wider than the terminal.
func assertFitsWidth(t *testing.T, out string, width int) {
	t.Helper()
	for i, line := range strings.Split(out, "\n") {
		if w := ansi.StringWidth(strings.TrimRight(line, " ")); w > width {
			t.Fatalf("line %d is %d cells wide, want at most %d:\n%s", i+1, w, width, line)
		}
	}
}

// populatedWorkspace is a workspace with a project and a terminal size, its
// tabs fed rows long enough to overflow a row that is not solved for width.
func populatedWorkspace(t *testing.T, size goldenSize) Model {
	t.Helper()

	longTitle := "A task title far longer than any column a fixed constant would have reserved for it"
	tasks := &data.FakeTask{ItemsByProject: map[string][]store.TaskListItem{"acme": {
		{Task: store.Task{ID: 1, JiraKey: strp("CDBS-10336"), Title: longTitle, Kind: "bugfix", State: "in_progress", Branch: strp("feature/a-long-branch-name")}, Observations: 4, Evidence: 2, StateStale: true},
	}}}
	evidence := &data.FakeEvidence{ItemsByProject: map[string][]store.EvidenceListItem{"acme": {
		{Evidence: store.Evidence{ID: 1, TaskID: 1, Path: "ACME-1/an-evidence-file-with-a-very-long-name.png", Kind: "png", Proves: "the cold start exceeds thirty seconds under a full cache flush", SHA256: strings.Repeat("a", 64), CapturedAt: "2026-01-15 10:05:00"}, JiraKey: strp("CDBS-10336")},
	}}}
	runbooks := &data.FakeRunbook{ItemsByProject: map[string][]store.RunbookIndexRow{"acme": {
		{ID: "RB-900", Project: "acme", VaultPath: "Runbooks/RB-900.md", Title: "Preview endpoint returns 503 under load and never recovers on its own", Category: "performance", Status: "verified", Stale: true, Symptoms: []string{"503 from the preview endpoint", "worker pool saturated"}},
	}}}
	memory := &data.FakeMemory{
		Observations: []store.Observation{{ID: 102, Type: "bugfix", Title: "Session delete refused while observations remain attached", Content: "Guard the delete path so orphan observations cannot appear.", CreatedAt: "2026-01-15 10:05:00", Project: strp("clarodrive")}},
	}

	m := New(memory, nil, tasks, evidence, runbooks, "test", theme.New(theme.KoiPond()), "acme")
	sized, _ := m.Update(tea.WindowSizeMsg{Width: size.width, Height: size.height})
	return sized.(Model)
}

// renderTab drives one tab's own load and renders the workspace on it.
func renderTab(t *testing.T, id tabs.ID, size goldenSize) string {
	t.Helper()

	m := populatedWorkspace(t, size)
	m.screen = screenTab
	m.active = id

	if tab := m.tab(id); tab != nil {
		if cmd := tab.Refresh(); cmd != nil {
			updated, _ := tab.Update(cmd())
			m = m.withTab(id, updated)
		}
	}
	return m.View()
}

// TestDetailPaneFollowsTheCursor pins the master-detail contract at the split
// breakpoint: the pane beside the list describes the row under the cursor,
// and moving the cursor moves what it describes.
func TestDetailPaneFollowsTheCursor(t *testing.T) {
	items := []store.EvidenceListItem{
		{Evidence: store.Evidence{ID: 1, TaskID: 1, Path: "ACME-1/first.png", Kind: "png", Proves: "the first capture", SHA256: strings.Repeat("a", 64)}},
		{Evidence: store.Evidence{ID: 2, TaskID: 1, Path: "ACME-1/second.png", Kind: "png", Proves: "the second capture", SHA256: strings.Repeat("b", 64)}},
	}
	fake := &data.FakeEvidence{ItemsByProject: map[string][]store.EvidenceListItem{"acme": items}}

	wide := goldenSize{name: "120x40", width: 120, height: 40}
	m := New(nil, nil, nil, fake, nil, "test", theme.New(theme.KoiPond()), "acme")
	sized, _ := m.Update(tea.WindowSizeMsg{Width: wide.width, Height: wide.height})
	m = sized.(Model)
	m.screen, m.active = screenTab, tabs.Evidence

	loaded, _ := m.evidence.Update(m.evidence.Refresh()())
	m = m.withTab(tabs.Evidence, loaded)

	first := ansi.Strip(m.View())
	if !strings.Contains(first, "the first capture") {
		t.Fatalf("the detail pane does not describe the first row:\n%s", first)
	}

	moved, _ := m.evidence.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = m.withTab(tabs.Evidence, moved)

	second := ansi.Strip(m.View())
	if !strings.Contains(second, "the second capture") {
		t.Fatalf("the detail pane did not follow the cursor:\n%s", second)
	}

	// At a single-column width there is no pane at all, so nothing follows.
	narrow := goldenSize{name: "80x24", width: 80, height: 24}
	resized, _ := m.Update(tea.WindowSizeMsg{Width: narrow.width, Height: narrow.height})
	m = resized.(Model)
	if out := ansi.Strip(m.View()); strings.Contains(out, "sha256") {
		t.Fatalf("80 columns still drew the detail pane:\n%s", out)
	}
}
