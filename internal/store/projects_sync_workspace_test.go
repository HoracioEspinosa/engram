package store

import (
	"strings"
	"testing"
)

const (
	fxAlias     = "nextcloud-legacy"
	fxBenchA    = "bench-aaaa000000000001"
	fxBenchB    = "bench-bbbb000000000002"
	fxParentSlg = "acme-umbrella"
)

func aliasPayload(t *testing.T, slug, source, updatedAt string, deletedAt *string) string {
	t.Helper()
	return mustJSONString(t, syncProjectAliasPayload{
		Alias:     fxAlias,
		SyncID:    "alias-0000000000000001",
		Slug:      slug,
		Source:    source,
		CreatedAt: "2026-01-01 09:00:00",
		UpdatedAt: updatedAt,
		DeletedAt: deletedAt,
		Project:   slug,
	})
}

func benchmarkPayload(t *testing.T, syncID string, baseline bool, baselineSetAt *string, capturedAt string, value float64) string {
	t.Helper()
	return mustJSONString(t, syncBenchmarkPayload{
		SyncID:        syncID,
		Project:       fxProject,
		TaskSyncID:    fxTaskA,
		Name:          "login",
		Metric:        "p95",
		Unit:          "ms",
		Direction:     BenchmarkDirectionLower,
		Value:         value,
		Baseline:      baseline,
		BaselineSetAt: baselineSetAt,
		CapturedAt:    capturedAt,
		Source:        "manual",
		CreatedAt:     capturedAt,
	})
}

// seedCardAndTask brings a replica to the point where the entities under test
// have something to hang from.
func seedCardAndTask(t *testing.T, s *Store) {
	t.Helper()
	applyAsReplica(t, s, []SyncMutation{
		mut(SyncEntityProjectCard, fxCardSync, cardPayload(t, "Nextcloud", "2026-01-02 09:00:00", nil, nil)),
		mut(SyncEntityTask, fxTaskA, taskPayload(t, syncTaskPayload{
			SyncID: fxTaskA, JiraKey: strp(fxJiraKey), Title: "tuning",
			CreatedAt: "2026-01-02 09:00:00", UpdatedAt: "2026-01-02 09:00:00",
		})),
	})
}

func aliasRow(t *testing.T, s *Store) (slug, source, deletedAt string) {
	t.Helper()
	if err := s.db.QueryRow(
		`SELECT slug, source, ifnull(deleted_at, '') FROM project_aliases WHERE alias = ?`, fxAlias,
	).Scan(&slug, &source, &deletedAt); err != nil {
		t.Fatalf("read alias row: %v", err)
	}
	return slug, source, deletedAt
}

// TestAliasMergeConverges pins that two replicas that saw the same alias
// history in different orders end up pointing the name at the same project,
// and that a retirement anyone saw is never undone.
func TestAliasMergeConverges(t *testing.T) {
	retired := "2026-01-05 10:00:00"
	forward := []SyncMutation{
		mut(SyncEntityProjectAlias, fxAlias, aliasPayload(t, fxProject, "normalizer", "2026-01-03 10:00:00", nil)),
		mut(SyncEntityProjectAlias, fxAlias, aliasPayload(t, fxProject, "manual", "2026-01-04 10:00:00", nil)),
		mut(SyncEntityProjectAlias, fxAlias, aliasPayload(t, fxProject, "manual", "2026-01-04 10:00:00", &retired)),
	}
	reversed := []SyncMutation{forward[2], forward[0], forward[1]}

	first := seedReplica(t)
	seedCardAndTask(t, first)
	applyAsReplica(t, first, forward)

	second := seedReplica(t)
	seedCardAndTask(t, second)
	applyAsReplica(t, second, reversed)

	slugA, sourceA, deletedA := aliasRow(t, first)
	slugB, sourceB, deletedB := aliasRow(t, second)
	if slugA != slugB || sourceA != sourceB || deletedA != deletedB {
		t.Fatalf("aliases diverged: %s/%s/%s vs %s/%s/%s", slugA, sourceA, deletedA, slugB, sourceB, deletedB)
	}
	if sourceA != "manual" {
		t.Fatalf("source %q, want the later writer's value", sourceA)
	}
	if deletedA != retired {
		t.Fatalf("deleted_at %q, want the retirement to stick", deletedA)
	}

	// A retired alias no longer answers for the project.
	resolution, err := first.ResolveProjectSlug(fxAlias)
	if err != nil {
		t.Fatalf("ResolveProjectSlug: %v", err)
	}
	if resolution.Via == ProjectResolvedViaAlias {
		t.Fatalf("a retired alias must not resolve: %+v", resolution)
	}
}

