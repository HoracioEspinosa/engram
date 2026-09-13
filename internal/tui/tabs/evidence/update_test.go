package evidence

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/shared"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs"

	tea "github.com/charmbracelet/bubbletea"
)

func strp(v string) *string { return &v }

// step feeds msg to m's Update and casts the result back to Model, the same
// idiom tabs/tasks's tests use for a leaf model.
func step(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(msg)
	next, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want evidence.Model", updated)
	}
	return next, cmd
}

// run executes cmd the way the Bubble Tea runtime would, turning a panic
// inside it into a test failure instead of a crashed test binary.
func run(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("run: nil command")
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("command panicked: %v", r)
		}
	}()
	return cmd()
}

func sampleItem(id, taskID int64, jiraKey, path string, attached bool) store.EvidenceListItem {
	return store.EvidenceListItem{
		Evidence: store.Evidence{
			ID: id, SyncID: "evd-" + path, Project: "acme", TaskID: taskID,
			TaskSyncID: "task-sync", Path: path, SHA256: "abc123",
			Kind: "png", Proves: "it works", CapturedAt: "2026-08-23 21:39:14",
			AttachedJira: attached,
		},
		JiraKey: strp(jiraKey),
	}
}

func withHeight(m Model, h int) Model {
	m.Height = h
	m.Width = 120
	return m
}

// ─── List (S6) ───────────────────────────────────────────────────────────────

func TestNewStartsOnTheListScreenWithNoProject(t *testing.T) {
	m := New(&data.FakeEvidence{})
	if m.Screen != ScreenList {
		t.Fatalf("Screen = %v, want ScreenList", m.Screen)
	}
	if cmd := m.Init(); cmd != nil {
		t.Fatal("Init without a project should have nothing to load")
	}
}

func TestInitLoadsEvidenceWhenAProjectIsAlreadyActive(t *testing.T) {
	fake := &data.FakeEvidence{ItemsByProject: map[string][]store.EvidenceListItem{
		"acme": {sampleItem(1, 9, "ACME-9", "ACME-9/a.png", false)},
	}}
	m := New(fake).WithProject("acme")

	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init with an active project should load its evidence")
	}
	m, _ = step(t, m, run(t, cmd))
	if len(m.Items) != 1 {
		t.Fatalf("Items = %+v, want the one seeded row", m.Items)
	}
}

func TestListCursorMovesAndClampsScroll(t *testing.T) {
	fake := &data.FakeEvidence{ItemsByProject: map[string][]store.EvidenceListItem{
		"acme": {
			sampleItem(1, 9, "ACME-9", "ACME-9/a.png", false),
			sampleItem(2, 9, "ACME-9", "ACME-9/b.png", false),
			sampleItem(3, 9, "ACME-9", "ACME-9/c.png", false),
		},
	}}
	m := withHeight(New(fake).WithProject("acme"), 20)
	m, _ = step(t, m, run(t, m.Init()))

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.Cursor != 1 {
		t.Fatalf("Cursor = %d, want 1 after moving down", m.Cursor)
	}
	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	if m.Cursor != 2 {
		t.Fatalf("Cursor = %d, want the last row after G", m.Cursor)
	}
	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	if m.Cursor != 0 {
		t.Fatalf("Cursor = %d, want the first row after g", m.Cursor)
	}
}

func TestEnterOpensDetailAndLoadsTheManifest(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(shared.EvidenceDirEnv, dir)
	fake := &data.FakeEvidence{ItemsByProject: map[string][]store.EvidenceListItem{
		"acme": {sampleItem(1, 9, "ACME-9", "ACME-9/a.png", false)},
	}}
	m := New(fake).WithProject("acme")
	m, _ = step(t, m, run(t, m.Init()))

	m, cmd := step(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter should load the manifest for the selected row")
	}
	if m.Screen != ScreenDetail {
		t.Fatalf("Screen = %v, want ScreenDetail", m.Screen)
	}
	if m.Selected == nil || m.Selected.ID != 1 {
		t.Fatalf("Selected = %+v, want the row under the cursor", m.Selected)
	}

	m, _ = step(t, m, run(t, cmd))
	if !m.ManifestChecked {
		t.Fatal("the manifest load should mark itself checked")
	}
	if m.ManifestExists {
		t.Fatal("no manifest.json was written for this fixture; ManifestExists should be false")
	}
}

