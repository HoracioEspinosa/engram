package cloudstore

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/store"
)

// insertRawCloudChunk stores a chunk exactly as an older client pushed it:
// straight into cloud_chunks, with nothing materialized into cloud_mutations.
func insertRawCloudChunk(t *testing.T, cs *CloudStore, project, chunkID, payload string) {
	t.Helper()
	_, err := cs.db.ExecContext(context.Background(), `
		INSERT INTO cloud_chunks (project_name, chunk_id, created_by, payload, sessions_count, observations_count, prompts_count)
		VALUES ($1, $2, $3, $4, 0, 0, 0)`, project, chunkID, "legacy-push", []byte(payload))
	if err != nil {
		t.Fatalf("insert raw cloud chunk %s: %v", chunkID, err)
	}
}

// TestMaterializeChunkMutationsSurfacesEntitiesStuckInOlderChunks pins the
// server-start repair.
//
// A project card has no typed collection, so it travels only as a mutation
// inside the chunk. Chunks written before the push materialized those entities
// still hold them and nothing else does, and ListMutationsSince — the only
// stream a replica can pull — reads cloud_mutations. The data was on the server
// and unreachable.
func TestMaterializeChunkMutationsSurfacesEntitiesStuckInOlderChunks(t *testing.T) {
	cs := openTestCloudStore(t)
	ctx := context.Background()
	project := uniqueCloudstoreTestProject("chunk-materialization")
	cleanupCloudstoreProject(t, cs, project)

	insertRawCloudChunk(t, cs, project, "legacy-chunk-1", `{
		"sessions": [],
		"observations": [],
		"prompts": [],
		"mutations": [
			{"entity":"project_card","entity_key":"card-1","op":"upsert","project":"`+project+`","payload":"{\"project\":\"`+project+`\",\"summary\":\"stuck inside a chunk\"}"},
			{"entity":"project_alias","entity_key":"alias-1","op":"upsert","project":"`+project+`","payload":"{\"project\":\"`+project+`\",\"alias\":\"legacy-name\"}"}
		]
	}`)

	before, _, _, err := cs.ListMutationsSince(ctx, 0, 100, []string{project})
	if err != nil {
		t.Fatalf("ListMutationsSince before: %v", err)
	}
	if len(before) != 0 {
		t.Fatalf("expected the chunk entities to be invisible to the stream before the pass, got %+v", before)
	}

	dryRun, err := cs.MaterializeChunkMutations(ctx, project, false)
	if err != nil {
		t.Fatalf("MaterializeChunkMutations dry run: %v", err)
	}
	if dryRun.Applied || dryRun.Candidates != 2 || dryRun.Materialized != 0 {
		t.Fatalf("unexpected dry-run report: %+v", dryRun)
	}
	if after, _, _, err := cs.ListMutationsSince(ctx, 0, 100, []string{project}); err != nil {
		t.Fatalf("ListMutationsSince after dry run: %v", err)
	} else if len(after) != 0 {
		t.Fatalf("a dry run must write nothing, got %+v", after)
	}

	report, err := cs.MaterializeChunkMutations(ctx, project, true)
	if err != nil {
		t.Fatalf("MaterializeChunkMutations apply: %v", err)
	}
	if !report.Applied || report.Materialized != 2 || report.ChunksScanned != 1 {
		t.Fatalf("unexpected apply report: %+v", report)
	}

	after, _, _, err := cs.ListMutationsSince(ctx, 0, 100, []string{project})
	if err != nil {
		t.Fatalf("ListMutationsSince after: %v", err)
	}
	if len(after) != 2 {
		t.Fatalf("expected both chunk entities in the stream, got %+v", after)
	}
	seen := map[string]string{}
	for _, mutation := range after {
		seen[mutation.Entity] = mutation.EntityKey
	}
	if seen["project_card"] != "card-1" || seen["project_alias"] != "alias-1" {
		t.Fatalf("expected the project card and alias in the stream, got %+v", seen)
	}

	// Running it again writes nothing: the server calls it on every start.
	second, err := cs.MaterializeChunkMutations(ctx, project, true)
	if err != nil {
		t.Fatalf("MaterializeChunkMutations second pass: %v", err)
	}
	if second.Materialized != 0 || second.AlreadyPresent != 2 {
		t.Fatalf("expected the second pass to be a no-op, got %+v", second)
	}
	if final, _, _, err := cs.ListMutationsSince(ctx, 0, 100, []string{project}); err != nil {
		t.Fatalf("ListMutationsSince final: %v", err)
	} else if len(final) != 2 {
		t.Fatalf("expected no duplicates after a second pass, got %d", len(final))
	}
}

