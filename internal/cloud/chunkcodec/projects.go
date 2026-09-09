package chunkcodec

// engram-projects mutation encoding (RFC rfc-engram-projects.md section 10.2).
//
// A chunk that leaves this machine must be canonical: the project stamped on
// every payload, required fields present and trimmed, and an entity_key that
// is derived from the payload rather than trusted from the sender. This file
// adds those rules for the five engram-projects entities, keeping the same
// shape the upstream entities already follow so normalizeChunkMutation needs
// only a default branch.
//
// The payload structs are deliberately a second, independent copy of the ones
// in internal/store: this package encodes the wire contract, and the wire
// contract must not silently follow a local schema refactor.

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/HoracioEspinosa/engram/internal/store"
)

type mutationProjectCardPayload struct {
	Slug             string  `json:"slug"`
	SyncID           string  `json:"sync_id"`
	DisplayName      string  `json:"display_name"`
	RepoURL          *string `json:"repo_url,omitempty"`
	DefaultBranch    string  `json:"default_branch"`
	JiraProject      string  `json:"jira_project"`
	JiraComponent    *string `json:"jira_component,omitempty"`
	KnowledgeHubPath *string `json:"knowledge_hub_path,omitempty"`
	GraphPath        string  `json:"graph_path"`
	GraphCommit      *string `json:"graph_commit,omitempty"`
	GraphBuiltAt     *string `json:"graph_built_at,omitempty"`
	GraphSummary     *string `json:"graph_summary,omitempty"`
	Owner            *string `json:"owner,omitempty"`
	CreatedAt        string  `json:"created_at"`
	UpdatedAt        string  `json:"updated_at"`
	DeletedAt        *string `json:"deleted_at,omitempty"`
	Project          string  `json:"project"`
}

type mutationTaskPayload struct {
	SyncID             string  `json:"sync_id"`
	Project            string  `json:"project"`
	JiraKey            *string `json:"jira_key,omitempty"`
	SDDChange          *string `json:"sdd_change,omitempty"`
	Title              string  `json:"title"`
	Kind               string  `json:"kind"`
	State              string  `json:"state"`
	JiraStatus         *string `json:"jira_status,omitempty"`
	JiraStatusCategory *string `json:"jira_status_category,omitempty"`
	StateSyncedAt      *string `json:"state_synced_at,omitempty"`
	Branch             *string `json:"branch,omitempty"`
	PRUrl              *string `json:"pr_url,omitempty"`
	KnowledgeRef       *string `json:"knowledge_ref,omitempty"`
	Assignee           *string `json:"assignee,omitempty"`
	CreatedAt          string  `json:"created_at"`
	UpdatedAt          string  `json:"updated_at"`
	ClosedAt           *string `json:"closed_at,omitempty"`
	DeletedAt          *string `json:"deleted_at,omitempty"`
}

type mutationEvidencePayload struct {
	SyncID                string  `json:"sync_id"`
	Project               string  `json:"project"`
	TaskSyncID            string  `json:"task_sync_id"`
	Path                  string  `json:"path"`
	SHA256                string  `json:"sha256"`
	Kind                  string  `json:"kind"`
	Proves                string  `json:"proves"`
	ConfigStamp           *string `json:"config_stamp,omitempty"`
	CapturedAt            string  `json:"captured_at"`
	AttachedJira          bool    `json:"attached_jira"`
	AttachedConfluenceURL *string `json:"attached_confluence_url,omitempty"`
	SizeBytes             *int64  `json:"size_bytes,omitempty"`
	ManifestPath          *string `json:"manifest_path,omitempty"`
	CreatedAt             string  `json:"created_at"`
	DeletedAt             *string `json:"deleted_at,omitempty"`
	OccurredAt            string  `json:"occurred_at"`
}

type mutationTaskLinkPayload struct {
	TaskSyncID        string  `json:"task_sync_id"`
	ObservationSyncID string  `json:"observation_sync_id"`
	Role              string  `json:"role"`
	LinkedAt          string  `json:"linked_at"`
	DeletedAt         *string `json:"deleted_at,omitempty"`
	Project           string  `json:"project"`
}

type mutationObservationRefPayload struct {
	ObservationSyncID string  `json:"observation_sync_id"`
	RefKind           string  `json:"ref_kind"`
	Ref               string  `json:"ref"`
	GraphCommit       *string `json:"graph_commit,omitempty"`
	CreatedAt         string  `json:"created_at"`
	Project           string  `json:"project"`
}

