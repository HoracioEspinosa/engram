package app

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	tea "github.com/charmbracelet/bubbletea"
)

// paletteHits is one hit of every kind, plus enough tasks to overflow the
// per-kind cap so the "+N more" marker has something to count.
func paletteHits() []data.SearchHit {
	hits := []data.SearchHit{
		{Kind: data.SearchKindCard, Slug: "clarodrive", Project: "clarodrive", Title: "Claro Drive"},
		{Kind: data.SearchKindObservation, ID: 501, Project: "clarodrive", Title: "preview worker pool exhausted"},
		{Kind: data.SearchKindEvidence, ID: 77, Project: "clarodrive", Title: "preview/cold-start.png"},
		{Kind: data.SearchKindBenchmark, ID: 88, Project: "clarodrive", Title: "preview p95"},
		{Kind: data.SearchKindRunbook, Slug: "RB-900", Project: "clarodrive", Title: "preview endpoint returns 503"},
	}
	for i := 1; i <= paletteLimitPerKind+2; i++ {
		hits = append(hits, data.SearchHit{
			Kind: data.SearchKindTask, ID: int64(i), Project: "clarodrive",
			Title: "preview task " + string(rune('a'+i-1)),
		})
	}
	return hits
}

// openPalette returns a workspace with the overlay open and a searcher bound.
func openPalette(t *testing.T, hits []data.SearchHit) (Model, *data.FakeSearch, *data.FakeSettings) {
	t.Helper()
	search := &data.FakeSearch{Hits: hits}
	settings := &data.FakeSettings{}

	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.KoiPond()), "clarodrive").
		WithSearch(search, settings)
	m.tree.open = false
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyCtrlK})
	return m, search, settings
}

