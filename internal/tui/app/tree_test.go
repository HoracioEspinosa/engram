package app

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	tea "github.com/charmbracelet/bubbletea"
)

// treeForest is the fixture every case below walks: one umbrella with two
// children and a second root, which is the smallest shape that can tell
// preorder apart from "roots first, then children".
func treeForest() []data.ProjectNode {
	return []data.ProjectNode{
		{
			Slug: "clarodrive", DisplayName: "Claro Drive", Kind: "umbrella",
			Tags:   []string{"platform"},
			Counts: store.ProjectCardCounts{Observations: 120, TasksActive: 4, Evidence: 9},
			Children: []data.ProjectNode{
				{
					Slug: "clarodrive-api", ParentSlug: "clarodrive", DisplayName: "Claro Drive API",
					Kind: "service", Aliases: []string{"thunderbird"}, Depth: 1,
					Counts: store.ProjectCardCounts{Observations: 18, TasksActive: 1, Evidence: 4, RunbooksStale: 3},
				},
				{
					Slug: "clarodrive-web", ParentSlug: "clarodrive", DisplayName: "Claro Drive Web",
					Kind: "repo", Depth: 1,
					Counts: store.ProjectCardCounts{Observations: 7, Evidence: 2},
				},
			},
		},
		{
			Slug: "engram", DisplayName: "Engram", Kind: "repo",
			Counts: store.ProjectCardCounts{Observations: 300, TasksActive: 2, Evidence: 1},
		},
	}
}

// loadedTree returns a workspace with the forest already in hand and the
// overlay open, which is where every key case below starts.
func loadedTree(t *testing.T) Model {
	t.Helper()
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.KoiPond()), "")
	m = m.WithProjectTree(&data.FakeProjectTree{Tree: treeForest()})
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m, _ = step(t, m, treeLoadedMsg{roots: treeForest()})
	return m
}

func treeSlugs(m Model) []string {
	slugs := make([]string, 0, len(m.tree.rows))
	for _, row := range m.tree.rows {
		slugs = append(slugs, row.node.Slug)
	}
	return slugs
}

func TestTheTreeFlattensTheForestInPreorder(t *testing.T) {
	m := loadedTree(t)

	want := []string{"clarodrive", "clarodrive-api", "clarodrive-web", "engram"}
	if got := treeSlugs(m); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("rows = %v, want %v: a child belongs directly under its parent, not after every root", got, want)
	}

	for i, depth := range []int{0, 1, 1, 0} {
		if m.tree.rows[i].depth != depth {
			t.Errorf("row %d (%s) has depth %d, want %d", i, m.tree.rows[i].node.Slug, m.tree.rows[i].depth, depth)
		}
	}
}

func TestSpaceFoldsAndUnfoldsTheSubtreeUnderTheCursor(t *testing.T) {
	m := loadedTree(t)
	m.tree.cursor = 0

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	want := []string{"clarodrive", "engram"}
	if got := treeSlugs(m); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("after folding rows = %v, want %v", got, want)
	}

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	if got := treeSlugs(m); len(got) != 4 {
		t.Fatalf("after unfolding rows = %v, want the whole forest back", got)
	}
}

// TestFoldingOneCopyOfTheModelDoesNotLeakIntoAnother pins that the fold set is
// rebuilt on every write. The root is a value: a map shared between copies
// would let a fold applied to a model the runtime discards still show up in
// the one it keeps.
func TestFoldingOneCopyOfTheModelDoesNotLeakIntoAnother(t *testing.T) {
	m := loadedTree(t)
	m.tree.cursor = 0

	kept := m
	folded, _ := step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})

	if folded.tree.collapsed["clarodrive"] != true {
		t.Fatal("the fold did not reach the model the update returned")
	}
	if kept.tree.collapsed["clarodrive"] {
		t.Fatal("the fold leaked into a copy of the model that never folded anything")
	}
}

func TestSpaceOnALeafFoldsNothing(t *testing.T) {
	m := loadedTree(t)
	m.tree.cursor = 3 // engram, a root with no children

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})

	if len(m.tree.collapsed) != 0 {
		t.Fatalf("collapsed = %v, want nothing: a leaf has no subtree to put away", m.tree.collapsed)
	}
	if got := treeSlugs(m); len(got) != 4 {
		t.Fatalf("rows = %v, want the forest untouched", got)
	}
}

// TestFilteringAChildKeepsItsParentOnScreen is §6.9's rule for the tree
// filter: a hit that is nested has to be reachable, so the path down to it
// stays visible even though the parent itself matched nothing.
func TestFilteringAChildKeepsItsParentOnScreen(t *testing.T) {
	m := loadedTree(t)
	m.tree.filterInput.SetValue("api")
	m.tree = m.tree.applyFilter()

	want := []string{"clarodrive", "clarodrive-api"}
	if got := treeSlugs(m); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("filtered rows = %v, want %v", got, want)
	}
	if !m.tree.rows[0].forced {
		t.Error("the parent is on screen only because of its child; it should be marked as such")
	}
	if m.tree.rows[1].forced {
		t.Error("the child matched the filter itself and must not be marked as a forced parent")
	}
}