// TestMaterializeChunkMutationsSkipsTypedCollections proves the pass does not
// duplicate what the typed collections already materialize: sessions,
// observations and prompts come from their own arrays.
func TestMaterializeChunkMutationsSkipsTypedCollections(t *testing.T) {
	cs := openTestCloudStore(t)
	ctx := context.Background()
	project := uniqueCloudstoreTestProject("chunk-materialization-typed")
	cleanupCloudstoreProject(t, cs, project)

	insertRawCloudChunk(t, cs, project, "typed-chunk-1", `{
		"sessions": [{"id":"sess-1","project":"`+project+`","directory":"/work/x","started_at":"2026-04-29T10:00:00Z"}],
		"observations": [],
		"prompts": [],
		"mutations": [
			{"entity":"session","entity_key":"sess-1","op":"upsert","project":"`+project+`","payload":"{\"id\":\"sess-1\"}"}
		]
	}`)
	insertLegacyCloudMutation(t, cs, project, store.SyncEntitySession, "sess-1", store.SyncOpUpsert, `{"id":"sess-1"}`)

	report, err := cs.MaterializeChunkMutations(ctx, project, true)
	if err != nil {
		t.Fatalf("MaterializeChunkMutations: %v", err)
	}
	if report.Candidates != 0 || report.Materialized != 0 {
		t.Fatalf("expected typed entities to be skipped, got %+v", report)
	}

	mutations, _, _, err := cs.ListMutationsSince(ctx, 0, 100, []string{project})
	if err != nil {
		t.Fatalf("ListMutationsSince: %v", err)
	}
	if len(mutations) != 1 || mutations[0].Entity != store.SyncEntitySession {
		t.Fatalf("expected the single session upsert the chunk write already materialized, got %+v", mutations)
	}
}

