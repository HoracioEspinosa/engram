package store

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Promotion is the engram half of the engram -> vault bridge (roadmap
// T-11.02). It answers two questions and nothing else:
//
//  1. which observations are eligible to become a curated vault document
//     (PromotionCandidates), and
//  2. how the resulting document is recorded back on the observation
//     (StampObservationKnowledgeRef).
//
// Rendering the document, opening the pull request and merging it happen
// outside engram, in the knowledge repository's bridge. engram never writes
// to the vault: the vault is human-curated and its only writer is a merged
// pull request.

// promotionAllowedTypes is the allowlist of observation types that may become
// curated knowledge.
//
// The two types share one property nothing else has: they state a fact that
// outlives the session that produced it. A `decision` records a choice and its
// reasoning; a `discovery` records how the system actually behaves. Everything
// else engram stores — bugfix, manual, session summaries, prompts — is a trace
// of one piece of work, and a vault full of work traces is a vault nobody
// reads.
//
// The list is deliberately short and deliberately code, not configuration:
// widening it is a governance decision with a review attached, not a flag
// someone flips at 2am to unblock a script.
var promotionAllowedTypes = []string{"decision", "discovery"}

// Sentinel errors of the promotion rules. Like the knowledge_ref sentinels,
// each maps 1:1 to the error `code` the CLI and MCP layers return.
var (
	// ErrTypeNotPromotable rejects an observation whose type is outside the
	// allowlist.
	ErrTypeNotPromotable = errors.New("observation type is not promotable to curated knowledge")
	// ErrObservationNotPinned rejects an unpinned observation. Pinning is the
	// human act that says "this one matters"; promoting without it would turn
	// the bridge into an unattended firehose into the vault.
	ErrObservationNotPinned = errors.New("observation is not pinned")
	// ErrObservationNotSyncable rejects an observation with no sync_id.
	// observation_refs is keyed by sync_id, so such an observation has
	// nowhere to carry the stamp.
	ErrObservationNotSyncable = errors.New("observation has no sync_id and cannot carry a knowledge_ref")
	// ErrObservationNoProject rejects an observation with no project. The
	// replication payload of an observation_ref requires one, and a payload
	// without it is discarded on the far side as permanently unusable, so the
	// stamp would silently exist on one machine only.
	ErrObservationNoProject = errors.New("observation has no project and its knowledge_ref could not replicate")
	// ErrKnowledgeRefConflict rejects a second, different knowledge_ref on an
	// observation that already carries one. The export path resolves ties by
	// keeping the earliest ref, so a contradicting second one would be
	// silently ignored rather than reviewed.
	ErrKnowledgeRefConflict = errors.New("observation already carries a different knowledge_ref")
	// ErrProjectsSchemaMissing reports that the engram-projects tables do not
	// exist, so the scan could not run. It exists so a caller can tell "no
	// candidates" from "I could not look", which are the same empty list.
	ErrProjectsSchemaMissing = errors.New("engram-projects schema is missing")
)

// PromotionAllowedTypes returns a copy of the allowlist.
func PromotionAllowedTypes() []string {
	out := make([]string, len(promotionAllowedTypes))
	copy(out, promotionAllowedTypes)
	return out
}

// IsPromotableType reports whether an observation type is in the allowlist.
// Comparison is case-insensitive and whitespace-tolerant because the type
// reaches this point from a CLI flag as often as from a column.
func IsPromotableType(obsType string) bool {
	t := strings.ToLower(strings.TrimSpace(obsType))
	for _, allowed := range promotionAllowedTypes {
		if t == allowed {
			return true
		}
	}
	return false
}

// NormalizePromotionTypes validates a caller-supplied type filter against the
// allowlist and returns it deduplicated and sorted. An empty input means the
// whole allowlist.
//
// The filter can only ever narrow: passing a type outside the allowlist is an
// error rather than a widening, so no caller can promote an arbitrary type by
// naming it.
func NormalizePromotionTypes(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return PromotionAllowedTypes(), nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(raw))
	for _, r := range raw {
		t := strings.ToLower(strings.TrimSpace(r))
		if t == "" {
			continue
		}
		if !IsPromotableType(t) {
			return nil, fmt.Errorf("%w: %q (allowed: %s)",
				ErrTypeNotPromotable, r, strings.Join(promotionAllowedTypes, ", "))
		}
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return PromotionAllowedTypes(), nil
	}
	sort.Strings(out)
	return out, nil
}

