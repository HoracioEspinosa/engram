package store

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
)

// countingReadHooks records every statement the store sends through the read
// seams and then runs it unchanged, so a test can count what a listing costs in
// round trips without changing what the listing returns.
func countingReadHooks(s *Store, statements *[]string) {
	hooks := defaultStoreHooks()
	innerQuery := hooks.query
	innerQueryRow := hooks.queryRow
	hooks.query = func(db queryer, query string, args ...any) (*sql.Rows, error) {
		*statements = append(*statements, query)
		return innerQuery(db, query, args...)
	}
	hooks.queryRow = func(db rowQueryer, query string, args ...any) *sql.Row {
		*statements = append(*statements, query)
		return innerQueryRow(db, query, args...)
	}
	s.hooks = hooks
}

// seedCountedTasks fills project with n tasks, giving task i exactly i linked
// observations and i evidence rows, so a counter that reads the wrong row is
// visible as a wrong number rather than as a coincidence.
func seedCountedTasks(t *testing.T, s *Store, project string, n int) []Task {
	t.Helper()
	if _, _, err := s.UpsertProjectCard(UpsertProjectCardParams{Slug: project}); err != nil {
		t.Fatalf("UpsertProjectCard(%q): %v", project, err)
	}
	sessionID := "session-" + project
	if err := s.CreateSession(sessionID, project, ""); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	out := make([]Task, 0, n)
	for i := 0; i < n; i++ {
		key := fmt.Sprintf("PERF-%d", i)
		title := fmt.Sprintf("counted task %d", i)
		kind := "bugfix"
		res, err := s.UpsertTask(UpsertTaskParams{
			Project: project, JiraKey: &key, Title: &title, Kind: &kind,
		})
		if err != nil {
			t.Fatalf("UpsertTask(%q): %v", key, err)
		}
		for o := 0; o < i; o++ {
			obsID, err := s.AddObservation(AddObservationParams{
				SessionID: sessionID,
				Type:      "discovery",
				Project:   project,
				Title:     fmt.Sprintf("%s note %d", key, o),
				Content:   fmt.Sprintf("content for %s note %d", key, o),
			})
			if err != nil {
				t.Fatalf("AddObservation: %v", err)
			}
			if _, err := s.LinkTaskObservation(LinkTaskObservationParams{
				Task: res.Task, ObservationID: obsID,
			}); err != nil {
				t.Fatalf("LinkTaskObservation: %v", err)
			}
		}
		for e := 0; e < i; e++ {
			if _, _, _, err := s.AddEvidence(AddEvidenceParams{
				Task:   res.Task,
				Path:   fmt.Sprintf("%s/%s/evidences/shot-%d.png", project, key, e),
				SHA256: fmt.Sprintf("%064x", i*1000+e+1),
				Kind:   "png",
				Proves: fmt.Sprintf("%s capture %d", key, e),
			}); err != nil {
				t.Fatalf("AddEvidence: %v", err)
			}
		}
		out = append(out, res.Task)
	}
	return out
}

func TestListTasksIssuesOneQuery(t *testing.T) {
	s := newProjectsSchemaTestStore(t)
	const project = "koi-perf"
	seedCountedTasks(t, s, project, 5)

	var statements []string
	countingReadHooks(s, &statements)

	items, total, err := s.ListTasks(project, TaskListFilter{Limit: 3})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(statements) != 1 {
		t.Fatalf("expected exactly 1 query, got %d:\n%s", len(statements), strings.Join(statements, "\n---\n"))
	}
	if total != 5 {
		t.Fatalf("expected the total to cover the whole filtered set (5), got %d", total)
	}
	if len(items) != 3 {
		t.Fatalf("expected the page to honour the limit (3), got %d", len(items))
	}

	// The seed gives task i exactly i observations and i evidence rows, so a
	// counter that read another task's rows shows up as the wrong number rather
	// than as a plausible one.
	for _, item := range items {
		if item.JiraKey == nil {
			t.Fatalf("expected every seeded task to carry a jira key, got %+v", item)
		}
		var i int
		if _, err := fmt.Sscanf(*item.JiraKey, "PERF-%d", &i); err != nil {
			t.Fatalf("unexpected jira key %q: %v", *item.JiraKey, err)
		}
		if item.Observations != i {
			t.Errorf("%s: expected %d observations, got %d", *item.JiraKey, i, item.Observations)
		}
		if item.Evidence != i {
			t.Errorf("%s: expected %d evidence rows, got %d", *item.JiraKey, i, item.Evidence)
		}
		if item.Title != fmt.Sprintf("counted task %d", i) {
			t.Errorf("%s: expected the full task projection, got title %q", *item.JiraKey, item.Title)
		}
		if !item.StateStale {
			t.Errorf("%s: expected state_stale=true when state_synced_at was never set", *item.JiraKey)
		}
	}
}