// TestTheFilterReachesAliasesAndTags pins that a project is findable by every
// name it answers to, not only by the two labels the row draws.
func TestTheFilterReachesAliasesAndTags(t *testing.T) {
	cases := []struct {
		term string
		want string
	}{
		{"thunderbird", "clarodrive-api"},
		{"platform", "clarodrive"},
	}

	for _, tc := range cases {
		t.Run(tc.term, func(t *testing.T) {
			m := loadedTree(t)
			m.tree.filterInput.SetValue(tc.term)
			m.tree = m.tree.applyFilter()

			var matched []string
			for _, row := range m.tree.rows {
				if !row.forced {
					matched = append(matched, row.node.Slug)
				}
			}
			if len(matched) != 1 || matched[0] != tc.want {
				t.Fatalf("%q matched %v, want exactly [%s]", tc.term, matched, tc.want)
			}
		})
	}
}

// TestTheFilterHighlightsWhatItMatchedInTheVisibleLabels pins that the
// highlight points at characters the reader can see: a hit found through an
// alias highlights nothing, because there is nothing on the row to underline.
func TestTheFilterHighlightsWhatItMatchedInTheVisibleLabels(t *testing.T) {
	m := loadedTree(t)
	m.tree.filterInput.SetValue("api")
	m.tree = m.tree.applyFilter()

	child := m.tree.rows[1]
	if len(child.slugMatched) != 3 {
		t.Fatalf("slug matched indexes = %v, want the three letters of %q inside %q", child.slugMatched, "api", child.node.Slug)
	}
	for _, i := range child.slugMatched {
		if i < 0 || i >= len(child.node.Slug) {
			t.Errorf("matched index %d falls outside %q", i, child.node.Slug)
		}
	}

	m = loadedTree(t)
	m.tree.filterInput.SetValue("thunderbird")
	m.tree = m.tree.applyFilter()
	if got := m.tree.rows[1].slugMatched; got != nil {
		t.Errorf("an alias hit highlighted %v in the slug, but the alias is not drawn on the row", got)
	}
}

// TestTheFilterIgnoresFolds pins the precedence: a fold hides what the reader
// put away, a filter hides what they did not ask for, and a hit nobody can see
// is the same as no hit at all.
func TestTheFilterIgnoresFolds(t *testing.T) {
	m := loadedTree(t)
	m.tree.cursor = 0
	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})

	m.tree.filterInput.SetValue("api")
	m.tree = m.tree.applyFilter()

	if got := treeSlugs(m); strings.Join(got, ",") != "clarodrive,clarodrive-api" {
		t.Fatalf("filtered rows = %v, want the folded child to surface for its hit", got)
	}
}

func TestSortingByHealthPutsTheWorstSiblingFirst(t *testing.T) {
	m := loadedTree(t)

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})

	want := []string{"clarodrive", "clarodrive-api", "clarodrive-web", "engram"}
	if got := treeSlugs(m); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("rows = %v, want %v: clarodrive-api carries the stale runbooks", got, want)
	}

	// Toggling back restores the store's own order, which for this fixture is
	// the same shape — so the flag itself is what is asserted.
	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	if m.tree.healthSort {
		t.Error("\"i\" is a toggle, not a one-way switch")
	}
}

func TestEnterScopesTheWholeWorkspaceToTheProjectUnderTheCursor(t *testing.T) {
	m := loadedTree(t)
	m.tree.cursor = 1 // clarodrive-api

	m, cmd := step(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	if m.project != "clarodrive-api" {
		t.Fatalf("project = %q, want clarodrive-api", m.project)
	}
	if m.tree.open {
		t.Error("choosing a project should close the overlay")
	}
	if m.active != tabs.Home {
		t.Errorf("active = %v, want the project's own Home tab", m.active)
	}
	if m.home.Project() != "clarodrive-api" {
		t.Errorf("Home is still scoped to %q", m.home.Project())
	}
	if cmd == nil {
		t.Error("choosing a project should load Home")
	}
	// Nothing any tab is holding belongs to the project now active, so every
	// one of them has to reload before it is shown again. Home is the
	// exception: it is the tab being opened, and its reload is in flight.
	for _, id := range registered {
		if id == tabs.Home {
			continue
		}
		if !m.freshness.stale(id, time.Now()) {
			t.Errorf("the %s tab was left holding the previous project's data", id)
		}
	}
}

func TestCtrlPOpensTheTreeOnTheProjectAlreadyActive(t *testing.T) {
	m := loadedTree(t)
	m.project = "clarodrive-web"
	m.tree.open = false
	m.tree.cursor = 0

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyCtrlP})

	if !m.tree.open {
		t.Fatal("ctrl+p should open the tree")
	}
	if got := m.tree.rows[m.tree.cursor].node.Slug; got != "clarodrive-web" {
		t.Fatalf("cursor is on %q, want the project already active", got)
	}
}