// isProjectsEntity reports whether entity is one of the five engram-projects
// entities of RFC section 10.2.
func isProjectsEntity(entity string) bool {
	switch strings.TrimSpace(entity) {
	case store.SyncEntityProjectCard, store.SyncEntityTask, store.SyncEntityEvidence,
		store.SyncEntityTaskLink, store.SyncEntityObservationRef:
		return true
	default:
		return false
	}
}

// validateSupportedProjectsMutation accepts upsert and delete for the three
// row entities and the link set, and upsert only for observation_ref, which is
// grow-only in v1 exactly like relation.
func validateSupportedProjectsMutation(entity, op string) error {
	unsupported := fmt.Errorf("unsupported mutation %q/%q", entity, op)
	switch strings.TrimSpace(entity) {
	case store.SyncEntityProjectCard, store.SyncEntityTask, store.SyncEntityEvidence, store.SyncEntityTaskLink:
		if op != store.SyncOpUpsert && op != store.SyncOpDelete {
			return unsupported
		}
		return nil
	case store.SyncEntityObservationRef:
		if op != store.SyncOpUpsert {
			return unsupported
		}
		return nil
	default:
		return unsupported
	}
}

func trimmedPtr(v *string) *string {
	if v == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*v)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// normalizeProjectsMutationPayload trims, validates and re-encodes an
