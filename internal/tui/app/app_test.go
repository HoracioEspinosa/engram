package app

import (
	"strings"
	"testing"

	"github.com/Gentleman-Programming/engram/internal/store"
	"github.com/Gentleman-Programming/engram/internal/tui/data"
	"github.com/Gentleman-Programming/engram/internal/tui/shared"
	"github.com/Gentleman-Programming/engram/internal/tui/tabs"
	"github.com/Gentleman-Programming/engram/internal/tui/tabs/memory"
	"github.com/Gentleman-Programming/engram/internal/tui/theme"

	tea "github.com/charmbracelet/bubbletea"
)

// step feeds a message to the root and returns the new root.
func step(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()

	updated, cmd := m.Update(msg)
	next, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want app.Model", updated)
	}
	return next, cmd
}

func TestNewStartsOnTheMemoryTab(t *testing.T) {
	m := New(nil, nil, "1.0.0-test", theme.New(theme.CatppuccinMocha()), "")

	if m.active != tabs.Memory {
		t.Fatalf("active tab = %v, want %v", m.active, tabs.Memory)
	}
	if m.memory.Screen != memory.ScreenDashboard {
		t.Fatalf("memory screen = %v, want the dashboard", m.memory.Screen)
	}
	if m.version != "1.0.0-test" {
		t.Fatalf("version = %q", m.version)
	}
}

func TestNewWiresTheMemoryReaderIntoTheMemoryTab(t *testing.T) {
	reader := &data.FakeMemory{StatsResult: &store.Stats{TotalSessions: 3, TotalObservations: 7}}
	m := New(reader, nil, "", theme.New(theme.CatppuccinMocha()), "")

	// The dashboard reload runs against whichever reader the tab was built
	// with; a tab wired to anything but the reader New received would either
	// report different counters or, bound to a nil store, panic here.
	cmd := m.memory.Refresh()
	if cmd == nil {
		t.Fatal("the memory dashboard should reload its counters")
	}
	m, _ = step(t, m, cmd())

	if m.memory.Stats == nil || m.memory.Stats.TotalSessions != 3 || m.memory.Stats.TotalObservations != 7 {
		t.Fatalf("memory stats = %+v, want the counters served by the reader New was given", m.memory.Stats)
	}
}

func TestInitLoadsEveryTab(t *testing.T) {
	if cmd := New(nil, nil, "", theme.New(theme.CatppuccinMocha()), "").Init(); cmd == nil {
		t.Fatal("Init should return the startup batch")
	}
}

func TestCtrlCQuitsFromAnyTab(t *testing.T) {
	for _, active := range registered {
		m := New(nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
		m.active = active

		if _, cmd := step(t, m, tea.KeyMsg{Type: tea.KeyCtrlC}); cmd == nil {
			t.Fatalf("ctrl+c on the %v tab should return the quit command", active)
		}
	}
}

func TestCtrlCQuitsEvenWithTheSearchInputFocused(t *testing.T) {
	m := New(nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.memory.Screen = memory.ScreenSearch
	m.memory.SearchInput.Focus()

	if _, cmd := step(t, m, tea.KeyMsg{Type: tea.KeyCtrlC}); cmd == nil {
		t.Fatal("ctrl+c should quit before a focused text input sees the key")
	}
}

func TestWindowSizeReachesEveryTab(t *testing.T) {
	m, cmd := step(t, New(nil, nil, "", theme.New(theme.CatppuccinMocha()), ""), tea.WindowSizeMsg{Width: 120, Height: 40})

	if cmd != nil {
		t.Fatal("a window resize should not produce a command")
	}
	if m.width != 120 || m.height != 40 {
		t.Fatalf("root size = %dx%d, want 120x40", m.width, m.height)
	}
	if m.memory.Width != 120 || m.memory.Height != 40 {
		t.Fatalf("memory size = %dx%d, want 120x40", m.memory.Width, m.memory.Height)
	}
}

func TestKeysReachOnlyTheActiveTab(t *testing.T) {
	m := New(nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.active = tabs.Cloud

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyDown})

	if m.cloud.Cursor != 1 {
		t.Fatalf("cloud cursor = %d, want the key to have moved it", m.cloud.Cursor)
	}
	if m.memory.Cursor != 0 {
		t.Fatalf("memory cursor = %d, want an inactive tab to be untouched", m.memory.Cursor)
	}
}

func TestBroadcastReachesAnInactiveTab(t *testing.T) {
	m := New(nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.memory.CopyFeedback = "✓ Copied!"
	m.active = tabs.Cloud

	m, _ = step(t, m, shared.ClearFeedbackMsg{})

	if m.memory.CopyFeedback != "" {
		t.Fatalf("copy feedback = %q, want a command that finished off-tab to still reach its owner", m.memory.CopyFeedback)
	}
}

func TestNavigateSwitchesTabsAndRefreshesTheTarget(t *testing.T) {
	m := New(nil, nil, "", theme.New(theme.CatppuccinMocha()), "")

	m, cmd := step(t, m, tabs.NavigateMsg{Target: tabs.Cloud})
	if m.active != tabs.Cloud {
		t.Fatalf("active tab = %v, want %v", m.active, tabs.Cloud)
	}
	if cmd != nil {
		t.Fatal("the cloud menu has nothing to reload")
	}

	m, cmd = step(t, m, tabs.NavigateMsg{Target: tabs.Memory})
	if m.active != tabs.Memory {
		t.Fatalf("active tab = %v, want %v", m.active, tabs.Memory)
	}
	if cmd == nil {
		t.Fatal("returning to the memory dashboard should reload its counters")
	}
}

func TestNavigateToAnUnimplementedTabIsANoOp(t *testing.T) {
	for _, target := range []tabs.ID{tabs.Tasks, tabs.Evidence, tabs.Runbooks, tabs.ID(99)} {
		m, cmd := step(t, New(nil, nil, "", theme.New(theme.CatppuccinMocha()), ""), tabs.NavigateMsg{Target: target})

		if m.active != tabs.Memory {
			t.Errorf("navigating to %v moved the workspace to %v", target, m.active)
		}
		if cmd != nil {
			t.Errorf("navigating to %v should not produce a command", target)
		}
	}
}

func TestCloudRoundTripFromTheDashboard(t *testing.T) {
	m := New(nil, nil, "", theme.New(theme.CatppuccinMocha()), "")

	// Walk the dashboard menu down to "Cloud sync settings".
	for i := 0; i < 4; i++ {
		m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyDown})
	}
	if m.memory.Cursor != 4 {
		t.Fatalf("dashboard cursor = %d, want the cloud entry", m.memory.Cursor)
	}

	m, cmd := step(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("selecting the cloud entry should ask the root to switch tabs")
	}
	m, _ = step(t, m, cmd())
	if m.active != tabs.Cloud {
		t.Fatalf("active tab = %v, want %v", m.active, tabs.Cloud)
	}
	if !strings.Contains(m.View(), "Cloud sync settings") {
		t.Fatal("the cloud tab should be on screen")
	}

	m, cmd = step(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("leaving the cloud tab should ask the root to switch back")
	}
	m, _ = step(t, m, cmd())
	if m.active != tabs.Memory {
		t.Fatalf("active tab = %v, want %v", m.active, tabs.Memory)
	}
	if m.memory.Cursor != 0 {
		t.Fatalf("dashboard cursor = %d, want it rewound to the first entry", m.memory.Cursor)
	}
	if m.cloud.Cursor != 0 {
		t.Fatalf("cloud cursor = %d, want it rewound for the next visit", m.cloud.Cursor)
	}
	if !strings.Contains(m.View(), "Actions") {
		t.Fatal("the memory dashboard should be back on screen")
	}
}