func TestCopyInTheListCopiesTheAbsolutePath(t *testing.T) {
	t.Setenv(shared.EvidenceDirEnv, "/evidence-root")
	fake := &data.FakeEvidence{ItemsByProject: map[string][]store.EvidenceListItem{
		"acme": {sampleItem(1, 9, "ACME-9", "ACME-9/a.png", false)},
	}}
	m := New(fake).WithProject("acme")
	m, _ = step(t, m, run(t, m.Init()))

	_, cmd := m.handleListKeys("c")
	msg, ok := run(t, cmd).(shared.CopiedMsg)
	if !ok {
		t.Fatalf("c produced %T, want shared.CopiedMsg", run(t, cmd))
	}
	want := shared.OSC52Sequence(filepath.Join("/evidence-root", "ACME-9/a.png"))
	if msg.Sequence != want {
		t.Fatalf("copied sequence = %q, want %q", msg.Sequence, want)
	}
}

func TestOpenInTheListUsesTheInjectableOpener(t *testing.T) {
	t.Setenv(shared.EvidenceDirEnv, "/evidence-root")
	fake := &data.FakeEvidence{ItemsByProject: map[string][]store.EvidenceListItem{
		"acme": {sampleItem(1, 9, "ACME-9", "ACME-9/a.png", false)},
	}}
	m := New(fake).WithProject("acme")
	m, _ = step(t, m, run(t, m.Init()))

	var gotPath string
	prev := openFile
	openFile = func(path string) error { gotPath = path; return nil }
	defer func() { openFile = prev }()

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	if gotPath != filepath.Join("/evidence-root", "ACME-9/a.png") {
		t.Fatalf("opened path = %q, want the resolved absolute path", gotPath)
	}
	if m.ErrorMsg != "" {
		t.Fatalf("ErrorMsg = %q, want none on a successful open", m.ErrorMsg)
	}
}

func TestOpenFailureFallsBackToShowingThePath(t *testing.T) {
	fake := &data.FakeEvidence{ItemsByProject: map[string][]store.EvidenceListItem{
		"acme": {sampleItem(1, 9, "ACME-9", "ACME-9/a.png", false)},
	}}
	m := New(fake).WithProject("acme")
	m, _ = step(t, m, run(t, m.Init()))

	prev := openFile
	openFile = func(string) error { return errors.New("exec: \"open\": executable file not found in $PATH") }
	defer func() { openFile = prev }()

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	if m.ErrorMsg == "" {
		t.Fatal("a failed open should surface the path so it can be copied instead")
	}
}

func TestTaskFilterTogglesOnTheCursorRowAndBackToAll(t *testing.T) {
	fake := &data.FakeEvidence{ItemsByProject: map[string][]store.EvidenceListItem{
		"acme": {
			sampleItem(1, 9, "ACME-9", "ACME-9/a.png", false),
			sampleItem(2, 10, "ACME-10", "ACME-10/b.png", false),
		},
	}}
	m := New(fake).WithProject("acme")
	m, _ = step(t, m, run(t, m.Init()))

	m, cmd := step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	if cmd == nil {
		t.Fatal("t on a non-empty list should reload with a task filter")
	}
	m, _ = step(t, m, run(t, cmd))
	if fake.LastFilter.TaskID != 9 {
		t.Fatalf("LastFilter.TaskID = %d, want the cursor row's task 9", fake.LastFilter.TaskID)
	}
	if len(m.Items) != 1 || m.Items[0].TaskID != 9 {
		t.Fatalf("Items = %+v, want only task 9's row", m.Items)
	}

	m, cmd = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	if cmd == nil {
		t.Fatal("t again should clear the filter back to all")
	}
	m, _ = step(t, m, run(t, cmd))
	if fake.LastFilter.TaskID != 0 {
		t.Fatalf("LastFilter.TaskID = %d, want 0 (cleared)", fake.LastFilter.TaskID)
	}
	if len(m.Items) != 2 {
		t.Fatalf("Items = %+v, want both rows once the filter clears", m.Items)
	}
}

