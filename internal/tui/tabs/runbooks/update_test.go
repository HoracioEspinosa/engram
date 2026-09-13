package runbooks

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/shared"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs"

	tea "github.com/charmbracelet/bubbletea"
)

// step feeds msg to m's Update and casts the result back to Model, the same
// idiom tabs/evidence's tests use for a leaf model.
func step(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(msg)
	next, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want runbooks.Model", updated)
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

func withHeight(m Model, h int) Model {
	m.Height = h
	m.Width = 120
	return m
}

func sampleRunbook(id, project, title string, stale bool) store.RunbookIndexRow {
	return store.RunbookIndexRow{
		ID: id, Project: project, VaultPath: "Runbooks/" + id + ".md",
		Title: title, Category: "performance", Status: "verified", Stale: stale,
	}
}

func newModel(reader data.RunbookReader, projects data.ProjectReader) Model {
	if projects == nil {
		projects = &data.FakeProject{}
	}
	return New(reader, projects)
}

// ─── Index (S8) ──────────────────────────────────────────────────────────────

func TestNewStartsOnTheIndexScreenWithNoProject(t *testing.T) {
	m := newModel(&data.FakeRunbook{}, nil)
	if m.Screen != ScreenIndex {
		t.Fatalf("Screen = %v, want ScreenIndex", m.Screen)
	}
	if cmd := m.Init(); cmd != nil {
		t.Fatal("Init without a project should have nothing to load")
	}
}

func TestInitLoadsRunbooksWhenAProjectIsAlreadyActive(t *testing.T) {
	fake := &data.FakeRunbook{ItemsByProject: map[string][]store.RunbookIndexRow{
		"acme": {sampleRunbook("RB-900", "acme", "Stale runbook", true)},
	}}
	m := newModel(fake, nil).WithProject("acme")

	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init with an active project should load its runbook index")
	}
	m, _ = step(t, m, run(t, cmd))
	if len(m.Items) != 1 || m.Items[0].ID != "RB-900" {
		t.Fatalf("Items = %+v, want the one seeded row", m.Items)
	}
}