// TestBenchmarkBaselineMergeConverges pins that two measurements that each
// claimed the baseline settle on the same one, whichever arrived first, and
// that only one of them holds it.
func TestBenchmarkBaselineMergeConverges(t *testing.T) {
	early := "2026-01-03 10:00:00"
	late := "2026-01-04 10:00:00"
	forward := []SyncMutation{
		mut(SyncEntityBenchmark, fxBenchA, benchmarkPayload(t, fxBenchA, true, &early, "2026-01-03 09:00:00", 400)),
		mut(SyncEntityBenchmark, fxBenchB, benchmarkPayload(t, fxBenchB, true, &late, "2026-01-04 09:00:00", 250)),
	}
	reversed := []SyncMutation{forward[1], forward[0]}

	read := func(s *Store) string {
		t.Helper()
		rows, err := s.db.Query(
			`SELECT sync_id, baseline, ifnull(baseline_set_at, '') FROM benchmarks ORDER BY sync_id`)
		if err != nil {
			t.Fatalf("read benchmarks: %v", err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var syncID, setAt string
			var baseline int
			if err := rows.Scan(&syncID, &baseline, &setAt); err != nil {
				t.Fatalf("scan benchmark: %v", err)
			}
			out = append(out, syncID+"|"+setAt+"|"+string(rune('0'+baseline)))
		}
		return strings.Join(out, "\n")
	}

	first := seedReplica(t)
	seedCardAndTask(t, first)
	applyAsReplica(t, first, forward)

	second := seedReplica(t)
	seedCardAndTask(t, second)
	applyAsReplica(t, second, reversed)

	if read(first) != read(second) {
		t.Fatalf("benchmarks diverged:\n%s\n---\n%s", read(first), read(second))
	}

	var baselines int
	if err := first.db.QueryRow(
		`SELECT COUNT(*) FROM benchmarks WHERE baseline = 1 AND deleted_at IS NULL`).Scan(&baselines); err != nil {
		t.Fatalf("count baselines: %v", err)
	}
	if baselines != 1 {
		t.Fatalf("baselines %d, want exactly 1", baselines)
	}
	var winner string
	if err := first.db.QueryRow(
		`SELECT sync_id FROM benchmarks WHERE baseline = 1 AND deleted_at IS NULL`).Scan(&winner); err != nil {
		t.Fatalf("read baseline: %v", err)
	}
	if winner != fxBenchB {
		t.Fatalf("baseline %q, want the later stamp %q", winner, fxBenchB)
	}
}

// TestCardHierarchyMergeBreaksCycle pins that a parent which is legal on the
// sender and a loop here is dropped rather than stored: a tree nothing can walk
// is worse than a card at the top of it, and the decision is recorded.
func TestCardHierarchyMergeBreaksCycle(t *testing.T) {
	s := seedReplica(t)

	parent := mustJSONString(t, syncProjectCardPayload{
		Slug: fxParentSlg, SyncID: "proj-parent00000001", DisplayName: "Umbrella",
		DefaultBranch: "master", JiraProject: "PROJ", GraphPath: "graphify-out/graph.json",
		CreatedAt: "2026-01-01 09:00:00", UpdatedAt: "2026-01-02 09:00:00",
		Project: fxParentSlg, Kind: "umbrella",
	})
	child := mustJSONString(t, syncProjectCardPayload{
		Slug: fxProject, SyncID: fxCardSync, DisplayName: "Nextcloud",
		DefaultBranch: "master", JiraProject: "PROJ", GraphPath: "graphify-out/graph.json",
		CreatedAt: "2026-01-01 09:00:00", UpdatedAt: "2026-01-03 09:00:00",
		Project: fxProject, Kind: "repo", ParentSlug: strp(fxParentSlg), Depth: 1,
	})
	// The umbrella now claims the repo as its own parent, which closes a loop.
	loop := mustJSONString(t, syncProjectCardPayload{
		Slug: fxParentSlg, SyncID: "proj-parent00000001", DisplayName: "Umbrella",
		DefaultBranch: "master", JiraProject: "PROJ", GraphPath: "graphify-out/graph.json",
		CreatedAt: "2026-01-01 09:00:00", UpdatedAt: "2026-01-04 09:00:00",
		Project: fxParentSlg, Kind: "umbrella", ParentSlug: strp(fxProject), Depth: 1,
	})

	applyAsReplica(t, s, []SyncMutation{
		{Entity: SyncEntityProjectCard, EntityKey: "proj-parent00000001", Op: SyncOpUpsert,
			Payload: parent, Project: fxParentSlg, Source: SyncSourceRemote},
		mut(SyncEntityProjectCard, fxCardSync, child),
		{Entity: SyncEntityProjectCard, EntityKey: "proj-parent00000001", Op: SyncOpUpsert,
			Payload: loop, Project: fxParentSlg, Source: SyncSourceRemote},
	})

	umbrella, err := s.GetProjectCard(fxParentSlg)
	if err != nil {
		t.Fatalf("GetProjectCard(umbrella): %v", err)
	}
	if umbrella.ParentSlug != nil {
		t.Fatalf("a parent that closes a loop must be dropped, got %q", *umbrella.ParentSlug)
	}
	if umbrella.Depth != 0 {
		t.Fatalf("depth %d, want 0", umbrella.Depth)
	}
	// The child keeps the parent it legally had.
	repo, err := s.GetProjectCard(fxProject)
	if err != nil {
		t.Fatalf("GetProjectCard(repo): %v", err)
	}
	if repo.ParentSlug == nil || *repo.ParentSlug != fxParentSlg {
		t.Fatalf("the legal parent was lost: %v", repo.ParentSlug)
	}

	var lastError string
	if err := s.db.QueryRow(
		`SELECT ifnull(last_error, '') FROM sync_apply_deferred
		 WHERE entity = ? AND apply_status = 'dead' AND last_error LIKE ?`,
		SyncEntityProjectCard, cardHierarchyCycleReason+"%",
	).Scan(&lastError); err != nil {
		t.Fatalf("the dropped parent must be recorded: %v", err)
	}
	if !strings.Contains(lastError, fxParentSlg) {
		t.Fatalf("last_error %q must name the card", lastError)
	}
}

// TestEvidenceLocationGroupLastWriterWins pins that a file reported in two
// places settles the same way on both replicas, whichever report arrived last.
func TestEvidenceLocationGroupLastWriterWins(t *testing.T) {
	base := syncEvidencePayload{
		SyncID: fxEvidence, Project: fxProject, TaskSyncID: fxTaskA,
		SHA256: strings.Repeat("a", 64), Kind: "png", Proves: "it works",
		CapturedAt: "2026-01-03 09:00:00", CreatedAt: "2026-01-03 09:00:00",
	}
	earlier := base
	earlier.Path = "acme/tuning/evidences/before.png"
	earlier.Category = "evidences"
	earlier.LocationSetAt = strp("2026-01-03 09:00:00")

	later := base
	later.Path = "acme/tuning/evidences-qa/after.png"
	later.Category = "evidences-qa"
	later.LocationSetAt = strp("2026-01-06 09:00:00")

	forward := []SyncMutation{
		mut(SyncEntityEvidence, fxEvidence, mustJSONString(t, earlier)),
		mut(SyncEntityEvidence, fxEvidence, mustJSONString(t, later)),
	}
	reversed := []SyncMutation{forward[1], forward[0]}

	read := func(s *Store) (string, string) {
		t.Helper()
		var path, category string
		if err := s.db.QueryRow(
			`SELECT path, category FROM evidence WHERE sync_id = ?`, fxEvidence).Scan(&path, &category); err != nil {
			t.Fatalf("read evidence: %v", err)
		}
		return path, category
	}

	first := seedReplica(t)
	seedCardAndTask(t, first)
	applyAsReplica(t, first, forward)

	second := seedReplica(t)
	seedCardAndTask(t, second)
	applyAsReplica(t, second, reversed)

	pathA, categoryA := read(first)
	pathB, categoryB := read(second)
	if pathA != pathB || categoryA != categoryB {
		t.Fatalf("evidence location diverged: %s/%s vs %s/%s", pathA, categoryA, pathB, categoryB)
	}
	if pathA != later.Path || categoryA != later.Category {
		t.Fatalf("location %s/%s, want the later report", pathA, categoryA)
	}
}

// TestCardStalenessColumnsStayLocal pins that the graph staleness verdict never
// travels: it describes this working copy, and a replica that shipped its
// answer would be overwriting a fact about a checkout it has never seen.
func TestCardStalenessColumnsStayLocal(t *testing.T) {
	t.Setenv(projectsSyncEnvVar, "1")
	s := newProjectsSchemaTestStore(t)
	if _, _, err := s.UpsertProjectCard(UpsertProjectCardParams{Slug: fxProject}); err != nil {
		t.Fatalf("UpsertProjectCard: %v", err)
	}
	if err := s.StampGraphStaleness(fxProject, "code_changed", 7, "2026-01-05 09:00:00"); err != nil {
		t.Fatalf("StampGraphStaleness: %v", err)
	}
	// A later card edit is what journals the card, staleness columns and all.
	if _, _, err := s.UpsertProjectCard(UpsertProjectCardParams{
		Slug: fxProject, DisplayName: strp("Nextcloud"),
	}); err != nil {
		t.Fatalf("UpsertProjectCard: %v", err)
	}

	rows, err := s.db.Query(
		`SELECT payload FROM sync_mutations WHERE entity = ?`, SyncEntityProjectCard)
	if err != nil {
		t.Fatalf("read mutations: %v", err)
	}
	defer rows.Close()
	found := 0
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			t.Fatalf("scan mutation: %v", err)
		}
		found++
		for _, column := range []string{"graph_stale_reason", "graph_changed_files", "graph_checked_at",
			"code_changed"} {
			if strings.Contains(payload, column) {
				t.Fatalf("payload leaks the local staleness column %q: %s", column, payload)
			}
		}
	}
	if found == 0 {
		t.Fatal("no card mutation was journalled")
	}

	// StampGraphStaleness itself journals nothing: a local check is not an
	// edit of the card.
	before := len(pendingProjectsMutations(t, s))
	if err := s.StampGraphStaleness(fxProject, "docs_only", 0, "2026-01-06 09:00:00"); err != nil {
		t.Fatalf("StampGraphStaleness: %v", err)
	}
	if after := len(pendingProjectsMutations(t, s)); after != before {
		t.Fatalf("stamping staleness journalled %d mutation(s)", after-before)
	}
}

