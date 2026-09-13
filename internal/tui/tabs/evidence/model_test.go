package evidence

import (
	"testing"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"
)

// TestWithStylesReturnsACopyPaintedWithTheGivenStyles pins app.New's own use
// of this seam: it repaints the tab after New built it with theme.Default(),
// without mutating the receiver (WithProject and the other With* methods on
// this Model follow the same copy-on-write shape).
func TestWithStylesReturnsACopyPaintedWithTheGivenStyles(t *testing.T) {
	m := New(&data.FakeEvidence{})
	custom := theme.New(theme.Elephant())

	repainted := m.WithStyles(custom)
	// lipgloss.Style is not comparable with ==, and its Render output is not
	// a reliable proxy either (go test's non-tty stdout can make two
	// differently-configured styles render identical plain text). Palette is
	// plain data (all lipgloss.Color/string fields), so compare that instead
	// — it is still "the styles passed in", not the New default.
	if got, want := repainted.Styles().Palette.Name, custom.Palette.Name; got != want {
		t.Fatalf("Styles().Palette.Name = %q, want %q (the styles passed in)", got, want)
	}
	if def := theme.Default().Palette.Name; repainted.Styles().Palette.Name == def {
		t.Fatal("Styles() still carries the New default's palette; WithStyles did not take effect")
	}
}

// TestTitleIsEvidence pins the tab bar label app.Model reads from every tab.
func TestTitleIsEvidence(t *testing.T) {
	m := New(&data.FakeEvidence{})
	if got := m.Title(); got != "Evidence" {
		t.Fatalf("Title() = %q, want %q", got, "Evidence")
	}
}

// TestRefreshOnTheListReloadsTheFilteredList pins the "r" key's route on S6
// and app.Model's "activate this tab" call: with no row selected, Refresh
// reloads the list under the current filter rather than trying a manifest
// read it has nothing to target.
func TestRefreshOnTheListReloadsTheFilteredList(t *testing.T) {
	fake := &data.FakeEvidence{ItemsByProject: map[string][]store.EvidenceListItem{
		"acme": {sampleItem(1, 9, "ACME-9", "ACME-9/a.png", false)},
	}}
	m := New(fake).WithProject("acme")

	cmd := m.Refresh()
	if cmd == nil {
		t.Fatal("Refresh on the list screen should reload it")
	}
	m, _ = step(t, m, run(t, cmd))
	if len(m.Items) != 1 {
		t.Fatalf("Items = %+v, want the reloaded row", m.Items)
	}
}

// TestRefreshOnTheDetailReloadsTheSelectedRowsManifest pins the same "r" key
// once a row is selected on S7 (rfc-tui.md §3.1): it must reload the
// manifest for the row on screen, not the list behind it.
func TestRefreshOnTheDetailReloadsTheSelectedRowsManifest(t *testing.T) {
	item := sampleItem(1, 9, "ACME-9", "ACME-9/a.png", false)
	m := detailModel(t, item)

	cmd := m.Refresh()
	if cmd == nil {
		t.Fatal("Refresh on the detail screen should reload the manifest")
	}
	msg, ok := run(t, cmd).(manifestLoadedMsg)
	if !ok {
		t.Fatalf("Refresh's command produced %T, want manifestLoadedMsg", run(t, cmd))
	}
	if msg.evidenceID != item.ID {
		t.Fatalf("manifestLoadedMsg.evidenceID = %d, want the selected row's id %d", msg.evidenceID, item.ID)
	}
}

// TestOpenForTaskScopesTheListToTheGivenTaskAndReloads pins the deep link
// rfc-tui.md §3.1 S4's "e" key drives via tabs.NavigateMsg.TaskID: the root
// calls this instead of replaying whatever filter S6 had before.
func TestOpenForTaskScopesTheListToTheGivenTaskAndReloads(t *testing.T) {
	fake := &data.FakeEvidence{ItemsByProject: map[string][]store.EvidenceListItem{
		"acme": {
			sampleItem(1, 9, "ACME-9", "ACME-9/a.png", false),
			sampleItem(2, 10, "ACME-10", "ACME-10/b.png", false),
		},
	}}
	m := New(fake).WithProject("acme")

	cmd := m.OpenForTask(9)
	if cmd == nil {
		t.Fatal("OpenForTask should return a non-nil command")
	}
	m, _ = step(t, m, run(t, cmd))
	if fake.LastFilter.TaskID != 9 {
		t.Fatalf("LastFilter.TaskID = %d, want 9", fake.LastFilter.TaskID)
	}
	if len(m.Items) != 1 || m.Items[0].TaskID != 9 {
		t.Fatalf("Items = %+v, want only task 9's row", m.Items)
	}
}
