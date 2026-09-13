package app

import (
	"strings"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/tui/tabs"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	tea "github.com/charmbracelet/bubbletea"
)

func questionMark() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")} }

// TestQuestionMarkOpensTheHelpOverlay pins rfc-tui.md §7.1: "?" opens the
// help overlay from a tab screen.
func TestQuestionMarkOpensTheHelpOverlay(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.active = tabs.Tasks

	m, cmd := step(t, m, questionMark())
	if !m.showHelp {
		t.Fatal("showHelp should be true after \"?\"")
	}
	if cmd != nil {
		t.Fatal("opening help should not produce a command")
	}
}

// TestHelpOverlayContentTracksTheActiveScreen pins rfc-tui.md §7.1: "?" abre
// la ayuda en cada pantalla y lista los atajos que **esa** pantalla
// declara, no una lista fija" — Tasks' list and Evidence's list advertise
// different keys, so the overlay must differ between them.
func TestHelpOverlayContentTracksTheActiveScreen(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.active = tabs.Tasks
	// New now opens the selector without a resolvable project (T-10.02);
	// this case's premise is being on a tab already.
	m.screen = screenTab
	m, _ = step(t, m, questionMark())
	tasksHelp := m.View()
	if !strings.Contains(tasksHelp, "state filter") {
		t.Fatalf("expected Tasks' own \"f\" binding in its help, got:\n%s", tasksHelp)
	}

	m, _ = step(t, m, questionMark()) // close
	m.active = tabs.Evidence
	m, _ = step(t, m, questionMark())
	evidenceHelp := m.View()
	if strings.Contains(evidenceHelp, "state filter") {
		t.Fatalf("Evidence's help should not carry Tasks' \"f\" binding, got:\n%s", evidenceHelp)
	}
	if !strings.Contains(evidenceHelp, "toggle attached") {
		t.Fatalf("expected Evidence's own \"a\" binding in its help, got:\n%s", evidenceHelp)
	}
}

// TestHelpOverlayIncludesTheGlobalBindings pins that the overlay shows the
// chrome-level keys (rfc-tui.md §7.1) alongside whatever the screen adds,
// not just the screen's own.
func TestHelpOverlayIncludesTheGlobalBindings(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.active = tabs.Tasks
	m, _ = step(t, m, questionMark())

	out := m.View()
	if !strings.Contains(out, "next tab") {
		t.Fatalf("expected the global Tab binding in the overlay, got:\n%s", out)
	}
	if !strings.Contains(out, "dashboard") {
		t.Fatalf("expected the global \"0\" binding in the overlay, got:\n%s", out)
	}
}

// TestQuestionMarkTogglesTheOverlayClosed pins that "?" is a toggle, not a
// one-way switch.
func TestQuestionMarkTogglesTheOverlayClosed(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")

	m, _ = step(t, m, questionMark())
	if !m.showHelp {
		t.Fatal("first \"?\" should open the overlay")
	}
	m, _ = step(t, m, questionMark())
	if m.showHelp {
		t.Fatal("second \"?\" should close the overlay")
	}
}

// TestEscAndQAlsoCloseTheHelpOverlay pins the other two documented ways out.
func TestEscAndQAlsoCloseTheHelpOverlay(t *testing.T) {
	for _, key := range []string{"esc", "q"} {
		t.Run(key, func(t *testing.T) {
			m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
			m, _ = step(t, m, questionMark())

			var msg tea.KeyMsg
			if key == "esc" {
				msg = tea.KeyMsg{Type: tea.KeyEsc}
			} else {
				msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}
			}
			m, _ = step(t, m, msg)
			if m.showHelp {
				t.Fatalf("%q should close the overlay", key)
			}
		})
	}
}

// TestOtherKeysAreSwallowedWhileHelpIsShowing pins that the screen
// underneath does not silently react to a key the user meant for reading
// help — e.g. "j" must not move a list's cursor behind the overlay.
func TestOtherKeysAreSwallowedWhileHelpIsShowing(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.active = tabs.Cloud
	m, _ = step(t, m, questionMark())

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.cloud.Cursor != 0 {
		t.Fatalf("cloud cursor = %d, want 0: a key while help is open must not reach the tab underneath", m.cloud.Cursor)
	}
	if !m.showHelp {
		t.Fatal("help should still be open")
	}
}

// TestCtrlCStillQuitsWhileHelpIsShowing pins that Ctrl+C is the one key that
// is never suspended, matching rfc-tui.md §7.1's textinput rule applied to
// the overlay too.
func TestCtrlCStillQuitsWhileHelpIsShowing(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m, _ = step(t, m, questionMark())

	if _, cmd := step(t, m, tea.KeyMsg{Type: tea.KeyCtrlC}); cmd == nil {
		t.Fatal("ctrl+c should quit even while help is showing")
	}
}

// TestHelpIsSuspendedWhileCapturingText pins rfc-tui.md §7.1's textinput
// suspension rule for "?" too: typing a literal "?" into a focused search
// box must not pop the overlay.
func TestHelpIsSuspendedWhileCapturingText(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.active = tabs.Tasks
	// New now opens the selector without a resolvable project (T-10.02);
	// this case's premise is being on the Tasks tab already, so that is set
	// explicitly rather than relied on as New's default.
	m.screen = screenTab
	m.tasks.Searching = true
	m.tasks.SearchInput.Focus()

	m, _ = step(t, m, questionMark())
	if m.showHelp {
		t.Fatal("\"?\" typed into the search box must not open help")
	}
	if got := m.tasks.SearchInput.Value(); got != "?" {
		t.Fatalf("tasks search input = %q, want the \"?\" to have reached it", got)
	}
}

// TestHelpOverlayOpensFromTheDashboardAndSelector pins that "?" is not
// tab-only: rfc-tui.md §5's S1 and S2 footers both advertise it.
func TestHelpOverlayOpensFromTheDashboardAndSelector(t *testing.T) {
	dash := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	dash.project = "nextcloud"
	dash.screen = screenDashboard
	dash, _ = step(t, dash, questionMark())
	if !dash.showHelp {
		t.Fatal("\"?\" should open help from the Dashboard")
	}

	sel := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	sel.screen = screenSelector
	sel, _ = step(t, sel, questionMark())
	if !sel.showHelp {
		t.Fatal("\"?\" should open help from the Selector")
	}
}
