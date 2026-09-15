package store

import (
	"errors"
	"slices"
	"testing"
)

// All tests in this file exercise engram-projects data access against a
// throwaway store opened with t.TempDir()
// (newProjectsSchemaTestStore, defined in projects_schema_test.go). None of
// them ever touch ~/.engram/engram.db.

func strp(v string) *string { return &v }

func TestUpsertProjectCard_CreateAndIdempotentUpdate(t *testing.T) {
	s := newProjectsSchemaTestStore(t)

	card, created, err := s.UpsertProjectCard(UpsertProjectCardParams{Slug: "nextcloud", DisplayName: strp("Nextcloud")})
	if err != nil {
		t.Fatalf("UpsertProjectCard: %v", err)
	}
	if !created {
		t.Fatal("expected created=true on first upsert")
	}
	if card.DisplayName != "Nextcloud" || card.DefaultBranch != "master" || card.JiraProject != "PROJ" {
		t.Fatalf("unexpected defaults: %+v", card)
	}

	// Second upsert with only repo_url set must not touch display_name.
	card2, created2, err := s.UpsertProjectCard(UpsertProjectCardParams{Slug: "nextcloud", RepoURL: strp("https://example/repo")})
	if err != nil {
		t.Fatalf("UpsertProjectCard (update): %v", err)
	}
	if created2 {
		t.Fatal("expected created=false on second upsert")
	}
	if card2.DisplayName != "Nextcloud" {
		t.Fatalf("expected display_name untouched, got %q", card2.DisplayName)
	}
	if card2.RepoURL == nil || *card2.RepoURL != "https://example/repo" {
		t.Fatalf("expected repo_url updated, got %+v", card2.RepoURL)
	}
}

// cardMutationSeqs lists the sequence numbers of the project_card mutations
// waiting in the outbox. Counting rows is not enough: the enqueue supersedes
// the pending mutation of the same row, so a journalled no-op leaves the
// count at one and only the sequence number betrays it.
func cardMutationSeqs(t *testing.T, s *Store) []int64 {
	t.Helper()
	rows, err := s.db.Query(
		`SELECT seq FROM sync_mutations WHERE entity = ? ORDER BY seq`, SyncEntityProjectCard)
	if err != nil {
		t.Fatalf("list card mutations: %v", err)
	}
	defer rows.Close()
	var seqs []int64
	for rows.Next() {
		var seq int64
		if err := rows.Scan(&seq); err != nil {
			t.Fatalf("scan mutation: %v", err)
		}
		seqs = append(seqs, seq)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("list card mutations: %v", err)
	}
	return seqs
}

// TestUpsertProjectCardIsIdempotent pins that restating what a card already
// says costs nothing. A rollout script re-running the same upsert used to move
// updated_at and journal a sync mutation for every card it touched — and under
// last-writer-wins a fresh updated_at carrying no change outranks a real edit
// another replica made just before it.
func TestUpsertProjectCardIsIdempotent(t *testing.T) {
	t.Setenv(projectsSyncEnvVar, "1")
	s := newProjectsSchemaTestStore(t)

	params := UpsertProjectCardParams{
		Slug:        "nextcloud",
		DisplayName: strp("Nextcloud"),
		RepoURL:     strp("git@example:clarodrive.git"),
		JiraProject: strp("CDBS"),
		Owner:       strp("dev-nextcloud"),
		Kind:        strp("repo"),
		Description: strp("Nextcloud server and its apps"),
	}
	if _, created, err := s.UpsertProjectCard(params); err != nil || !created {
		t.Fatalf("UpsertProjectCard: created=%v, err=%v", created, err)
	}

	// Backdate the row so a rewrite cannot hide behind the one-second
	// resolution updated_at is stored at.
	const backdated = "2020-01-01 00:00:00"
	if _, err := s.db.Exec(`UPDATE project_cards SET updated_at = ? WHERE slug = ?`, backdated, "nextcloud"); err != nil {
		t.Fatalf("backdate the card: %v", err)
	}
	before := cardMutationSeqs(t, s)
	if len(before) == 0 {
		t.Fatal("creating the card journalled nothing; the outbox probe is not measuring anything")
	}

	card, created, err := s.UpsertProjectCard(params)
	if err != nil {
		t.Fatalf("second UpsertProjectCard: %v", err)
	}
	if created {
		t.Error("the second upsert reported the card as created")
	}
	if card.UpdatedAt != backdated {
		t.Errorf("updated_at moved to %q on an upsert that changed nothing, want %q", card.UpdatedAt, backdated)
	}
	if after := cardMutationSeqs(t, s); !slices.Equal(after, before) {
		t.Errorf("the outbox holds %v after an upsert that changed nothing, want %v", after, before)
	}

	// The guard must not swallow a real edit.
	changed := params
	changed.Owner = strp("dev-platform")
	edited, _, err := s.UpsertProjectCard(changed)
	if err != nil {
		t.Fatalf("UpsertProjectCard (real edit): %v", err)
	}
	if edited.Owner == nil || *edited.Owner != "dev-platform" {
		t.Fatalf("owner = %v, want dev-platform", edited.Owner)
	}
	if edited.UpdatedAt == backdated {
		t.Error("updated_at stayed backdated across a real edit")
	}
	if after := cardMutationSeqs(t, s); slices.Equal(after, before) {
		t.Error("a real edit journalled no mutation")
	}
}

