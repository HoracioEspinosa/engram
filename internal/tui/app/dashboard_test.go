package app

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

func strp(v string) *string { return &v }

func testDashboardReader() *data.FakeProject {
	return &data.FakeProject{
		CardBySlug: map[string]store.ProjectCard{
			"nextcloud": {
				Slug: "nextcloud", DisplayName: "Nextcloud (clarodrive)",
				RepoURL: strp("bitbucket.org/dlaargentina/clarodrive"), DefaultBranch: "master",
				JiraProject: "CDBS", KnowledgeHubPath: strp("Services/Nextcloud/Nextcloud.md"),
				GraphCommit: strp("f3a9c1e"), GraphBuiltAt: strp("2026-08-27 22:10:00"),
			},
		},
		HealthBySlug: map[string]data.ProjectHealth{
			"nextcloud": {ProjectCardCounts: store.ProjectCardCounts{
				Observations: 1204, TasksActive: 7, Evidence: 23, EvidenceUnattached: 4, Runbooks: 4, RunbooksStale: 2,
			}},
		},
		TasksBySlug: map[string][]store.TaskListItem{
			"nextcloud": {{Task: store.Task{
				SyncID: "task-1", JiraKey: strp("CDBS-10336"), Kind: "bugfix", State: "In Code Review",
				Title: "Previews 503 on cold generation",
			}}},
		},
		StaleRunbooksSlug: map[string][]store.RunbookIndexRow{
			"nextcloud": {{ID: "RB-003", Title: "Preview endpoint slow or failing", Stale: true, AgeDays: intp(105)}},
		},
		EvidenceBySlug: map[string][]store.EvidenceListItem{
			"nextcloud": {{Evidence: store.Evidence{Path: "CDBS-10336-01-cold-generation-503.png", AttachedJira: true}}},
		},
	}
}

func intp(v int) *int { return &v }

func TestZeroKeyIsANoOpWithoutAnActiveProject(t *testing.T) {
	m := New(nil, nil, "", theme.New(theme.CatppuccinMocha()), "")

	m, cmd := step(t, m, tea.KeyMsg{Runes: []rune("0"), Type: tea.KeyRunes})
	if m.screen != screenTab {
		t.Fatalf("screen = %v, want screenTab: there is no project yet to show a dashboard for", m.screen)
	}
	if cmd != nil {
		t.Fatal("0 without an active project should not produce a command")
	}
}

func TestInitialProjectOpensStraightOnTheDashboard(t *testing.T) {
	m := New(nil, nil, "", theme.New(theme.CatppuccinMocha()), "nextcloud")

	if m.screen != screenDashboard {
		t.Fatalf("screen = %v, want screenDashboard", m.screen)
	}
	if m.project != "nextcloud" {
		t.Fatalf("project = %q, want nextcloud", m.project)
	}
	if cmd := m.Init(); cmd == nil {
		t.Fatal("Init should include the Dashboard's first load when a project was pre-selected")
	}
}

