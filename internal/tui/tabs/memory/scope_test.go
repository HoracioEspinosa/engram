package memory

import (
	"errors"
	"strings"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	tea "github.com/charmbracelet/bubbletea"
)

// scopedFixture is a Memory tab on a project, with a reader that records the
// scope every read was made at.
func scopedFixture(t *testing.T, screen Screen) (Model, *data.FakeMemory, *data.FakeSettings) {
	t.Helper()
	reader := &data.FakeMemory{
		Observations: []store.Observation{{ID: 1, Type: "note", Title: "a note"}},
		Sessions:     []store.SessionSummary{{ID: "session-1", Project: "clarodrive"}},
	}
	settings := &data.FakeSettings{}

	m := New(reader, "1.0.0-test").
		WithStyles(theme.New(theme.KoiPond())).
		WithProject("clarodrive").
		WithSettings(settings)
	m.Screen = screen
	return m, reader, settings
}

func TestAScopeReadsBackAsItWasWritten(t *testing.T) {
	for _, s := range []Scope{ScopeProject, ScopeSubtree, ScopeAll} {
		if got := ParseScope(s.String()); got != s {
			t.Errorf("ParseScope(%q) = %v, want %v", s.String(), got, s)
		}
	}
	// A value written by a newer build, or by hand, reads as everything:
	// too much memory is never wrong for the wrong reason.
	if got := ParseScope("a-width-from-a-newer-build"); got != ScopeAll {
		t.Errorf("an unknown scope read as %v, want everything", got)
	}
	if got := Scope(99).String(); got != "all" {
		t.Errorf("Scope(99).String() = %q, want the safe default", got)
	}
}

func TestTheTabOpensWorkspaceWide(t *testing.T) {
	m := New(nil, "")

	if m.Scope != ScopeAll {
		t.Fatalf("Scope = %v, want the whole workspace", m.Scope)
	}
	if m.Title() != "Memory" {
		t.Errorf("Title() = %q, want no scope in it", m.Title())
	}
}

// TestTheTitleCarriesTheScope pins why the title changes at all: a narrowed
// Memory tab that looked exactly like a workspace-wide one would have the
// reader concluding their observations had been lost.
func TestTheTitleCarriesTheScope(t *testing.T) {
	m, _, _ := scopedFixture(t, ScreenRecent)

	cases := map[Scope]string{
		ScopeProject: "Memory (project)",
		ScopeSubtree: "Memory (subtree)",
		ScopeAll:     "Memory",
	}
	for scope, want := range cases {
		if got := m.WithScope(scope).Title(); got != want {
			t.Errorf("Title() at %v = %q, want %q", scope, got, want)
		}
	}

	// Without a project there is nothing to narrow to, so the title says
	// nothing about a scope that cannot apply.
	unscoped := New(nil, "").WithScope(ScopeProject)
	if got := unscoped.Title(); got != "Memory" {
		t.Errorf("Title() with no project = %q, want plain", got)
	}
}

func TestACycleNarrowsRemembersAndReloads(t *testing.T) {
	m, reader, settings := scopedFixture(t, ScreenRecent)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	next := mustModel(t, updated)
	if next.Scope != ScopeProject {
		t.Fatalf("scope = %v, want it cycled to this project", next.Scope)
	}
	if cmd == nil {
		t.Fatal("cycling should remember the choice and reload")
	}

	for _, one := range batchOf(t, cmd) {
		tab, _ := next.Update(one())
		next = mustModel(t, tab)
	}

	if got := settings.Value(ScopeSettingKey); got != "project" {
		t.Errorf("settings hold %q, want the chosen width", got)
	}
	if got := reader.LastScope(); got.Project != "clarodrive" || got.Subtree {
		t.Errorf("the reader was asked at %+v, want this project alone", got)
	}
}

func TestTheCycleWalksAllThreeWidths(t *testing.T) {
	m, reader, _ := scopedFixture(t, ScreenRecent)

	want := []struct {
		scope   Scope
		project string
		subtree bool
	}{
		{ScopeProject, "clarodrive", false},
		{ScopeSubtree, "clarodrive", true},
		{ScopeAll, "", false},
	}

	for _, step := range want {
		updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
		m = mustModel(t, updated)
		if m.Scope != step.scope {
			t.Fatalf("scope = %v, want %v", m.Scope, step.scope)
		}
		for _, one := range batchOf(t, cmd) {
			tab, _ := m.Update(one())
			m = mustModel(t, tab)
		}
		if got := reader.LastScope(); got.Project != step.project || got.Subtree != step.subtree {
			t.Fatalf("at %v the reader was asked %+v, want project=%q subtree=%v",
				step.scope, got, step.project, step.subtree)
		}
	}
}