func TestAttachedFilterToggles(t *testing.T) {
	fake := &data.FakeEvidence{ItemsByProject: map[string][]store.EvidenceListItem{
		"acme": {
			sampleItem(1, 9, "ACME-9", "ACME-9/a.png", true),
			sampleItem(2, 9, "ACME-9", "ACME-9/b.png", false),
		},
	}}
	m := New(fake).WithProject("acme")
	m, _ = step(t, m, run(t, m.Init()))

	m, cmd := step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m, _ = step(t, m, run(t, cmd))
	if len(m.Items) != 1 || !m.Items[0].AttachedJira {
		t.Fatalf("Items = %+v, want only the attached row", m.Items)
	}

	m, cmd = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m, _ = step(t, m, run(t, cmd))
	if len(m.Items) != 2 {
		t.Fatalf("Items = %+v, want both rows once the filter clears", m.Items)
	}
}

func TestEscFromTheListGoesHome(t *testing.T) {
	m := New(&data.FakeEvidence{}).WithProject("acme")

	_, cmd := m.handleListKeys("esc")
	if cmd == nil {
		t.Fatal("esc should navigate home")
	}
	if _, ok := run(t, cmd).(tabs.HomeMsg); !ok {
		t.Fatalf("esc produced %T, want tabs.HomeMsg", run(t, cmd))
	}
}

// ─── Detail (S7) ─────────────────────────────────────────────────────────────

func detailModel(t *testing.T, item store.EvidenceListItem) Model {
	t.Helper()
	m := New(&data.FakeEvidence{}).WithProject("acme")
	m.Items = []store.EvidenceListItem{item}
	m.Screen = ScreenDetail
	m.Selected = &item
	return m
}

func TestDetailCopyCopiesSha256NotPath(t *testing.T) {
	item := sampleItem(1, 9, "ACME-9", "ACME-9/a.png", false)
	m := detailModel(t, item)

	_, cmd := m.handleDetailKeys("c")
	msg, ok := run(t, cmd).(shared.CopiedMsg)
	if !ok {
		t.Fatalf("c produced %T, want shared.CopiedMsg", run(t, cmd))
	}
	if msg.Sequence != shared.OSC52Sequence(item.SHA256) {
		t.Fatalf("c copied the wrong content, want sha256 %q", item.SHA256)
	}
}

func TestDetailPKeyCopiesThePath(t *testing.T) {
	t.Setenv(shared.EvidenceDirEnv, "/evidence-root")
	item := sampleItem(1, 9, "ACME-9", "ACME-9/a.png", false)
	m := detailModel(t, item)

	_, cmd := m.handleDetailKeys("p")
	msg, ok := run(t, cmd).(shared.CopiedMsg)
	if !ok {
		t.Fatalf("p produced %T, want shared.CopiedMsg", run(t, cmd))
	}
	want := shared.OSC52Sequence(filepath.Join("/evidence-root", item.Path))
	if msg.Sequence != want {
		t.Fatalf("p copied %q, want the absolute path %q", msg.Sequence, want)
	}
}

func TestDetailEnterNavigatesToTheTask(t *testing.T) {
	item := sampleItem(1, 9, "ACME-9", "ACME-9/a.png", false)
	m := detailModel(t, item)

	_, cmd := m.handleDetailKeys("enter")
	if cmd == nil {
		t.Fatal("enter should navigate to the task")
	}
	msg, ok := run(t, cmd).(tabs.NavigateMsg)
	if !ok {
		t.Fatalf("enter produced %T, want tabs.NavigateMsg", run(t, cmd))
	}
	if msg.Target != tabs.Tasks || msg.TaskID != 9 {
		t.Fatalf("NavigateMsg = %+v, want Tasks/9", msg)
	}
}

func TestDetailMKeyWithNoManifestReportsItInsteadOfOpeningAnything(t *testing.T) {
	item := sampleItem(1, 9, "ACME-9", "ACME-9/a.png", false)
	m := detailModel(t, item)
	m.ManifestChecked = true
	m.ManifestExists = false

	called := false
	prev := openFile
	openFile = func(string) error { called = true; return nil }
	defer func() { openFile = prev }()

	updated, _ := m.handleDetailKeys("m")
	m = updated.(Model)
	if called {
		t.Fatal("m must not try to open a manifest.json that does not exist")
	}
	if m.ErrorMsg == "" {
		t.Fatal("m should report that no manifest.json exists")
	}
}

