package app

import (
	"testing"
	"time"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	tea "github.com/charmbracelet/bubbletea"
)

// TestTargetedMessageReachesOnlyItsOwner pins what the interface buys: a
// task list coming back no longer wakes Memory, Evidence, Runbooks and
// Cloud. Every tab's own load message names its tab, and the root's delivery
// leaves the other four exactly as they were.
func TestTargetedMessageReachesOnlyItsOwner(t *testing.T) {
	fake := &data.FakeTask{ItemsByProject: map[string][]store.TaskListItem{"acme": {
		{Task: store.Task{ID: 1, JiraKey: strp("ACME-1"), Title: "a task"}},
	}}}
	m := New(nil, nil, fake, nil, nil, "", theme.New(theme.KoiPond()), "acme")

	msg := m.tasks.Refresh()()
	targeted, ok := msg.(tabs.Targeted)
	if !ok {
		t.Fatalf("%T does not declare its owning tab", msg)
	}
	if targeted.TabOwner() != tabs.Tasks {
		t.Fatalf("a tasks message names %v as its owner", targeted.TabOwner())
	}

	updated, _ := m.Update(msg)
	next := updated.(Model)

	if len(next.tasks.Items) != 1 {
		t.Fatalf("the owning tab did not receive its own message: %+v", next.tasks.Items)
	}

	// Delivery is recorded against the owner alone. A broadcast would have
	// handed the message to all five and marked none of them, which is
	// exactly the difference this test is here to catch.
	now := time.Now()
	if next.freshness.stale(tabs.Tasks, now) {
		t.Fatal("the owning tab was not recorded as loaded")
	}
	for _, other := range []tabs.ID{tabs.Memory, tabs.Evidence, tabs.Runbooks, tabs.Settings} {
		if !next.freshness.stale(other, now) {
			t.Fatalf("%v was touched by a message it does not own", other)
		}
	}
}

// TestEveryTabsLoadMessagesNameTheirOwner walks each tab's own commands, so a
// message added without a TabOwner is caught here rather than by a screen
// that silently stops updating.
func TestEveryTabsLoadMessagesNameTheirOwner(t *testing.T) {
	m := New(
		&data.FakeMemory{},
		&data.FakeProject{},
		&data.FakeTask{},
		&data.FakeEvidence{},
		&data.FakeRunbook{},
		"", theme.New(theme.KoiPond()), "acme")

	for _, id := range []tabs.ID{tabs.Memory, tabs.Tasks, tabs.Evidence, tabs.Runbooks} {
		tab := m.tab(id)
		cmd := tab.Refresh()
		if cmd == nil {
			continue
		}
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, inner := range batch {
				assertOwnedBy(t, id, inner())
			}
			continue
		}
		assertOwnedBy(t, id, msg)
	}
}

func assertOwnedBy(t *testing.T, id tabs.ID, msg tea.Msg) {
	t.Helper()
	targeted, ok := msg.(tabs.Targeted)
	if !ok {
		t.Fatalf("%v issues %T, which names no owning tab", id, msg)
	}
	if targeted.TabOwner() != id {
		t.Fatalf("%v issues %T, which names %v as its owner", id, msg, targeted.TabOwner())
	}
}

// TestBroadcastStillReachesEveryTabForWindowSize pins the exception: the
// terminal's size is the one thing every tab must hear, active or not.
func TestBroadcastStillReachesEveryTabForWindowSize(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.KoiPond()), "acme")

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	next := updated.(Model)

	for _, got := range []struct {
		name          string
		width, height int
	}{
		{"memory", next.memory.Width, next.memory.Height},
		{"tasks", next.tasks.Width, next.tasks.Height},
		{"evidence", next.evidence.Width, next.evidence.Height},
		{"runbooks", next.runbooks.Width, next.runbooks.Height},
	} {
		if got.width != 120 || got.height != 40 {
			t.Fatalf("%s saw %dx%d, want the size every tab was told", got.name, got.width, got.height)
		}
	}
}

// TestActivateDoesNotRefreshAFreshTab pins the cost this change removes:
// flicking through tabs no longer re-runs every tab's queries.
func TestActivateDoesNotRefreshAFreshTab(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.KoiPond()), "acme")
	m.freshness = m.freshness.loaded(tabs.Tasks)

	if _, cmd := m.activate(tabs.Tasks); cmd != nil {
		t.Fatal("switching to a tab loaded a moment ago re-ran its queries")
	}
}

// TestActivateRefreshesAStaleTab pins the other half: a tab nobody has
// loaded, one whose TTL has run out, and one explicitly invalidated all
// reload.
func TestActivateRefreshesAStaleTab(t *testing.T) {
	fake := &data.FakeTask{ItemsByProject: map[string][]store.TaskListItem{"acme": {}}}

	t.Run("never loaded", func(t *testing.T) {
		m := New(nil, nil, fake, nil, nil, "", theme.New(theme.KoiPond()), "acme")
		if _, cmd := m.activate(tabs.Tasks); cmd == nil {
			t.Fatal("a tab that has never loaded was shown without loading")
		}
	})

	t.Run("ttl expired", func(t *testing.T) {
		m := New(nil, nil, fake, nil, nil, "", theme.New(theme.KoiPond()), "acme")
		m.freshness = m.freshness.loaded(tabs.Tasks)
		m.freshness.at[int(tabs.Tasks)] = time.Now().Add(-refreshTTL - time.Second)
		if _, cmd := m.activate(tabs.Tasks); cmd == nil {
			t.Fatal("a tab older than the TTL was shown without reloading")
		}
	})

	t.Run("invalidated", func(t *testing.T) {
		m := New(nil, nil, fake, nil, nil, "", theme.New(theme.KoiPond()), "acme")
		m.freshness = m.freshness.loaded(tabs.Tasks).invalidate(tabs.Tasks)
		if _, cmd := m.activate(tabs.Tasks); cmd == nil {
			t.Fatal("a tab marked out of date was shown without reloading")
		}
	})
}

// TestFreshnessIgnoresAnUnknownTab covers the guard that keeps a tab added to
// tabs without widening the array from indexing past its end.
func TestFreshnessIgnoresAnUnknownTab(t *testing.T) {
	var f tabFreshness
	unknown := tabs.ID(tabCount + 3)

	if !f.stale(unknown, time.Now()) {
		t.Fatal("an unknown tab should read as stale rather than as current")
	}
	if got := f.loaded(unknown).invalidate(unknown); got != f {
		t.Fatal("an unknown tab changed the record instead of being ignored")
	}
}