// TestMaterializeChunkMutationsRecoversRowsOlderChunksLeftOut is the repair
// side of the ingestion gap.
//
// Chunks written while the dedupe was per entity kept every session,
// observation and prompt mutation their typed collection did not happen to
// carry. Those rows are in cloud_chunks and nowhere else, the client that
// pushed them acked them, and no client can re-push a chunk the server already
// holds, so only a pass over stored chunks gets them back.
func TestMaterializeChunkMutationsRecoversRowsOlderChunksLeftOut(t *testing.T) {
	cs := openTestCloudStore(t)
	ctx := context.Background()
	project := uniqueCloudstoreTestProject("chunk-materialization-gap")
	cleanupCloudstoreProject(t, cs, project)

	// The shape of a real chunk: 56 session mutations, 20 of which the typed
	// collection carries. Those 20 were materialized when the chunk landed;
	// the other 36 were dropped.
	const (
		mutationSessions = 56
		typedSessions    = 20
	)
	sessions := make([]string, 0, typedSessions)
	mutations := make([]string, 0, mutationSessions)
	for i := 1; i <= mutationSessions; i++ {
		sessionID := fmt.Sprintf("sess-%02d", i)
		mutations = append(mutations,
			`{"entity":"session","entity_key":"`+sessionID+`","op":"upsert","project":"`+project+`","payload":"{\"id\":\"`+sessionID+`\"}"}`)
		if i > typedSessions {
			continue
		}
		sessions = append(sessions,
			`{"id":"`+sessionID+`","project":"`+project+`","directory":"/work/x","started_at":"2026-04-29T10:00:00Z"}`)
		insertLegacyCloudMutation(t, cs, project, store.SyncEntitySession, sessionID, store.SyncOpUpsert, `{"id":"`+sessionID+`"}`)
	}
	insertRawCloudChunk(t, cs, project, "legacy-typed-gap", `{
		"sessions": [`+strings.Join(sessions, ",")+`],
		"observations": [],
		"prompts": [],
		"mutations": [`+strings.Join(mutations, ",")+`]
	}`)

	countStreamSessions := func(t *testing.T, stage string) int {
		t.Helper()
		total := 0
		var sinceSeq int64
		for {
			page, hasMore, latestSeq, err := cs.ListMutationsSince(ctx, sinceSeq, 100, []string{project})
			if err != nil {
				t.Fatalf("ListMutationsSince %s: %v", stage, err)
			}
			for _, mutation := range page {
				if mutation.Entity == store.SyncEntitySession {
					total++
				}
			}
			if !hasMore {
				return total
			}
			sinceSeq = latestSeq
		}
	}

	if before := countStreamSessions(t, "before"); before != typedSessions {
		t.Fatalf("expected only the typed sessions in the stream before the pass, got %d", before)
	}

	dryRun, err := cs.MaterializeChunkMutations(ctx, project, false)
	if err != nil {
		t.Fatalf("MaterializeChunkMutations dry run: %v", err)
	}
	if dryRun.Candidates != mutationSessions-typedSessions || dryRun.Materialized != 0 {
		t.Fatalf("unexpected dry-run report: %+v", dryRun)
	}

	report, err := cs.MaterializeChunkMutations(ctx, project, true)
	if err != nil {
		t.Fatalf("MaterializeChunkMutations apply: %v", err)
	}
	if report.Materialized != mutationSessions-typedSessions {
		t.Fatalf("expected the sessions the typed collection never carried, got %+v", report)
	}
	if report.MaterializedByEntity[store.SyncEntitySession] != mutationSessions-typedSessions {
		t.Fatalf("the report must say what it recovered per entity, got %+v", report.MaterializedByEntity)
	}
	if after := countStreamSessions(t, "after"); after != mutationSessions {
		t.Fatalf("expected every session mutation the chunk carried in the stream, got %d", after)
	}

	second, err := cs.MaterializeChunkMutations(ctx, project, true)
	if err != nil {
		t.Fatalf("MaterializeChunkMutations second pass: %v", err)
	}
	if second.Materialized != 0 || second.AlreadyPresent != mutationSessions-typedSessions {
		t.Fatalf("expected the second pass to be a no-op, got %+v", second)
	}
	if len(second.MaterializedByEntity) != 0 {
		t.Fatalf("a no-op pass recovered nothing, so it reports nothing: %+v", second.MaterializedByEntity)
	}
	if final := countStreamSessions(t, "final"); final != mutationSessions {
		t.Fatalf("expected no duplicates after a second pass, got %d", final)
	}
}

// TestMaterializeAllChunkMutationsCoversEveryProject pins the shape the server
// start uses: no project argument, every project that has chunks.
func TestMaterializeAllChunkMutationsCoversEveryProject(t *testing.T) {
	cs := openTestCloudStore(t)
	ctx := context.Background()
	first := uniqueCloudstoreTestProject("chunk-materialization-all-a")
	second := uniqueCloudstoreTestProject("chunk-materialization-all-b")
	cleanupCloudstoreProject(t, cs, first)
	cleanupCloudstoreProject(t, cs, second)

	insertRawCloudChunk(t, cs, first, "all-chunk-a", `{"mutations":[{"entity":"project_card","entity_key":"card-a","op":"upsert","project":"`+first+`","payload":"{}"}]}`)
	insertRawCloudChunk(t, cs, second, "all-chunk-b", `{"mutations":[{"entity":"project_card","entity_key":"card-b","op":"upsert","project":"`+second+`","payload":"{}"}]}`)

	reports, err := cs.MaterializeAllChunkMutations(ctx, true)
	if err != nil {
		t.Fatalf("MaterializeAllChunkMutations: %v", err)
	}
	materialized := map[string]int{}
	for _, report := range reports {
		materialized[report.Project] = report.Materialized
	}
	if materialized[first] != 1 || materialized[second] != 1 {
		t.Fatalf("expected both projects to be materialized, got %+v", materialized)
	}
}
