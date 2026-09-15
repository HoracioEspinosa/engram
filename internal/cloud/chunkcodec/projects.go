package chunkcodec

// engram-projects mutation encoding.
//
// A chunk that leaves this machine must be canonical: the project stamped on
// every payload, required fields present and trimmed, and an entity_key that
// is derived from the payload rather than trusted from the sender. This file
// adds those rules for the seven engram-projects entities, keeping the same
// shape the upstream entities already follow so normalizeChunkMutation needs
// only a default branch.
//
// The payload structs are deliberately a second, independent copy of the ones
// in internal/store: this package encodes the wire contract, and the wire
// contract must not silently follow a local schema refactor. The cost of that
// independence is that a column the store replicates but this file never
// declared is dropped here without a word — encoding a struct cannot fail on a
// field it does not know — so every field added to a replicated row has to be
// added here too, and asserted on the payload rather than on a round trip:
// two replicas that both lost the same field still agree with each other.

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

	// The hierarchy and the appearance travel with the card. The three
	// staleness columns deliberately do not: they answer "is the graph in this
	// checkout current", and a replica shipping its own answer would overwrite
	// a fact about a working copy it has never seen.
	ParentSlug  *string `json:"parent_slug,omitempty"`
	Depth       int     `json:"depth"`
	Kind        string  `json:"kind,omitempty"`
	Description *string `json:"description,omitempty"`
	Icon        *string `json:"icon,omitempty"`
	Color       *string `json:"color,omitempty"`
	Tags        *string `json:"tags,omitempty"`
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

	// What the vault gave the task. The parent travels as a sync_id only: the
	// local row id means nothing on another machine.
	Slug             *string `json:"slug,omitempty"`
	Summary          *string `json:"summary,omitempty"`
	PendingNote      *string `json:"pending_note,omitempty"`
	VaultPath        *string `json:"vault_path,omitempty"`
	ParentTaskSyncID *string `json:"parent_task_sync_id,omitempty"`
}

type mutationEvidencePayload struct {
	SyncID                string  `json:"sync_id"`
	Project               string  `json:"project"`
	TaskSyncID            string  `json:"task_sync_id"`
	Path                  string  `json:"path"`
	SHA256                string  `json:"sha256"`
	Category              string  `json:"category,omitempty"`
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
	// LocationSetAt is the clock of the location group. Without it an incoming
	// relocation is decided against a local side that has no clock of its own,
	// so the last delivery wins and two replicas that received the same two
	// reports in different orders stop agreeing.
	LocationSetAt *string `json:"location_set_at,omitempty"`
}

type mutationTaskLinkPayload struct {
	TaskSyncID        string  `json:"task_sync_id"`
	ObservationSyncID string  `json:"observation_sync_id"`
	Role              string  `json:"role"`
	LinkedAt          string  `json:"linked_at"`
	DeletedAt         *string `json:"deleted_at,omitempty"`
	Project           string  `json:"project"`
}