func TestACycleResetsThePageItWasOn(t *testing.T) {
	m, _, _ := scopedFixture(t, ScreenRecent)
	m.RecentOffset = 100

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	next := mustModel(t, updated)

	if next.RecentOffset != 0 {
		t.Fatalf("offset = %d, want the narrowed list to start at its first page", next.RecentOffset)
	}
}

func TestTheCycleReachesSearchResultsAndSessionsToo(t *testing.T) {
	for _, screen := range []Screen{ScreenSearchResults, ScreenSessions} {
		m, _, _ := scopedFixture(t, screen)
		updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
		next := mustModel(t, updated)

		if next.Scope != ScopeProject {
			t.Errorf("on %v the scope did not cycle", screen)
		}
		if cmd == nil {
			t.Errorf("on %v the cycle issued no reload", screen)
		}
	}
}

func TestACycleWithNoProjectIsInert(t *testing.T) {
	m := New(&data.FakeMemory{}, "").WithStyles(theme.New(theme.KoiPond()))
	m.Screen = ScreenRecent

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	next := mustModel(t, updated)

	if next.Scope != ScopeAll {
		t.Fatalf("scope = %v, want it left alone: there is nothing to narrow to", next.Scope)
	}
	if cmd != nil {
		t.Error("a cycle with nothing to narrow to should issue no reload")
	}
}

func TestAScopeThatCannotBeRememberedStillApplies(t *testing.T) {
	m, _, settings := scopedFixture(t, ScreenRecent)
	settings.SetErr(errors.New("database is locked"))

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	next := mustModel(t, updated)
	for _, one := range batchOf(t, cmd) {
		tab, _ := next.Update(one())
		next = mustModel(t, tab)
	}

	if next.Scope != ScopeProject {
		t.Fatal("the narrowing should hold: what failed is remembering it")
	}
}

func TestATabWithNoSettingsStoreStillCycles(t *testing.T) {
	reader := &data.FakeMemory{}
	m := New(reader, "").WithStyles(theme.New(theme.KoiPond())).WithProject("clarodrive")
	m.Screen = ScreenRecent

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	next := mustModel(t, updated)

	if next.Scope != ScopeProject {
		t.Fatal("a tab with nowhere to write the choice should still honour it")
	}
}

func TestTheHintNamesTheWidthItWouldMoveTo(t *testing.T) {
	m, _, _ := scopedFixture(t, ScreenRecent)

	found := false
	for _, b := range m.WithScope(ScopeSubtree).Help() {
		if b.Help().Key == "a" {
			found = true
			if !strings.Contains(b.Help().Desc, "subtree") {
				t.Errorf("the hint reads %q, want it to name the width in force", b.Help().Desc)
			}
		}
	}
	if !found {
		t.Fatal("the scope key is not advertised at all")
	}

	// With no project the key answers nothing, so it is not advertised.
	unscoped := New(nil, "")
	unscoped.Screen = ScreenRecent
	for _, b := range unscoped.Help() {
		if b.Help().Key == "a" && b.Enabled() {
			t.Fatal("a key that answers nothing should not be advertised")
		}
	}
}

func TestTheSaveMessageNamesItsOwner(t *testing.T) {
	if got := (scopeSavedMsg{}).TabOwner(); got != tabs.Memory {
		t.Errorf("scopeSavedMsg.TabOwner() = %v, want Memory", got)
	}
}

// mustModel unwraps an Update result.
func mustModel(t *testing.T, tab tabs.Tab) Model {
	t.Helper()
	m, ok := tab.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want memory.Model", tab)
	}
	return m
}

// batchOf unwraps a batch into the commands it holds, or the single command
// it was.
func batchOf(t *testing.T, cmd tea.Cmd) []tea.Cmd {
	t.Helper()
	if cmd == nil {
		return nil
	}
	if batch, ok := cmd().(tea.BatchMsg); ok {
		return batch
	}
	return []tea.Cmd{cmd}
}