// typeQuery feeds text to the open palette, one keystroke at a time, the way a
// terminal would.
func typeQuery(t *testing.T, m Model, text string) (Model, tea.Cmd) {
	t.Helper()
	var cmd tea.Cmd
	for _, r := range text {
		m, cmd = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m, cmd
}

func TestCtrlKOpensThePaletteAndEscClosesIt(t *testing.T) {
	m, _, _ := openPalette(t, nil)

	if !m.palette.open {
		t.Fatal("ctrl+k should open the palette")
	}
	if !m.CapturingText() {
		t.Error("an open palette owns the keyboard: every key is a character")
	}

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.palette.open {
		t.Fatal("esc should close the palette")
	}
	if m.palette.input.Value() != "" {
		t.Error("closing should clear the query: reopening is a new search")
	}
}

// TestSlashOpensThePaletteOnlyWhereNothingElseClaimsIt pins the borrowed key:
// a tab with a search of its own keeps "/", and Home, which has none, lends it
// to the palette.
func TestSlashOpensThePaletteOnlyWhereNothingElseClaimsIt(t *testing.T) {
	slash := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")}

	onHome := New(nil, nil, nil, nil, nil, "", theme.New(theme.KoiPond()), "clarodrive")
	onHome.tree.open = false
	onHome.active = tabs.Home
	if opened, _ := step(t, onHome, slash); !opened.palette.open {
		t.Error("Home declares no \"/\" of its own, so the palette may borrow it")
	}

	onTasks := onHome
	onTasks.active = tabs.Tasks
	if opened, _ := step(t, onTasks, slash); opened.palette.open {
		t.Error("Tasks declares \"/\" as its own search; the palette must not steal it")
	}
}

func TestAQueryBelowTheFloorSearchesNothing(t *testing.T) {
	m, _, _ := openPalette(t, paletteHits())

	m, cmd := typeQuery(t, m, "p")

	if m.palette.pending {
		t.Fatal("one character is below the floor: nothing should be in flight")
	}
	if cmd != nil {
		if _, ok := cmd().(searchTickMsg); ok {
			t.Fatal("a query below the floor scheduled a search")
		}
	}
	if !strings.Contains(m.View(), "at least 2") {
		t.Errorf("the palette should say what it is waiting for, got:\n%s", m.View())
	}
}

// TestPaletteDropsStaleGenerations is the whole reason the debounce carries a
// generation: typing quickly leaves several searches in flight, and the one
// that answers last is not the answer to what is on screen.
func TestPaletteDropsStaleGenerations(t *testing.T) {
	m, _, _ := openPalette(t, paletteHits())
	m, _ = typeQuery(t, m, "preview")

	current := m.palette.gen

	// A tick from an older generation must not issue a search.
	before, cmd := step(t, m, searchTickMsg{gen: current - 1})
	if cmd != nil {
		t.Fatal("a tick for a query that has moved on issued a search")
	}
	if before.palette.rows != nil {
		t.Fatal("a stale tick changed the list")
	}

	// A result from an older generation must not reach the screen.
	stale := searchDoneMsg{gen: current - 1, query: "prev", hits: paletteHits()}
	after, _ := step(t, m, stale)
	if len(after.palette.rows) != 0 {
		t.Fatalf("a result for a query nobody is looking at reached the screen: %d rows", len(after.palette.rows))
	}

	// The current generation's own result does.
	fresh := searchDoneMsg{gen: current, query: "preview", hits: paletteHits()}
	live, _ := step(t, m, fresh)
	if len(live.palette.rows) == 0 {
		t.Fatal("the current generation's result was dropped too")
	}
}

func TestTheDebounceRunsTheSearchOnceItElapses(t *testing.T) {
	m, search, _ := openPalette(t, paletteHits())
	m, cmd := typeQuery(t, m, "preview")

	if !m.palette.pending {
		t.Fatal("a query over the floor should have a search in flight")
	}
	if cmd == nil {
		t.Fatal("typing should schedule the debounce")
	}

	m, cmd = step(t, m, searchTickMsg{gen: m.palette.gen})
	if cmd == nil {
		t.Fatal("the tick should issue the search")
	}
	m, _ = step(t, m, cmd())

	if got := search.LastQuery(); got.Text != "preview" {
		t.Errorf("the store was asked for %q, want %q", got.Text, "preview")
	}
	if got := search.LastQuery(); got.LimitPerKind != paletteLimitPerKind {
		t.Errorf("LimitPerKind = %d, want %d", got.LimitPerKind, paletteLimitPerKind)
	}
	if got := search.LastQuery(); got.Scope.Project != "clarodrive" || !got.Scope.Subtree {
		t.Errorf("scope = %+v, want the active project's subtree", got.Scope)
	}
}

func TestAPrefixNarrowsTheSearchToOneKind(t *testing.T) {
	cases := map[string]data.SearchKind{
		"t:": data.SearchKindTask,
		"e:": data.SearchKindEvidence,
		"m:": data.SearchKindObservation,
		"r:": data.SearchKindRunbook,
		"p:": data.SearchKindCard,
		"b:": data.SearchKindBenchmark,
	}

	for prefix, want := range cases {
		t.Run(prefix, func(t *testing.T) {
			m, search, _ := openPalette(t, paletteHits())
			m, _ = typeQuery(t, m, prefix+"preview")

			m, cmd := step(t, m, searchTickMsg{gen: m.palette.gen})
			if cmd == nil {
				t.Fatal("the tick should issue the search")
			}
			step(t, m, cmd())

			got := search.LastQuery()
			if len(got.Kinds) != 1 || got.Kinds[0] != want {
				t.Fatalf("kinds = %v, want [%s]", got.Kinds, want)
			}
			if got.Text != "preview" {
				t.Fatalf("query text = %q, want the chip stripped off", got.Text)
			}
		})
	}
}

func TestAPrefixNobodyDeclaredStaysPartOfTheQuery(t *testing.T) {
	_, hasChip, query := splitPalettePrefix("https://example.test")

	if hasChip {
		t.Fatal("\"https\" is not one of the six chips")
	}
	if query != "https://example.test" {
		t.Fatalf("query = %q, want the text untouched", query)
	}
}

func TestThePrefixIsShownAsAChip(t *testing.T) {
	m, _, _ := openPalette(t, paletteHits())
	m, _ = typeQuery(t, m, "t:preview")

	if !strings.Contains(m.View(), "[tasks]") {
		t.Fatalf("the chip should name the kind it narrows to, got:\n%s", m.View())
	}
}

func TestResultsAreGroupedInTheDeclaredOrderAndCapped(t *testing.T) {
	m, _, _ := openPalette(t, paletteHits())
	m, _ = typeQuery(t, m, "preview")
	m, _ = step(t, m, searchDoneMsg{gen: m.palette.gen, query: "preview", hits: paletteHits()})

	var headings []string
	tasksShown := 0
	more := 0
	for _, row := range m.palette.rows {
		switch {
		case row.heading != "":
			headings = append(headings, row.heading)
		case row.more > 0:
			more = row.more
		case row.hit.Kind == data.SearchKindTask:
			tasksShown++
		}
	}

	want := []string{"projects", "tasks", "memory", "evidence", "benchmarks", "runbooks"}
	if strings.Join(headings, ",") != strings.Join(want, ",") {
		t.Fatalf("groups = %v, want %v", headings, want)
	}
	if tasksShown != paletteLimitPerKind {
		t.Errorf("%d tasks shown, want the group capped at %d", tasksShown, paletteLimitPerKind)
	}
	if more != 2 {
		t.Errorf("the cap left out %d rows, want 2 reported", more)
	}
	if !strings.Contains(m.View(), "+2 more") {
		t.Errorf("what the cap left out should be counted on screen, got:\n%s", m.View())
	}
}

func TestTheCursorSkipsHeadingsAndTabJumpsGroups(t *testing.T) {
	m, _, _ := openPalette(t, paletteHits())
	m, _ = typeQuery(t, m, "preview")
	m, _ = step(t, m, searchDoneMsg{gen: m.palette.gen, query: "preview", hits: paletteHits()})

	hit, ok := m.palette.selected()
	if !ok {
		t.Fatal("the cursor should open on the first hit, not on a heading")
	}
	if hit.Kind != data.SearchKindCard {
		t.Fatalf("the cursor opened on a %s, want the first group's hit", hit.Kind)
	}

	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyTab})
	next, ok := m.palette.selected()
	if !ok || next.Kind != data.SearchKindTask {
		t.Fatalf("tab landed on %+v, want the first hit of the next group", next)
	}

	// Moving down from the last hit of a group steps over its heading rather
	// than stopping on it.
	for i := 0; i < 20; i++ {
		m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyDown})
		if _, ok := m.palette.selected(); !ok {
			t.Fatal("the cursor landed on a row that is not a hit")
		}
	}
}

