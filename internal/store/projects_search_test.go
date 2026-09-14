package store

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// seedWorkspace fills one project with a row of every kind the workspace can
// search, all of them mentioning "cookie-flags", so a query that reaches only
// some of the arms is visible as a missing kind rather than as an empty result.
func seedWorkspace(t *testing.T, s *Store, project string) Task {
	t.Helper()
	displayName := project + " cookie-flags service"
	description := "everything about cookie-flags in " + project
	if _, _, err := s.UpsertProjectCard(UpsertProjectCardParams{
		Slug:        project,
		DisplayName: &displayName,
		Description: &description,
	}); err != nil {
		t.Fatalf("UpsertProjectCard(%q): %v", project, err)
	}

	sessionID := "session-" + project
	if err := s.CreateSession(sessionID, project, ""); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := s.AddObservation(AddObservationParams{
		SessionID: sessionID,
		Type:      "discovery",
		Project:   project,
		Title:     "cookie-flags land on the response",
		Content:   "the proxy rewrote the cookie-flags before the browser saw them",
	}); err != nil {
		t.Fatalf("AddObservation: %v", err)
	}

	key := strings.ToUpper(strings.ReplaceAll(project, "-", "")) + "-1"
	title := "harden cookie-flags"
	kind := "bugfix"
	res, err := s.UpsertTask(UpsertTaskParams{
		Project: project, JiraKey: &key, Title: &title, Kind: &kind,
	})
	if err != nil {
		t.Fatalf("UpsertTask: %v", err)
	}

	if _, _, _, err := s.AddEvidence(AddEvidenceParams{
		Task:   res.Task,
		Path:   project + "/" + key + "/evidences/cookie-flags.png",
		SHA256: strings.Repeat("a", 63) + string(rune('a'+len(project)%16)),
		Kind:   "png",
		Proves: "cookie-flags are set on every response",
	}); err != nil {
		t.Fatalf("AddEvidence: %v", err)
	}

	if _, err := s.SyncRunbookIndex(RunbookIndexSyncParams{
		Source: "vault-fs",
		Entries: []RunbookIndexEntryInput{{
			ID:        nextRunbookID(t),
			VaultPath: "Runbooks/" + project + "-cookies.md",
			Title:     "restore cookie-flags after a deploy",
			Service:   project,
			Category:  "auth",
			Status:    "verified",
			Symptoms:  []string{"cookie-flags missing"},
		}},
	}); err != nil {
		t.Fatalf("SyncRunbookIndex: %v", err)
	}

	if _, err := s.AddBenchmark(AddBenchmarkParams{
		Task:       res.Task,
		Name:       "cookie-flags roundtrip",
		Metric:     "p95",
		Unit:       "ms",
		Value:      12.5,
		CapturedAt: "2026-09-14T10:00:00Z",
	}); err != nil {
		t.Fatalf("AddBenchmark: %v", err)
	}
	return res.Task
}

func TestProjectCardsFTSTracksTheCard(t *testing.T) {
	s := newProjectsSchemaTestStore(t)
	if !tableExists(t, s.db, "project_cards_fts") {
		t.Fatal("expected project_cards_fts after migration")
	}

	display := "koi garden"
	if _, _, err := s.UpsertProjectCard(UpsertProjectCardParams{
		Slug: "koi-fts", DisplayName: &display,
	}); err != nil {
		t.Fatalf("UpsertProjectCard: %v", err)
	}
	matches := func(term string) int {
		t.Helper()
		var n int
		if err := s.db.QueryRow(
			`SELECT COUNT(*) FROM project_cards_fts WHERE project_cards_fts MATCH ?`, term,
		).Scan(&n); err != nil {
			t.Fatalf("match %q: %v", term, err)
		}
		return n
	}
	if matches(`"garden"`) != 1 {
		t.Fatal("expected the insert trigger to index the new card")
	}

	renamed := "koi pond"
	if _, _, err := s.UpsertProjectCard(UpsertProjectCardParams{
		Slug: "koi-fts", DisplayName: &renamed,
	}); err != nil {
		t.Fatalf("UpsertProjectCard (rename): %v", err)
	}
	if matches(`"garden"`) != 0 || matches(`"pond"`) != 1 {
		t.Fatal("expected the update trigger to replace the indexed card")
	}
}

