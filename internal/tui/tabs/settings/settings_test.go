package settings

import (
	"errors"
	"strings"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/shared"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	tea "github.com/charmbracelet/bubbletea"
)

func step(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	updated, _ := m.Update(msg)
	next, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want settings.Model", updated)
	}
	return next
}

func stepCmd(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(msg)
	next, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want settings.Model", updated)
	}
	return next, cmd
}

func press(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func built(t *testing.T) (Model, *data.FakeSettings) {
	t.Helper()
	store := &data.FakeSettings{Values: map[string]string{"tui.theme": "koi-pond"}}
	m := New().WithStyles(theme.New(theme.KoiPond())).WithSettings(store, store)
	return step(t, m, tea.WindowSizeMsg{Width: 120, Height: 40}), store
}

func TestTheTabIsTitledAndReadsNothingOfItsOwn(t *testing.T) {
	m := New()

	if m.Title() != "Settings" {
		t.Errorf("Title() = %q", m.Title())
	}
	if m.CapturingText() {
		t.Error("Settings has no text input")
	}
	if m.Init() != nil || m.Refresh() != nil {
		t.Error("every row reads its own source when drawn; there is nothing to load")
	}
}

func TestEveryRowIsOnScreen(t *testing.T) {
	m, _ := built(t)

	out := m.View()
	for _, want := range []string{"theme", "koi-pond", "icons", "unicode", "vault root", "evidence dir", "cloud", "doctor"} {
		if !strings.Contains(out, want) {
			t.Errorf("the settings list omits %q, got:\n%s", want, out)
		}
	}
}

// TestAConfiguredPathSaysWhetherItIsThere pins the one reading this screen
// exists for: a vault root pointing at a directory nobody created is the most
// common reason the workspace looks empty, and it is invisible until
// something says so.
func TestAConfiguredPathSaysWhetherItIsThere(t *testing.T) {
	m, _ := built(t)

	t.Run("present", func(t *testing.T) {
		dir := t.TempDir()
		if got := m.presence(dir, dirExists(dir)); !strings.Contains(got, "exists") {
			t.Fatalf("presence(%q) = %q, want it marked present", dir, got)
		}
	})

	t.Run("missing", func(t *testing.T) {
		gone := t.TempDir() + "/never-created"
		if got := m.presence(gone, dirExists(gone)); !strings.Contains(got, "missing") {
			t.Fatalf("presence(%q) = %q, want it marked missing", gone, got)
		}
	})

	t.Run("unset", func(t *testing.T) {
		if got := m.presence("", false); !strings.Contains(got, "not configured") {
			t.Fatalf("presence(\"\") = %q, want it marked unconfigured", got)
		}
	})
}

func TestTheVaultAndEvidenceRowsReadTheRealEnvironment(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(shared.VaultRootEnv, dir)
	t.Setenv(shared.EvidenceDirEnv, dir+"/never-created")

	m, _ := built(t)
	out := m.View()

	if !strings.Contains(out, "exists") {
		t.Errorf("a real vault root should be marked present, got:\n%s", out)
	}
	if !strings.Contains(out, "missing") {
		t.Errorf("an evidence dir that is not there should be marked missing, got:\n%s", out)
	}
}

func TestTheThemeRowAsksTheRootForThePicker(t *testing.T) {
	m, _ := built(t)
	m.Cursor = int(rowTheme)

	_, cmd := stepCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("the theme row should open the picker")
	}
	if _, ok := cmd().(OpenThemePickerMsg); !ok {
		t.Fatalf("the theme row produced %T, want OpenThemePickerMsg", cmd())
	}
}

func TestTheIconRowCyclesTheVocabularyAndRemembersIt(t *testing.T) {
	m, store := built(t)
	m.Cursor = int(rowIcons)

	want := []theme.IconMode{theme.IconModeNerd, theme.IconModeASCII, theme.IconModeUnicode}
	for _, mode := range want {
		var cmd tea.Cmd
		m, cmd = stepCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
		if m.IconMode != mode {
			t.Fatalf("icon mode = %q, want %q", m.IconMode, mode)
		}
		if cmd == nil {
			t.Fatal("cycling should remember the choice")
		}
		m = step(t, m, cmd())

		if got := store.Value(IconSettingKey); got != string(mode) {
			t.Fatalf("settings hold %q, want %q", got, mode)
		}
		if !strings.Contains(m.View(), string(mode)) {
			t.Errorf("the row should show the vocabulary in force, got:\n%s", m.View())
		}
	}
}