func TestUpsertProjectCard_MinimalDefaultsDisplayNameToSlug(t *testing.T) {
	s := newProjectsSchemaTestStore(t)
	card, _, err := s.UpsertProjectCard(UpsertProjectCardParams{Slug: "middleware"})
	if err != nil {
		t.Fatalf("UpsertProjectCard: %v", err)
	}
	if card.DisplayName != "middleware" {
		t.Fatalf("expected display_name fallback to slug, got %q", card.DisplayName)
	}
}

// TestProjectKnownSeesCardOnlyProject covers the window between creating a
// project card and writing the first observation under it: the project is real
// and every read tool has to accept it. It also pins the fallback for a store
// that never created the engram-projects tables, where the answer is "not
// known" rather than an error.
func TestProjectKnownSeesCardOnlyProject(t *testing.T) {
	s := newProjectsSchemaTestStore(t)

	known, err := s.ProjectKnown("koi-garden")
	if err != nil {
		t.Fatalf("ProjectKnown before the card: %v", err)
	}
	if known {
		t.Fatal("expected koi-garden to be unknown before its card exists")
	}

	if _, _, err := s.UpsertProjectCard(UpsertProjectCardParams{Slug: "koi-garden"}); err != nil {
		t.Fatalf("UpsertProjectCard: %v", err)
	}
	if exists, err := s.ProjectExists("koi-garden"); err != nil || exists {
		t.Fatalf("ProjectExists = %v, %v; want false, nil for a card without rows", exists, err)
	}

	known, err = s.ProjectKnown("koi-garden")
	if err != nil {
		t.Fatalf("ProjectKnown with a card: %v", err)
	}
	if !known {
		t.Fatal("expected a project with only a card to be known")
	}

	if err := s.DropProjectsSchema(); err != nil {
		t.Fatalf("DropProjectsSchema: %v", err)
	}
	known, err = s.ProjectKnown("koi-garden")
	if err != nil {
		t.Fatalf("ProjectKnown without the projects schema: %v", err)
	}
	if known {
		t.Fatal("expected an unknown project without the projects schema, not an error")
	}
}

func TestGetProjectCard_NoCard(t *testing.T) {
	s := newProjectsSchemaTestStore(t)
	_, err := s.GetProjectCard("ghost")
	if !errors.Is(err, ErrNoProjectCard) {
		t.Fatalf("expected ErrNoProjectCard, got %v", err)
	}
}

func TestProjectCardCounts(t *testing.T) {
	s := newProjectsSchemaTestStore(t)
	if _, _, err := s.UpsertProjectCard(UpsertProjectCardParams{Slug: "nextcloud"}); err != nil {
		t.Fatalf("UpsertProjectCard: %v", err)
	}
	if err := s.CreateSession("s1", "nextcloud", ""); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := s.AddObservation(AddObservationParams{SessionID: "s1", Type: "manual", Title: "t", Content: "c", Project: "nextcloud"}); err != nil {
		t.Fatalf("AddObservation: %v", err)
	}
	result, err := s.UpsertTask(UpsertTaskParams{Project: "nextcloud", JiraKey: strp("PROJ-1"), Title: strp("t"), Kind: strp("bugfix")})
	if err != nil {
		t.Fatalf("UpsertTask: %v", err)
	}

	counts, err := s.ProjectCardCounts("nextcloud")
	if err != nil {
		t.Fatalf("ProjectCardCounts: %v", err)
	}
	if counts.Observations != 1 {
		t.Errorf("expected 1 observation, got %d", counts.Observations)
	}
	if counts.TasksTotal != 1 || counts.TasksActive != 1 {
		t.Errorf("expected 1 active task, got total=%d active=%d", counts.TasksTotal, counts.TasksActive)
	}
	_ = result
}

func TestUpsertTask_PrecedenceAndConflict(t *testing.T) {
	s := newProjectsSchemaTestStore(t)

	// Create via jira_key.
	r1, err := s.UpsertTask(UpsertTaskParams{
		Project: "nextcloud", JiraKey: strp("PROJ-100"), Title: strp("Fix previews"), Kind: strp("incident"),
	})
	if err != nil {
		t.Fatalf("UpsertTask create: %v", err)
	}
	if !r1.Created || !r1.CardCreated {
		t.Fatalf("expected created task + auto-created card, got %+v", r1)
	}

	// Update same task via jira_key, only branch changes.
	r2, err := s.UpsertTask(UpsertTaskParams{Project: "nextcloud", JiraKey: strp("PROJ-100"), Branch: strp("fix/PROJ-100")})
	if err != nil {
		t.Fatalf("UpsertTask update: %v", err)
	}
	if r2.Created {
		t.Fatal("expected update, not create")
	}
	if r2.Task.ID != r1.Task.ID {
		t.Fatalf("expected same task id, got %d vs %d", r2.Task.ID, r1.Task.ID)
	}
	if r2.Task.Branch == nil || *r2.Task.Branch != "fix/PROJ-100" {
		t.Fatalf("expected branch updated, got %+v", r2.Task.Branch)
	}
	if r2.Task.Title != "Fix previews" {
		t.Fatalf("expected title untouched, got %q", r2.Task.Title)
	}

	// Same jira_key under a different project must conflict.
	_, err = s.UpsertTask(UpsertTaskParams{Project: "middleware", JiraKey: strp("PROJ-100"), Title: strp("x"), Kind: strp("bugfix")})
	var conflict *TaskKeyConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("expected TaskKeyConflictError, got %v", err)
	}
	if conflict.ExistingProject != "nextcloud" {
		t.Fatalf("expected existing_project=nextcloud, got %q", conflict.ExistingProject)
	}

	// Missing title/kind on create.
	_, err = s.UpsertTask(UpsertTaskParams{Project: "nextcloud", JiraKey: strp("PROJ-200")})
	var missing *MissingFieldError
	if !errors.As(err, &missing) || missing.Field != "title" {
		t.Fatalf("expected MissingFieldError(title), got %v", err)
	}

	// sdd_change precedence for a brand new task.
	r3, err := s.UpsertTask(UpsertTaskParams{Project: "nextcloud", SDDChange: strp("amx-upgrade"), Title: strp("Upgrade"), Kind: strp("feature")})
	if err != nil || !r3.Created {
		t.Fatalf("UpsertTask via sdd_change: created=%v err=%v", r3.Created, err)
	}
	r4, err := s.UpsertTask(UpsertTaskParams{Project: "nextcloud", SDDChange: strp("amx-upgrade"), PRUrl: strp("https://pr")})
	if err != nil || r4.Created || r4.Task.ID != r3.Task.ID {
		t.Fatalf("expected update of same sdd_change task, got %+v err=%v", r4, err)
	}
}