func TestEscClosesTheTreeEvenWithNoProjectChosen(t *testing.T) {
	m := loadedTree(t)

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyEsc})

	if m.tree.open {
		t.Fatal("esc should close the overlay: Memory is browsable unscoped and ctrl+p brings the tree back")
	}
}

// countingTree records how many reads the overlay issues. The fake reader has
// no counters of its own, and what matters here is a property of the screen —
// one bulk read per load — rather than of the adapter underneath it.
type countingTree struct {
	*data.FakeProjectTree

	mu    sync.Mutex
	reads int
}

func (c *countingTree) ProjectTree() ([]data.ProjectNode, error) {
	c.mu.Lock()
	c.reads++
	c.mu.Unlock()
	return c.FakeProjectTree.ProjectTree()
}

func (c *countingTree) Reads() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reads
}

// TestProjectTreeIssuesAtMostTwoReads is the whole reason the overlay asks for
// a forest instead of walking parents itself: opening it costs a bounded
// number of reads, not one per project. Forty-eight cards used to mean
// forty-eight round trips before the tree existed.
func TestProjectTreeIssuesAtMostTwoReads(t *testing.T) {
	reader := &countingTree{FakeProjectTree: &data.FakeProjectTree{Tree: treeForest()}}

	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.KoiPond()), "")
	m = m.WithProjectTree(reader)
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})

	cmd := loadTree(m.tree.reader)
	if cmd == nil {
		t.Fatal("opening the tree issued no load")
	}
	m, _ = step(t, m, cmd())

	// Everything the overlay does after the load is local: folding,
	// filtering, sorting and moving the cursor read nothing.
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune(" ")},
		{Type: tea.KeyRunes, Runes: []rune("i")},
		{Type: tea.KeyRunes, Runes: []rune("j")},
		{Type: tea.KeyRunes, Runes: []rune("G")},
	} {
		m, _ = step(t, m, key)
	}

	if got := reader.Reads(); got > 2 {
		t.Fatalf("the project tree issued %d reads, want at most 2", got)
	}
	if reader.Reads() == 0 {
		t.Fatal("the project tree issued no read at all")
	}
}

func TestTheTreeReportsAReadThatFailed(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.KoiPond()), "")
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})

	m, _ = step(t, m, treeLoadedMsg{err: errors.New("database is locked")})

	if !strings.Contains(m.View(), "database is locked") {
		t.Fatalf("a failed read should say so, got:\n%s", m.View())
	}
}

func TestTheTreeSaysWhenNoReaderIsBound(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.KoiPond()), "")
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})

	msg, ok := loadTree(nil)().(treeLoadedMsg)
	if !ok {
		t.Fatalf("loadTree(nil) produced %T, want treeLoadedMsg", msg)
	}
	if msg.err == nil {
		t.Fatal("a workspace with no forest reader should say so rather than open empty")
	}
}

func TestTheTreeRendersNestingCountersAndTheEmptyFilter(t *testing.T) {
	m := loadedTree(t)
	m.tree.open = true

	out := m.View()
	for _, want := range []string{"Claro Drive API", "clarodrive-web", "120", "space fold"} {
		if !strings.Contains(out, want) {
			t.Errorf("the tree does not show %q, got:\n%s", want, out)
		}
	}

	m.tree.filterInput.SetValue("no-such-project")
	m.tree = m.tree.applyFilter()
	if !strings.Contains(m.View(), "No projects match the filter.") {
		t.Errorf("a filter that matches nothing should say so, got:\n%s", m.View())
	}
}

// TestTheTreeIsTheHelpItAdvertises keeps the status bar honest while the
// overlay has the keyboard: the hints come from the overlay's own bindings,
// not from the screen underneath it.
func TestTheTreeIsTheHelpItAdvertises(t *testing.T) {
	m := loadedTree(t)
	m.active = tabs.Memory

	got := m.activeScreenHelp()
	if len(got) != len(treeHelp()) {
		t.Fatalf("the active screen's help lists %d bindings, want the tree's %d", len(got), len(treeHelp()))
	}
}

func TestTheTreeCapturesTextOnlyWhileItsFilterIsFocused(t *testing.T) {
	m := loadedTree(t)

	if m.CapturingText() {
		t.Fatal("an open overlay with an unfocused filter is answering commands, not typing")
	}

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	if !m.CapturingText() {
		t.Fatal("\"/\" should give the filter the keyboard")
	}

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.CapturingText() || m.tree.filterInput.Value() != "" {
		t.Fatal("esc should blur the filter and clear it")
	}
	if !m.tree.open {
		t.Fatal("esc out of the filter should leave the overlay open: it closes the box, not the screen")
	}
}