func kindsOf(results WorkspaceResults) map[string]int {
	seen := map[string]int{}
	for _, hit := range results.Hits {
		seen[hit.Kind]++
	}
	return seen
}

func TestSearchWorkspaceReturnsAllKinds(t *testing.T) {
	s := newProjectsSchemaTestStore(t)
	seedWorkspace(t, s, "koi-search")

	results, err := s.SearchWorkspace(SearchWorkspaceParams{Query: "cookie-flags"})
	if err != nil {
		t.Fatalf("SearchWorkspace: %v", err)
	}
	seen := kindsOf(results)
	for _, kind := range workspaceKindOrder {
		if seen[kind] == 0 {
			t.Errorf("expected at least one %s hit, got %v", kind, seen)
		}
		if results.Totals[kind] == 0 {
			t.Errorf("expected a total for %s, got %v", kind, results.Totals)
		}
	}

	// Hits arrive grouped: a caller renders one section per kind without
	// having to sort them again.
	position := map[string]int{}
	for i, kind := range workspaceKindOrder {
		position[kind] = i
	}
	last := -1
	for _, hit := range results.Hits {
		if position[hit.Kind] < last {
			t.Fatalf("expected hits grouped by kind, got %s after %s", hit.Kind, workspaceKindOrder[last])
		}
		last = position[hit.Kind]
		if hit.Project == "" || hit.Ref == "" || hit.Title == "" || hit.UpdatedAt == "" {
			t.Errorf("incomplete hit: %+v", hit)
		}
	}
}

func TestSearchWorkspacePrefixMatchesPartialToken(t *testing.T) {
	s := newProjectsSchemaTestStore(t)
	seedWorkspace(t, s, "koi-prefix")

	results, err := s.SearchWorkspace(SearchWorkspaceParams{Query: "cook"})
	if err != nil {
		t.Fatalf("SearchWorkspace: %v", err)
	}
	if len(results.Hits) == 0 {
		t.Fatal("expected a partial token to match the word it starts")
	}
	if seen := kindsOf(results); seen[WorkspaceKindObservation] == 0 {
		t.Errorf("expected the observation to answer a prefix query, got %v", seen)
	}

	// One character is not a search: it matches most of a workspace.
	if _, err := s.SearchWorkspace(SearchWorkspaceParams{Query: "c"}); !errors.Is(err, ErrWorkspaceQueryTooShort) {
		t.Fatalf("expected ErrWorkspaceQueryTooShort, got %v", err)
	}
}

func TestSearchWorkspaceSubtreeIncludesChildren(t *testing.T) {
	s := newProjectsSchemaTestStore(t)
	seedWorkspace(t, s, "koi-parent")
	seedWorkspace(t, s, "koi-kid")
	seedWorkspace(t, s, "koi-stranger")
	parent := "koi-parent"
	if err := s.SetProjectParent("koi-kid", &parent); err != nil {
		t.Fatalf("SetProjectParent: %v", err)
	}

	scoped, err := s.SearchWorkspace(SearchWorkspaceParams{
		Query: "cookie-flags", Project: "koi-parent", PerKind: 25,
	})
	if err != nil {
		t.Fatalf("SearchWorkspace(project): %v", err)
	}
	for _, hit := range scoped.Hits {
		if hit.Project != "koi-parent" {
			t.Fatalf("expected only koi-parent without subtree, got %+v", hit)
		}
	}

	subtree, err := s.SearchWorkspace(SearchWorkspaceParams{
		Query: "cookie-flags", Project: "koi-parent", Subtree: true, PerKind: 25,
	})
	if err != nil {
		t.Fatalf("SearchWorkspace(subtree): %v", err)
	}
	projects := map[string]bool{}
	for _, hit := range subtree.Hits {
		projects[hit.Project] = true
	}
	if !projects["koi-parent"] || !projects["koi-kid"] {
		t.Fatalf("expected the subtree to cover parent and child, got %v", projects)
	}
	if projects["koi-stranger"] {
		t.Fatalf("expected the subtree to stop at the family, got %v", projects)
	}
}