type mutationProjectAliasPayload struct {
	Alias     string  `json:"alias"`
	SyncID    string  `json:"sync_id"`
	Slug      string  `json:"slug"`
	Source    string  `json:"source"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
	DeletedAt *string `json:"deleted_at,omitempty"`
	Project   string  `json:"project"`
}

type mutationBenchmarkPayload struct {
	SyncID        string  `json:"sync_id"`
	Project       string  `json:"project"`
	TaskSyncID    string  `json:"task_sync_id"`
	Name          string  `json:"name"`
	Metric        string  `json:"metric"`
	Unit          string  `json:"unit"`
	Direction     string  `json:"direction"`
	Value         float64 `json:"value"`
	Baseline      bool    `json:"baseline"`
	BaselineSetAt *string `json:"baseline_set_at,omitempty"`
	RunPath       *string `json:"run_path,omitempty"`
	SHA256        *string `json:"sha256,omitempty"`
	ConfigStamp   *string `json:"config_stamp,omitempty"`
	CapturedAt    string  `json:"captured_at"`
	Notes         *string `json:"notes,omitempty"`
	Source        string  `json:"source"`
	CreatedAt     string  `json:"created_at"`
	DeletedAt     *string `json:"deleted_at,omitempty"`
}

type mutationObservationRefPayload struct {
	ObservationSyncID string  `json:"observation_sync_id"`
	RefKind           string  `json:"ref_kind"`
	Ref               string  `json:"ref"`
	GraphCommit       *string `json:"graph_commit,omitempty"`
	CreatedAt         string  `json:"created_at"`
	Project           string  `json:"project"`
}

// isProjectsEntity reports whether entity is one of the engram-projects
// entities of RFC section 10.2.
func isProjectsEntity(entity string) bool {
	switch strings.TrimSpace(entity) {
	case store.SyncEntityProjectCard, store.SyncEntityTask, store.SyncEntityEvidence,
		store.SyncEntityTaskLink, store.SyncEntityObservationRef,
		store.SyncEntityProjectAlias, store.SyncEntityBenchmark:
		return true
	default:
		return false
	}
}

// validateSupportedProjectsMutation accepts upsert and delete for the row
// entities and the link set, and upsert only for observation_ref, which is
// grow-only in v1 exactly like relation.
func validateSupportedProjectsMutation(entity, op string) error {
	unsupported := fmt.Errorf("unsupported mutation %q/%q", entity, op)
	switch strings.TrimSpace(entity) {
	case store.SyncEntityProjectCard, store.SyncEntityTask, store.SyncEntityEvidence,
		store.SyncEntityTaskLink, store.SyncEntityProjectAlias, store.SyncEntityBenchmark:
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
		// A graph fact without the commit it was built from must never
		// happen; reject it here so it never reaches a replica's DDL.
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
	case store.SyncEntityProjectAlias:
		var body mutationProjectAliasPayload
		if err := DecodeSyncMutationPayload(payload, &body); err != nil {
			return "", "", fmt.Errorf("decode mutation payload: %w", err)
		}
		// The alias is the primary key, so it is also the portable identity —
		// and it is compared folded to lower case for the self-reference check
		// only, never rewritten: nothing is ever stored under a folded name.
		body.Alias = strings.TrimSpace(body.Alias)
		body.SyncID = strings.TrimSpace(body.SyncID)
		body.Slug = strings.TrimSpace(body.Slug)
		body.Source = strings.TrimSpace(body.Source)
		if body.Alias == "" {
			return "", "", fmt.Errorf("project_alias payload alias is required")
		}
		if body.SyncID == "" {
			return "", "", fmt.Errorf("project_alias payload sync_id is required")
		}
		if op == store.SyncOpUpsert {
			if body.Slug == "" {
				return "", "", fmt.Errorf("project_alias payload slug is required for upsert")
			}
			if strings.EqualFold(body.Alias, body.Slug) {
				return "", "", fmt.Errorf("project_alias %q cannot point at itself", body.Alias)
			}
			if !isProjectAliasSource(body.Source) {
				return "", "", fmt.Errorf("project_alias payload source %q is not one of %s",
					body.Source, strings.Join(projectAliasSources, ", "))
			}
			if strings.TrimSpace(body.UpdatedAt) == "" {
				return "", "", fmt.Errorf("project_alias payload updated_at is required for upsert")
			}
		}
		body.Project = project
		return encodeProjectsPayload(body, body.Alias)
	case store.SyncEntityBenchmark:
		var body mutationBenchmarkPayload
		if err := DecodeSyncMutationPayload(payload, &body); err != nil {
			return "", "", fmt.Errorf("decode mutation payload: %w", err)
		}
		body.SyncID = strings.TrimSpace(body.SyncID)
		body.TaskSyncID = strings.TrimSpace(body.TaskSyncID)
		body.Name = strings.TrimSpace(body.Name)
		body.Metric = strings.TrimSpace(body.Metric)
		body.Unit = strings.TrimSpace(body.Unit)
		body.Direction = strings.TrimSpace(body.Direction)
		body.Source = strings.TrimSpace(body.Source)
		if body.SyncID == "" {
			return "", "", fmt.Errorf("benchmark payload sync_id is required")
		}
		if body.TaskSyncID == "" {
			return "", "", fmt.Errorf("benchmark payload task_sync_id is required")
		}
		if op == store.SyncOpUpsert {
			if body.Name == "" {
				return "", "", fmt.Errorf("benchmark payload name is required for upsert")
			}
			if body.Metric == "" {
				return "", "", fmt.Errorf("benchmark payload metric is required for upsert")
			}
			// A number with no unit, or with a direction nobody reads it
			// against, is not a measurement anything can be compared with.
			if body.Unit == "" {
				return "", "", fmt.Errorf("benchmark payload unit is required for upsert")
			}
			if body.Direction != store.BenchmarkDirectionLower && body.Direction != store.BenchmarkDirectionHigher {
				return "", "", fmt.Errorf("benchmark payload direction %q must be %s or %s",
					body.Direction, store.BenchmarkDirectionLower, store.BenchmarkDirectionHigher)
			}
			if strings.TrimSpace(body.CapturedAt) == "" {
				return "", "", fmt.Errorf("benchmark payload captured_at is required for upsert")
			}
			if body.Source == "" {
				return "", "", fmt.Errorf("benchmark payload source is required for upsert")
			}
		}
		body.Project = project
		return encodeProjectsPayload(body, body.SyncID)
	default:
		return "", "", fmt.Errorf("unsupported mutation %q/%q", entity, op)
	}
}

// projectAliasSources mirrors the project_aliases.source CHECK. A value
// outside it reaches the replica's DDL and is rejected there, which turns a
// typo into a deferred row instead of a refusal the sender can see.
var projectAliasSources = []string{"git_remote", "dir", "env", "manual", "normalizer"}

func isProjectAliasSource(source string) bool {
	for _, known := range projectAliasSources {
		if source == known {
			return true
		}
	}
	return false
}

func encodeProjectsPayload(body any, entityKey string) (string, string, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return "", "", fmt.Errorf("encode mutation payload: %w", err)
	}
	return string(encoded), entityKey, nil
}