func TestUpsertTask_JiraStatusDerivesStateAndClosedAt(t *testing.T) {
	s := newProjectsSchemaTestStore(t)

	r, err := s.UpsertTask(UpsertTaskParams{
		Project: "nextcloud", JiraKey: strp("PROJ-300"), Title: strp("t"), Kind: strp("incident"),
		JiraStatus: strp("In Develop"),
	})
	if err != nil {
		t.Fatalf("UpsertTask: %v", err)
	}
	if r.Task.State != "in_progress" {
		t.Fatalf("expected state derived to in_progress, got %q", r.Task.State)
	}
	if r.Task.StateSyncedAt == nil {
		t.Fatal("expected state_synced_at to be set")
	}

	r2, err := s.UpsertTask(UpsertTaskParams{Project: "nextcloud", JiraKey: strp("PROJ-300"), JiraStatus: strp("Done")})
	if err != nil {
		t.Fatalf("UpsertTask transition to done: %v", err)
	}
	if r2.Task.State != "done" {
		t.Fatalf("expected state=done, got %q", r2.Task.State)
	}
	if r2.Task.ClosedAt == nil {
		t.Fatal("expected closed_at set when state becomes done")
	}

	r3, err := s.UpsertTask(UpsertTaskParams{Project: "nextcloud", JiraKey: strp("PROJ-300"), JiraStatus: strp("Reopened")})
	if err != nil {
		t.Fatalf("UpsertTask reopen: %v", err)
	}
	if r3.Task.State != "open" {
		t.Fatalf("expected state=open after Reopened, got %q", r3.Task.State)
	}
	if r3.Task.ClosedAt != nil {
		t.Fatalf("expected closed_at cleared after reopen, got %v", *r3.Task.ClosedAt)
	}
}

func TestListTasks_FiltersAndStale(t *testing.T) {
	s := newProjectsSchemaTestStore(t)
	if _, err := s.UpsertTask(UpsertTaskParams{Project: "nextcloud", JiraKey: strp("PROJ-1"), Title: strp("Active one"), Kind: strp("bugfix")}); err != nil {
		t.Fatalf("UpsertTask: %v", err)
	}
	if _, err := s.UpsertTask(UpsertTaskParams{Project: "nextcloud", JiraKey: strp("PROJ-2"), Title: strp("Closed one"), Kind: strp("bugfix"), State: strp("done")}); err != nil {
		t.Fatalf("UpsertTask: %v", err)
	}

	items, total, err := s.ListTasks("nextcloud", TaskListFilter{})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("expected default active filter to exclude done task, got total=%d items=%d", total, len(items))
	}
	if !items[0].StateStale {
		t.Fatal("expected state_stale=true when state_synced_at was never set")
	}

	itemsAll, totalAll, err := s.ListTasks("nextcloud", TaskListFilter{State: "done"})
	if err != nil {
		t.Fatalf("ListTasks(state=done): %v", err)
	}
	if totalAll != 1 || len(itemsAll) != 1 {
		t.Fatalf("expected 1 done task, got total=%d items=%d", totalAll, len(itemsAll))
	}

	byQuery, _, err := s.ListTasks("nextcloud", TaskListFilter{State: "active", Query: "Active"})
	if err != nil {
		t.Fatalf("ListTasks(query): %v", err)
	}
	if len(byQuery) != 1 || byQuery[0].JiraKey == nil || *byQuery[0].JiraKey != "PROJ-1" {
		t.Fatalf("expected FTS query to find PROJ-1, got %+v", byQuery)
	}
}