func TestEnterOpensWhereTheHitLives(t *testing.T) {
	cases := []struct {
		hit    data.SearchHit
		target tabs.ID
		check  func(t *testing.T, msg tabs.NavigateMsg)
	}{
		{
			hit: data.SearchHit{Kind: data.SearchKindTask, ID: 9, Project: "clarodrive", Title: "preview"},
			check: func(t *testing.T, msg tabs.NavigateMsg) {
				if msg.Target != tabs.Tasks || msg.TaskID != 9 {
					t.Fatalf("navigation = %+v, want the Tasks tab on task 9", msg)
				}
			},
		},
		{
			hit: data.SearchHit{Kind: data.SearchKindObservation, ID: 501, Project: "clarodrive", Title: "preview"},
			check: func(t *testing.T, msg tabs.NavigateMsg) {
				if msg.Target != tabs.Memory || msg.ObservationID != 501 {
					t.Fatalf("navigation = %+v, want Memory on observation 501", msg)
				}
			},
		},
		{
			hit: data.SearchHit{Kind: data.SearchKindEvidence, ID: 77, Project: "clarodrive", Title: "preview"},
			check: func(t *testing.T, msg tabs.NavigateMsg) {
				if msg.Target != tabs.Evidence || msg.EvidenceID != 77 {
					t.Fatalf("navigation = %+v, want Evidence on file 77", msg)
				}
			},
		},
		{
			hit: data.SearchHit{Kind: data.SearchKindBenchmark, ID: 88, Project: "clarodrive", Title: "preview"},
			check: func(t *testing.T, msg tabs.NavigateMsg) {
				if msg.Target != tabs.Benchmarks || msg.BenchmarkID != 88 {
					t.Fatalf("navigation = %+v, want Benchmarks on measurement 88", msg)
				}
			},
		},
		{
			hit: data.SearchHit{Kind: data.SearchKindRunbook, Slug: "RB-900", Project: "clarodrive", Title: "preview"},
			check: func(t *testing.T, msg tabs.NavigateMsg) {
				if msg.Target != tabs.Runbooks || msg.Query != "RB-900" {
					t.Fatalf("navigation = %+v, want Runbooks on RB-900", msg)
				}
			},
		},
		{
			hit: data.SearchHit{Kind: data.SearchKindCard, Slug: "other", Project: "other", Title: "preview"},
			check: func(t *testing.T, msg tabs.NavigateMsg) {
				if msg.Target != tabs.Home || msg.Slug != "other" {
					t.Fatalf("navigation = %+v, want Home rescoped to the project", msg)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.hit.Kind, func(t *testing.T) {
			m, _, _ := openPalette(t, []data.SearchHit{tc.hit})
			m, _ = typeQuery(t, m, "preview")
			m, _ = step(t, m, searchDoneMsg{gen: m.palette.gen, query: "preview", hits: []data.SearchHit{tc.hit}})

			m, cmd := step(t, m, tea.KeyMsg{Type: tea.KeyEnter})
			if m.palette.open {
				t.Error("opening a hit should close the palette")
			}
			if cmd == nil {
				t.Fatal("enter should navigate")
			}

			var nav tabs.NavigateMsg
			batch, ok := cmd().(tea.BatchMsg)
			if !ok {
				t.Fatalf("enter produced %T, want a batch", cmd())
			}
			for _, one := range batch {
				if msg, ok := one().(tabs.NavigateMsg); ok {
					nav = msg
				}
			}
			tc.check(t, nav)
		})
	}
}

// TestAHitInAnotherProjectMovesTheWholeWorkspace pins why NavigateMsg carries
// a slug at all: a row opened inside the wrong project's tab is a row nobody
// can find again.
func TestAHitInAnotherProjectMovesTheWholeWorkspace(t *testing.T) {
	m, _, _ := openPalette(t, nil)

	m, _ = step(t, m, tabs.NavigateMsg{Target: tabs.Tasks, Slug: "somewhere-else", TaskID: 9})

	if m.project != "somewhere-else" {
		t.Fatalf("project = %q, want the workspace rescoped to the hit's own", m.project)
	}
	if m.active != tabs.Tasks {
		t.Fatalf("active = %v, want the tab the hit lives in", m.active)
	}
}

func TestOpeningAHitRemembersTheQuery(t *testing.T) {
	hit := data.SearchHit{Kind: data.SearchKindTask, ID: 9, Project: "clarodrive", Title: "preview"}
	m, _, settings := openPalette(t, []data.SearchHit{hit})
	m, _ = typeQuery(t, m, "preview")
	m, _ = step(t, m, searchDoneMsg{gen: m.palette.gen, query: "preview", hits: []data.SearchHit{hit}})

	_, cmd := step(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("enter produced %T, want a batch", cmd())
	}
	for _, one := range batch {
		one()
	}

	raw := settings.Value(SearchHistoryKey)
	var history []string
	if err := json.Unmarshal([]byte(raw), &history); err != nil {
		t.Fatalf("the history is not a list of queries: %q", raw)
	}
	if len(history) != 1 || history[0] != "preview" {
		t.Fatalf("history = %v, want the query at its head", history)
	}
}

func TestTheHistoryKeepsTenWithoutDuplicates(t *testing.T) {
	var history []string
	for i := 0; i < 15; i++ {
		history = rememberQuery(history, string(rune('a'+i)))
	}
	if len(history) != paletteHistoryMax {
		t.Fatalf("history holds %d queries, want %d", len(history), paletteHistoryMax)
	}
	if history[0] != "o" {
		t.Fatalf("history[0] = %q, want the newest query", history[0])
	}

	history = rememberQuery(history, "o")
	if len(history) != paletteHistoryMax {
		t.Fatalf("repeating a query grew the history to %d", len(history))
	}
	if history[0] != "o" || history[1] == "o" {
		t.Fatalf("a repeated query should move to the head, not be duplicated: %v", history)
	}

	if got := rememberQuery(history, "   "); len(got) != len(history) {
		t.Error("a blank query is not worth remembering")
	}
}

func TestTheHistoryIsShownBeforeAnythingIsTyped(t *testing.T) {
	m, _, _ := openPalette(t, nil)
	m = m.WithSearchHistory([]string{"preview 503", "cold start"})

	out := m.View()
	for _, want := range []string{"recent searches", "preview 503", "cold start"} {
		if !strings.Contains(out, want) {
			t.Errorf("the palette omits %q, got:\n%s", want, out)
		}
	}
}

func TestAMalformedHistorySettingIsTreatedAsNone(t *testing.T) {
	if got := DecodeSearchHistory("not json"); got != nil {
		t.Errorf("DecodeSearchHistory(%q) = %v, want none", "not json", got)
	}
	if got := DecodeSearchHistory("  "); got != nil {
		t.Errorf("an empty setting should decode to no history, got %v", got)
	}
	if got := DecodeSearchHistory(`["a","b"]`); len(got) != 2 {
		t.Errorf("a real history decoded to %v", got)
	}
}

func TestASearchThatFailedIsReported(t *testing.T) {
	m, search, _ := openPalette(t, nil)
	search.SetErr(errors.New("database is locked"))
	m, _ = typeQuery(t, m, "preview")

	m, cmd := step(t, m, searchTickMsg{gen: m.palette.gen})
	m, _ = step(t, m, cmd())

	if !strings.Contains(m.View(), "database is locked") {
		t.Fatalf("a failed search should say so, got:\n%s", m.View())
	}
}

func TestAWorkspaceWithNoSearcherSaysSo(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.KoiPond()), "clarodrive")
	m.tree.open = false
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyCtrlK})
	m, _ = typeQuery(t, m, "preview")

	m, cmd := step(t, m, searchTickMsg{gen: m.palette.gen})
	m, _ = step(t, m, cmd())

	if !strings.Contains(m.View(), "no workspace search") {
		t.Fatalf("a workspace with no searcher should say so, got:\n%s", m.View())
	}
}