func TestTheIconRowRepaintsItsOwnGlyphs(t *testing.T) {
	m, _ := built(t)
	m.Cursor = int(rowIcons)

	m, _ = stepCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	if got := m.Styles().Icons.Mode(); got != theme.IconModeNerd {
		t.Fatalf("the tab still draws in %q after cycling to nerd", got)
	}
}

func TestACycleWithNowhereToWriteItDownSaysSo(t *testing.T) {
	m := New().WithStyles(theme.New(theme.KoiPond()))
	m.Cursor = int(rowIcons)

	m, cmd := stepCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = step(t, m, cmd())

	if !strings.Contains(m.View(), "could not remember") {
		t.Fatalf("a choice that cannot be saved should say so, got:\n%s", m.View())
	}
	if m.IconMode != theme.IconModeNerd {
		t.Error("the vocabulary should still change: what failed is remembering it")
	}
}

func TestASettingsStoreThatWillNotAnswerIsReported(t *testing.T) {
	store := &data.FakeSettings{}
	store.SetErr(errors.New("database is locked"))
	m := New().WithStyles(theme.New(theme.KoiPond())).WithSettings(store, store)
	m.Cursor = int(rowIcons)

	m, cmd := stepCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = step(t, m, cmd())

	if !strings.Contains(m.View(), "database is locked") {
		t.Fatalf("a failed write should say so, got:\n%s", m.View())
	}
}

func TestTheDoctorRowCountsWhatTheWorkspaceRemembers(t *testing.T) {
	t.Run("none", func(t *testing.T) {
		store := &data.FakeSettings{}
		m := New().WithStyles(theme.New(theme.KoiPond())).WithSettings(store, store)
		if got := m.doctorSummary(); !strings.Contains(got, "nothing remembered") {
			t.Fatalf("doctorSummary() = %q", got)
		}
	})

	t.Run("one", func(t *testing.T) {
		store := &data.FakeSettings{Values: map[string]string{"tui.theme": "koi-pond"}}
		m := New().WithStyles(theme.New(theme.KoiPond())).WithSettings(store, store)
		if got := m.doctorSummary(); got != "1 remembered setting" {
			t.Fatalf("doctorSummary() = %q", got)
		}
	})

	t.Run("several", func(t *testing.T) {
		store := &data.FakeSettings{Values: map[string]string{
			"tui.theme": "koi-pond", "tui.icons": "nerd", "tui.memory_scope": "subtree",
		}}
		m := New().WithStyles(theme.New(theme.KoiPond())).WithSettings(store, store)
		if got := m.doctorSummary(); got != "3 remembered settings" {
			t.Fatalf("doctorSummary() = %q", got)
		}
	})

	t.Run("unreadable", func(t *testing.T) {
		store := &data.FakeSettings{}
		store.SetErr(errors.New("database is locked"))
		m := New().WithStyles(theme.New(theme.KoiPond())).WithSettings(store, store)
		if got := m.doctorSummary(); !strings.Contains(got, "database is locked") {
			t.Fatalf("doctorSummary() = %q", got)
		}
	})

	t.Run("unbound", func(t *testing.T) {
		if got := New().doctorSummary(); !strings.Contains(got, "no settings store") {
			t.Fatalf("doctorSummary() = %q", got)
		}
	})
}