func TestResolveTaskRef_AllForms(t *testing.T) {
	s := newProjectsSchemaTestStore(t)
	r, err := s.UpsertTask(UpsertTaskParams{
		Project: "nextcloud", JiraKey: strp("PROJ-42"), SDDChange: strp("proj-42"), Title: strp("t"), Kind: strp("incident"),
	})
	if err != nil {
		t.Fatalf("UpsertTask: %v", err)
	}

	byJira, err := s.ResolveTaskRef("nextcloud", "PROJ-42")
	if err != nil || byJira.ID != r.Task.ID {
		t.Fatalf("resolve by jira_key failed: %+v err=%v", byJira, err)
	}
	bySync, err := s.ResolveTaskRef("nextcloud", r.Task.SyncID)
	if err != nil || bySync.ID != r.Task.ID {
		t.Fatalf("resolve by sync_id failed: %+v err=%v", bySync, err)
	}
	byID, err := s.ResolveTaskRef("nextcloud", "#"+itoa(r.Task.ID))
	if err != nil || byID.ID != r.Task.ID {
		t.Fatalf("resolve by #id failed: %+v err=%v", byID, err)
	}
	byChange, err := s.ResolveTaskRef("nextcloud", "change:proj-42")
	if err != nil || byChange.ID != r.Task.ID {
		t.Fatalf("resolve by change: failed: %+v err=%v", byChange, err)
	}
	if _, err := s.ResolveTaskRef("nextcloud", "PROJ-999"); !errors.Is(err, ErrUnknownTask) {
		t.Fatalf("expected ErrUnknownTask, got %v", err)
	}
	// Scoped to project: the same #id under another project must not resolve.
	if _, err := s.ResolveTaskRef("middleware", "#"+itoa(r.Task.ID)); !errors.Is(err, ErrUnknownTask) {
		t.Fatalf("expected ErrUnknownTask across projects, got %v", err)
	}
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b []byte
	for v > 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

func TestLinkTaskObservation_CrossProjectRejected(t *testing.T) {
	s := newProjectsSchemaTestStore(t)
	r, err := s.UpsertTask(UpsertTaskParams{Project: "nextcloud", JiraKey: strp("PROJ-1"), Title: strp("t"), Kind: strp("bugfix")})
	if err != nil {
		t.Fatalf("UpsertTask: %v", err)
	}
	if err := s.CreateSession("s1", "middleware", ""); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	obsID, err := s.AddObservation(AddObservationParams{SessionID: "s1", Type: "manual", Title: "t", Content: "c", Project: "middleware"})
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}
	_, err = s.LinkTaskObservation(LinkTaskObservationParams{Task: r.Task, ObservationID: obsID})
	if !errors.Is(err, ErrCrossProjectLink) {
		t.Fatalf("expected ErrCrossProjectLink, got %v", err)
	}
}

func TestLinkTaskObservation_RefsAndRoleDefault(t *testing.T) {
	s := newProjectsSchemaTestStore(t)
	r, err := s.UpsertTask(UpsertTaskParams{Project: "nextcloud", JiraKey: strp("PROJ-1"), Title: strp("t"), Kind: strp("incident")})
	if err != nil {
		t.Fatalf("UpsertTask: %v", err)
	}
	if err := s.CreateSession("s1", "nextcloud", ""); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	obsID, err := s.AddObservation(AddObservationParams{
		SessionID: "s1", Type: "bugfix", Title: "root cause", Content: "c", Project: "nextcloud", TopicKey: "incident/PROJ-1",
	})
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}

	commit := "3f9c2a7d1b8e4c6f0a2d9e1b7c5f3a8d2e6b4c1a"
	result, err := s.LinkTaskObservation(LinkTaskObservationParams{
		Task: r.Task, ObservationID: obsID,
		KnowledgeRef: strp("Services/Nextcloud/Architecture.md"),
		GraphRef:     strp("ObjectStoreStorage::writeStream"),
		GraphCommit:  &commit,
	})
	if err != nil {
		t.Fatalf("LinkTaskObservation: %v", err)
	}
	if !result.Linked {
		t.Fatal("expected linked=true on first link")
	}
	if result.Role != "root_cause" {
		t.Fatalf("expected role defaulted to root_cause from topic_key, got %q", result.Role)
	}
	if result.RefsAdded != 2 {
		t.Fatalf("expected 2 refs added, got %d (%+v)", result.RefsAdded, result.Refs)
	}

	// graph_ref without graph_commit must fail.
	obsID2, err := s.AddObservation(AddObservationParams{SessionID: "s1", Type: "manual", Title: "t2", Content: "c", Project: "nextcloud"})
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}
	_, err = s.LinkTaskObservation(LinkTaskObservationParams{Task: r.Task, ObservationID: obsID2, GraphRef: strp("Foo::bar")})
	if !errors.Is(err, ErrGraphCommitRequired) {
		t.Fatalf("expected ErrGraphCommitRequired, got %v", err)
	}

	// Re-linking the same pair reports linked=false.
	result2, err := s.LinkTaskObservation(LinkTaskObservationParams{Task: r.Task, ObservationID: obsID})
	if err != nil {
		t.Fatalf("LinkTaskObservation (again): %v", err)
	}
	if result2.Linked {
		t.Fatal("expected linked=false when the link already existed")
	}
}

func TestAddEvidence_DuplicateAndLimits(t *testing.T) {
	s := newProjectsSchemaTestStore(t)
	r, err := s.UpsertTask(UpsertTaskParams{Project: "nextcloud", JiraKey: strp("PROJ-1"), Title: strp("t"), Kind: strp("incident")})
	if err != nil {
		t.Fatalf("UpsertTask: %v", err)
	}
	sha := "9f2b1c0a7e4d5b6c8a1f3e2d4c5b6a7f8e9d0c1b2a3f4e5d6c7b8a9f0e1d2c3b"
	oversized := int64(3_000_000)
	ev, dup, limits, err := s.AddEvidence(AddEvidenceParams{
		Task: r.Task, Path: "acme/PROJ-1/shot.png", SHA256: sha, Kind: "png", Proves: "it works",
		SizeBytes: &oversized,
	})
	if err != nil {
		t.Fatalf("AddEvidence: %v", err)
	}
	if dup {
		t.Fatal("expected duplicate=false on first insert")
	}
	if limits.OK {
		t.Fatalf("expected png over 2097152 bytes to violate the limit, got %+v", limits)
	}
	if ev.SyncID == "" {
		t.Fatal("expected sync_id assigned")
	}

	ev2, dup2, _, err := s.AddEvidence(AddEvidenceParams{
		Task: r.Task, Path: "acme/PROJ-1/shot.png", SHA256: sha, Kind: "png", Proves: "it works (retry)",
	})
	if err != nil {
		t.Fatalf("AddEvidence (duplicate): %v", err)
	}
	if !dup2 {
		t.Fatal("expected duplicate=true on the second insert with the same (task, sha256)")
	}
	if ev2.ID != ev.ID {
		t.Fatalf("expected duplicate to return the original row, got %d vs %d", ev2.ID, ev.ID)
	}
}