// PromotionCandidate is one observation eligible to become a vault document.
//
// Content travels whole: the bridge renders the document body from it, and a
// truncated body would produce a curated document that silently says less
// than the memory it came from.
type PromotionCandidate struct {
	ObservationID int64  `json:"observation_id"`
	SyncID        string `json:"sync_id"`
	Type          string `json:"type"`
	Title         string `json:"title"`
	Content       string `json:"content"`
	Project       string `json:"project"`
	Scope         string `json:"scope"`
	TopicKey      string `json:"topic_key,omitempty"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
	// The pointers already stamped on the observation. They are carried into
	// the rendered document's frontmatter so a reviewer sees the ticket, the
	// runbook and the graph commit the fact came from.
	JiraKey     string `json:"jira_key,omitempty"`
	RunbookID   string `json:"runbook_id,omitempty"`
	GraphCommit string `json:"graph_commit,omitempty"`
}

// PromotionScan is the outcome of one scan: the candidates plus the counters
// that say how the rest of the pinned corpus was classified.
//
// The counters are not decoration. An empty Candidates list has four distinct
// causes — nothing is pinned, nothing pinned is of an allowed type, everything
// eligible is already promoted, or the eligible ones cannot carry a stamp —
// and a bridge that reports "0 candidates" without saying which one it was
// reads as success in all four cases.
type PromotionScan struct {
	Candidates []PromotionCandidate `json:"candidates"`
	// Types is the allowlist actually applied, after NormalizePromotionTypes.
	Types []string `json:"types"`
	// Project is the scope the scan ran over; empty means every project.
	Project string `json:"project"`
	// PinnedInspected counts the pinned, non-deleted observations the scan
	// read, before any filter. It is the "how many units did you inspect"
	// number: zero means the scan had nothing to judge, not that it passed.
	PinnedInspected int `json:"pinned_inspected"`
	// TypeExcluded counts pinned observations rejected by the allowlist.
	TypeExcluded int `json:"type_excluded"`
	// AlreadyPromoted counts eligible observations that already carry a
	// knowledge ref. They are the bridge's successes, not its leftovers.
	AlreadyPromoted int `json:"already_promoted"`
	// NotSyncable counts eligible observations with no sync_id, which cannot
	// carry a stamp and therefore cannot be promoted without repair.
	NotSyncable int `json:"not_syncable"`
	// NoProject counts eligible observations with no project, whose stamp
	// could not replicate.
	NoProject int `json:"no_project"`
}

// PromotionCandidates scans the pinned observations of a project and returns
// the ones eligible for promotion, together with the counters describing what
// was excluded and why.
//
// An empty project scans every project. A missing engram-projects schema is an
// error, not an empty result: the caller must be able to tell "I looked and
// found nothing" from "I could not look".
func (s *Store) PromotionCandidates(project string, types []string, limit int) (PromotionScan, error) {
	scan := PromotionScan{Candidates: []PromotionCandidate{}}

	allowed, err := NormalizePromotionTypes(types)
	if err != nil {
		return scan, err
	}
	scan.Types = allowed

	schema, err := s.ProjectsSchemaStatus()
	if err != nil {
		return scan, err
	}
	if !schema.Present {
		return scan, ErrProjectsSchemaMissing
	}

	project = strings.TrimSpace(project)
	if project != "" {
		project, _ = NormalizeProject(project)
	}
	scan.Project = project

	// The whole pinned set is read, not just the eligible slice: the counters
	// are computed from the same rows the filter runs on, so they can never
	// disagree with it.
	pinned, err := s.PinnedObservations(project, "")
	if err != nil {
		return scan, fmt.Errorf("engram-projects: read pinned observations: %w", err)
	}
	scan.PinnedInspected = len(pinned)

	allowedSet := map[string]bool{}
	for _, t := range allowed {
		allowedSet[t] = true
	}

	refs, err := s.ObservationExportRefs(project)
	if err != nil {
		return scan, err
	}

	for _, obs := range pinned {
		obsType := strings.ToLower(strings.TrimSpace(obs.Type))
		if !allowedSet[obsType] {
			scan.TypeExcluded++
			continue
		}
		syncID := strings.TrimSpace(obs.SyncID)
		if syncID == "" {
			scan.NotSyncable++
			continue
		}
		obsProject := ""
		if obs.Project != nil {
			obsProject, _ = NormalizeProject(*obs.Project)
		}
		if obsProject == "" {
			scan.NoProject++
			continue
		}
		ref := refs[syncID]
		if strings.TrimSpace(ref.KnowledgeRef) != "" {
			scan.AlreadyPromoted++
			continue
		}
		if limit > 0 && len(scan.Candidates) >= limit {
			continue
		}
		scan.Candidates = append(scan.Candidates, PromotionCandidate{
			ObservationID: obs.ID,
			SyncID:        syncID,
			Type:          obsType,
			Title:         obs.Title,
			Content:       obs.Content,
			Project:       obsProject,
			Scope:         obs.Scope,
			TopicKey:      strVal(obs.TopicKey),
			CreatedAt:     obs.CreatedAt,
			UpdatedAt:     obs.UpdatedAt,
			JiraKey:       ref.JiraKey,
			RunbookID:     ref.RunbookID,
			GraphCommit:   ref.GraphCommit,
		})
	}

	return scan, nil
}

// StampKnowledgeRefParams is the input of StampObservationKnowledgeRef.
type StampKnowledgeRefParams struct {
	// ObservationID is the local id of the observation to stamp.
	ObservationID int64
	// KnowledgeRef is the vault-relative path of the merged document. It goes
	// through NormalizeKnowledgeRef, so a pasted wikilink is accepted.
	KnowledgeRef string
	// AllowUnpinned lifts the pinned requirement. It exists for the repair
	// case — a document merged for an observation that was unpinned in the
	// meantime — and is never the default.
	AllowUnpinned bool
	// AllowAnyType lifts the allowlist. Same reasoning as AllowUnpinned: a
	// document that already exists in the vault should be able to receive its
	// backlink even if the observation's type would not have started the
	// promotion.
	AllowAnyType bool
}

// StampKnowledgeRefResult reports what the stamp did.
type StampKnowledgeRefResult struct {
	ObservationID     int64  `json:"observation_id"`
	ObservationSyncID string `json:"observation_sync_id"`
	Project           string `json:"project"`
	Type              string `json:"type"`
	KnowledgeRef      string `json:"knowledge_ref"`
	// Stamped is false when the observation already carried this exact ref.
	// Re-running the bridge over a merged batch is expected, so that case is
	// a success, not an error — but the caller still gets to tell the two
	// apart.
	Stamped bool `json:"stamped"`
}

// StampObservationKnowledgeRef records the curated document that now describes
// an observation's fact, without requiring a task.
//
// `mem_task_link` can already stamp a knowledge_ref, but only on an
// observation linked to a task. A pinned decision often has no ticket at all,
// and inventing a task to hang the reference on would put a fake row in the
// task list to satisfy a foreign key. This is the same write with the task
// requirement removed.
//
// Ordering is deliberate: the stamp belongs AFTER the pull request that adds
// the document is merged. Stamped earlier, it points at a document that does
// not exist in any checkout, which is exactly what the doctor check
// `knowledge_ref_dangling` reports as a defect.
func (s *Store) StampObservationKnowledgeRef(p StampKnowledgeRefParams) (StampKnowledgeRefResult, error) {
	var result StampKnowledgeRefResult

	obs, err := s.GetObservation(p.ObservationID)
	if err != nil || obs == nil {
		return result, ErrUnknownObservation
	}
	if obs.DeletedAt != nil && strings.TrimSpace(*obs.DeletedAt) != "" {
		return result, ErrUnknownObservation
	}

	obsType := strings.ToLower(strings.TrimSpace(obs.Type))
	if !p.AllowAnyType && !IsPromotableType(obsType) {
		return result, fmt.Errorf("%w: %q (allowed: %s)",
			ErrTypeNotPromotable, obs.Type, strings.Join(promotionAllowedTypes, ", "))
	}
	if !p.AllowUnpinned && !obs.Pinned {
		return result, ErrObservationNotPinned
	}

	syncID := strings.TrimSpace(obs.SyncID)
	if syncID == "" {
		return result, ErrObservationNotSyncable
	}
	obsProject := ""
	if obs.Project != nil {
		obsProject, _ = NormalizeProject(*obs.Project)
	}
	if obsProject == "" {
		return result, ErrObservationNoProject
	}

	ref, err := NormalizeKnowledgeRef(p.KnowledgeRef)
	if err != nil {
		return result, err
	}

	// A contradicting second reference is refused before anything is written.
	// The export path keeps the earliest knowledge ref and ignores the rest,
	// so accepting this insert would produce a row nothing ever reads while
	// reporting success.
	var existing string
	err = s.db.QueryRow(
		`SELECT ref FROM observation_refs
		 WHERE observation_sync_id = ? AND ref_kind = 'knowledge'
		 ORDER BY id ASC LIMIT 1`, syncID).Scan(&existing)
	switch {
	case err == nil:
		if existing != ref {
			return result, fmt.Errorf("%w: %q is already stamped, refusing %q",
				ErrKnowledgeRefConflict, existing, ref)
		}
	case errors.Is(err, sql.ErrNoRows):
		// No reference yet: the normal promotion path.
	default:
		return result, fmt.Errorf("engram-projects: read existing knowledge_ref: %w", err)
	}

	result = StampKnowledgeRefResult{
		ObservationID:     obs.ID,
		ObservationSyncID: syncID,
		Project:           obsProject,
		Type:              obsType,
		KnowledgeRef:      ref,
	}

	now := s.nowUTC()
	if err := s.withTx(func(tx *sql.Tx) error {
		result.Stamped = false
		res, err := s.execHook(tx, `
			INSERT OR IGNORE INTO observation_refs (observation_sync_id, ref_kind, ref, graph_commit, created_at)
			VALUES (?, 'knowledge', ?, NULL, ?)`, syncID, ref, now)
		if err != nil {
			return fmt.Errorf("engram-projects: stamp knowledge_ref: %w", err)
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("engram-projects: stamp knowledge_ref: %w", err)
		}
		if affected == 0 {
			// Already present and identical: idempotent re-run.
			return nil
		}
		result.Stamped = true
		return s.enqueueObservationRefTx(tx, obsProject, syncID, "knowledge", ref)
	}); err != nil {
		return StampKnowledgeRefResult{}, err
	}

	return result, nil
}