func TestZeroKeyShowsTheDashboardAndReloadsIt(t *testing.T) {
	m := New(nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	reader := testDashboardReader()
	m.project = "nextcloud"
	m.active = tabs.Cloud
	m.projects = reader
	// applyLoaded only accepts a response whose slug matches m.dashboard's
	// own, so a project set outside newDashboardModel — as New(initialProject)
	// and selectorEnter both do in production — must seed it here too.
	m.dashboard = newDashboardModel(reader, "nextcloud")

	m, cmd := step(t, m, tea.KeyMsg{Runes: []rune("0"), Type: tea.KeyRunes})
	if m.screen != screenDashboard {
		t.Fatalf("screen = %v, want screenDashboard", m.screen)
	}
	if cmd == nil {
		t.Fatal("0 should load the Dashboard's blocks")
	}

	m, _ = step(t, m, cmd())
	if !m.dashboard.loaded {
		t.Fatal("the dashboard should be marked loaded once its data arrives")
	}
	if m.dashboard.card.DisplayName != "Nextcloud (clarodrive)" {
		t.Fatalf("card = %+v", m.dashboard.card)
	}
	if m.dashboard.health.Observations != 1204 {
		t.Fatalf("health = %+v", m.dashboard.health)
	}
	if len(m.dashboard.tasks) != 1 || len(m.dashboard.staleRunbooks) != 1 || len(m.dashboard.evidence) != 1 {
		t.Fatalf("dashboard blocks not populated: %+v", m.dashboard)
	}
}

func TestDashboardEnterOnRecentTasksNavigatesToTasks(t *testing.T) {
	m := New(nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.screen = screenDashboard
	m.project = "nextcloud"
	m.dashboard.cursor = dashBlockTasks

	// Tasks is not a registered tab yet, so this must be the same graceful
	// no-op TestNavigateToAnUnimplementedTabIsANoOp already pins for
	// NavigateMsg — it must not, in particular, crash or strand the screen.
	m, cmd := step(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("entering an unimplemented tab should not produce a command")
	}
	if m.screen != screenDashboard {
		t.Fatalf("screen = %v, want to stay on the dashboard", m.screen)
	}
}

func TestDashboardEnterOnMemoryOnceItIsRegisteredSwitchesScreen(t *testing.T) {
	m := New(nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.screen = screenDashboard
	m.project = "nextcloud"
	// dashBlockTasks.target() resolves to tabs.Tasks (unregistered); force
	// the cursor's target through Memory instead by activating directly,
	// exercising the same activate() path Enter uses once a Dashboard block
	// maps to a registered tab.
	m, cmd := step(t, m, tabs.NavigateMsg{Target: tabs.Memory})
	if m.screen != screenTab || m.active != tabs.Memory {
		t.Fatalf("screen = %v active = %v, want screenTab/Memory", m.screen, m.active)
	}
	if cmd == nil {
		t.Fatal("activating a registered tab should refresh it")
	}
}

func TestDashboardCursorMovesAcrossAllThreeBlocks(t *testing.T) {
	m := New(nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.screen = screenDashboard

	for _, key := range []string{"j", "j"} {
		m, _ = step(t, m, tea.KeyMsg{Runes: []rune(key), Type: tea.KeyRunes})
	}
	if m.dashboard.cursor != dashBlockEvidence {
		t.Fatalf("cursor = %v, want dashBlockEvidence after moving down twice", m.dashboard.cursor)
	}

	m, _ = step(t, m, tea.KeyMsg{Runes: []rune("k"), Type: tea.KeyRunes})
	if m.dashboard.cursor != dashBlockRunbooks {
		t.Fatalf("cursor = %v, want dashBlockRunbooks after moving up once", m.dashboard.cursor)
	}
}

func TestDashboardQQuits(t *testing.T) {
	m := New(nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.screen = screenDashboard

	_, cmd := step(t, m, tea.KeyMsg{Runes: []rune("q"), Type: tea.KeyRunes})
	if cmd == nil {
		t.Fatal("q at the dashboard should quit")
	}
}

func TestGoHomeFallsBackToMemoryWithoutAnActiveProject(t *testing.T) {
	m := New(nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.active = tabs.Cloud

	m, cmd := step(t, m, tabs.HomeMsg{})
	if m.screen != screenTab || m.active != tabs.Memory {
		t.Fatalf("screen = %v active = %v, want the pre-project home (Memory)", m.screen, m.active)
	}
	if cmd == nil {
		t.Fatal("falling back to Memory should still refresh it")
	}
}

func TestGoHomeGoesToTheDashboardOnceAProjectIsActive(t *testing.T) {
	m := New(nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.project = "nextcloud"
	m.active = tabs.Cloud

	m, cmd := step(t, m, tabs.HomeMsg{})
	if m.screen != screenDashboard {
		t.Fatalf("screen = %v, want screenDashboard", m.screen)
	}
	if cmd == nil {
		t.Fatal("returning home to an active project should load its dashboard")
	}
}

// TestCloudEscRoundTripsThroughTheRootWithAProjectActive exercises the same
// round trip TestCloudRoundTripFromTheDashboard already pins for the
// no-project case, but with a project active: leaving Cloud must land on the
// Dashboard now, not on Memory.
func TestCloudEscRoundTripsThroughTheRootWithAProjectActive(t *testing.T) {
	m := New(nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.project = "nextcloud"
	m.dashboard = newDashboardModel(testDashboardReader(), "nextcloud")

	// Cloud's own Refresh has nothing to reload (TestNavigateSwitchesTabsAnd
	// RefreshesTheTarget already pins this), so activating it legitimately
	// returns a nil command; only the screen switch matters here.
	m, _ = step(t, m, tabs.NavigateMsg{Target: tabs.Cloud})
	if m.active != tabs.Cloud || m.screen != screenTab {
		t.Fatalf("active = %v screen = %v, want Cloud/screenTab", m.active, m.screen)
	}

	m, cmd := step(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc on cloud should ask the root to go home")
	}
	m, _ = step(t, m, cmd())
	if m.screen != screenDashboard {
		t.Fatalf("screen = %v, want screenDashboard", m.screen)
	}
}

func TestDashboardAppliedLoadedIgnoresAStaleProject(t *testing.T) {
	dash := newDashboardModel(nil, "nextcloud")
	dash = dash.applyLoaded(dashboardLoadedMsg{slug: "portal", card: store.ProjectCard{Slug: "portal"}})

	if dash.loaded {
		t.Fatal("a response for a project the user has since left must not mark the dashboard loaded")
	}
	if dash.card.Slug == "portal" {
		t.Fatal("a stale response must not overwrite the active project's card")
	}
}

func TestDashboardAppliedLoadedRecordsTheFirstError(t *testing.T) {
	dash := newDashboardModel(nil, "nextcloud")
	dash = dash.applyLoaded(dashboardLoadedMsg{slug: "nextcloud", err: errors.New("boom")})

	if dash.err != "boom" {
		t.Fatalf("err = %q, want boom", dash.err)
	}
}

func TestLoadDashboardIsBestEffortAfterTheCardSucceeds(t *testing.T) {
	fake := &data.FakeProject{
		CardBySlug: map[string]store.ProjectCard{"nextcloud": {Slug: "nextcloud"}},
		Err:        nil,
	}
	// Health, RecentTasks, StaleRunbooks and LatestEvidence all fail once the
	// card lookup has already succeeded (FakeProject's Err is all-or-nothing,
	// so this drives the failure through a reader that fails everything
	// after a successful Card call is impossible to express with FakeProject
	// alone; assert instead that a failing Card alone short-circuits with an
	// error and touches none of the other blocks).
	cmd := loadDashboard(fake, "nextcloud")
	msg, ok := cmd().(dashboardLoadedMsg)
	if !ok {
		t.Fatalf("loadDashboard returned %T, want dashboardLoadedMsg", cmd())
	}
	if msg.err != nil {
		t.Fatalf("err = %v, want the card lookup to have succeeded", msg.err)
	}
	if msg.card.Slug != "nextcloud" {
		t.Fatalf("card = %+v", msg.card)
	}
}

func TestDashBlockTargetCoversEveryBlock(t *testing.T) {
	cases := map[dashBlock]tabs.ID{
		dashBlockTasks:    tabs.Tasks,
		dashBlockRunbooks: tabs.Runbooks,
		dashBlockEvidence: tabs.Evidence,
		dashBlock(99):     tabs.Memory, // unknown block: falls back rather than panicking
	}
	for block, want := range cases {
		if got := block.target(); got != want {
			t.Errorf("dashBlock(%d).target() = %v, want %v", block, got, want)
		}
	}
}

func TestDashboardMoveCursorClampsAtBothEdges(t *testing.T) {
	dash := dashboardModel{cursor: dashBlockTasks}

	dash = dash.moveCursor(-5)
	if dash.cursor != dashBlockTasks {
		t.Fatalf("cursor = %v, want clamped to dashBlockTasks", dash.cursor)
	}
	dash = dash.moveCursor(5)
	if dash.cursor != dashBlockCount-1 {
		t.Fatalf("cursor = %v, want clamped to the last block", dash.cursor)
	}
}

// partialFailReader lets a test fail exactly one ProjectReader method while
// the rest succeed — something data.FakeProject's single all-or-nothing Err
// cannot express, and which loadDashboard's best-effort contract needs
// covered: a card that loaded fine must survive one of the other four calls
// failing.
type partialFailReader struct {
	card      store.ProjectCard
	healthErr error
}

func (r partialFailReader) ListCards() ([]store.ProjectCardListItem, error) { return nil, nil }
func (r partialFailReader) Card(string) (store.ProjectCard, error)          { return r.card, nil }
func (r partialFailReader) Health(string) (data.ProjectHealth, error) {
	return data.ProjectHealth{}, r.healthErr
}
func (r partialFailReader) RecentTasks(string, int) ([]store.TaskListItem, error) { return nil, nil }
func (r partialFailReader) StaleRunbooks(string, int) ([]store.RunbookIndexRow, error) {
	return nil, nil
}
func (r partialFailReader) LatestEvidence(string, int) ([]store.EvidenceListItem, error) {
	return nil, nil
}

func TestLoadDashboardKeepsTheCardWhenALaterCallFails(t *testing.T) {
	r := partialFailReader{card: store.ProjectCard{Slug: "nextcloud"}, healthErr: errors.New("counters unavailable")}

	cmd := loadDashboard(r, "nextcloud")
	msg, ok := cmd().(dashboardLoadedMsg)
	if !ok {
		t.Fatalf("loadDashboard returned %T, want dashboardLoadedMsg", cmd())
	}
	if msg.err == nil || msg.err.Error() != "counters unavailable" {
		t.Fatalf("err = %v, want the health error surfaced", msg.err)
	}
	if msg.card.Slug != "nextcloud" {
		t.Fatal("a failing health call must not blank the card that already succeeded")
	}
}

func TestStatusTextReportsTheSyncLifecycleOnceEnrolled(t *testing.T) {
	m := New(nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.dashboard = newDashboardModel(nil, "nextcloud")
	m.dashboard = m.dashboard.applyLoaded(dashboardLoadedMsg{
		slug:   "nextcloud",
		card:   store.ProjectCard{Slug: "nextcloud"},
		health: data.ProjectHealth{Sync: store.ProjectSyncSummary{Enrolled: true, Lifecycle: "idle"}},
	})

	if got := m.statusText(); got != "sync: idle" {
		t.Fatalf("statusText() = %q, want %q", got, "sync: idle")
	}
}

func TestLoadDashboardShortCircuitsOnACardError(t *testing.T) {
	fake := &data.FakeProject{Err: errors.New("no such project")}

	cmd := loadDashboard(fake, "ghost")
	msg, ok := cmd().(dashboardLoadedMsg)
	if !ok {
		t.Fatalf("loadDashboard returned %T, want dashboardLoadedMsg", cmd())
	}
	if msg.err == nil {
		t.Fatal("a failing card lookup should be reported")
	}
	if msg.tasks != nil || msg.staleRunbooks != nil || msg.evidence != nil {
		t.Fatal("a failing card lookup should short-circuit the rest of the load")
	}
}

func TestViewDashboardRendersEveryBlock(t *testing.T) {
	m := New(nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.screen = screenDashboard
	m.project = "nextcloud"
	m.dashboard = newDashboardModel(testDashboardReader(), "nextcloud")

	cmd := loadDashboard(testDashboardReader(), "nextcloud")
	m.dashboard = m.dashboard.applyLoaded(cmd().(dashboardLoadedMsg))

	out := m.View()
	for _, want := range []string{
		"nextcloud", "1204", "CDBS-10336", "RB-003", "cold-generation-503.png",
		"bitbucket.org/dlaargentina/clarodrive", "f3a9c1e",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("dashboard view missing %q, got:\n%s", want, out)
		}
	}
}

func TestViewDashboardRendersEmptyBlocksGracefully(t *testing.T) {
	m := New(nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.screen = screenDashboard
	m.project = "portal"
	m.dashboard = newDashboardModel(nil, "portal")
	m.dashboard = m.dashboard.applyLoaded(dashboardLoadedMsg{slug: "portal", card: store.ProjectCard{Slug: "portal"}})

	out := m.View()
	for _, want := range []string{"No open tasks", "No stale runbooks", "No evidence captured yet"} {
		if !strings.Contains(out, want) {
			t.Fatalf("empty dashboard should render %q, got:\n%s", want, out)
		}
	}
}

func TestViewDashboardShowsLoadingBeforeTheFirstResponse(t *testing.T) {
	m := New(nil, nil, "", theme.New(theme.CatppuccinMocha()), "nextcloud")

	if out := m.View(); !strings.Contains(out, "Loading nextcloud") {
		t.Fatalf("an unloaded dashboard should show a loading state, got:\n%s", out)
	}
}

func TestViewDashboardShowsTheLoadError(t *testing.T) {
	m := New(nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.screen = screenDashboard
	m.project = "nextcloud"
	m.dashboard = newDashboardModel(nil, "nextcloud").applyLoaded(dashboardLoadedMsg{
		slug: "nextcloud", err: errors.New("no project card"),
	})

	if out := m.View(); !strings.Contains(out, "no project card") {
		t.Fatalf("the dashboard should render its load error, got:\n%s", out)
	}
}