func TestListEvidence(t *testing.T) {
	s := newProjectsSchemaTestStore(t)
	r, err := s.UpsertTask(UpsertTaskParams{Project: "nextcloud", JiraKey: strp("PROJ-1"), Title: strp("t"), Kind: strp("incident")})
	if err != nil {
		t.Fatalf("UpsertTask: %v", err)
	}
	size := int64(1000)
	if _, _, _, err := s.AddEvidence(AddEvidenceParams{
		Task: r.Task, Path: "a.png", SHA256: "9f2b1c0a7e4d5b6c8a1f3e2d4c5b6a7f8e9d0c1b2a3f4e5d6c7b8a9f0e1d2c3b",
		Kind: "png", Proves: "p1", SizeBytes: &size, AttachedJira: true,
	}); err != nil {
		t.Fatalf("AddEvidence: %v", err)
	}

	items, total, totalBytes, err := s.ListEvidence("nextcloud", EvidenceListFilter{TaskSyncID: r.Task.SyncID})
	if err != nil {
		t.Fatalf("ListEvidence: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("expected 1 evidence item, got total=%d items=%d", total, len(items))
	}
	if totalBytes != 1000 {
		t.Fatalf("expected total_bytes=1000, got %d", totalBytes)
	}
	if items[0].JiraKey == nil || *items[0].JiraKey != "PROJ-1" {
		t.Fatalf("expected jira_key joined from task, got %+v", items[0].JiraKey)
	}
	// The Evidence tab deep-links from a task's numeric row id, the same way
	// tabs.NavigateMsg.ObservationID already does for Memory, so this query
	// filters by e.task_id directly; TaskSyncID alone cannot serve that
	// without a second lookup.
	if items[0].TaskID != r.Task.ID {
		t.Fatalf("expected task_id joined from the insert, got %d want %d", items[0].TaskID, r.Task.ID)
	}
}

// TestListEvidenceFiltersByTaskID pins the query (`e.task_id = ?2`) behind
// the Evidence tab's tabs.NavigateMsg.TaskID deep link (from the "e" key):
// it filters by the numeric task id, not by TaskSyncID.
func TestListEvidenceFiltersByTaskID(t *testing.T) {
	s := newProjectsSchemaTestStore(t)
	r1, err := s.UpsertTask(UpsertTaskParams{Project: "nextcloud", JiraKey: strp("PROJ-1"), Title: strp("t1"), Kind: strp("incident")})
	if err != nil {
		t.Fatalf("UpsertTask 1: %v", err)
	}
	r2, err := s.UpsertTask(UpsertTaskParams{Project: "nextcloud", JiraKey: strp("PROJ-2"), Title: strp("t2"), Kind: strp("incident")})
	if err != nil {
		t.Fatalf("UpsertTask 2: %v", err)
	}
	if _, _, _, err := s.AddEvidence(AddEvidenceParams{
		Task: r1.Task, Path: "a.png", SHA256: "9f2b1c0a7e4d5b6c8a1f3e2d4c5b6a7f8e9d0c1b2a3f4e5d6c7b8a9f0e1d2c3b",
		Kind: "png", Proves: "p1",
	}); err != nil {
		t.Fatalf("AddEvidence (task 1): %v", err)
	}
	if _, _, _, err := s.AddEvidence(AddEvidenceParams{
		Task: r2.Task, Path: "b.png", SHA256: "8f2b1c0a7e4d5b6c8a1f3e2d4c5b6a7f8e9d0c1b2a3f4e5d6c7b8a9f0e1d2c4c",
		Kind: "png", Proves: "p2",
	}); err != nil {
		t.Fatalf("AddEvidence (task 2): %v", err)
	}

	items, total, _, err := s.ListEvidence("nextcloud", EvidenceListFilter{TaskID: r1.Task.ID})
	if err != nil {
		t.Fatalf("ListEvidence: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("expected 1 evidence item scoped to task 1, got total=%d items=%d", total, len(items))
	}
	if items[0].Path != "a.png" {
		t.Fatalf("expected task 1's evidence only, got %+v", items[0])
	}
}

func TestSyncRunbookIndex_SkipsTemplatesAndInvalidStatus(t *testing.T) {
	s := newProjectsSchemaTestStore(t)
	result, err := s.SyncRunbookIndex(RunbookIndexSyncParams{
		Source: "knowledge-mcp",
		Entries: []RunbookIndexEntryInput{
			{
				ID: "RB-003", VaultPath: "Runbooks/Performance/RB-003.md", Title: "Preview slow", Service: "nextcloud",
				Category: "performance", Status: "verified", Symptoms: []string{"503 on preview"}, NeedsReview: boolp(true),
			},
			{
				ID: "RB-000", VaultPath: "Runbooks/Templates/Auth Issue Template.md", Title: "tpl", Service: "middleware",
				Category: "auth", Status: "draft", Tags: []string{"template"},
			},
			{
				ID: "RB-004", VaultPath: "Runbooks/Foo.md", Title: "bad status", Service: "nextcloud",
				Category: "auth", Status: "open",
			},
		},
	})
	if err != nil {
		t.Fatalf("SyncRunbookIndex: %v", err)
	}
	if result.Upserted != 1 {
		t.Fatalf("expected 1 upserted, got %d", result.Upserted)
	}
	if len(result.Skipped) != 2 {
		t.Fatalf("expected 2 skipped entries, got %+v", result.Skipped)
	}
	if result.StaleCount != 1 {
		t.Fatalf("expected stale_count=1 (needs_review), got %d", result.StaleCount)
	}

	reasons := map[string]string{}
	for _, sk := range result.Skipped {
		reasons[sk.ID] = sk.Reason
	}
	if reasons["RB-000"] != "template" {
		t.Errorf("expected RB-000 skipped as template, got %q", reasons["RB-000"])
	}
	if reasons["RB-004"] != "invalid_status" {
		t.Errorf("expected RB-004 skipped as invalid_status, got %q", reasons["RB-004"])
	}

	// Re-sending the identical valid entry reports unchanged, not upserted.
	result2, err := s.SyncRunbookIndex(RunbookIndexSyncParams{
		Source: "knowledge-mcp",
		Entries: []RunbookIndexEntryInput{
			{
				ID: "RB-003", VaultPath: "Runbooks/Performance/RB-003.md", Title: "Preview slow", Service: "nextcloud",
				Category: "performance", Status: "verified", Symptoms: []string{"503 on preview"}, NeedsReview: boolp(true),
			},
		},
	})
	if err != nil {
		t.Fatalf("SyncRunbookIndex (resend): %v", err)
	}
	if result2.Unchanged != 1 || result2.Upserted != 0 {
		t.Fatalf("expected unchanged=1 upserted=0 on resend, got %+v", result2)
	}
}

func TestSyncRunbookIndex_PruneMissing(t *testing.T) {
	s := newProjectsSchemaTestStore(t)
	if _, err := s.SyncRunbookIndex(RunbookIndexSyncParams{
		Source: "vault-fs",
		Entries: []RunbookIndexEntryInput{
			{ID: "RB-001", VaultPath: "Runbooks/RB-001.md", Title: "one", Service: "nextcloud", Category: "auth", Status: "verified"},
			{ID: "RB-002", VaultPath: "Runbooks/RB-002.md", Title: "two", Service: "nextcloud", Category: "auth", Status: "verified"},
		},
	}); err != nil {
		t.Fatalf("seed SyncRunbookIndex: %v", err)
	}

	result, err := s.SyncRunbookIndex(RunbookIndexSyncParams{
		Project:      "nextcloud",
		Source:       "vault-fs",
		PruneMissing: true,
		Entries: []RunbookIndexEntryInput{
			{ID: "RB-001", VaultPath: "Runbooks/RB-001.md", Title: "one", Service: "nextcloud", Category: "auth", Status: "verified"},
		},
	})
	if err != nil {
		t.Fatalf("SyncRunbookIndex (prune): %v", err)
	}
	if result.Pruned != 1 {
		t.Fatalf("expected 1 pruned, got %d", result.Pruned)
	}

	items, _, err := s.FindRunbooks(RunbookFindParams{Query: "two", Project: "nextcloud"})
	if err != nil {
		t.Fatalf("FindRunbooks: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected pruned runbook RB-002 to no longer be findable, got %+v", items)
	}
}

func TestFindRunbooks(t *testing.T) {
	s := newProjectsSchemaTestStore(t)
	if _, err := s.SyncRunbookIndex(RunbookIndexSyncParams{
		Source: "knowledge-mcp",
		Entries: []RunbookIndexEntryInput{
			{
				ID: "RB-003", VaultPath: "Runbooks/RB-003.md", Title: "Preview endpoint slow or failing", Service: "nextcloud",
				Category: "performance", Pattern: "missing-files", Severity: "P2", Status: "verified",
				Symptoms: []string{"GET /index.php/core/preview returns 503", "previews time out"},
			},
		},
	}); err != nil {
		t.Fatalf("SyncRunbookIndex: %v", err)
	}

	items, total, err := s.FindRunbooks(RunbookFindParams{Query: "preview 503 object store", Project: "nextcloud"})
	if err != nil {
		t.Fatalf("FindRunbooks: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].ID != "RB-003" {
		t.Fatalf("expected RB-003 to match, got total=%d items=%+v", total, items)
	}
}

// TestSearchRunbookIndex is SearchRunbookIndex's counterpart to TestFindRunbooks:
// unlike FindRunbooks (mem_runbook_find's thinner item shape for the MCP
// envelope), the TUI's Runbooks tab renders search results in the exact
// same table as the unfiltered index, so this proves the full
// RunbookIndexRow — including Symptoms, which RunbookFindItem drops — comes
// back ranked by BM25 over runbook_index_fts.
func TestSearchRunbookIndex(t *testing.T) {
	s := newProjectsSchemaTestStore(t)
	if _, err := s.SyncRunbookIndex(RunbookIndexSyncParams{
		Source: "knowledge-mcp",
		Entries: []RunbookIndexEntryInput{
			{
				ID: "RB-003", VaultPath: "Runbooks/RB-003.md", Title: "Preview endpoint slow or failing", Service: "nextcloud",
				Category: "performance", Status: "verified",
				Symptoms: []string{"GET /index.php/core/preview returns 503", "previews time out"},
			},
			{
				ID: "RB-004", VaultPath: "Runbooks/RB-004.md", Title: "BSS subscription desync", Service: "middleware",
				Category: "registration", Status: "verified",
				Symptoms: []string{"external BSS state differs from internal state"},
			},
		},
	}); err != nil {
		t.Fatalf("SyncRunbookIndex: %v", err)
	}

	// Scoped to the project: RB-004 (middleware) never matches even if its
	// symptoms happened to contain the term, because the WHERE clause filters
	// by project before ranking.
	scoped, err := s.SearchRunbookIndex("503", "nextcloud", 10)
	if err != nil {
		t.Fatalf("SearchRunbookIndex (scoped): %v", err)
	}
	if len(scoped) != 1 || scoped[0].ID != "RB-003" {
		t.Fatalf("scoped search = %+v, want only RB-003", scoped)
	}
	if len(scoped[0].Symptoms) != 2 {
		t.Fatalf("scoped[0].Symptoms = %+v, want the 2 seeded lines (RunbookFindItem has no such field)", scoped[0].Symptoms)
	}

	// project="" means every project — the "a" (all projects) toggle.
	all, err := s.SearchRunbookIndex("desync", "", 10)
	if err != nil {
		t.Fatalf("SearchRunbookIndex (all projects): %v", err)
	}
	if len(all) != 1 || all[0].ID != "RB-004" {
		t.Fatalf("all-projects search = %+v, want RB-004", all)
	}
}

// TestListRunbookIndex_EmptyProjectListsEveryProject pins the "a" (all
// projects) toggle the Runbooks tab needs: ListRunbookIndex today (backing
// only the single-project GET /projects/{slug}/runbooks route) filters on
// `project = ?` unconditionally, so an empty project returns zero rows
// instead of "every project" — this is the gap the Runbooks tab's "a" key
// needs closed.
func TestListRunbookIndex_EmptyProjectListsEveryProject(t *testing.T) {
	s := newProjectsSchemaTestStore(t)
	if _, err := s.SyncRunbookIndex(RunbookIndexSyncParams{
		Source: "knowledge-mcp",
		Entries: []RunbookIndexEntryInput{
			{ID: "RB-003", VaultPath: "Runbooks/RB-003.md", Title: "Preview endpoint slow", Service: "nextcloud", Category: "performance", Status: "verified"},
			{ID: "RB-001", VaultPath: "Runbooks/RB-001.md", Title: "Line not recognized", Service: "middleware", Category: "registration", Status: "verified"},
		},
	}); err != nil {
		t.Fatalf("SyncRunbookIndex: %v", err)
	}

	scoped, total, err := s.ListRunbookIndex("nextcloud", RunbookListFilter{})
	if err != nil {
		t.Fatalf("ListRunbookIndex (scoped): %v", err)
	}
	if total != 1 || len(scoped) != 1 || scoped[0].ID != "RB-003" {
		t.Fatalf("scoped listing = %+v (total %d), want only RB-003", scoped, total)
	}

	all, total, err := s.ListRunbookIndex("", RunbookListFilter{})
	if err != nil {
		t.Fatalf("ListRunbookIndex (all projects): %v", err)
	}
	if total != 2 || len(all) != 2 {
		t.Fatalf("all-projects listing = %+v (total %d), want both RB-001 and RB-003", all, total)
	}
}

func boolp(v bool) *bool { return &v }

func TestDefaultJiraProject(t *testing.T) {
	// Same reasoning as the Jira base URL: the fallback key must not name a
	// particular organisation's project.
	t.Setenv("ENGRAM_JIRA_PROJECT", "")
	if got := DefaultJiraProject(); got != "PROJ" {
		t.Fatalf("unset: expected the generic default, got %q", got)
	}

	t.Setenv("ENGRAM_JIRA_PROJECT", "ACME")
	if got := DefaultJiraProject(); got != "ACME" {
		t.Fatalf("set: expected the configured key, got %q", got)
	}

	t.Setenv("ENGRAM_JIRA_PROJECT", "  ")
	if got := DefaultJiraProject(); got != "PROJ" {
		t.Fatalf("blank: expected the default, got %q", got)
	}
}

// TestLinkTaskObservation_GraphCommitRejectionPersistsNothing pins the rule
// that a rejected link leaves no trace. Before the ref candidates were built
// ahead of the INSERT, a graph_ref without its commit returned the error with
// the link row already written under the default role, and the corrected
// retry silently kept that role because of INSERT OR IGNORE.
func TestLinkTaskObservation_GraphCommitRejectionPersistsNothing(t *testing.T) {
	s := newProjectsSchemaTestStore(t)
	if _, _, err := s.UpsertProjectCard(UpsertProjectCardParams{Slug: "nextcloud"}); err != nil {
		t.Fatalf("UpsertProjectCard: %v", err)
	}
	if err := s.CreateSession("sess-link-reject", "nextcloud", t.TempDir()); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	obsID, err := s.AddObservation(AddObservationParams{
		SessionID: "sess-link-reject", Type: "discovery", Title: "t", Content: "c", Project: "nextcloud",
	})
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}
	upserted, err := s.UpsertTask(UpsertTaskParams{
		Project: "nextcloud", JiraKey: strp("CDBS-10336"), Title: strp("previews"), Kind: strp("incident"),
	})
	if err != nil {
		t.Fatalf("UpsertTask: %v", err)
	}

	_, err = s.LinkTaskObservation(LinkTaskObservationParams{
		Task: upserted.Task, ObservationID: obsID, Role: "root_cause",
		GraphRef: strp(`OC\Files\Storage\Wrapper`),
	})
	if !errors.Is(err, ErrGraphCommitRequired) {
		t.Fatalf("err = %v, want ErrGraphCommitRequired", err)
	}

	var links int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM task_observations WHERE task_id = ?`, upserted.Task.ID).
		Scan(&links); err != nil {
		t.Fatalf("count links: %v", err)
	}
	if links != 0 {
		t.Fatalf("a rejected link left %d rows behind", links)
	}

	// The corrected retry now lands the role the caller asked for.
	result, err := s.LinkTaskObservation(LinkTaskObservationParams{
		Task: upserted.Task, ObservationID: obsID, Role: "root_cause",
		GraphRef:    strp(`OC\Files\Storage\Wrapper`),
		GraphCommit: strp("7a79ef43a9570000000000000000000000000000"),
	})
	if err != nil {
		t.Fatalf("LinkTaskObservation: %v", err)
	}
	if !result.Linked || result.Role != "root_cause" {
		t.Fatalf("retry produced linked=%v role=%q, want true/root_cause", result.Linked, result.Role)
	}
}

// ─── TUI Tasks tab ──────────────────────────────────────────────────────────

func TestGetTask_ReturnsAnyProjectByID(t *testing.T) {
	s := newProjectsSchemaTestStore(t)
	upserted, err := s.UpsertTask(UpsertTaskParams{
		Project: "nextcloud", JiraKey: strp("CDBS-10336"), Title: strp("previews"), Kind: strp("incident"),
	})
	if err != nil {
		t.Fatalf("UpsertTask: %v", err)
	}

	got, err := s.GetTask(upserted.Task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.JiraKey == nil || *got.JiraKey != "CDBS-10336" {
		t.Fatalf("GetTask = %+v, want CDBS-10336", got)
	}

	if _, err := s.GetTask(999999); !errors.Is(err, ErrUnknownTask) {
		t.Fatalf("GetTask(missing) err = %v, want ErrUnknownTask", err)
	}
}

// TestUpdateTaskStateMirror_LeavesJiraSyncFieldsUntouched pins the rule that
// a state change is only ever a local mirror: a TUI-driven state change must
// never look like a Jira-confirmed transition, or the state_stale badge the
// dashboard and context pack both rely on would lie.
func TestUpdateTaskStateMirror_LeavesJiraSyncFieldsUntouched(t *testing.T) {
	s := newProjectsSchemaTestStore(t)
	upserted, err := s.UpsertTask(UpsertTaskParams{
		Project: "nextcloud", JiraKey: strp("CDBS-1"), Title: strp("t"), Kind: strp("bugfix"),
		JiraStatus: strp("In Progress"), JiraStatusCategory: strp("indeterminate"),
	})
	if err != nil {
		t.Fatalf("UpsertTask: %v", err)
	}
	before, err := s.GetTask(upserted.Task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if before.StateSyncedAt == nil {
		t.Fatal("expected state_synced_at to be set by the Jira-driven upsert")
	}

	if err := s.UpdateTaskStateMirror(upserted.Task.ID, "review"); err != nil {
		t.Fatalf("UpdateTaskStateMirror: %v", err)
	}

	after, err := s.GetTask(upserted.Task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if after.State != "review" {
		t.Fatalf("state = %q, want review", after.State)
	}
	if after.JiraStatus == nil || *after.JiraStatus != "In Progress" {
		t.Fatalf("jira_status = %v, want untouched \"In Progress\"", after.JiraStatus)
	}
	if after.StateSyncedAt == nil || *after.StateSyncedAt != *before.StateSyncedAt {
		t.Fatalf("state_synced_at = %v, want untouched %v", after.StateSyncedAt, before.StateSyncedAt)
	}
}

// TestUpdateTaskStateMirror_ClearsClosedAtWhenReopening pins the CHECK
// constraint `closed_at IS NULL OR state IN ('done','cancelled')`: mirroring
// a closed task back open without clearing closed_at would violate it.
func TestUpdateTaskStateMirror_ClearsClosedAtWhenReopening(t *testing.T) {
	s := newProjectsSchemaTestStore(t)
	upserted, err := s.UpsertTask(UpsertTaskParams{
		Project: "nextcloud", JiraKey: strp("CDBS-2"), Title: strp("t"), Kind: strp("bugfix"), State: strp("done"),
	})
	if err != nil {
		t.Fatalf("UpsertTask: %v", err)
	}
	closed, err := s.GetTask(upserted.Task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if closed.ClosedAt == nil {
		t.Fatal("expected closed_at to be set for a task created with state=done")
	}

	if err := s.UpdateTaskStateMirror(upserted.Task.ID, "open"); err != nil {
		t.Fatalf("UpdateTaskStateMirror: %v", err)
	}

	reopened, err := s.GetTask(upserted.Task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if reopened.ClosedAt != nil {
		t.Fatalf("closed_at = %v, want cleared after mirroring the state back to open", reopened.ClosedAt)
	}
}

func TestUpdateTaskStateMirror_RejectsAnUnknownState(t *testing.T) {
	s := newProjectsSchemaTestStore(t)
	upserted, err := s.UpsertTask(UpsertTaskParams{
		Project: "nextcloud", JiraKey: strp("CDBS-3"), Title: strp("t"), Kind: strp("bugfix"),
	})
	if err != nil {
		t.Fatalf("UpsertTask: %v", err)
	}

	if err := s.UpdateTaskStateMirror(upserted.Task.ID, "in-flight"); !errors.Is(err, ErrInvalidTaskState) {
		t.Fatalf("err = %v, want ErrInvalidTaskState", err)
	}
}

func TestUpdateTaskStateMirror_UnknownTaskID(t *testing.T) {
	s := newProjectsSchemaTestStore(t)

	if err := s.UpdateTaskStateMirror(999999, "open"); !errors.Is(err, ErrUnknownTask) {
		t.Fatalf("err = %v, want ErrUnknownTask", err)
	}
}