func TestIndexCursorMovesAndClampsScroll(t *testing.T) {
	fake := &data.FakeRunbook{ItemsByProject: map[string][]store.RunbookIndexRow{
		"acme": {
			sampleRunbook("RB-001", "acme", "First", false),
			sampleRunbook("RB-002", "acme", "Second", false),
			sampleRunbook("RB-003", "acme", "Third", true),
		},
	}}
	m := withHeight(newModel(fake, nil).WithProject("acme"), 20)
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

func TestAllToggleReloadsAcrossProjects(t *testing.T) {
	fake := &data.FakeRunbook{ItemsByProject: map[string][]store.RunbookIndexRow{
		"acme":       {sampleRunbook("RB-900", "acme", "Stale runbook", true)},
		"middleware": {sampleRunbook("RB-901", "middleware", "Unrelated", false)},
	}}
	m := newModel(fake, nil).WithProject("acme")
	m, _ = step(t, m, run(t, m.Init()))

	m, cmd := step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if cmd == nil {
		t.Fatal("a should reload")
	}
	if !m.All {
		t.Fatal("a should flip All to true")
	}
	m, _ = step(t, m, run(t, cmd))
	if len(m.Items) != 2 {
		t.Fatalf("Items = %+v, want both projects' rows once All is toggled on", m.Items)
	}

	m, cmd = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m, _ = step(t, m, run(t, cmd))
	if m.All {
		t.Fatal("a again should flip All back to false")
	}
	if len(m.Items) != 1 || m.Items[0].ID != "RB-900" {
		t.Fatalf("Items = %+v, want only acme's row once All is toggled off", m.Items)
	}
}

// TestAllToggleWhileSearchingReRunsTheSameSearch pins reload()'s contract:
// toggling "a" mid-search must re-rank the same query across the new scope,
// not silently fall back to the unfiltered index and drop what the user
// typed.
func TestAllToggleWhileSearchingReRunsTheSameSearch(t *testing.T) {
	fake := &data.FakeRunbook{ItemsByProject: map[string][]store.RunbookIndexRow{
		"acme":       {sampleRunbook("RB-900", "acme", "Preview endpoint slow", false)},
		"middleware": {sampleRunbook("RB-901", "middleware", "Preview endpoint slow too", false)},
	}}
	m := newModel(fake, nil).WithProject("acme")
	m, _ = step(t, m, run(t, m.Init()))
	m.Query = "preview"

	_, cmd := m.handleIndexKeys("a")
	if cmd == nil {
		t.Fatal("a should reload")
	}
	run(t, cmd)
	if fake.LastSearch.Query != "preview" {
		t.Fatalf("LastSearch.Query = %q, want the active search re-issued, not dropped", fake.LastSearch.Query)
	}
	if !fake.LastSearch.All {
		t.Fatal("LastSearch.All should reflect the toggle just applied")
	}
}

func TestSearchEnterCommitsTheQueryAndReloads(t *testing.T) {
	fake := &data.FakeRunbook{ItemsByProject: map[string][]store.RunbookIndexRow{
		"acme": {
			sampleRunbook("RB-900", "acme", "Preview endpoint slow", false),
			sampleRunbook("RB-901", "acme", "Unrelated", false),
		},
	}}
	m := newModel(fake, nil).WithProject("acme")
	m, _ = step(t, m, run(t, m.Init()))

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	if !m.Searching || !m.SearchInput.Focused() {
		t.Fatal("/ should open the search input, focused")
	}

	for _, r := range "preview" {
		m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m, cmd := step(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.Searching {
		t.Fatal("enter should close the search input")
	}
	if cmd == nil {
		t.Fatal("enter should issue the search")
	}
	m, _ = step(t, m, run(t, cmd))
	if m.Query != "preview" {
		t.Fatalf("Query = %q, want %q committed", m.Query, "preview")
	}
	if len(m.Items) != 1 || m.Items[0].ID != "RB-900" {
		t.Fatalf("Items = %+v, want only the symptom match", m.Items)
	}
}

func TestSearchEscCancelsWithoutClearingTheActiveQuery(t *testing.T) {
	fake := &data.FakeRunbook{ItemsByProject: map[string][]store.RunbookIndexRow{
		"acme": {sampleRunbook("RB-900", "acme", "Preview endpoint slow", false)},
	}}
	m := newModel(fake, nil).WithProject("acme")
	m, _ = step(t, m, run(t, m.Init()))
	m.Query = "preview"

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m, cmd := step(t, m, tea.KeyMsg{Type: tea.KeyEsc})

	if m.Searching {
		t.Fatal("esc should close the search input")
	}
	if cmd != nil {
		t.Fatal("esc should cancel typing, not reload")
	}
	if m.Query != "preview" {
		t.Fatalf("Query = %q, want the previously committed search left untouched", m.Query)
	}
	if m.SearchInput.Value() != "preview" {
		t.Fatalf("SearchInput value = %q, want it reset to the active query", m.SearchInput.Value())
	}
}

func TestCopyInTheIndexCopiesVaultPath(t *testing.T) {
	fake := &data.FakeRunbook{ItemsByProject: map[string][]store.RunbookIndexRow{
		"acme": {sampleRunbook("RB-900", "acme", "Stale runbook", true)},
	}}
	m := newModel(fake, nil).WithProject("acme")
	m, _ = step(t, m, run(t, m.Init()))

	_, cmd := m.handleIndexKeys("c")
	msg, ok := run(t, cmd).(shared.CopiedMsg)
	if !ok {
		t.Fatalf("c produced %T, want shared.CopiedMsg", run(t, cmd))
	}
	want := shared.OSC52Sequence("Runbooks/RB-900.md")
	if msg.Sequence != want {
		t.Fatalf("copied sequence = %q, want %q", msg.Sequence, want)
	}
}

func TestTKeyFromTheIndexNavigatesToMemorySearch(t *testing.T) {
	fake := &data.FakeRunbook{ItemsByProject: map[string][]store.RunbookIndexRow{
		"acme": {sampleRunbook("RB-003", "acme", "Preview endpoint slow", true)},
	}}
	m := newModel(fake, nil).WithProject("acme")
	m, _ = step(t, m, run(t, m.Init()))

	_, cmd := m.handleIndexKeys("t")
	msg, ok := run(t, cmd).(tabs.NavigateMsg)
	if !ok {
		t.Fatalf("t produced %T, want tabs.NavigateMsg", run(t, cmd))
	}
	if msg.Target != tabs.Memory || msg.Query != "runbook/RB-003" {
		t.Fatalf("NavigateMsg = %+v, want Memory/\"runbook/RB-003\"", msg)
	}
}

func TestEscFromTheIndexGoesHome(t *testing.T) {
	m := newModel(&data.FakeRunbook{}, nil).WithProject("acme")

	_, cmd := m.handleIndexKeys("esc")
	if cmd == nil {
		t.Fatal("esc should navigate home")
	}
	if _, ok := run(t, cmd).(tabs.HomeMsg); !ok {
		t.Fatalf("esc produced %T, want tabs.HomeMsg", run(t, cmd))
	}
}

func TestRunbooksLoadedIgnoresAResponseForAnAbandonedProject(t *testing.T) {
	m := newModel(&data.FakeRunbook{}, nil).WithProject("acme")
	m.Items = []store.RunbookIndexRow{sampleRunbook("RB-900", "acme", "Stale runbook", true)}

	m, _ = step(t, m, runbooksLoadedMsg{project: "other", items: nil})

	if len(m.Items) != 1 {
		t.Fatal("a load response for a project the user left must not clobber the current list")
	}
}

func TestErrorFromTheIndexLoadIsSurfaced(t *testing.T) {
	fake := &data.FakeRunbook{Err: errors.New("database is locked")}
	m := newModel(fake, nil).WithProject("acme")

	m, _ = step(t, m, run(t, m.Init()))
	if m.ErrorMsg == "" {
		t.Fatal("a failing load should surface an error")
	}
}

// ─── Markdown view (S9) ──────────────────────────────────────────────────────

func TestEnterOpensTheMarkdownViewAndRendersAnExistingFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(shared.VaultRootEnv, dir)
	item := sampleRunbook("RB-003", "acme", "Preview endpoint slow", true)
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, item.VaultPath)), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, item.VaultPath), []byte("# Preview endpoint slow\n\nSymptoms here.\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	fake := &data.FakeRunbook{ItemsByProject: map[string][]store.RunbookIndexRow{"acme": {item}}}
	m := newModel(fake, nil).WithProject("acme")
	m, _ = step(t, m, run(t, m.Init()))

	m, cmd := step(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.Screen != ScreenView {
		t.Fatalf("Screen = %v, want ScreenView", m.Screen)
	}
	if m.Selected == nil || m.Selected.ID != "RB-003" {
		t.Fatalf("Selected = %+v, want RB-003", m.Selected)
	}
	if cmd == nil {
		t.Fatal("enter should load the runbook's markdown")
	}

	m, _ = step(t, m, run(t, cmd))
	if !m.FileExists {
		t.Fatal("the fixture file exists on disk; FileExists should be true")
	}
	if m.Rendered == "" {
		t.Fatal("glamour should have produced some rendered content")
	}
	if !containsFold(m.Rendered, "Symptoms here") {
		t.Fatalf("rendered = %q, want it to still carry the source text", m.Rendered)
	}
}

func TestMarkdownViewReportsWhenTheFileIsNotClonedLocally(t *testing.T) {
	t.Setenv(shared.VaultRootEnv, t.TempDir())
	item := sampleRunbook("RB-003", "acme", "Preview endpoint slow", true)
	fake := &data.FakeRunbook{ItemsByProject: map[string][]store.RunbookIndexRow{"acme": {item}}}
	m := newModel(fake, nil).WithProject("acme")
	m, _ = step(t, m, run(t, m.Init()))

	m, cmd := step(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = step(t, m, run(t, cmd))

	if m.FileExists {
		t.Fatal("no fixture was written; FileExists should be false")
	}
	if m.ErrorMsg != "" {
		t.Fatalf("ErrorMsg = %q, want none: a missing checkout is documented, not an error", m.ErrorMsg)
	}
	view := m.View()
	if !containsFold(view, "not cloned locally") {
		t.Fatalf("view = %q, want the clone instruction", view)
	}
}

// TestMarkdownLoadedGuardsAgainstAStaleSelection pins the same guard
// tabs/evidence's manifestLoadedMsg documents: a slow markdown read for a
// runbook the user has since left must not resurrect content for whatever
// is on screen now.
func TestMarkdownLoadedGuardsAgainstAStaleSelection(t *testing.T) {
	item := sampleRunbook("RB-003", "acme", "Preview endpoint slow", true)
	m := newModel(&data.FakeRunbook{}, nil).WithProject("acme")
	m.Screen = ScreenView
	m.Selected = &item

	m, _ = step(t, m, markdownLoadedMsg{id: "RB-999", exists: true, raw: "stale", rendered: "stale"})

	if m.MarkdownRaw != "" || m.Rendered != "" {
		t.Fatalf("a markdown result for a different runbook id must be ignored, got raw=%q rendered=%q", m.MarkdownRaw, m.Rendered)
	}
}

func TestEditorKeyUsesTheInjectableExecEditor(t *testing.T) {
	item := sampleRunbook("RB-003", "acme", "Preview endpoint slow", true)
	m := newModel(&data.FakeRunbook{}, nil).WithProject("acme")
	m.Screen = ScreenView
	m.Selected = &item

	var gotPath string
	prev := execEditor
	execEditor = func(path string) tea.Cmd {
		gotPath = path
		return func() tea.Msg { return editorClosedMsg{} }
	}
	defer func() { execEditor = prev }()

	m, cmd := step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	if cmd == nil {
		t.Fatal("e should invoke execEditor")
	}
	run(t, cmd)
	want := filepath.Join(shared.VaultRoot(), item.VaultPath)
	if gotPath != want {
		t.Fatalf("opened path = %q, want %q", gotPath, want)
	}

	m, _ = step(t, m, editorClosedMsg{err: errors.New("exec: \"vi\": executable file not found in $PATH")})
	if m.ErrorMsg == "" {
		t.Fatal("a failed $EDITOR launch should surface an error")
	}
}

func TestTKeyFromTheViewNavigatesToMemorySearch(t *testing.T) {
	item := sampleRunbook("RB-003", "acme", "Preview endpoint slow", true)
	m := newModel(&data.FakeRunbook{}, nil).WithProject("acme")
	m.Screen = ScreenView
	m.Selected = &item

	_, cmd := m.handleViewKeys("t")
	msg, ok := run(t, cmd).(tabs.NavigateMsg)
	if !ok {
		t.Fatalf("t produced %T, want tabs.NavigateMsg", run(t, cmd))
	}
	if msg.Target != tabs.Memory || msg.Query != "runbook/RB-003" {
		t.Fatalf("NavigateMsg = %+v, want Memory/\"runbook/RB-003\"", msg)
	}
}

func TestOKeyOpensTheHubViaTheInjectableExecEditor(t *testing.T) {
	hub := "Services/nextcloud.md"
	item := sampleRunbook("RB-003", "nextcloud", "Preview endpoint slow", true)
	projects := &data.FakeProject{CardBySlug: map[string]store.ProjectCard{
		"nextcloud": {Slug: "nextcloud", KnowledgeHubPath: &hub},
	}}
	m := newModel(&data.FakeRunbook{}, projects).WithProject("nextcloud")
	m.Screen = ScreenView
	m.Selected = &item

	var gotPath string
	prev := execEditor
	execEditor = func(path string) tea.Cmd {
		gotPath = path
		return func() tea.Msg { return editorClosedMsg{} }
	}
	defer func() { execEditor = prev }()

	_, cmd := m.handleViewKeys("o")
	if cmd == nil {
		t.Fatal("o should resolve the hub path")
	}
	m, cmd = step(t, m, run(t, cmd))
	if cmd == nil {
		t.Fatal("resolving the hub should hand off to execEditor")
	}
	run(t, cmd)
	want := filepath.Join(shared.VaultRoot(), hub)
	if gotPath != want {
		t.Fatalf("opened path = %q, want %q", gotPath, want)
	}
}

func TestOKeyReportsWhenTheProjectHasNoHubConfigured(t *testing.T) {
	item := sampleRunbook("RB-003", "acme", "Preview endpoint slow", true)
	projects := &data.FakeProject{CardBySlug: map[string]store.ProjectCard{"acme": {Slug: "acme"}}}
	m := newModel(&data.FakeRunbook{}, projects).WithProject("acme")
	m.Screen = ScreenView
	m.Selected = &item

	_, cmd := m.handleViewKeys("o")
	m, _ = step(t, m, run(t, cmd))
	if m.ErrorMsg == "" {
		t.Fatal("a project with no knowledge_hub_path should report it instead of opening nothing silently")
	}
}

func TestEscFromTheViewReturnsToTheIndexAndReloads(t *testing.T) {
	fake := &data.FakeRunbook{ItemsByProject: map[string][]store.RunbookIndexRow{
		"acme": {sampleRunbook("RB-900", "acme", "Stale runbook", true)},
	}}
	item := sampleRunbook("RB-900", "acme", "Stale runbook", true)
	m := newModel(fake, nil).WithProject("acme")
	m.Screen = ScreenView
	m.Selected = &item

	m, cmd := step(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.Screen != ScreenIndex {
		t.Fatalf("Screen = %v, want ScreenIndex", m.Screen)
	}
	if m.Selected != nil {
		t.Fatal("leaving the view should clear the selected row")
	}
	if cmd == nil {
		t.Fatal("esc should reload the index")
	}
}

func containsFold(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}
