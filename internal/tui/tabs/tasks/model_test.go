package tasks

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
	m := New(&data.FakeTask{})
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

// TestTitleIsTasks pins the tab bar label app.Model reads from every tab.
func TestTitleIsTasks(t *testing.T) {
	m := New(&data.FakeTask{})
	if got := m.Title(); got != "Tasks" {
		t.Fatalf("Title() = %q, want %q", got, "Tasks")
	}
}

// TestRefreshOnTheListReloadsTheFilteredList pins the "r" key's route on S3
// and app.Model's "activate this tab" call: with no task in Detail, Refresh
// falls through to reloading the list under the current filter.
func TestRefreshOnTheListReloadsTheFilteredList(t *testing.T) {
	fake := &data.FakeTask{ItemsByProject: map[string][]store.TaskListItem{
		"acme": {{Task: sampleTask(1, "ACME-1", "open")}},
	}}
	m := New(fake).WithProject("acme")

	cmd := m.Refresh()
	if cmd == nil {
		t.Fatal("Refresh on the list screen should reload it")
	}
	m, _ = step(t, m, run(t, cmd))
	if len(m.Items) != 1 {
		t.Fatalf("Items = %+v, want the reloaded task", m.Items)
	}
}

// TestRefreshOnTheDetailReloadsTheTask pins the same "r" key once a task is
// open on S4: it must reload that task's detail, not the list behind it.
func TestRefreshOnTheDetailReloadsTheTask(t *testing.T) {
	task := sampleTask(1, "ACME-1", "open")
	fake := &data.FakeTask{DetailByID: map[int64]data.TaskDetail{1: sampleDetail(task)}}
	m := New(fake).WithProject("acme")
	detail := sampleDetail(task)
	m.Detail = &detail
	m.Screen = ScreenDetail

	cmd := m.Refresh()
	if cmd == nil {
		t.Fatal("Refresh on the detail screen should reload the task")
	}
	msg, ok := run(t, cmd).(taskDetailLoadedMsg)
	if !ok {
		t.Fatalf("Refresh's command produced %T, want taskDetailLoadedMsg", run(t, cmd))
	}
	if msg.detail.Task.ID != 1 {
		t.Fatalf("reloaded task id = %d, want 1", msg.detail.Task.ID)
	}
}

// TestRefreshOnTheContextPackReloadsIt pins the "r" key on S5 (rfc-tui.md
// §3.1: "r rebuild"), which must rebuild the pack rather than the task
// detail or the list.
func TestRefreshOnTheContextPackReloadsIt(t *testing.T) {
	task := sampleTask(1, "ACME-1", "open")
	fake := &data.FakeTask{ContextPackByID: map[int64]string{1: "# pack v2"}}
	m := New(fake).WithProject("acme")
	detail := sampleDetail(task)
	m.Detail = &detail
	m.Screen = ScreenContextPack

	cmd := m.Refresh()
	if cmd == nil {
		t.Fatal("Refresh on the context pack screen should rebuild it")
	}
	m, _ = step(t, m, run(t, cmd))
	if m.ContextPack != "# pack v2" {
		t.Fatalf("ContextPack = %q, want the rebuilt pack", m.ContextPack)
	}
}

// TestRefreshOnTheDetailWithNoDetailFallsBackToTheList pins Refresh's
// defensive branch: ScreenDetail/ScreenContextPack with a nil Detail (the
// screen transitioned but the load has not landed yet) must not panic on
// m.Detail.Task.ID, falling back to the list load like the zero-value
// Screen does.
func TestRefreshOnTheDetailWithNoDetailFallsBackToTheList(t *testing.T) {
	fake := &data.FakeTask{ItemsByProject: map[string][]store.TaskListItem{
		"acme": {{Task: sampleTask(1, "ACME-1", "open")}},
	}}
	m := New(fake).WithProject("acme")
	m.Screen = ScreenDetail

	cmd := m.Refresh()
	if cmd == nil {
		t.Fatal("Refresh with no Detail yet should still return a command")
	}
	m, _ = step(t, m, run(t, cmd))
	if len(m.Items) != 1 {
		t.Fatalf("Items = %+v, want the list load Refresh fell back to", m.Items)
	}
}
