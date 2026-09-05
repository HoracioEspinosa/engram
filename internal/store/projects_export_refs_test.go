package store

import "testing"

func TestObservationExportRefs(t *testing.T) {
	s := newProjectsSchemaTestStore(t)

	r, err := s.UpsertTask(UpsertTaskParams{
		Project: "nextcloud", JiraKey: strp("CDBS-10336"), Title: strp("preview 503"), Kind: strp("incident"),
	})
	if err != nil {
		t.Fatalf("UpsertTask: %v", err)
	}
	if err := s.CreateSession("s1", "nextcloud", ""); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	obsID, err := s.AddObservation(AddObservationParams{
		SessionID: "s1", Type: "decision", Title: "cap previews", Content: "c", Project: "nextcloud",
	})
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}
	commit := "3f9c2a7d1b8e4c6f0a2d9e1b7c5f3a8d2e6b4c1a"
	if _, err := s.LinkTaskObservation(LinkTaskObservationParams{
		Task: r.Task, ObservationID: obsID,
		KnowledgeRef: strp("Services/Nextcloud/Architecture.md#Object Store"),
		RunbookID:    strp("RB-003"),
		GraphRef:     strp("PreviewController::index"),
		GraphCommit:  &commit,
	}); err != nil {
		t.Fatalf("LinkTaskObservation: %v", err)
	}

	obs, err := s.GetObservation(obsID)
	if err != nil {
		t.Fatalf("GetObservation: %v", err)
	}

	refs, err := s.ObservationExportRefs("nextcloud")
	if err != nil {
		t.Fatalf("ObservationExportRefs: %v", err)
	}
	got := refs[obs.SyncID]
	if got.KnowledgeRef != "Services/Nextcloud/Architecture.md#Object Store" {
		t.Fatalf("knowledge_ref = %q", got.KnowledgeRef)
	}
	if got.JiraKey != "CDBS-10336" {
		t.Fatalf("jira_key = %q", got.JiraKey)
	}
	if got.RunbookID != "RB-003" {
		t.Fatalf("runbook_id = %q", got.RunbookID)
	}
	if got.GraphCommit != commit {
		t.Fatalf("graph_commit = %q", got.GraphCommit)
	}

	// Another project's export must not inherit them.
	other, err := s.ObservationExportRefs("middleware")
	if err != nil {
		t.Fatalf("ObservationExportRefs(middleware): %v", err)
	}
	if len(other) != 0 {
		t.Fatalf("refs leaked across projects: %+v", other)
	}

	// An observation with no refs at all is simply absent from the map, and
	// the exporter reads a zero value for it.
	plainID, err := s.AddObservation(AddObservationParams{
		SessionID: "s1", Type: "manual", Title: "no refs", Content: "c", Project: "nextcloud",
	})
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}
	plain, err := s.GetObservation(plainID)
	if err != nil {
		t.Fatalf("GetObservation: %v", err)
	}
	refs, err = s.ObservationExportRefs("")
	if err != nil {
		t.Fatalf("ObservationExportRefs(all): %v", err)
	}
	if entry, ok := refs[plain.SyncID]; ok && !entry.Empty() {
		t.Fatalf("an observation with no refs got %+v", entry)
	}
}

func TestObservationExportRefs_SchemaAbsentIsNotAnError(t *testing.T) {
	s := newProjectsSchemaTestStore(t)
	if err := s.DropProjectsSchema(); err != nil {
		t.Fatalf("DropProjectsSchema: %v", err)
	}
	refs, err := s.ObservationExportRefs("")
	if err != nil {
		t.Fatalf("ObservationExportRefs without the projects schema: %v", err)
	}
	if len(refs) != 0 {
		t.Fatalf("refs = %+v, want empty", refs)
	}
}