// engram-projects payload, returning the canonical bytes and the entity_key
// the caller must match. The project is always overwritten with the chunk's
// project: the server derives scope from the authenticated push, so a payload
// claiming a different project would be a scope escape, not a hint.
func normalizeProjectsMutationPayload(entity, op, payload, project string) (string, string, error) {
	switch strings.TrimSpace(entity) {
	case store.SyncEntityProjectCard:
		var body mutationProjectCardPayload
		if err := DecodeSyncMutationPayload(payload, &body); err != nil {
			return "", "", fmt.Errorf("decode mutation payload: %w", err)
		}
		body.Slug = strings.TrimSpace(body.Slug)
		body.SyncID = strings.TrimSpace(body.SyncID)
		body.DisplayName = strings.TrimSpace(body.DisplayName)
		if body.SyncID == "" {
			return "", "", fmt.Errorf("project_card payload sync_id is required")
		}
		if body.Slug == "" {
			return "", "", fmt.Errorf("project_card payload slug is required")
		}
		if op == store.SyncOpUpsert {
			if body.DisplayName == "" {
				return "", "", fmt.Errorf("project_card payload display_name is required for upsert")
			}
			if strings.TrimSpace(body.UpdatedAt) == "" {
				return "", "", fmt.Errorf("project_card payload updated_at is required for upsert")
			}
		}
		// A graph fact without the commit it was built from is exactly what
		// ADR-026 forbids; reject it here so it never reaches a replica's DDL.
		if trimmedPtr(body.GraphSummary) != nil && trimmedPtr(body.GraphCommit) == nil {
			return "", "", fmt.Errorf("project_card payload graph_summary requires graph_commit")
		}
		if (trimmedPtr(body.GraphCommit) == nil) != (trimmedPtr(body.GraphBuiltAt) == nil) {
			return "", "", fmt.Errorf("project_card payload graph_commit and graph_built_at must be set together")
		}
		body.Project = project
		return encodeProjectsPayload(body, body.SyncID)
	case store.SyncEntityTask:
		var body mutationTaskPayload
		if err := DecodeSyncMutationPayload(payload, &body); err != nil {
			return "", "", fmt.Errorf("decode mutation payload: %w", err)
		}
		body.SyncID = strings.TrimSpace(body.SyncID)
		body.Title = strings.TrimSpace(body.Title)
		body.Kind = strings.TrimSpace(body.Kind)
		body.State = strings.TrimSpace(body.State)
		body.JiraKey = trimmedPtr(body.JiraKey)
		body.SDDChange = trimmedPtr(body.SDDChange)
		if body.SyncID == "" {
			return "", "", fmt.Errorf("task payload sync_id is required")
		}
		if op == store.SyncOpUpsert {
			if body.Title == "" {
				return "", "", fmt.Errorf("task payload title is required for upsert")
			}
			if body.Kind == "" {
				return "", "", fmt.Errorf("task payload kind is required for upsert")
			}
			if body.State == "" {
				return "", "", fmt.Errorf("task payload state is required for upsert")
			}
			if strings.TrimSpace(body.UpdatedAt) == "" {
				return "", "", fmt.Errorf("task payload updated_at is required for upsert")
			}
			if body.JiraKey == nil && body.SDDChange == nil {
				return "", "", fmt.Errorf("task payload requires jira_key or sdd_change")
			}
		}
		body.Project = project
		return encodeProjectsPayload(body, body.SyncID)
	case store.SyncEntityEvidence:
		var body mutationEvidencePayload
		if err := DecodeSyncMutationPayload(payload, &body); err != nil {
			return "", "", fmt.Errorf("decode mutation payload: %w", err)
		}
		body.SyncID = strings.TrimSpace(body.SyncID)
		body.TaskSyncID = strings.TrimSpace(body.TaskSyncID)
		body.Path = strings.TrimSpace(body.Path)
		body.SHA256 = strings.ToLower(strings.TrimSpace(body.SHA256))
		body.Kind = strings.TrimSpace(body.Kind)
		body.Proves = strings.TrimSpace(body.Proves)
		if body.SyncID == "" {
			return "", "", fmt.Errorf("evidence payload sync_id is required")
		}
		if body.TaskSyncID == "" {
			return "", "", fmt.Errorf("evidence payload task_sync_id is required")
		}
		if op == store.SyncOpUpsert {
			if body.Path == "" {
				return "", "", fmt.Errorf("evidence payload path is required for upsert")
			}
			// Evidence carries metadata only, and an absolute or home-relative
			// path is machine-specific: it must never leave this machine.
			if strings.HasPrefix(body.Path, "/") || strings.HasPrefix(body.Path, "~") {
				return "", "", fmt.Errorf("evidence payload path must be relative")
			}
			if len(body.SHA256) != 64 {
				return "", "", fmt.Errorf("evidence payload sha256 must be 64 hex characters")
			}
			if body.Kind == "" {
				return "", "", fmt.Errorf("evidence payload kind is required for upsert")
			}
			if body.Proves == "" {
				return "", "", fmt.Errorf("evidence payload proves is required for upsert")
			}
			if strings.TrimSpace(body.CapturedAt) == "" {
				return "", "", fmt.Errorf("evidence payload captured_at is required for upsert")
			}
		}
		body.Project = project
		return encodeProjectsPayload(body, body.SyncID)
	case store.SyncEntityTaskLink:
		var body mutationTaskLinkPayload
		if err := DecodeSyncMutationPayload(payload, &body); err != nil {
			return "", "", fmt.Errorf("decode mutation payload: %w", err)
		}
		body.TaskSyncID = strings.TrimSpace(body.TaskSyncID)
		body.ObservationSyncID = strings.TrimSpace(body.ObservationSyncID)
		body.Role = strings.TrimSpace(body.Role)
		body.LinkedAt = strings.TrimSpace(body.LinkedAt)
		if body.TaskSyncID == "" {
			return "", "", fmt.Errorf("task_link payload task_sync_id is required")
		}
		if body.ObservationSyncID == "" {
			return "", "", fmt.Errorf("task_link payload observation_sync_id is required")
		}
		if body.LinkedAt == "" {
			return "", "", fmt.Errorf("task_link payload linked_at is required")
		}
		if op == store.SyncOpUpsert && body.Role == "" {
			body.Role = "context"
		}
		if op == store.SyncOpDelete && trimmedPtr(body.DeletedAt) == nil {
			return "", "", fmt.Errorf("task_link delete payload deleted_at is required")
		}
		body.Project = project
		return encodeProjectsPayload(body,
			body.TaskSyncID+"|"+body.ObservationSyncID)
	case store.SyncEntityObservationRef:
		var body mutationObservationRefPayload
		if err := DecodeSyncMutationPayload(payload, &body); err != nil {
			return "", "", fmt.Errorf("decode mutation payload: %w", err)
		}
		body.ObservationSyncID = strings.TrimSpace(body.ObservationSyncID)
		body.RefKind = strings.TrimSpace(body.RefKind)
		body.Ref = strings.TrimSpace(body.Ref)
		body.GraphCommit = trimmedPtr(body.GraphCommit)
		if body.ObservationSyncID == "" {
			return "", "", fmt.Errorf("observation_ref payload observation_sync_id is required")
		}
		if body.RefKind == "" {
			return "", "", fmt.Errorf("observation_ref payload ref_kind is required")
		}
		if body.Ref == "" {
			return "", "", fmt.Errorf("observation_ref payload ref is required")
		}
		if body.RefKind == "graph" && body.GraphCommit == nil {
			return "", "", fmt.Errorf("observation_ref payload graph_commit is required for ref_kind graph")
		}
		body.Project = project
		return encodeProjectsPayload(body,
			body.ObservationSyncID+"|"+body.RefKind+"|"+body.Ref)
	default:
		return "", "", fmt.Errorf("unsupported mutation %q/%q", entity, op)
	}
}

func encodeProjectsPayload(body any, entityKey string) (string, string, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return "", "", fmt.Errorf("encode mutation payload: %w", err)
	}
	return string(encoded), entityKey, nil
}
