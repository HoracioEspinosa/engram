package store

import (
	"errors"
	"testing"
)

// The canary battery of the knowledge_ref shape rule (RFC §9.1/§9.2). Half of
// it is what the rule MUST reject; the other half is what it must leave
// alone, which is the half a stricter-than-intended rule breaks silently.
func TestNormalizeKnowledgeRef(t *testing.T) {
	accepted := []struct {
		name string
		in   string
		want string
	}{
		{"plain tool path", "Services/Nextcloud/Architecture.md", "Services/Nextcloud/Architecture.md"},
		{"anchor kept verbatim", "Services/Nextcloud/Architecture.md#Object Store", "Services/Nextcloud/Architecture.md#Object Store"},
		{"wikilink brackets stripped", "[[Services/Lookup/Lookup.md]]", "Services/Lookup/Lookup.md"},
		{"vault prefix stripped", "Work/Claro drive/Services/Lookup/Lookup.md", "Services/Lookup/Lookup.md"},
		{"wikilink with prefix and anchor", "[[Work/Claro drive/Services/Lookup/Lookup.md#Search]]", "Services/Lookup/Lookup.md#Search"},
		{"wikilink alias dropped", "[[Services/Lookup/Lookup.md|Lookup]]", "Services/Lookup/Lookup.md"},
		{"surrounding whitespace", "  Runbooks/Performance/RB-003 Preview.md  ", "Runbooks/Performance/RB-003 Preview.md"},
		{"uppercase extension accepted", "Services/Portal/Portal.MD", "Services/Portal/Portal.MD"},
		{"folder named like the memory folder but deeper", "Services/90 - Engram/Notes.md", "Services/90 - Engram/Notes.md"},
		{"dots inside a name are not traversal", "Services/Metadata/v1.2.Notes.md", "Services/Metadata/v1.2.Notes.md"},
	}
	for _, tc := range accepted {
		t.Run("accepts/"+tc.name, func(t *testing.T) {
			got, err := NormalizeKnowledgeRef(tc.in)
			if err != nil {
				t.Fatalf("NormalizeKnowledgeRef(%q) = error %v, want %q", tc.in, err, tc.want)
			}
			if got != tc.want {
				t.Fatalf("NormalizeKnowledgeRef(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}

	rejected := []struct {
		name string
		in   string
		want error
	}{
		{"absolute posix path", "/absolute/vault/Services/Doc.md", ErrKnowledgeRefAbsolute},
		{"home-relative path", "~/vault/Services/Doc.md", ErrKnowledgeRefAbsolute},
		{"absolute path inside a wikilink", "[[/vault/Services/Doc.md]]", ErrKnowledgeRefAbsolute},
		{"bridge export folder", "90 - Engram/engram/nextcloud/decision/42.md", ErrKnowledgeRefNotCurated},
		{"bridge export folder via wikilink prefix", "[[Work/Claro drive/90 - Engram/engram/x.md]]", ErrKnowledgeRefNotCurated},
		{"no extension", "Services/Nextcloud/Architecture", ErrKnowledgeRefInvalid},
		{"not a markdown document", "Services/Nextcloud/diagram.png", ErrKnowledgeRefInvalid},
		{"traversal segment", "Services/../../etc/passwd.md", ErrKnowledgeRefInvalid},
		{"anchor with no path", "#Object Store", ErrKnowledgeRefInvalid},
		{"empty", "   ", ErrKnowledgeRefInvalid},
	}
	for _, tc := range rejected {
		t.Run("rejects/"+tc.name, func(t *testing.T) {
			got, err := NormalizeKnowledgeRef(tc.in)
			if !errors.Is(err, tc.want) {
				t.Fatalf("NormalizeKnowledgeRef(%q) = (%q, %v), want error %v", tc.in, got, err, tc.want)
			}
		})
	}
}

// A rejected knowledge_ref must not leave the reference or the task behind.
func TestUpsertTask_KnowledgeRefIsNormalizedAndValidated(t *testing.T) {
	s := newProjectsSchemaTestStore(t)

	r, err := s.UpsertTask(UpsertTaskParams{
		Project: "nextcloud", JiraKey: strp("PROJ-1"), Title: strp("t"), Kind: strp("incident"),
		KnowledgeRef: strp("[[Work/Claro drive/Services/Nextcloud/Architecture.md#Object Store]]"),
	})
	if err != nil {
		t.Fatalf("UpsertTask: %v", err)
	}
	if r.Task.KnowledgeRef == nil || *r.Task.KnowledgeRef != "Services/Nextcloud/Architecture.md#Object Store" {
		t.Fatalf("knowledge_ref = %v, want the normalized tool form", r.Task.KnowledgeRef)
	}

	_, err = s.UpsertTask(UpsertTaskParams{
		Project: "nextcloud", JiraKey: strp("PROJ-2"), Title: strp("t2"), Kind: strp("incident"),
		KnowledgeRef: strp("90 - Engram/engram/nextcloud/decision/7.md"),
	})
	if !errors.Is(err, ErrKnowledgeRefNotCurated) {
		t.Fatalf("UpsertTask with a memory-folder ref = %v, want ErrKnowledgeRefNotCurated", err)
	}
	if _, err := s.ResolveTaskRef("nextcloud", "PROJ-2"); !errors.Is(err, ErrUnknownTask) {
		t.Fatalf("a rejected knowledge_ref still created the task: %v", err)
	}

	// A task upsert that carries no knowledge_ref at all is untouched.
	if _, err := s.UpsertTask(UpsertTaskParams{
		Project: "nextcloud", JiraKey: strp("PROJ-3"), Title: strp("t3"), Kind: strp("incident"),
	}); err != nil {
		t.Fatalf("UpsertTask without knowledge_ref: %v", err)
	}
}

func TestLinkTaskObservation_KnowledgeRefIsNormalizedAndValidated(t *testing.T) {
	s := newProjectsSchemaTestStore(t)
	r, err := s.UpsertTask(UpsertTaskParams{Project: "nextcloud", JiraKey: strp("PROJ-1"), Title: strp("t"), Kind: strp("incident")})
	if err != nil {
		t.Fatalf("UpsertTask: %v", err)
	}
	if err := s.CreateSession("s1", "nextcloud", ""); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	obsID, err := s.AddObservation(AddObservationParams{
		SessionID: "s1", Type: "bugfix", Title: "root cause", Content: "c", Project: "nextcloud",
	})
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}

	result, err := s.LinkTaskObservation(LinkTaskObservationParams{
		Task: r.Task, ObservationID: obsID,
		KnowledgeRef: strp("[[Work/Claro drive/Services/Nextcloud/Architecture.md]]"),
	})
	if err != nil {
		t.Fatalf("LinkTaskObservation: %v", err)
	}
	if len(result.Refs) != 1 || result.Refs[0].Ref != "Services/Nextcloud/Architecture.md" {
		t.Fatalf("refs = %+v, want the normalized tool form", result.Refs)
	}

	// A second observation with a forbidden ref must be rejected before the
	// link row is written: mem_task_link's whole rejection contract is that
	// nothing is persisted when any reference is invalid.
	obsID2, err := s.AddObservation(AddObservationParams{
		SessionID: "s1", Type: "manual", Title: "t2", Content: "c", Project: "nextcloud",
	})
	if err != nil {
		t.Fatalf("AddObservation: %v", err)
	}
	if _, err := s.LinkTaskObservation(LinkTaskObservationParams{
		Task: r.Task, ObservationID: obsID2, KnowledgeRef: strp("/abs/Doc.md"),
	}); !errors.Is(err, ErrKnowledgeRefAbsolute) {
		t.Fatalf("LinkTaskObservation with an absolute ref = %v, want ErrKnowledgeRefAbsolute", err)
	}
	var links int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM task_observations WHERE observation_id = ?`, obsID2).Scan(&links); err != nil {
		t.Fatalf("count links: %v", err)
	}
	if links != 0 {
		t.Fatalf("a rejected knowledge_ref still wrote %d link row(s)", links)
	}
}