func TestSearchWorkspaceHonoursKindsAndPerKind(t *testing.T) {
	s := newProjectsSchemaTestStore(t)
	task := seedWorkspace(t, s, "koi-kinds")
	// Enough observations that the cap has something to cut. Each one says
	// something different: identical bodies are folded into one by the dedupe
	// window, which would leave the cap nothing to cut.
	for i := 0; i < 8; i++ {
		if _, err := s.AddObservation(AddObservationParams{
			SessionID: "session-koi-kinds",
			Type:      "discovery",
			Project:   "koi-kinds",
			Title:     fmt.Sprintf("cookie-flags note %d", i),
			Content:   fmt.Sprintf("observation %d about cookie-flags", i),
		}); err != nil {
			t.Fatalf("AddObservation: %v", err)
		}
	}
	_ = task

	results, err := s.SearchWorkspace(SearchWorkspaceParams{
		Query:   "cookie-flags",
		Kinds:   []string{WorkspaceKindObservation, WorkspaceKindTask},
		PerKind: 2,
	})
	if err != nil {
		t.Fatalf("SearchWorkspace: %v", err)
	}
	seen := kindsOf(results)
	if seen[WorkspaceKindEvidence] != 0 || seen[WorkspaceKindCard] != 0 || seen[WorkspaceKindBenchmark] != 0 {
		t.Fatalf("expected only the named kinds, got %v", seen)
	}
	if seen[WorkspaceKindObservation] != 2 {
		t.Fatalf("expected the per-kind cap to hold at 2, got %d", seen[WorkspaceKindObservation])
	}
	// The cap limits what comes back, not what was counted.
	if results.Totals[WorkspaceKindObservation] < 9 {
		t.Fatalf("expected the total to report every match, got %d", results.Totals[WorkspaceKindObservation])
	}
	if _, ok := results.Totals[WorkspaceKindEvidence]; ok {
		t.Fatalf("expected totals to cover only the kinds asked for, got %v", results.Totals)
	}

	if _, err := s.SearchWorkspace(SearchWorkspaceParams{
		Query: "cookie-flags", Kinds: []string{"sandwich"},
	}); !errors.Is(err, ErrUnknownWorkspaceKind) {
		t.Fatalf("expected ErrUnknownWorkspaceKind, got %v", err)
	}
}

func TestSearchWorkspaceEscapesQuotes(t *testing.T) {
	s := newProjectsSchemaTestStore(t)
	seedWorkspace(t, s, "koi-quotes")

	for _, query := range []string{`cookie"flags`, `"cookie-flags"`, `cookie-flags "`, `100% "safe"`, `a_b`} {
		results, err := s.SearchWorkspace(SearchWorkspaceParams{Query: query})
		if err != nil {
			t.Fatalf("SearchWorkspace(%q): %v", query, err)
		}
		_ = results
	}

	// A query that is nothing but quotes leaves no token to match. That is an
	// empty result, not a syntax error thrown at the user.
	empty, err := s.SearchWorkspace(SearchWorkspaceParams{Query: `""`})
	if err != nil {
		t.Fatalf(`SearchWorkspace(""): %v`, err)
	}
	if len(empty.Hits) != 0 {
		t.Fatalf("expected no hits for a query with no tokens, got %d", len(empty.Hits))
	}
}
