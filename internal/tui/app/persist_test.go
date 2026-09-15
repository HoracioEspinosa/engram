package app

import (
	"testing"

	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs/memory"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	tea "github.com/charmbracelet/bubbletea"
)

// rememberingWorkspace is a workspace bound to a settings writer that records
// every remembered choice.
func rememberingWorkspace(t *testing.T, project string) (Model, *data.FakeSettings) {
	t.Helper()

	settings := &data.FakeSettings{}
	// Real fakes rather than nil readers: drain runs every command the switch
	// batched, and a tab's own reload is one of them.
	m := New(&data.FakeMemory{}, &data.FakeProject{}, &data.FakeTask{}, &data.FakeEvidence{}, &data.FakeRunbook{},
		"test", theme.New(theme.KoiPond()), project).
		WithThemePicker(nil, settings)
	return m, settings
}

// drain runs a command and every command it batches, so a setting written off
// the frame is actually written by the time the test looks.
func drain(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, next := range msg {
			drain(next)
		}
	}
}

func rememberedValue(t *testing.T, settings *data.FakeSettings, key string) string {
	t.Helper()
	value, ok, err := settings.Setting(key)
	if err != nil {
		t.Fatalf("read %s: %v", key, err)
	}
	if !ok {
		return ""
	}
	return value
}

// TestRootPersistsLastTabOnSwitch is the whole point of the setting: the tab
// the user was on is written as they leave for it, so the next run opens
// there.
func TestRootPersistsLastTabOnSwitch(t *testing.T) {
	m, settings := rememberingWorkspace(t, "acme")

	next, cmd := m.activate(tabs.Runbooks)
	if got := next.(Model).active; got != tabs.Runbooks {
		t.Fatalf("active = %v, want runbooks", got)
	}
	drain(cmd)

	if got := rememberedValue(t, settings, lastTabSettingKey); got != tabs.Runbooks.String() {
		t.Fatalf("%s = %q, want %q", lastTabSettingKey, got, tabs.Runbooks.String())
	}
}

// TestADeepLinkIsRememberedToo: the three cross-tab deep links move the active
// tab without going through activate(), and used to be the one way of changing
// tabs the workspace forgot.
func TestADeepLinkIsRememberedToo(t *testing.T) {
	m, settings := rememberingWorkspace(t, "acme")

	_, cmd := m.Update(tabs.NavigateMsg{Target: tabs.Evidence, TaskID: 7})
	drain(cmd)

	if got := rememberedValue(t, settings, lastTabSettingKey); got != tabs.Evidence.String() {
		t.Fatalf("%s = %q, want %q", lastTabSettingKey, got, tabs.Evidence.String())
	}
}

// TestPickingAProjectRemembersIt: the tree overlay is where a project is
// chosen, so it is where the choice is recorded.
func TestPickingAProjectRemembersIt(t *testing.T) {
	m, settings := rememberingWorkspace(t, "")
	m.tree.open = true
	m.tree = m.tree.applyLoaded(treeLoadedMsg{roots: []data.ProjectNode{
		{Slug: "clarodrive", DisplayName: "ClaroDrive"},
	}})

	handled, next, cmd := m.updateProjectTree(tea.KeyMsg{Type: tea.KeyEnter})
	if !handled {
		t.Fatal("the open tree overlay did not answer enter")
	}
	if got := next.(Model).project; got != "clarodrive" {
		t.Fatalf("project = %q, want clarodrive", got)
	}
	drain(cmd)

	if got := rememberedValue(t, settings, lastProjectSettingKey); got != "clarodrive" {
		t.Fatalf("%s = %q, want clarodrive", lastProjectSettingKey, got)
	}
}

// TestRememberingIsNeverSynchronous: SetSetting goes to SQLite, so a write in
// Update would stall the frame behind a busy disk every time the user pressed
// Tab. The command is what defers it, and this is the test that fails if
// somebody inlines the write.
func TestRememberingIsNeverSynchronous(t *testing.T) {
	m, settings := rememberingWorkspace(t, "acme")

	_, cmd := m.activate(tabs.Tasks)
	if got := len(settings.SetCalls()); got != 0 {
		t.Fatalf("Update wrote %d settings before its command ran", got)
	}
	drain(cmd)
	if got := len(settings.SetCalls()); got != 1 {
		t.Fatalf("the command wrote %d settings, want 1", got)
	}
}