// TestOldCardPayloadKeepsNewLocalFields pins the rollout order: a peer still
// running the previous binary sends a card without the hierarchy fields, and
// that must not wipe what this replica already knows about them.
func TestOldCardPayloadKeepsNewLocalFields(t *testing.T) {
	s := seedReplica(t)
	if _, _, err := s.UpsertProjectCard(UpsertProjectCardParams{
		Slug:        fxProject,
		DisplayName: strp("Nextcloud"),
		Kind:        strPtr("instance"),
		Icon:        strPtr("cod-repo"),
		Color:       strPtr("#1e88e5"),
		Description: strPtr("the first instance"),
	}); err != nil {
		t.Fatalf("UpsertProjectCard: %v", err)
	}
	if err := s.StampGraphStaleness(fxProject, "docs_only", 0, "2026-01-05 09:00:00"); err != nil {
		t.Fatalf("StampGraphStaleness: %v", err)
	}

	// The old payload has no parent_slug, kind, icon, colour or description,
	// and it is older than the local row.
	applyAsReplica(t, s, []SyncMutation{
		mut(SyncEntityProjectCard, fxCardSync, cardPayload(t, "Nextcloud", "2020-01-01 09:00:00", nil, nil)),
	})

	card, err := s.GetProjectCard(fxProject)
	if err != nil {
		t.Fatalf("GetProjectCard: %v", err)
	}
	if card.Kind != "instance" {
		t.Fatalf("kind %q, want instance", card.Kind)
	}
	if card.Icon == nil || *card.Icon != "cod-repo" {
		t.Fatalf("icon %v", card.Icon)
	}
	if card.Color == nil || *card.Color != "#1e88e5" {
		t.Fatalf("color %v", card.Color)
	}
	if card.Description == nil || *card.Description != "the first instance" {
		t.Fatalf("description %v", card.Description)
	}
	if card.GraphStaleReason == nil || *card.GraphStaleReason != "docs_only" {
		t.Fatalf("a pulled card must never touch the local staleness verdict: %v", card.GraphStaleReason)
	}
	if card.GraphCheckedAt == nil || *card.GraphCheckedAt != "2026-01-05 09:00:00" {
		t.Fatalf("graph_checked_at %v", card.GraphCheckedAt)
	}
}
