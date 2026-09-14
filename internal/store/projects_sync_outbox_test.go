package store

import (
	"encoding/json"
	"testing"
)

// pendingCardPayloads returns the payload of every unacked project_card
// mutation, oldest first.
func pendingCardPayloads(t *testing.T, s *Store) []syncProjectCardPayload {
	t.Helper()
	rows, err := s.db.Query(
		`SELECT payload FROM sync_mutations WHERE entity = ? AND acked_at IS NULL ORDER BY seq`,
		SyncEntityProjectCard)
	if err != nil {
		t.Fatalf("list card mutations: %v", err)
	}
	defer rows.Close()

	var out []syncProjectCardPayload
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			t.Fatalf("scan card mutation: %v", err)
		}
		var payload syncProjectCardPayload
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			t.Fatalf("decode card mutation: %v", err)
		}
		out = append(out, payload)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("list card mutations: %v", err)
	}
	return out
}

// TestTheOutboxKeepsOneSnapshotPerRow pins the invariant that makes a round
// trip reproducible.
//
// The journal carries snapshots, not deltas, so an unacked older snapshot of a
// row holds nothing the newer one lacks. Keeping it is worse than redundant:
// both carry the same second in updated_at, and the receiver breaks that tie by
// hashing the payload — so which of the two states survives replication is
// decided by a digest rather than by which write came last, and the answer
// changes with the random sync id the row happens to have.
func TestTheOutboxKeepsOneSnapshotPerRow(t *testing.T) {
	t.Setenv("ENGRAM_PROJECTS_SYNC", "1")
	s := newTestStore(t)
	seedCards(t, s, "koi-garden", "koi-garden-pond-02")

	// Two more writes against the same card, within the same second.
	kind, description := "instance", "el segundo estanque"
	if _, _, err := s.UpsertProjectCard(UpsertProjectCardParams{
		Slug: "koi-garden-pond-02", Kind: &kind, Description: &description,
	}); err != nil {
		t.Fatalf("UpsertProjectCard: %v", err)
	}
	setParent(t, s, "koi-garden-pond-02", "koi-garden")

	pending := pendingCardPayloads(t, s)
	if len(pending) != 2 {
		t.Fatalf("pending card mutations = %d, want one per card: %+v", len(pending), pending)
	}

	var pond *syncProjectCardPayload
	for i := range pending {
		if pending[i].Slug == "koi-garden-pond-02" {
			pond = &pending[i]
		}
	}
	if pond == nil {
		t.Fatalf("no pending mutation for koi-garden-pond-02: %+v", pending)
	}
	// The one that survived is the last state, not the first.
	if pond.ParentSlug == nil || *pond.ParentSlug != "koi-garden" || pond.Depth != 1 {
		t.Fatalf("pending snapshot = parent %v depth %d, want koi-garden at depth 1", pond.ParentSlug, pond.Depth)
	}
	if pond.Kind != "instance" || pond.Description == nil || *pond.Description != description {
		t.Fatalf("pending snapshot lost the metadata: %+v", pond)
	}
}

// TestAnAckedMutationIsNeverSuperseded pins the limit of the coalescing: a
// snapshot that already left this machine is history, and deleting it would
// rewrite what the peer was told.
func TestAnAckedMutationIsNeverSuperseded(t *testing.T) {
	t.Setenv("ENGRAM_PROJECTS_SYNC", "1")
	s := newTestStore(t)
	seedCards(t, s, "koi-garden")

	if _, err := s.db.Exec(
		`UPDATE sync_mutations SET acked_at = '2026-09-14 10:00:00' WHERE entity = ?`,
		SyncEntityProjectCard); err != nil {
		t.Fatalf("ack the first mutation: %v", err)
	}

	owner := "koi"
	if _, _, err := s.UpsertProjectCard(UpsertProjectCardParams{Slug: "koi-garden", Owner: &owner}); err != nil {
		t.Fatalf("UpsertProjectCard: %v", err)
	}

	var total int
	if err := s.db.QueryRow(
		`SELECT count(*) FROM sync_mutations WHERE entity = ?`, SyncEntityProjectCard).Scan(&total); err != nil {
		t.Fatalf("count card mutations: %v", err)
	}
	if total != 2 {
		t.Fatalf("card mutations = %d, want the acked one kept beside the new one", total)
	}
}