// TestAWorkspaceWithNoSettingsWriterStillSwitchesTabs: the writer is optional
// wiring, and a workspace built without one — every tab's own package test —
// must not fail to navigate over it.
func TestAWorkspaceWithNoSettingsWriterStillSwitchesTabs(t *testing.T) {
	m := New(&data.FakeMemory{}, &data.FakeProject{}, &data.FakeTask{}, &data.FakeEvidence{}, &data.FakeRunbook{},
		"test", theme.New(theme.KoiPond()), "acme")

	next, cmd := m.activate(tabs.Tasks)
	if got := next.(Model).active; got != tabs.Tasks {
		t.Fatalf("active = %v, want tasks", got)
	}
	drain(cmd)
}

// TestWithLastTabReopensOnARegisteredTab covers the three answers: a tab this
// build draws, a name it does not know, and nothing remembered at all.
func TestWithLastTabReopensOnARegisteredTab(t *testing.T) {
	base := New(nil, nil, nil, nil, nil, "test", theme.New(theme.KoiPond()), "acme")

	cases := []struct {
		name       string
		remembered string
		want       tabs.ID
	}{
		{name: "a registered tab", remembered: tabs.Evidence.String(), want: tabs.Evidence},
		{name: "a name this build has no tab for", remembered: "cloud", want: base.active},
		{name: "nothing remembered", remembered: "", want: base.active},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := base.WithLastTab(tc.remembered)
			if got.active != tc.want {
				t.Fatalf("active = %v, want %v", got.active, tc.want)
			}
		})
	}
}

// TestWithIconsRepaintsEveryTab: the icon vocabulary fans out the same way a
// palette does, because a tab that missed the fan-out draws glyphs the
// terminal cannot render and reads as a broken font.
func TestWithIconsRepaintsEveryTab(t *testing.T) {
	base := New(nil, nil, nil, nil, nil, "test", theme.New(theme.KoiPond()), "acme")
	ascii := base.WithIcons(theme.IconModeASCII)

	if got := ascii.styles.Icons.Mode(); got != theme.IconModeASCII {
		t.Fatalf("the root draws in %v, want ascii", got)
	}

	// The cursor marker is the glyph every list screen draws, and it differs
	// between the two vocabularies, so a tab that kept its old style set
	// renders the same body it did before.
	for _, id := range registered {
		before, after := base.tab(id), ascii.tab(id)
		if before == nil || after == nil {
			continue
		}
		if before.View() == after.View() {
			continue
		}
		return
	}
	t.Fatalf("no registered tab changed its rendering when the icon vocabulary did")
}

// TestTheSettingsStoreReachesTheMemoryScope: the root's one binding for "what
// the workspace writes down about itself" has to reach every row that writes
// something, and Memory's own width is one of them.
//
// A binding that stopped at the Settings tab left "a" cycling a width that
// never outlived the session, in a tab whose own title reports which width it
// is reading — so the workspace opened wide on every start and looked as if
// the choice had never been made.
func TestTheSettingsStoreReachesTheMemoryScope(t *testing.T) {
	remembered := &data.FakeSettings{}
	m := New(&data.FakeMemory{}, &data.FakeProject{}, &data.FakeTask{}, &data.FakeEvidence{}, &data.FakeRunbook{},
		"test", theme.New(theme.KoiPond()), "acme").
		WithSettingsStore(remembered, remembered)
	m.memory.Screen = memory.ScreenRecent

	_, cmd := m.memory.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	drain(cmd)

	if got := rememberedValue(t, remembered, memory.ScopeSettingKey); got != memory.ScopeProject.String() {
		t.Fatalf("settings hold %q for %s, want the width the key chose",
			got, memory.ScopeSettingKey)
	}
}

// TestWithMemoryScopeOpensTheTabWhereItWasLeft is the read side of the same
// setting: the root is told the remembered width and opens the tab at it.
func TestWithMemoryScopeOpensTheTabWhereItWasLeft(t *testing.T) {
	m := New(&data.FakeMemory{}, nil, nil, nil, nil, "test", theme.New(theme.KoiPond()), "acme").
		WithMemoryScope(memory.ScopeSubtree)

	if got := m.memory.Scope; got != memory.ScopeSubtree {
		t.Fatalf("Memory opened at %v, want the width it was told", got)
	}
	if got := m.memory.Title(); got != "Memory (subtree)" {
		t.Fatalf("the tab bar reads %q, want the width in it", got)
	}
}
