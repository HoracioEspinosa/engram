package sync

import (
	"fmt"
	"strings"

	"github.com/HoracioEspinosa/engram/internal/store"
)

// The typed collections of a cloud chunk — sessions, observations, prompts —
// are upserts. Everything the cloud needs to apply one has to be on the row, so
// a row that cannot be upserted has no business in them; the rules that decide
// that live here, in one place, because three callers depend on them agreeing:
// the server applies them to accept a push, the exporter applies them to keep
// rows out that cannot meet them, and the upgrade doctor applies them to
// predict the next push. A predicate copied into any of the three turns the
// doctor's `ready` into a guess, which is what it was.

// ObservationIsSoftDeleted reports whether an observation row is a tombstone.
//
// A soft delete keeps the row so the deletion can replicate, and in a cloud
// chunk the deletion travels as its own `delete` mutation. The tombstone
// therefore adds nothing to the typed collections, and it actively harms:
// deletion makes no promise about the row's remaining fields, so a row deleted
// because it had no title is offered to the cloud as an upsert without one and
// the whole chunk is rejected.
//
// This is a cloud-chunk rule. A local filesystem chunk carries no delete
// mutations, so there the tombstone in the collection is the deletion signal
// and the import path turns it back into a delete.
func ObservationIsSoftDeleted(o store.Observation) bool {
	return o.DeletedAt != nil && strings.TrimSpace(*o.DeletedAt) != ""
}

// ChunkRowRejection is one row of a chunk's typed collections that the cloud
// refuses, and the field it is missing.
type ChunkRowRejection struct {
	Entity string
	Key    string
	Field  string
}

// String renders the rejection for an operator: the row has to be found and
// completed by hand, so the identity comes first.
func (r ChunkRowRejection) String() string {
	key := strings.TrimSpace(r.Key)
	if key == "" {
		key = "(no key)"
	}
	return fmt.Sprintf("%s %s: %s is required", r.Entity, key, r.Field)
}

// SessionRowRejection names the field the cloud requires and a session row does
// not carry, or "" when the cloud accepts it.
//
// The id is the identity and is required. The directory is not: it records
// where a session was opened, and a session saved against an explicit project —
// mem_save with a project name — was never opened in a checkout. The local
// store writes those legitimately, so demanding a directory rejected whole
// chunks over rows that had nothing wrong with them.
func SessionRowRejection(s store.Session) string {
	if strings.TrimSpace(s.ID) == "" {
		return "id"
	}
	return ""
}

// ObservationRowRejection names the field the cloud requires and an observation
// row does not carry, or "" when the cloud accepts it.
func ObservationRowRejection(o store.Observation) string {
	switch {
	case strings.TrimSpace(o.SyncID) == "":
		return "sync_id"
	case strings.TrimSpace(o.SessionID) == "":
		return "session_id"
	case strings.TrimSpace(o.Type) == "":
		return "type"
	case strings.TrimSpace(o.Title) == "":
		return "title"
	case strings.TrimSpace(o.Content) == "":
		return "content"
	case strings.TrimSpace(o.Scope) == "":
		return "scope"
	}
	return ""
}

// PromptRowRejection names the field the cloud requires and a prompt row does
// not carry, or "" when the cloud accepts it.
func PromptRowRejection(p store.Prompt) string {
	switch {
	case strings.TrimSpace(p.SyncID) == "":
		return "sync_id"
	case strings.TrimSpace(p.SessionID) == "":
		return "session_id"
	case strings.TrimSpace(p.Content) == "":
		return "content"
	}
	return ""
}

// ValidateChunkRows applies the contract to the typed collections of a chunk
// and reports the first row the cloud refuses, positioned the way the push
// response names it. A rejection is chunk-wide, so the first one is the whole
// answer.
func ValidateChunkRows(chunk ChunkData) error {
	for i, session := range chunk.Sessions {
		if field := SessionRowRejection(session); field != "" {
			return fmt.Errorf("sessions[%d].%s is required", i, field)
		}
	}
	for i, observation := range chunk.Observations {
		if field := ObservationRowRejection(observation); field != "" {
			return fmt.Errorf("observations[%d].%s is required", i, field)
		}
	}
	for i, prompt := range chunk.Prompts {
		if field := PromptRowRejection(prompt); field != "" {
			return fmt.Errorf("prompts[%d].%s is required", i, field)
		}
	}
	return nil
}

// ChunkRowRejections lists every row of the typed collections the cloud
// refuses. Same rules as ValidateChunkRows; all of them instead of the first,
// because a report is only useful when it names the whole repair.
func ChunkRowRejections(chunk ChunkData) []ChunkRowRejection {
	rejections := []ChunkRowRejection{}
	for _, session := range chunk.Sessions {
		if field := SessionRowRejection(session); field != "" {
			rejections = append(rejections, ChunkRowRejection{Entity: "session", Key: session.ID, Field: field})
		}
	}
	for _, observation := range chunk.Observations {
		if field := ObservationRowRejection(observation); field != "" {
			rejections = append(rejections, ChunkRowRejection{
				Entity: "observation",
				Key:    observationRejectionKey(observation),
				Field:  field,
			})
		}
	}
	for _, prompt := range chunk.Prompts {
		if field := PromptRowRejection(prompt); field != "" {
			rejections = append(rejections, ChunkRowRejection{
				Entity: "prompt",
				Key:    promptRejectionKey(prompt),
				Field:  field,
			})
		}
	}
	return rejections
}

// PendingPushRowRejections builds the chunk the next push of a project would
// carry and returns the rows the cloud would refuse.
//
// It goes through the exporter's own selection rather than scanning the tables,
// so the answer covers exactly what would travel: a row with nothing pending is
// not offered and does not block, and a tombstone the collections drop does not
// block either. Anything else would report work the operator cannot act on, or
// miss the rejection that is about to strand the project.
func PendingPushRowRejections(s *store.Store, project string) ([]ChunkRowRejection, error) {
	project, _ = store.NormalizeProject(project)
	project = strings.TrimSpace(project)
	if s == nil || project == "" {
		return nil, nil
	}

	data, err := storeExportDataForProject(s, project)
	if err != nil {
		return nil, fmt.Errorf("export project %q: %w", project, err)
	}
	sy := &Syncer{store: s, cloudMode: true, project: project}
	chunk, _, err := sy.filterByPendingMutations(data, project)
	if err != nil {
		return nil, fmt.Errorf("build pending chunk for project %q: %w", project, err)
	}
	return ChunkRowRejections(*chunk), nil
}

func observationRejectionKey(o store.Observation) string {
	if syncID := strings.TrimSpace(o.SyncID); syncID != "" {
		return syncID
	}
	return fmt.Sprintf("id=%d", o.ID)
}

func promptRejectionKey(p store.Prompt) string {
	if syncID := strings.TrimSpace(p.SyncID); syncID != "" {
		return syncID
	}
	return fmt.Sprintf("id=%d", p.ID)
}