func TestListTasksPageReportsTotalAndOffset(t *testing.T) {
	s := newProjectsSchemaTestStore(t)
	const project = "koi-page"
	seedCountedTasks(t, s, project, 5)

	page, err := s.ListTasksPage(project, TaskListFilter{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("ListTasksPage: %v", err)
	}
	if page.Total != 5 {
		t.Errorf("expected total 5, got %d", page.Total)
	}
	if page.Limit != 2 || page.Offset != 2 {
		t.Errorf("expected limit=2 offset=2, got limit=%d offset=%d", page.Limit, page.Offset)
	}
	if len(page.Items) != 2 {
		t.Fatalf("expected 2 items on the page, got %d", len(page.Items))
	}

	// A page past the end still reports how many rows the filter matched:
	// a pager that is told zero has no way back.
	beyond, err := s.ListTasksPage(project, TaskListFilter{Limit: 2, Offset: 99})
	if err != nil {
		t.Fatalf("ListTasksPage(offset=99): %v", err)
	}
	if beyond.Total != 5 {
		t.Errorf("expected total 5 past the end, got %d", beyond.Total)
	}
	if len(beyond.Items) != 0 {
		t.Errorf("expected no items past the end, got %d", len(beyond.Items))
	}

	// The default limit is the one ListTasks already applied.
	def, err := s.ListTasksPage(project, TaskListFilter{})
	if err != nil {
		t.Fatalf("ListTasksPage(default): %v", err)
	}
	if def.Limit != defaultTaskListLimit {
		t.Errorf("expected the default limit %d, got %d", defaultTaskListLimit, def.Limit)
	}
}

func TestFindRunbooksIssuesOneQuery(t *testing.T) {
	s := newProjectsSchemaTestStore(t)
	const project = "koi-runbooks"
	seedCountedCard(t, s, project, 0, 0, 0, 6)

	var statements []string
	countingReadHooks(s, &statements)

	items, total, err := s.FindRunbooks(RunbookFindParams{
		Query: project, IncludeStale: true, Limit: 2,
	})
	if err != nil {
		t.Fatalf("FindRunbooks: %v", err)
	}
	if len(statements) != 1 {
		t.Fatalf("expected exactly 1 query, got %d:\n%s", len(statements), strings.Join(statements, "\n---\n"))
	}
	if total != 6 {
		t.Fatalf("expected the total to cover every match (6), got %d", total)
	}
	if len(items) != 2 {
		t.Fatalf("expected the page to honour the limit (2), got %d", len(items))
	}
	for _, item := range items {
		if item.Project != project || item.Title == "" {
			t.Errorf("incomplete runbook hit: %+v", item)
		}
	}
}

func TestListRunbooksPageReportsTotal(t *testing.T) {
	s := newProjectsSchemaTestStore(t)
	const project = "koi-runbook-page"
	seedCountedCard(t, s, project, 0, 0, 0, 5)

	page, err := s.ListRunbooksPage(project, RunbookListFilter{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("ListRunbooksPage: %v", err)
	}
	if page.Total != 5 {
		t.Errorf("expected total 5, got %d", page.Total)
	}
	if page.Limit != 2 || page.Offset != 2 {
		t.Errorf("expected limit=2 offset=2, got limit=%d offset=%d", page.Limit, page.Offset)
	}
	if len(page.Items) != 2 {
		t.Fatalf("expected 2 items on the page, got %d", len(page.Items))
	}

	def, err := s.ListRunbooksPage(project, RunbookListFilter{})
	if err != nil {
		t.Fatalf("ListRunbooksPage(default): %v", err)
	}
	if def.Limit != defaultRunbookListLimit {
		t.Errorf("expected the default limit %d, got %d", defaultRunbookListLimit, def.Limit)
	}
	if def.Total != 5 || len(def.Items) != 5 {
		t.Errorf("expected every runbook on the default page, got total=%d items=%d", def.Total, len(def.Items))
	}
}

func TestListEvidencePageReportsTotalAndBytes(t *testing.T) {
	s := newProjectsSchemaTestStore(t)
	const project = "koi-evidence-page"
	if _, _, err := s.UpsertProjectCard(UpsertProjectCardParams{Slug: project}); err != nil {
		t.Fatalf("UpsertProjectCard: %v", err)
	}
	key := "EVID-1"
	title := "evidence page"
	kind := "bugfix"
	res, err := s.UpsertTask(UpsertTaskParams{Project: project, JiraKey: &key, Title: &title, Kind: &kind})
	if err != nil {
		t.Fatalf("UpsertTask: %v", err)
	}
	var wantBytes int64
	for i := 0; i < 5; i++ {
		size := int64(100 * (i + 1))
		wantBytes += size
		if _, _, _, err := s.AddEvidence(AddEvidenceParams{
			Task:      res.Task,
			Path:      fmt.Sprintf("%s/%s/evidences/shot-%d.png", project, key, i),
			SHA256:    fmt.Sprintf("%064x", i+1),
			Kind:      "png",
			Proves:    fmt.Sprintf("capture %d", i),
			SizeBytes: &size,
		}); err != nil {
			t.Fatalf("AddEvidence: %v", err)
		}
	}

	page, totalBytes, err := s.ListEvidencePage(project, EvidenceListFilter{Limit: 2, Offset: 1})
	if err != nil {
		t.Fatalf("ListEvidencePage: %v", err)
	}
	if page.Total != 5 {
		t.Errorf("expected total 5, got %d", page.Total)
	}
	if page.Limit != 2 || page.Offset != 1 {
		t.Errorf("expected limit=2 offset=1, got limit=%d offset=%d", page.Limit, page.Offset)
	}
	if len(page.Items) != 2 {
		t.Fatalf("expected 2 items on the page, got %d", len(page.Items))
	}
	// The byte total belongs to the filtered set, not to the page.
	if totalBytes != wantBytes {
		t.Errorf("expected %d bytes across the filtered set, got %d", wantBytes, totalBytes)
	}

	def, _, err := s.ListEvidencePage(project, EvidenceListFilter{})
	if err != nil {
		t.Fatalf("ListEvidencePage(default): %v", err)
	}
	if def.Limit != defaultEvidenceListLimit {
		t.Errorf("expected the default limit %d, got %d", defaultEvidenceListLimit, def.Limit)
	}
}

// runbookIDSeq hands out the three-digit ids runbook_index demands. They are
// unique across this file so two projects seeded in the same test never collide
// on one — the index is keyed by id alone, not by (project, id).
var runbookIDSeq int

func nextRunbookID(t *testing.T) string {
	t.Helper()
	runbookIDSeq++
	if runbookIDSeq > 999 {
		t.Fatalf("ran out of runbook ids: the index accepts RB-000 through RB-999")
	}
	return fmt.Sprintf("RB-%03d", runbookIDSeq)
}

// seedCountedCard gives one project a card with a row in every table the
// counters read, so a batch that mixes two projects up is visible as a wrong
// number rather than as a zero.
func seedCountedCard(t *testing.T, s *Store, project string, observations, tasks, evidence, runbooks int) {
	t.Helper()
	if _, _, err := s.UpsertProjectCard(UpsertProjectCardParams{Slug: project}); err != nil {
		t.Fatalf("UpsertProjectCard(%q): %v", project, err)
	}
	sessionID := "session-" + project
	if err := s.CreateSession(sessionID, project, ""); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	for i := 0; i < observations; i++ {
		id, err := s.AddObservation(AddObservationParams{
			SessionID: sessionID,
			Type:      "discovery",
			Project:   project,
			Title:     fmt.Sprintf("%s note %d", project, i),
			Content:   fmt.Sprintf("content for %s note %d", project, i),
		})
		if err != nil {
			t.Fatalf("AddObservation: %v", err)
		}
		// Every other observation is pinned, so SUM(pinned) has something to
		// disagree with COUNT(*) about.
		if i%2 == 0 {
			if err := s.setObservationPinned(id, true); err != nil {
				t.Fatalf("setObservationPinned: %v", err)
			}
		}
	}
	for i := 0; i < tasks; i++ {
		key := fmt.Sprintf("%s-%d", strings.ToUpper(strings.ReplaceAll(project, "-", "")), i)
		title := fmt.Sprintf("%s task %d", project, i)
		kind := "bugfix"
		params := UpsertTaskParams{Project: project, JiraKey: &key, Title: &title, Kind: &kind}
		// One task in three is closed, so active and total differ.
		if i%3 == 2 {
			done := "done"
			params.State = &done
		}
		res, err := s.UpsertTask(params)
		if err != nil {
			t.Fatalf("UpsertTask(%q): %v", key, err)
		}
		if i == 0 {
			for e := 0; e < evidence; e++ {
				attached := e%2 == 0
				if _, _, _, err := s.AddEvidence(AddEvidenceParams{
					Task:         res.Task,
					Path:         fmt.Sprintf("%s/%s/evidences/shot-%d.png", project, key, e),
					SHA256:       fmt.Sprintf("%064x", len(project)*100000+e+1),
					Kind:         "png",
					Proves:       fmt.Sprintf("%s capture %d", key, e),
					AttachedJira: attached,
				}); err != nil {
					t.Fatalf("AddEvidence: %v", err)
				}
			}
		}
	}
	if runbooks > 0 {
		entries := make([]RunbookIndexEntryInput, 0, runbooks)
		for i := 0; i < runbooks; i++ {
			id := nextRunbookID(t)
			// Every other runbook is old enough to be stale, so the stale
			// counter has something to disagree with the total about.
			age := 1
			status := "verified"
			if i%2 == 1 {
				age = RunbookStaleAgeDays + 1
				status = "outdated"
			}
			entries = append(entries, RunbookIndexEntryInput{
				ID:        id,
				VaultPath: fmt.Sprintf("Runbooks/%s-%d.md", project, i),
				Title:     fmt.Sprintf("%s runbook %d", project, i),
				Service:   project,
				Category:  "auth",
				Status:    status,
				AgeDays:   &age,
			})
		}
		if _, err := s.SyncRunbookIndex(RunbookIndexSyncParams{Source: "vault-fs", Entries: entries}); err != nil {
			t.Fatalf("SyncRunbookIndex: %v", err)
		}
	}
}

func TestProjectCardCountsIssuesTwoQueries(t *testing.T) {
	s := newProjectsSchemaTestStore(t)
	const project = "koi-counts"
	seedCountedCard(t, s, project, 4, 3, 3, 2)

	var statements []string
	countingReadHooks(s, &statements)

	counts, err := s.ProjectCardCounts(project)
	if err != nil {
		t.Fatalf("ProjectCardCounts: %v", err)
	}
	if len(statements) != 2 {
		t.Fatalf("expected exactly 2 queries, got %d:\n%s", len(statements), strings.Join(statements, "\n---\n"))
	}
	want := ProjectCardCounts{
		Observations: 4, Pinned: 2,
		TasksTotal: 3, TasksActive: 2,
		Evidence: 3, EvidenceUnattached: 1,
		Runbooks: 2, RunbooksStale: 1,
	}
	if counts != want {
		t.Fatalf("counts mismatch:\n got %+v\nwant %+v", counts, want)
	}
}

func TestProjectCardCountsBatchMatchesPerCard(t *testing.T) {
	s := newProjectsSchemaTestStore(t)
	slugs := []string{"koi-alpha", "koi-beta", "koi-gamma"}
	seedCountedCard(t, s, slugs[0], 5, 4, 3, 2)
	seedCountedCard(t, s, slugs[1], 2, 1, 0, 1)
	// A card with nothing under it still has to come back, as zeros.
	if _, _, err := s.UpsertProjectCard(UpsertProjectCardParams{Slug: slugs[2]}); err != nil {
		t.Fatalf("UpsertProjectCard: %v", err)
	}

	var statements []string
	countingReadHooks(s, &statements)

	batch, err := s.ProjectCardCountsBatch(slugs)
	if err != nil {
		t.Fatalf("ProjectCardCountsBatch: %v", err)
	}
	if len(statements) != 4 {
		t.Fatalf("expected one query per source table (4), got %d:\n%s",
			len(statements), strings.Join(statements, "\n---\n"))
	}
	s.hooks = defaultStoreHooks()

	for _, slug := range slugs {
		single, err := s.ProjectCardCounts(slug)
		if err != nil {
			t.Fatalf("ProjectCardCounts(%q): %v", slug, err)
		}
		got, ok := batch[slug]
		if !ok {
			t.Fatalf("expected %q in the batch, got %v", slug, batch)
		}
		if got != single {
			t.Errorf("%s: batch %+v disagrees with per-card %+v", slug, got, single)
		}
	}

	// A slug with no card at all is still answered, with zeros, so a caller
	// never has to decide what a missing key meant.
	missing, err := s.ProjectCardCountsBatch([]string{"koi-nowhere"})
	if err != nil {
		t.Fatalf("ProjectCardCountsBatch(missing): %v", err)
	}
	if got, ok := missing["koi-nowhere"]; !ok || got != (ProjectCardCounts{}) {
		t.Errorf("expected zeroed counts for an unknown slug, got %+v ok=%v", got, ok)
	}
}

func TestListProjectCardsIssuesFourQueriesForManyCards(t *testing.T) {
	s := newProjectsSchemaTestStore(t)
	const cards = 30
	for i := 0; i < cards; i++ {
		if _, _, err := s.UpsertProjectCard(UpsertProjectCardParams{Slug: fmt.Sprintf("koi-card-%02d", i)}); err != nil {
			t.Fatalf("UpsertProjectCard: %v", err)
		}
	}
	seedCountedCard(t, s, "koi-card-00", 3, 2, 1, 1)

	var statements []string
	countingReadHooks(s, &statements)

	items, total, err := s.ListProjectCards(true)
	if err != nil {
		t.Fatalf("ListProjectCards: %v", err)
	}
	if total != cards || len(items) != cards {
		t.Fatalf("expected %d cards, got total=%d items=%d", cards, total, len(items))
	}
	// One listing query plus one per source table: the cost no longer grows
	// with the number of cards.
	if len(statements) > 5 {
		t.Fatalf("expected at most 5 queries for %d cards, got %d:\n%s",
			cards, len(statements), strings.Join(statements, "\n---\n"))
	}
	for _, item := range items {
		if item.Counts == nil {
			t.Fatalf("expected counts on every card, got nil for %q", item.Slug)
		}
		if item.Slug == "koi-card-00" && item.Counts.Observations != 3 {
			t.Errorf("expected the seeded card to report 3 observations, got %d", item.Counts.Observations)
		}
	}
}

func TestProjectTreeWithCountsIssuesBoundedQueries(t *testing.T) {
	s := newProjectsSchemaTestStore(t)
	const children = 12
	if _, _, err := s.UpsertProjectCard(UpsertProjectCardParams{Slug: "koi-root"}); err != nil {
		t.Fatalf("UpsertProjectCard: %v", err)
	}
	for i := 0; i < children; i++ {
		slug := fmt.Sprintf("koi-child-%02d", i)
		if _, _, err := s.UpsertProjectCard(UpsertProjectCardParams{Slug: slug}); err != nil {
			t.Fatalf("UpsertProjectCard: %v", err)
		}
		parent := "koi-root"
		if err := s.SetProjectParent(slug, &parent); err != nil {
			t.Fatalf("SetProjectParent(%q): %v", slug, err)
		}
	}
	seedCountedCard(t, s, "koi-child-00", 2, 1, 1, 1)

	var statements []string
	countingReadHooks(s, &statements)

	nodes, err := s.ProjectTree("", true)
	if err != nil {
		t.Fatalf("ProjectTree: %v", err)
	}
	if len(nodes) != children+1 {
		t.Fatalf("expected %d nodes, got %d", children+1, len(nodes))
	}
	// One walk of the tree plus one query per source table, whatever the tree
	// holds.
	if len(statements) > 5 {
		t.Fatalf("expected at most 5 queries for %d nodes, got %d:\n%s",
			len(nodes), len(statements), strings.Join(statements, "\n---\n"))
	}
	for _, node := range nodes {
		if node.Counts == nil {
			t.Fatalf("expected counts on every node, got nil for %q", node.Slug)
		}
		if node.Slug == "koi-child-00" && node.Counts.Observations != 2 {
			t.Errorf("expected the seeded child to report 2 observations, got %d", node.Counts.Observations)
		}
	}

	// A rooted walk pays one lookup more: the root has to be known to exist
	// before the tree below it is worth reading.
	statements = statements[:0]
	if _, err := s.ProjectTree("koi-root", true); err != nil {
		t.Fatalf("ProjectTree(root): %v", err)
	}
	if len(statements) > 6 {
		t.Fatalf("expected at most 6 queries for a rooted walk, got %d:\n%s",
			len(statements), strings.Join(statements, "\n---\n"))
	}
}
