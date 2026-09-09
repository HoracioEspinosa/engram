package sync

import (
	"strings"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/store"
)

// The offline half of the T-04.05 acceptance criterion: what
// `engram sync --cloud --project <slug>` does to tasks and evidence, exercised
// end to end over the same chunk path the command uses, with an in-memory
// transport standing in for the cloud. What it cannot cover is the other half
// — a real :18081 with a token, a grant, and a second machine — which needs
// credentials this test has no business holding.

func seedProjectsReplica(t *testing.T, s *store.Store) (store.Task, string) {
	t.Helper()
	if err := s.EnrollProject("nextcloud"); err != nil {
		t.Fatalf("EnrollProject: %v", err)
	}
	if _, _, err := s.UpsertProjectCard(store.UpsertProjectCardParams{
		Slug: "nextcloud", DisplayName: ptr("Nextcloud"),
	}); err != nil {
		t.Fatalf("UpsertProjectCard: %v", err)
	}
	result, err := s.UpsertTask(store.UpsertTaskParams{
		Project: "nextcloud", JiraKey: ptr("PROJ-4405"),
		Title: ptr("preview returns 503"), Kind: ptr("bugfix"),
	})
	if err != nil {
		t.Fatalf("UpsertTask: %v", err)
	}
	if err := s.CreateSession("sess-rt", "nextcloud", t.TempDir()); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	obsID, err := s.AddObservation(store.AddObservationParams{
		SessionID: "sess-rt", Type: "decision", Title: "root cause",
		Content: "imaginary was out of memory", Project: "nextcloud", Scope: "project",
	})
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}
	if _, err := s.LinkTaskObservation(store.LinkTaskObservationParams{
		Task: result.Task, ObservationID: obsID, Role: "root_cause",
		KnowledgeRef: ptr("Runbooks/RB-003.md"),
	}); err != nil {
		t.Fatalf("LinkTaskObservation: %v", err)
	}
	sha := strings.Repeat("b", 64)
	if _, _, _, err := s.AddEvidence(store.AddEvidenceParams{
		Task: result.Task, Path: "evidence/preview-503.png", SHA256: sha,
		Kind: "png", Proves: "the preview endpoint answers 503",
	}); err != nil {
		t.Fatalf("AddEvidence: %v", err)
	}
	return result.Task, sha
}

func ptr(v string) *string { return &v }

func TestCloudChunkRoundTrip_ReplicatesTasksAndEvidence(t *testing.T) {
	t.Setenv("ENGRAM_PROJECTS_SYNC", "1")

	source := newTestStore(t)
	task, sha := seedProjectsReplica(t, source)

	transport := newFakeCloudTransport()
	exported, err := NewCloudWithTransport(source, transport, "nextcloud").Export("alice", "nextcloud")
	if err != nil {
		t.Fatalf("cloud export: %v", err)
	}
	if exported.IsEmpty {
		t.Fatal("expected the engram-projects mutations to produce a chunk")
	}
	if exported.MutationsExported == 0 {
		t.Fatalf("chunk carried no mutations: %+v", exported)
	}

	// A second machine: same transport, empty database.
	target := newTestStore(t)
	if err := target.EnrollProject("nextcloud"); err != nil {
		t.Fatalf("EnrollProject (target): %v", err)
	}
	imported, err := NewCloudWithTransport(target, transport, "nextcloud").Import()
	if err != nil {
		t.Fatalf("cloud import: %v", err)
	}
	if imported.ChunksImported != 1 {
		t.Fatalf("expected one imported chunk, got %+v", imported)
	}

	tasks, total, err := target.ListTasks("nextcloud", store.TaskListFilter{})
	if err != nil {
		t.Fatalf("ListTasks on the second replica: %v", err)
	}
	if total != 1 || len(tasks) != 1 {
		t.Fatalf("expected the task to replicate, got total=%d items=%d", total, len(tasks))
	}
	if tasks[0].SyncID != task.SyncID || tasks[0].JiraKey == nil || *tasks[0].JiraKey != "PROJ-4405" {
		t.Fatalf("replicated task does not match the source: %+v", tasks[0])
	}
	if tasks[0].Observations != 1 {
		t.Fatalf("the task<->observation link did not replicate: %+v", tasks[0])
	}

	evidence, evidenceTotal, _, err := target.ListEvidence("nextcloud", store.EvidenceListFilter{})
	if err != nil {
		t.Fatalf("ListEvidence on the second replica: %v", err)
	}
	if evidenceTotal != 1 || len(evidence) != 1 || evidence[0].SHA256 != sha {
		t.Fatalf("evidence did not replicate: total=%d items=%+v", evidenceTotal, evidence)
	}

	card, err := target.GetProjectCard("nextcloud")
	if err != nil {
		t.Fatalf("the project card did not replicate: %v", err)
	}
	if card.DisplayName != "Nextcloud" {
		t.Fatalf("unexpected replicated card: %+v", card)
	}

	// Re-importing the same chunk changes nothing.
	again, err := NewCloudWithTransport(target, transport, "nextcloud").Import()
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if again.ChunksImported != 0 || again.ChunksSkipped != 1 {
		t.Fatalf("expected the second import to be a no-op, got %+v", again)
	}
}

// TestCloudChunkRoundTrip_FlagOffShipsNothing is the control for the test
// above: with ENGRAM_PROJECTS_SYNC off, the very same writes must leave the
// chunk without a single engram-projects mutation. Without this, a green
// round-trip would not prove the flag does anything.
func TestCloudChunkRoundTrip_FlagOffShipsNothing(t *testing.T) {
	t.Setenv("ENGRAM_PROJECTS_SYNC", "0")

	source := newTestStore(t)
	seedProjectsReplica(t, source)

	transport := newFakeCloudTransport()
	exported, err := NewCloudWithTransport(source, transport, "nextcloud").Export("alice", "nextcloud")
	if err != nil {
		t.Fatalf("cloud export: %v", err)
	}

	target := newTestStore(t)
	if err := target.EnrollProject("nextcloud"); err != nil {
		t.Fatalf("EnrollProject (target): %v", err)
	}
	if !exported.IsEmpty {
		if _, err := NewCloudWithTransport(target, transport, "nextcloud").Import(); err != nil {
			t.Fatalf("cloud import: %v", err)
		}
	}

	_, total, err := target.ListTasks("nextcloud", store.TaskListFilter{})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if total != 0 {
		t.Fatalf("ENGRAM_PROJECTS_SYNC=0 must not replicate tasks, got %d", total)
	}
}