func TestViewWrapsTheActiveTabInTheApplicationFrame(t *testing.T) {
	m := New(nil, nil, "", theme.New(theme.CatppuccinMocha()), "")

	framed := m.View()
	body := m.memory.View()
	if framed == body {
		t.Fatal("the frame should pad the tab body, not return it unchanged")
	}
	assertFramedBody(t, framed, body)

	m.active = tabs.Cloud
	assertFramedBody(t, m.View(), m.cloud.View())
	if strings.Contains(m.View(), "Actions") {
		t.Fatal("switching tabs should switch the framed body")
	}
}

// assertFramedBody checks that every line of body survives into framed. The
// frame indents and pads, so the comparison is per trimmed line rather than a
// substring match on the whole body.
func assertFramedBody(t *testing.T, framed, body string) {
	t.Helper()

	framedLines := map[string]bool{}
	for _, line := range strings.Split(framed, "\n") {
		framedLines[strings.TrimSpace(line)] = true
	}
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if !framedLines[trimmed] {
			t.Fatalf("the frame dropped the tab body line %q", trimmed)
		}
	}
}

func TestViewSurvivesAnUnregisteredActiveTab(t *testing.T) {
	m := New(nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.active = tabs.ID(99)

	if !strings.Contains(m.View(), "Unknown tab") {
		t.Fatal("an unregistered active tab should render a fallback, not panic")
	}
}

func TestUpdateWithAnUnregisteredActiveTabIsANoOp(t *testing.T) {
	m := New(nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.active = tabs.ID(99)

	m, cmd := step(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if cmd != nil {
		t.Fatal("a key with no active tab should not produce a command")
	}
	if m.memory.Cursor != 0 {
		t.Fatal("a key with no active tab should not reach another tab")
	}
}

func TestRegisteredTabsAreDistinctAndTitled(t *testing.T) {
	m := New(nil, nil, "", theme.New(theme.CatppuccinMocha()), "")

	seen := map[string]bool{}
	for _, id := range registered {
		tab := m.tab(id)
		if tab == nil {
			t.Fatalf("tab %v is registered but has no sub-model", id)
		}
		title := tab.Title()
		if title == "" {
			t.Fatalf("tab %v has no title", id)
		}
		if seen[title] {
			t.Fatalf("two registered tabs share the title %q", title)
		}
		seen[title] = true
	}
}

func TestWithTabIgnoresAMismatchedSubModel(t *testing.T) {
	m := New(nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.memory.Cursor = 3

	// Storing the cloud sub-model under the memory id must be refused rather
	// than silently corrupting the root.
	updated := m.withTab(tabs.Memory, m.cloud)

	if updated.memory.Cursor != 3 {
		t.Fatalf("memory cursor = %d, want the original sub-model preserved", updated.memory.Cursor)
	}
}
