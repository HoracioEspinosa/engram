package runbooks

import (
	"testing"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"
)

// TestTitleIsRunbooks pins the tab bar label, the same trivial-looking
// assertion every sibling tab's model_test.go carries for its own Title.
func TestTitleIsRunbooks(t *testing.T) {
	if got := (Model{}).Title(); got != "Runbooks" {
		t.Fatalf("Title() = %q, want %q", got, "Runbooks")
	}
}

// TestWithStylesReplacesThePalette pins the seam app.New relies on to paint
// every tab with the resolved theme instead of its own default: New() starts
// a tab on theme.Default(), and WithStyles must actually replace it, not
// silently keep the default.
func TestWithStylesReplacesThePalette(t *testing.T) {
	m := newModel(&data.FakeRunbook{}, nil)
	if m.Styles().Palette.Primary != theme.Default().Palette.Primary {
		t.Fatalf("a fresh Model should start on theme.Default()")
	}

	kanagawa := theme.New(theme.Kanagawa())
	m = m.WithStyles(kanagawa)
	if m.Styles().Palette.Primary != kanagawa.Palette.Primary {
		t.Fatalf("WithStyles did not replace the palette: got %v, want kanagawa's %v",
			m.Styles().Palette.Primary, kanagawa.Palette.Primary)
	}
}

// TestRefreshReloadsTheIndexWithNoActiveSearch pins Refresh()'s default
// branch: on the index with no committed query, "r" (and becoming the
// active tab) re-issues the plain, unfiltered load.
func TestRefreshReloadsTheIndexWithNoActiveSearch(t *testing.T) {
	fake := &data.FakeRunbook{ItemsByProject: map[string][]store.RunbookIndexRow{
		"acme": {sampleRunbook("RB-900", "acme", "Stale runbook", true)},
	}}
	m := newModel(fake, nil).WithProject("acme")

	cmd := m.Refresh()
	if cmd == nil {
		t.Fatal("Refresh on the index should reload it")
	}
	msg, ok := run(t, cmd).(runbooksLoadedMsg)
	if !ok {
		t.Fatalf("Refresh produced %T, want runbooksLoadedMsg", run(t, cmd))
	}
	if len(msg.page.Items) != 1 || msg.page.Items[0].ID != "RB-900" {
		t.Fatalf("Refresh loaded %+v, want the seeded RB-900", msg.page.Items)
	}
}

// TestRefreshReRunsTheActiveSearch pins Refresh()'s search branch: reload()
// itself already has TestAllToggleWhileSearchingReRunsTheSameSearch as
// coverage, but nothing called Refresh() directly with m.Query set before
// this task.
func TestRefreshReRunsTheActiveSearch(t *testing.T) {
	fake := &data.FakeRunbook{ItemsByProject: map[string][]store.RunbookIndexRow{
		"acme": {sampleRunbook("RB-900", "acme", "Preview endpoint slow", false)},
	}}
	m := newModel(fake, nil).WithProject("acme")
	m.Query = "preview"

	cmd := m.Refresh()
	if cmd == nil {
		t.Fatal("Refresh with an active query should reload it")
	}
	run(t, cmd)
	if fake.LastSearch().Query != "preview" {
		t.Fatalf("LastSearch.Query = %q, want the active search re-issued", fake.LastSearch().Query)
	}
}

// TestRefreshReloadsTheMarkdownOnTheView pins Refresh()'s markdown branch,
// the one "r" from the Markdown view
// (TestOKeyOpensTheHubViaTheInjectableExecEditor and its siblings only ever
// drive handleViewKeys("r") through Update, never Refresh() itself, which
// the root also calls when this tab becomes active again while already on
// the view).
func TestRefreshReloadsTheMarkdownOnTheView(t *testing.T) {
	item := sampleRunbook("RB-003", "acme", "Preview endpoint slow", true)
	m := newModel(&data.FakeRunbook{}, nil).WithProject("acme")
	m.Screen = ScreenView
	m.Selected = &item

	cmd := m.Refresh()
	if cmd == nil {
		t.Fatal("Refresh on the markdown view should reload it")
	}
	msg, ok := run(t, cmd).(markdownLoadedMsg)
	if !ok {
		t.Fatalf("Refresh produced %T, want markdownLoadedMsg", run(t, cmd))
	}
	if msg.id != "RB-003" {
		t.Fatalf("markdownLoadedMsg.id = %q, want RB-003", msg.id)
	}
}