// TestTheCloudRowOpensWhatUsedToBeItsOwnTab pins the fold: the screen a
// reader knew is still the screen they find, one level in rather than one tab
// over.
func TestTheCloudRowOpensWhatUsedToBeItsOwnTab(t *testing.T) {
	m, _ := built(t)
	m.Cursor = int(rowCloud)

	m, _ = stepCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.Screen != ScreenCloud {
		t.Fatalf("screen = %v, want the cloud sub-screen", m.Screen)
	}
	out := m.View()
	for _, want := range []string{"Cloud sync settings", "Configure server", "Enroll projects"} {
		if !strings.Contains(out, want) {
			t.Errorf("the cloud sub-screen omits %q, got:\n%s", want, out)
		}
	}

	m = step(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.Screen != ScreenList || m.Cursor != int(rowCloud) {
		t.Fatalf("esc left screen=%v cursor=%d, want the list with the cursor back on cloud", m.Screen, m.Cursor)
	}
}

func TestBackInsideTheCloudSubScreenReturnsToTheList(t *testing.T) {
	m, _ := built(t)
	m.Cursor = int(rowCloud)
	m, _ = stepCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	for i := 0; i < len(cloudItems)-1; i++ {
		m = step(t, m, press("j"))
	}
	if m.Cursor != len(cloudItems)-1 {
		t.Fatalf("cursor = %d, want it on Back", m.Cursor)
	}

	m, cmd := stepCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Error("walking back out of a sub-screen is not a navigation")
	}
	if m.Screen != ScreenList {
		t.Fatal("Back should return to the settings list")
	}
}

func TestAnyOtherCloudItemIsInertForNow(t *testing.T) {
	m, _ := built(t)
	m.Cursor = int(rowCloud)
	m, _ = stepCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	m, cmd := stepCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || m.Screen != ScreenCloud {
		t.Fatal("the sync entry points have nothing behind them yet")
	}
}

func TestTheCursorMovesAndClampsAtBothEnds(t *testing.T) {
	m, _ := built(t)

	m = step(t, m, press("k"))
	if m.Cursor != 0 {
		t.Fatalf("cursor = %d past the top, want it clamped", m.Cursor)
	}

	m = step(t, m, press("G"))
	if m.Cursor != int(rowCount)-1 {
		t.Fatalf("cursor = %d, want the last row", m.Cursor)
	}
	m = step(t, m, press("j"))
	if m.Cursor != int(rowCount)-1 {
		t.Fatalf("cursor = %d past the end, want it clamped", m.Cursor)
	}

	m = step(t, m, press("g"))
	if m.Cursor != 0 {
		t.Fatalf("cursor = %d, want the first row", m.Cursor)
	}
}

func TestARowThatIsAReadingDoesNothing(t *testing.T) {
	m, _ := built(t)

	for _, r := range []row{rowVaultRoot, rowEvidenceDir, rowDoctor} {
		m.Cursor = int(r)
		next, cmd := stepCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
		if cmd != nil {
			t.Errorf("row %d is a reading, not an action", r)
		}
		if next.Screen != ScreenList {
			t.Errorf("row %d moved the screen", r)
		}
	}
}

func TestEscAndQGoHomeFromTheList(t *testing.T) {
	for _, key := range []tea.KeyMsg{{Type: tea.KeyEsc}, press("q")} {
		m, _ := built(t)
		_, cmd := stepCmd(t, m, key)
		if cmd == nil {
			t.Fatalf("%v should ask the root to go home", key)
		}
		msg, ok := cmd().(tabs.NavigateMsg)
		if !ok || msg.Target != tabs.Home {
			t.Fatalf("%v produced %#v, want a navigation to Home", key, cmd())
		}
	}
}

func TestTheHelpFollowsWhicheverScreenIsShowing(t *testing.T) {
	m, _ := built(t)

	list := m.Help()
	if len(list) == 0 {
		t.Fatal("the settings list declares no bindings")
	}

	m.Cursor = int(rowCloud)
	m, _ = stepCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if got := m.Help(); len(got) != 3 {
		t.Fatalf("the cloud sub-screen declares %d bindings, want three", len(got))
	}
}

func TestEveryMessageNamesItsOwner(t *testing.T) {
	for _, msg := range []tabs.Targeted{OpenThemePickerMsg{}, iconModeSavedMsg{}} {
		if got := msg.TabOwner(); got != tabs.Settings {
			t.Errorf("%T.TabOwner() = %v, want Settings", msg, got)
		}
	}
}

func TestAnUnknownKeyIsLeftAlone(t *testing.T) {
	m, _ := built(t)
	before := m.Cursor

	m, cmd := stepCmd(t, m, press("z"))
	if cmd != nil || m.Cursor != before {
		t.Error("an unbound key should change nothing")
	}
}