func TestAQueryThatMatchesNothingSaysSo(t *testing.T) {
	m, _, _ := openPalette(t, nil)
	m, _ = typeQuery(t, m, "nothing")
	m, _ = step(t, m, searchDoneMsg{gen: m.palette.gen, query: "nothing"})

	if !strings.Contains(m.View(), "Nothing matches nothing.") {
		t.Fatalf("an empty result should say so, got:\n%s", m.View())
	}
}

// TestThePaletteHighlightsWhatTheReaderCanSee pins the one thing a workspace
// search cannot supply: FTS5 has no notion of which characters matched, so the
// highlight is computed here against the titles actually on screen.
func TestThePaletteHighlightsWhatTheReaderCanSee(t *testing.T) {
	hits := []data.SearchHit{{Kind: data.SearchKindTask, ID: 1, Title: "preview timeout"}}
	rows := paletteRows(hits, "prev")

	for _, row := range rows {
		if row.hit == nil {
			continue
		}
		if len(row.matched) == 0 {
			t.Fatal("the hit's title was not highlighted at all")
		}
		for _, i := range row.matched {
			if i < 0 || i >= len(row.hit.Title) {
				t.Errorf("matched index %d falls outside %q", i, row.hit.Title)
			}
		}
	}
}

func TestThePaletteSwallowsEveryKeyWhileItIsOpen(t *testing.T) {
	m, _, _ := openPalette(t, nil)
	m.active = tabs.Memory

	// A digit is a tab switch everywhere else; inside the palette it is text.
	m, _ = step(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})

	if m.active != tabs.Memory {
		t.Fatalf("a digit typed into the palette switched to %v", m.active)
	}
	if m.palette.input.Value() != "3" {
		t.Fatalf("the digit did not reach the query: %q", m.palette.input.Value())
	}
}
