package autosync

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/Gentleman-Programming/engram/internal/cloud/constants"
	"github.com/Gentleman-Programming/engram/internal/store"
)

// ─── Fakes for the two-chunk push ────────────────────────────────────────────

// oldServerError stands in for what a cloud image predating engram-projects
// answers: a 400 naming the entity it does not know.
type oldServerError struct{}

func (oldServerError) Error() string {
	return `cloud: mutation push: status 400: unsupported mutation "task"/"upsert"`
}
func (oldServerError) IsUnsupportedEntity() bool { return true }

// splitAwareTransport accepts every batch it understands and rejects the one
// carrying engram-projects entities when rejectProjects is set. It records each
// batch so a test can assert the split actually happened.
type splitAwareTransport struct {
	rejectProjects bool
	batches        [][]MutationEntry
	pushCalls      int32
}

func (t *splitAwareTransport) PushMutations(entries []MutationEntry) (*PushMutationsResult, error) {
	atomic.AddInt32(&t.pushCalls, 1)
	for _, e := range entries {
		switch e.Entity {
		case store.SyncEntityProjectCard, store.SyncEntityTask, store.SyncEntityEvidence,
			store.SyncEntityTaskLink, store.SyncEntityObservationRef:
			if t.rejectProjects {
				return nil, oldServerError{}
			}
		}
	}
	t.batches = append(t.batches, append([]MutationEntry(nil), entries...))
	seqs := make([]int64, len(entries))
	for i := range entries {
		seqs[i] = int64(i)
	}
	return &PushMutationsResult{AcceptedSeqs: seqs}, nil
}

func (t *splitAwareTransport) PullMutations(_ int64, _ int) (*PullMutationsResponse, error) {
	return &PullMutationsResponse{Mutations: []PulledMutation{}}, nil
}

func mixedPendingMutations() []store.SyncMutation {
	return []store.SyncMutation{
		{Seq: 1, Project: "nextcloud", Entity: store.SyncEntityObservation, EntityKey: "obs-1", Op: store.SyncOpUpsert, Payload: `{"sync_id":"obs-1"}`},
		{Seq: 2, Project: "nextcloud", Entity: store.SyncEntityTask, EntityKey: "task-1", Op: store.SyncOpUpsert, Payload: `{"sync_id":"task-1"}`},
		{Seq: 3, Project: "nextcloud", Entity: store.SyncEntityPrompt, EntityKey: "prompt-1", Op: store.SyncOpUpsert, Payload: `{"sync_id":"prompt-1"}`},
		{Seq: 4, Project: "nextcloud", Entity: store.SyncEntityEvidence, EntityKey: "evd-1", Op: store.SyncOpUpsert, Payload: `{"sync_id":"evd-1"}`},
	}
}

// ─── Tests ───────────────────────────────────────────────────────────────────

// TestPushSplitsUpstreamAndProjectsIntoTwoChunks pins the shape RFC section
// 10.2 requires: one request for the entities every server understands, a
// second one for the new ones, upstream first.
func TestPushSplitsUpstreamAndProjectsIntoTwoChunks(t *testing.T) {
	ls := newFakeLocalStore()
	ls.mutations = mixedPendingMutations()
	tr := &splitAwareTransport{}
	mgr := New(ls, tr, DefaultConfig())

	if err := mgr.push(context.Background()); err != nil {
		t.Fatalf("push: %v", err)
	}
	if got := atomic.LoadInt32(&tr.pushCalls); got != 2 {
		t.Fatalf("expected two chunks for one project, got %d push calls", got)
	}
	if len(tr.batches) != 2 {
		t.Fatalf("expected two recorded batches, got %d", len(tr.batches))
	}
	for _, e := range tr.batches[0] {
		if e.Entity == store.SyncEntityTask || e.Entity == store.SyncEntityEvidence {
			t.Fatalf("engram-projects entity %q travelled in the first chunk", e.Entity)
		}
	}
	for _, e := range tr.batches[1] {
		if e.Entity != store.SyncEntityTask && e.Entity != store.SyncEntityEvidence {
			t.Fatalf("upstream entity %q travelled in the second chunk", e.Entity)
		}
	}

	ls.mu.Lock()
	acked := append([]int64(nil), ls.ackedSeqs...)
	ls.mu.Unlock()
	if len(acked) != 4 {
		t.Fatalf("expected all four sequences acked, got %v", acked)
	}
}

// TestPushKeepsUpstreamAckedWhenTheServerRefusesTheNewEntities is the reason
// the split exists at all: against an old cloud, sessions, observations,
// prompts and relations must still get through.
func TestPushKeepsUpstreamAckedWhenTheServerRefusesTheNewEntities(t *testing.T) {
	ls := newFakeLocalStore()
	ls.mutations = mixedPendingMutations()
	tr := &splitAwareTransport{rejectProjects: true}
	mgr := New(ls, tr, DefaultConfig())

	err := mgr.push(context.Background())
	if err == nil {
		t.Fatal("expected the rejected engram-projects chunk to surface as an error")
	}
	var unsupported *unsupportedEntityError
	if !errors.As(err, &unsupported) {
		t.Fatalf("expected an unsupportedEntityError, got %T: %v", err, err)
	}
	if unsupported.pending != 2 {
		t.Fatalf("expected the two engram-projects mutations to stay pending, got %d", unsupported.pending)
	}

	ls.mu.Lock()
	acked := append([]int64(nil), ls.ackedSeqs...)
	ls.mu.Unlock()
	if len(acked) != 2 {
		t.Fatalf("upstream mutations must still be acked; acked=%v", acked)
	}
	for _, seq := range acked {
		if seq == 2 || seq == 4 {
			t.Fatalf("a rejected engram-projects mutation was acked (seq %d); it would be lost", seq)
		}
	}
}

// TestCycleReportsUnsupportedEntityReasonCode checks the operator-visible half:
// `engram cloud status` must say why the backlog is not draining.
func TestCycleReportsUnsupportedEntityReasonCode(t *testing.T) {
	ls := newFakeLocalStore()
	ls.mutations = mixedPendingMutations()
	tr := &splitAwareTransport{rejectProjects: true}
	mgr := New(ls, tr, DefaultConfig())

	mgr.cycle(context.Background())

	if got := mgr.Status().ReasonCode; got != constants.ReasonUnsupportedEntity {
		t.Fatalf("reason_code = %q, want %q", got, constants.ReasonUnsupportedEntity)
	}
}

// TestCycleKeepsTransportFailedForOrdinaryErrors is the negative control: a
// plain transport failure must not be relabelled as an unsupported entity.
func TestCycleKeepsTransportFailedForOrdinaryErrors(t *testing.T) {
	ls := newFakeLocalStore()
	ls.mutations = mixedPendingMutations()
	tr := newFakeTransport()
	tr.pushErr = errors.New("connection refused")
	mgr := New(ls, tr, DefaultConfig())

	mgr.cycle(context.Background())

	if got := mgr.Status().ReasonCode; got != constants.ReasonTransportFailed {
		t.Fatalf("reason_code = %q, want %q", got, constants.ReasonTransportFailed)
	}
}

func TestSplitProjectsMutationsPreservesJournalOrder(t *testing.T) {
	upstream, projects := splitProjectsMutations(mixedPendingMutations())
	if len(upstream) != 2 || upstream[0].Seq != 1 || upstream[1].Seq != 3 {
		t.Fatalf("upstream half lost its order: %+v", upstream)
	}
	if len(projects) != 2 || projects[0].Seq != 2 || projects[1].Seq != 4 {
		t.Fatalf("engram-projects half lost its order: %+v", projects)
	}
}