func TestDetailMKeyOpensTheManifestWhenItExists(t *testing.T) {
	item := sampleItem(1, 9, "ACME-9", "ACME-9/a.png", false)
	m := detailModel(t, item)
	m.ManifestChecked = true
	m.ManifestExists = true

	var gotPath string
	prev := openFile
	openFile = func(path string) error { gotPath = path; return nil }
	defer func() { openFile = prev }()

	t.Setenv(shared.EvidenceDirEnv, "/evidence-root")
	updated, _ := m.handleDetailKeys("m")
	m = updated.(Model)
	want := filepath.Join("/evidence-root", "ACME-9", "manifest.json")
	if gotPath != want {
		t.Fatalf("opened path = %q, want %q", gotPath, want)
	}
}

func TestDetailEscReturnsToTheListAndReloads(t *testing.T) {
	fake := &data.FakeEvidence{ItemsByProject: map[string][]store.EvidenceListItem{
		"acme": {sampleItem(1, 9, "ACME-9", "ACME-9/a.png", false)},
	}}
	m := New(fake).WithProject("acme")
	item := sampleItem(1, 9, "ACME-9", "ACME-9/a.png", false)
	m.Screen = ScreenDetail
	m.Selected = &item

	updated, cmd := m.handleDetailKeys("esc")
	m = updated.(Model)
	if m.Screen != ScreenList {
		t.Fatalf("Screen = %v, want ScreenList", m.Screen)
	}
	if m.Selected != nil {
		t.Fatal("leaving the detail should clear the selected row")
	}
	if cmd == nil {
		t.Fatal("esc should reload the list")
	}
}

// TestManifestLoadedGuardsAgainstAStaleSelection pins the same guard
// tasks.stateUpdatedMsg documents: a slow manifest read for a row the user
// has since left must not resurrect state for whatever is on screen now.
func TestManifestLoadedGuardsAgainstAStaleSelection(t *testing.T) {
	item := sampleItem(1, 9, "ACME-9", "ACME-9/a.png", false)
	m := detailModel(t, item)

	entry := &ManifestEntry{File: "a.png", PositiveControl: "p"}
	m, _ = step(t, m, manifestLoadedMsg{evidenceID: 999, entry: entry, exists: true})

	if m.ManifestChecked {
		t.Fatal("a manifest result for a different evidence id must be ignored")
	}
	if m.Manifest != nil {
		t.Fatal("a stale manifest result must not be stored")
	}
}

func TestManifestLoadedStoresTheMatchingResult(t *testing.T) {
	item := sampleItem(1, 9, "ACME-9", "ACME-9/a.png", false)
	m := detailModel(t, item)

	entry := &ManifestEntry{File: "a.png", PositiveControl: "p", NegativeControl: "n"}
	m, _ = step(t, m, manifestLoadedMsg{evidenceID: 1, entry: entry, exists: true})

	if !m.ManifestChecked || !m.ManifestExists {
		t.Fatalf("m = %+v, want checked and existing", m)
	}
	if m.Manifest == nil || m.Manifest.PositiveControl != "p" {
		t.Fatalf("Manifest = %+v, want the matching entry stored", m.Manifest)
	}
}

func TestEvidenceLoadedIgnoresAResponseForAnAbandonedProject(t *testing.T) {
	m := New(&data.FakeEvidence{}).WithProject("acme")
	m.Items = []store.EvidenceListItem{sampleItem(1, 9, "ACME-9", "a.png", false)}

	m, _ = step(t, m, evidenceLoadedMsg{project: "other", items: nil})

	if len(m.Items) != 1 {
		t.Fatal("a load response for a project the user left must not clobber the current list")
	}
}

func TestErrorFromTheListLoadIsSurfaced(t *testing.T) {
	fake := &data.FakeEvidence{Err: errors.New("database is locked")}
	m := New(fake).WithProject("acme")

	m, _ = step(t, m, run(t, m.Init()))
	if m.ErrorMsg == "" {
		t.Fatal("a failing load should surface an error")
	}
}
