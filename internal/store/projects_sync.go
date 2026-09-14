// Package store: engram-projects cloud replication (RFC rfc-engram-projects.md
// section 10).
//
// This file owns both halves of the project-scoped mutation journal for the
// five engram-projects entities:
//
//   - the enqueue half — enqueueProjects*Tx, called inside the very
//     transaction that writes the row, so a row and its mutation are never
//     committed apart (the same contract mem_save already honours for
//     observations);
//   - the apply half — applyProjectsMutationTx, which merges a pulled
//     mutation with deterministic, order-independent rules so two replicas
//     that receive the same mutations in different orders reach byte-identical
//     state.
//
// The merge rules are the ones RFC section 10.3 fixes: last-writer-wins
// records keyed on updated_at (ties broken by the SHA-256 of the canonical
// payload), a monotone group for the code-graph pointer, a Jira mirror group
// with its own freshness clock, immutable evidence with monotone flags, an
// LWW-element-set for task<->observation links, and a grow-only set for
// observation references. They are the coordination-free replicated data
// types of Shapiro et al. (https://hal.inria.fr/inria-00609399): convergence
// is a property of the rules, not of the delivery order.
package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ─── Deferral and flag ───────────────────────────────────────────────────────

// ErrProjectsFKMissing marks a projects mutation whose referenced row has not
// arrived locally yet (an evidence row ahead of its task, a task_link ahead of
// either side, a task ahead of its project card). It wraps
// ErrRelationFKMissing so every path that already classifies that sentinel as
// retryable — the deferral branch of ApplyPulledMutation and the retry/dead
// accounting of ReplayDeferred — treats it the same way without a second
// classification rule.
var ErrProjectsFKMissing = fmt.Errorf("%w: engram-projects referenced row missing", ErrRelationFKMissing)

// projectsSyncEnvVar gates the enqueue half only. ADR-025 fixes the rollout
// order as cloud image first, then binaries, then this flag: a mutation for an
// entity the other replicas do not understand yet would halt their pull, so
// nothing is enqueued until the operator says every binary is current.
//
// The apply half is never gated. A replica must be able to absorb what a
// newer peer sends it the moment it receives it, whatever its own flag says.
const projectsSyncEnvVar = "ENGRAM_PROJECTS_SYNC"

// ProjectsSyncEnabled reports whether engram-projects rows are replicated.
// Default is off (ADR-025).
func ProjectsSyncEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(projectsSyncEnvVar))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// ProjectsSyncEntities lists the engram-projects entities RFC section 10.2
// replicates, in the order a replica should apply them when it has a free
// choice: parents before the rows that reference them.
func ProjectsSyncEntities() []string {
	return []string{
		SyncEntityProjectCard,
		SyncEntityProjectAlias,
		SyncEntityTask,
		SyncEntityEvidence,
		SyncEntityBenchmark,
		SyncEntityTaskLink,
		SyncEntityObservationRef,
	}
}

// isProjectsEntity reports whether entity is one of the engram-projects
// entities.
func isProjectsEntity(entity string) bool {
	switch strings.TrimSpace(entity) {
	case SyncEntityProjectCard, SyncEntityProjectAlias, SyncEntityTask, SyncEntityEvidence,
		SyncEntityBenchmark, SyncEntityTaskLink, SyncEntityObservationRef:
		return true
	default:
		return false
	}
}

// isProjectsMutation reports whether the (entity, op) pair is a supported
// engram-projects mutation. observation_ref is grow-only and has no delete, in
// the same way relation does not.
func isProjectsMutation(entity, op string) bool {
	if !isProjectsEntity(entity) {
		return false
	}
	if strings.TrimSpace(entity) == SyncEntityObservationRef {
		return strings.TrimSpace(op) == SyncOpUpsert
	}
	switch strings.TrimSpace(op) {
	case SyncOpUpsert, SyncOpDelete:
		return true
	default:
		return false
	}
}

// ─── Payloads ────────────────────────────────────────────────────────────────
//
// Every payload is self-contained: it carries its own project, its own
// identity, and — where the entity can be removed — its own deleted_at. The
// apply half therefore never reads mutation.Op, which matters because
// ReplayDeferred reconstructs a parked mutation as an upsert and would
// otherwise turn a deferred delete into a resurrection.

type syncProjectCardPayload struct {
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

	// The hierarchy and appearance travel with the card. The three staleness
	// columns deliberately do not: they answer "is the graph in this checkout
	// current", and a replica that shipped its own answer would be overwriting
	// a fact about a working copy it has never seen.
	ParentSlug  *string `json:"parent_slug,omitempty"`
	Depth       int     `json:"depth"`
	Kind        string  `json:"kind,omitempty"`
	Description *string `json:"description,omitempty"`
	Icon        *string `json:"icon,omitempty"`
	Color       *string `json:"color,omitempty"`
	Tags        *string `json:"tags,omitempty"`
}

type syncTaskPayload struct {
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

	// The vault fields a person edits, under the same clock as the title.
	// The parent travels as a sync_id only: the local row id means nothing on
	// another machine.
	Slug             *string `json:"slug,omitempty"`
	Summary          *string `json:"summary,omitempty"`
	PendingNote      *string `json:"pending_note,omitempty"`
	VaultPath        *string `json:"vault_path,omitempty"`
	ParentTaskSyncID *string `json:"parent_task_sync_id,omitempty"`
}

type syncEvidencePayload struct {
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
	// OccurredAt is the clock for the two mutable flags of an otherwise
	// immutable row (attached_jira, attached_confluence_url). It lives in the
	// payload rather than in sync_mutations.occurred_at because a replayed
	// deferred row loses the journal metadata but keeps the payload.
	OccurredAt string `json:"occurred_at"`
	// LocationSetAt is the clock of the location group, and it is a column
	// rather than a stamp taken at enqueue time: comparing an incoming report
	// against a local side that has no clock of its own makes the last
	// delivery win, and two replicas that received the same two reports in
	// different orders would stop agreeing.
	LocationSetAt *string `json:"location_set_at,omitempty"`
}

type syncTaskLinkPayload struct {
	TaskSyncID        string  `json:"task_sync_id"`
	ObservationSyncID string  `json:"observation_sync_id"`
	Role              string  `json:"role"`
	LinkedAt          string  `json:"linked_at"`
	DeletedAt         *string `json:"deleted_at,omitempty"`
	Project           string  `json:"project"`
}

type syncObservationRefPayload struct {
	ObservationSyncID string  `json:"observation_sync_id"`
	RefKind           string  `json:"ref_kind"`
	Ref               string  `json:"ref"`
	GraphCommit       *string `json:"graph_commit,omitempty"`
	CreatedAt         string  `json:"created_at"`
	Project           string  `json:"project"`
}

type syncProjectAliasPayload struct {
	Alias     string  `json:"alias"`
	SyncID    string  `json:"sync_id"`
	Slug      string  `json:"slug"`
	Source    string  `json:"source"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
	DeletedAt *string `json:"deleted_at,omitempty"`
	Project   string  `json:"project"`
}

type syncBenchmarkPayload struct {
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

// ProjectsEntityKey returns the portable entity_key for a projects payload,
// as fixed by RFC section 10.2: the sync_id for the three row entities, and
// the pipe-joined identity tuple for the two set entities.
func ProjectsEntityKey(entity string, payload []byte) (string, error) {
	switch strings.TrimSpace(entity) {
	case SyncEntityProjectCard:
		var body syncProjectCardPayload
		if err := decodeSyncPayload(payload, &body); err != nil {
			return "", err
		}
		return strings.TrimSpace(body.SyncID), nil
	case SyncEntityTask:
		var body syncTaskPayload
		if err := decodeSyncPayload(payload, &body); err != nil {
			return "", err
		}
		return strings.TrimSpace(body.SyncID), nil
	case SyncEntityEvidence:
		var body syncEvidencePayload
		if err := decodeSyncPayload(payload, &body); err != nil {
			return "", err
		}
		return strings.TrimSpace(body.SyncID), nil
	case SyncEntityTaskLink:
		var body syncTaskLinkPayload
		if err := decodeSyncPayload(payload, &body); err != nil {
			return "", err
		}
		return taskLinkKey(body.TaskSyncID, body.ObservationSyncID), nil
	case SyncEntityObservationRef:
		var body syncObservationRefPayload
		if err := decodeSyncPayload(payload, &body); err != nil {
			return "", err
		}
		return observationRefKey(body.ObservationSyncID, body.RefKind, body.Ref), nil
	case SyncEntityProjectAlias:
		var body syncProjectAliasPayload
		if err := decodeSyncPayload(payload, &body); err != nil {
			return "", err
		}
		// The alias is the primary key, so it is also the portable identity.
		return strings.TrimSpace(body.Alias), nil
	case SyncEntityBenchmark:
		var body syncBenchmarkPayload
		if err := decodeSyncPayload(payload, &body); err != nil {
			return "", err
		}
		return strings.TrimSpace(body.SyncID), nil
	default:
		return "", fmt.Errorf("unsupported engram-projects entity %q", entity)
	}
}

func taskLinkKey(taskSyncID, observationSyncID string) string {
	return strings.TrimSpace(taskSyncID) + "|" + strings.TrimSpace(observationSyncID)
}

func observationRefKey(observationSyncID, refKind, ref string) string {
	return strings.TrimSpace(observationSyncID) + "|" + strings.TrimSpace(refKind) + "|" + strings.TrimSpace(ref)
}

// ─── Deterministic comparison helpers ────────────────────────────────────────

// groupDigest is the tie-breaker RFC section 10.3 prescribes for two updates
// carrying the exact same clock: the SHA-256 of a canonical encoding.
//
// It hashes only the fields of the group being decided, never the whole
// payload. That distinction is what makes the tie-break sound: the local side
// of a comparison is a row that may already carry another group's values
// merged in from a third replica, so a whole-payload hash would not reproduce
// the hash of the payload that originally wrote this group, and two replicas
// could break the same tie differently.
func groupDigest(fields ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(fields, "\x00")))
	return hex.EncodeToString(sum[:])
}

// deferredRowKey is the primary key a parked mutation gets in
// sync_apply_deferred. It is the entity key plus a short digest of the payload,
// because several distinct payloads for the same entity can be in flight at
// once — a descriptive edit and a Jira refresh of one task, two versions of one
// link. Keying them by the entity alone lets the second one overwrite the
// first, and a payload that is overwritten before it applies is simply lost:
// the replica that received them in a different order keeps both and the two
// databases stop agreeing.
func deferredRowKey(entityKey, payload string) string {
	sum := sha256.Sum256([]byte(payload))
	return entityKey + "#" + hex.EncodeToString(sum[:])[:8]
}

// timestampLess reports whether a is strictly older than b. Both are parsed
// with the store's timestamp formats; anything unparseable falls back to a
// lexicographic comparison so the ordering stays total and identical on every
// replica rather than becoming "unknown".
func timestampLess(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == b {
		return false
	}
	if a == "" {
		return true
	}
	if b == "" {
		return false
	}
	pa, errA := parseObservationTime(a)
	pb, errB := parseObservationTime(b)
	if errA == nil && errB == nil {
		if pa.Equal(pb) {
			return a < b
		}
		return pa.Before(pb)
	}
	return a < b
}

// incomingWinsLWW implements the last-writer-wins rule of RFC section 10.3:
// the greater updated_at wins; on an exact tie the greater canonical payload
// digest wins. Both replicas evaluate the same two operands, so both reach the
// same verdict.
func incomingWinsLWW(localUpdatedAt, incomingUpdatedAt, localDigest, incomingDigest string) bool {
	if timestampLess(localUpdatedAt, incomingUpdatedAt) {
		return true
	}
	if timestampLess(incomingUpdatedAt, localUpdatedAt) {
		return false
	}
	return incomingDigest > localDigest
}

func trimPtr(v *string) *string {
	if v == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*v)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func ptrOrEmpty(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

// earliestTimestamp returns the older of two optional timestamps, treating nil
// as "not set". It is how every monotone deletion flag converges: once a
// replica has seen a deletion it never un-sees it, and two replicas that saw
// different deletion timestamps settle on the same one.
func earliestTimestamp(a, b *string) *string {
	a = trimPtr(a)
	b = trimPtr(b)
	switch {
	case a == nil:
		return b
	case b == nil:
		return a
	case timestampLess(*b, *a):
		return b
	default:
		return a
	}
}

// cardDescriptiveDigest hashes the fields the card's last-writer-wins group
// owns, and nothing else.
func cardDescriptiveDigest(c syncProjectCardPayload) string {
	return groupDigest(c.SyncID, c.DisplayName, ptrOrEmpty(c.RepoURL), c.DefaultBranch, c.JiraProject,
		ptrOrEmpty(c.JiraComponent), ptrOrEmpty(c.KnowledgeHubPath), c.GraphPath, ptrOrEmpty(c.Owner),
		ptrOrEmpty(c.DeletedAt), ptrOrEmpty(c.ParentSlug), strconv.Itoa(c.Depth), c.Kind,
		ptrOrEmpty(c.Description), ptrOrEmpty(c.Icon), ptrOrEmpty(c.Color), ptrOrEmpty(c.Tags))
}

// aliasDigest hashes the fields of an alias that the updated_at clock governs.
func aliasDigest(a syncProjectAliasPayload) string {
	return groupDigest(a.SyncID, a.Alias, a.Slug, a.Source, ptrOrEmpty(a.DeletedAt))
}

// benchmarkBaselineDigest hashes the one group of a benchmark that can move.
func benchmarkBaselineDigest(b syncBenchmarkPayload) string {
	baseline := "0"
	if b.Baseline {
		baseline = "1"
	}
	return groupDigest(b.SyncID, baseline, ptrOrEmpty(b.BaselineSetAt))
}

// evidenceLocationDigest hashes where an evidence file lives. Everything else
// about the row is immutable, so this is the only group with a tie to break.
func evidenceLocationDigest(e syncEvidencePayload) string {
	return groupDigest(e.SyncID, e.Path, e.Category)
}

// cardGraphDigest hashes the code-graph group, used only to separate two
// builds stamped at the same instant.
func cardGraphDigest(c syncProjectCardPayload) string {
	return groupDigest(ptrOrEmpty(c.GraphCommit), ptrOrEmpty(c.GraphBuiltAt), ptrOrEmpty(c.GraphSummary))
}

// taskDescriptiveDigest hashes the fields a person edits directly, which the
// updated_at clock governs.
func taskDescriptiveDigest(t syncTaskPayload) string {
	return groupDigest(t.Project, ptrOrEmpty(t.JiraKey), ptrOrEmpty(t.SDDChange), t.Title, t.Kind,
		ptrOrEmpty(t.Branch), ptrOrEmpty(t.PRUrl), ptrOrEmpty(t.KnowledgeRef), ptrOrEmpty(t.Assignee),
		ptrOrEmpty(t.Slug), ptrOrEmpty(t.Summary), ptrOrEmpty(t.PendingNote), ptrOrEmpty(t.VaultPath),
		ptrOrEmpty(t.ParentTaskSyncID))
}

// taskMirrorDigest hashes the two fields copied verbatim from Jira.
func taskMirrorDigest(t syncTaskPayload) string {
	return groupDigest(ptrOrEmpty(t.JiraStatus), ptrOrEmpty(t.JiraStatusCategory))
}

// taskStateDigest hashes the state group.
func taskStateDigest(t syncTaskPayload) string {
	return groupDigest(t.State, ptrOrEmpty(t.ClosedAt))
}

// taskStateClock is the clock that decides `state`, and it is deliberately not
// either of the other two clocks.
//
// state has two legitimate writers: a Jira refresh, which derives it and
// stamps state_synced_at, and a person changing it by hand in the TUI or the
// CLI, which leaves state_synced_at untouched (RFC section 10.4). Putting
// state under the descriptive clock would let a stale edit undo a fresh Jira
// read; putting it under the mirror clock would make a manual change invisible
// as soon as any Jira read existed. Worse, letting it belong to both groups at
// once makes the merge non-commutative — the two groups would each claim it
// and the winner would depend on arrival order.
//
// So state gets one clock of its own: the freshest evidence the payload has
// about it, which is its Jira read when it has one and its own edit time
// otherwise.
func taskStateClock(t syncTaskPayload) string {
	if synced := trimPtr(t.StateSyncedAt); synced != nil {
		return *synced
	}
	return strings.TrimSpace(t.UpdatedAt)
}

// taskClosedAt derives closed_at from the payload that won the state, so the
// DDL's `closed_at IS NULL OR state IN ('done','cancelled')` holds and the
// value is a pure function of the winner rather than of the merge order.
func taskClosedAt(t syncTaskPayload) *string {
	if !isClosedState(t.State) {
		return nil
	}
	if closed := trimPtr(t.ClosedAt); closed != nil {
		return closed
	}
	clock := taskStateClock(t)
	if clock == "" {
		return nil
	}
	return &clock
}

// ─── Enqueue half ────────────────────────────────────────────────────────────

// enqueueProjectsMutationTx encodes payload and appends it to the mutation
// journal inside the caller's transaction. It reuses enqueueSyncMutationTx, so
// the project is derived from the payload's own project field and the mutation
// lands on both the shared target and the project target exactly like an
// observation does.
//
// It is a no-op while ENGRAM_PROJECTS_SYNC is off; the row is still written by
// the caller, it simply does not travel.
func (s *Store) enqueueProjectsMutationTx(tx *sql.Tx, entity, entityKey string, deleted bool, payload any) error {
	if !ProjectsSyncEnabled() {
		return nil
	}
	op := SyncOpUpsert
	if deleted {
		op = SyncOpDelete
	}
	return s.enqueueSyncMutationTx(tx, entity, entityKey, op, payload)
}

// projectCardSyncSelect is the replicated projection of a card. It stops short
// of graph_stale_reason, graph_changed_files and graph_checked_at on purpose:
// those three describe this checkout, so shipping them would let one machine's
// answer overwrite another's.
const projectCardSyncSelect = `slug, sync_id, display_name, repo_url, default_branch, jira_project,
	jira_component, knowledge_hub_path, graph_path, graph_commit, graph_built_at, graph_summary,
	owner, created_at, updated_at, deleted_at, parent_slug, depth, kind, description, icon, color, tags`

func scanProjectCardSyncPayload(row interface{ Scan(dest ...any) error }) (syncProjectCardPayload, error) {
	var p syncProjectCardPayload
	err := row.Scan(&p.Slug, &p.SyncID, &p.DisplayName, &p.RepoURL, &p.DefaultBranch, &p.JiraProject,
		&p.JiraComponent, &p.KnowledgeHubPath, &p.GraphPath, &p.GraphCommit, &p.GraphBuiltAt,
		&p.GraphSummary, &p.Owner, &p.CreatedAt, &p.UpdatedAt, &p.DeletedAt, &p.ParentSlug, &p.Depth,
		&p.Kind, &p.Description, &p.Icon, &p.Color, &p.Tags)
	if err != nil {
		return p, err
	}
	p.Project = p.Slug
	return p, nil
}

// enqueueProjectCardTx reads the freshly written card back inside tx and
// journals it. Reading through tx (not s.db) is what makes the row and its
// mutation atomic: an uncommitted card is still visible to its own
// transaction, and a rollback discards both.
func (s *Store) enqueueProjectCardTx(tx *sql.Tx, slug string) error {
	if !ProjectsSyncEnabled() {
		return nil
	}
	payload, err := scanProjectCardSyncPayload(
		tx.QueryRow(`SELECT `+projectCardSyncSelect+` FROM project_cards WHERE slug = ?`, slug))
	if err != nil {
		return fmt.Errorf("engram-projects: read card for sync: %w", err)
	}
	return s.enqueueProjectsMutationTx(tx, SyncEntityProjectCard, payload.SyncID, payload.DeletedAt != nil, payload)
}

const taskSyncSelect = `sync_id, project, jira_key, sdd_change, title, kind, state, jira_status,
	jira_status_category, state_synced_at, branch, pr_url, knowledge_ref, assignee,
	created_at, updated_at, closed_at, deleted_at, slug, summary, pending_note, vault_path,
	parent_task_sync_id`

func scanTaskSyncPayload(row interface{ Scan(dest ...any) error }) (syncTaskPayload, error) {
	var p syncTaskPayload
	err := row.Scan(&p.SyncID, &p.Project, &p.JiraKey, &p.SDDChange, &p.Title, &p.Kind, &p.State,
		&p.JiraStatus, &p.JiraStatusCategory, &p.StateSyncedAt, &p.Branch, &p.PRUrl, &p.KnowledgeRef,
		&p.Assignee, &p.CreatedAt, &p.UpdatedAt, &p.ClosedAt, &p.DeletedAt, &p.Slug, &p.Summary,
		&p.PendingNote, &p.VaultPath, &p.ParentTaskSyncID)
	return p, err
}

func (s *Store) enqueueTaskTx(tx *sql.Tx, id int64) error {
	if !ProjectsSyncEnabled() {
		return nil
	}
	payload, err := scanTaskSyncPayload(
		tx.QueryRow(`SELECT `+taskSyncSelect+` FROM tasks WHERE id = ?`, id))
	if err != nil {
		return fmt.Errorf("engram-projects: read task for sync: %w", err)
	}
	return s.enqueueProjectsMutationTx(tx, SyncEntityTask, payload.SyncID, payload.DeletedAt != nil, payload)
}

const evidenceSyncSelect = `sync_id, project, task_sync_id, path, sha256, category, kind, proves,
	config_stamp, captured_at, attached_jira, attached_confluence_url, size_bytes, manifest_path,
	location_set_at, created_at, deleted_at`

func scanEvidenceSyncPayload(row interface{ Scan(dest ...any) error }) (syncEvidencePayload, error) {
	var p syncEvidencePayload
	var attachedJira int
	err := row.Scan(&p.SyncID, &p.Project, &p.TaskSyncID, &p.Path, &p.SHA256, &p.Category, &p.Kind,
		&p.Proves, &p.ConfigStamp, &p.CapturedAt, &attachedJira, &p.AttachedConfluenceURL, &p.SizeBytes,
		&p.ManifestPath, &p.LocationSetAt, &p.CreatedAt, &p.DeletedAt)
	p.AttachedJira = attachedJira == 1
	return p, err
}

func (s *Store) enqueueEvidenceTx(tx *sql.Tx, id int64) error {
	if !ProjectsSyncEnabled() {
		return nil
	}
	payload, err := scanEvidenceSyncPayload(
		tx.QueryRow(`SELECT `+evidenceSyncSelect+` FROM evidence WHERE id = ?`, id))
	if err != nil {
		return fmt.Errorf("engram-projects: read evidence for sync: %w", err)
	}
	payload.OccurredAt = s.nowUTC()
	return s.enqueueProjectsMutationTx(tx, SyncEntityEvidence, payload.SyncID, payload.DeletedAt != nil, payload)
}

const projectAliasSyncSelect = `alias, sync_id, slug, source, created_at, updated_at, deleted_at`

// enqueueProjectAliasTx journals an alias inside the transaction that wrote it.
// An alias is the only thing that says a name still in use out there means a
// project here, so a replica that never received it would report that name
// unknown.
func (s *Store) enqueueProjectAliasTx(tx *sql.Tx, alias string) error {
	if !ProjectsSyncEnabled() {
		return nil
	}
	var payload syncProjectAliasPayload
	if err := tx.QueryRow(
		`SELECT `+projectAliasSyncSelect+` FROM project_aliases WHERE alias = ?`, alias,
	).Scan(&payload.Alias, &payload.SyncID, &payload.Slug, &payload.Source,
		&payload.CreatedAt, &payload.UpdatedAt, &payload.DeletedAt); err != nil {
		return fmt.Errorf("engram-projects: read alias for sync: %w", err)
	}
	payload.Project = payload.Slug
	return s.enqueueProjectsMutationTx(tx, SyncEntityProjectAlias, payload.Alias,
		payload.DeletedAt != nil, payload)
}

const benchmarkSyncSelect = `sync_id, project, task_sync_id, name, metric, unit, direction, value,
	baseline, baseline_set_at, run_path, sha256, config_stamp, captured_at, notes, source,
	created_at, deleted_at`

func scanBenchmarkSyncPayload(row interface{ Scan(dest ...any) error }) (syncBenchmarkPayload, error) {
	var p syncBenchmarkPayload
	var baseline int
	err := row.Scan(&p.SyncID, &p.Project, &p.TaskSyncID, &p.Name, &p.Metric, &p.Unit, &p.Direction,
		&p.Value, &baseline, &p.BaselineSetAt, &p.RunPath, &p.SHA256, &p.ConfigStamp, &p.CapturedAt,
		&p.Notes, &p.Source, &p.CreatedAt, &p.DeletedAt)
	p.Baseline = baseline == 1
	return p, err
}

func (s *Store) enqueueBenchmarkTx(tx *sql.Tx, syncID string) error {
	if !ProjectsSyncEnabled() {
		return nil
	}
	payload, err := scanBenchmarkSyncPayload(
		tx.QueryRow(`SELECT `+benchmarkSyncSelect+` FROM benchmarks WHERE sync_id = ?`, syncID))
	if err != nil {
		return fmt.Errorf("engram-projects: read benchmark for sync: %w", err)
	}
	return s.enqueueProjectsMutationTx(tx, SyncEntityBenchmark, payload.SyncID,
		payload.DeletedAt != nil, payload)
}

func (s *Store) enqueueTaskLinkTx(tx *sql.Tx, project, taskSyncID, observationSyncID string) error {
	if !ProjectsSyncEnabled() {
		return nil
	}
	var payload syncTaskLinkPayload
	err := tx.QueryRow(
		`SELECT task_sync_id, observation_sync_id, role, linked_at
		 FROM task_observations WHERE task_sync_id = ? AND observation_sync_id = ?`,
		taskSyncID, observationSyncID,
	).Scan(&payload.TaskSyncID, &payload.ObservationSyncID, &payload.Role, &payload.LinkedAt)
	if err != nil {
		return fmt.Errorf("engram-projects: read task link for sync: %w", err)
	}
	payload.Project = project
	return s.enqueueProjectsMutationTx(tx, SyncEntityTaskLink,
		taskLinkKey(payload.TaskSyncID, payload.ObservationSyncID), false, payload)
}

func (s *Store) enqueueObservationRefTx(tx *sql.Tx, project, observationSyncID, refKind, ref string) error {
	if !ProjectsSyncEnabled() {
		return nil
	}
	var payload syncObservationRefPayload
	err := tx.QueryRow(
		`SELECT observation_sync_id, ref_kind, ref, graph_commit, created_at
		 FROM observation_refs WHERE observation_sync_id = ? AND ref_kind = ? AND ref = ?`,
		observationSyncID, refKind, ref,
	).Scan(&payload.ObservationSyncID, &payload.RefKind, &payload.Ref, &payload.GraphCommit, &payload.CreatedAt)
	if err != nil {
		return fmt.Errorf("engram-projects: read observation ref for sync: %w", err)
	}
	payload.Project = project
	return s.enqueueProjectsMutationTx(tx, SyncEntityObservationRef,
		observationRefKey(payload.ObservationSyncID, payload.RefKind, payload.Ref), false, payload)
}

// ─── Apply half ──────────────────────────────────────────────────────────────

// applyProjectsMutationTx applies one pulled engram-projects mutation with the
// convergence rules of RFC section 10.3. Every branch is order-independent:
// applying the same set of mutations in any order yields the same rows.
//
// Two error classes leave this function, and both are handled by the caller
// rather than halting a pull: ErrProjectsFKMissing (retry later, the
// referenced row has not arrived) and ErrApplyDead (never retry, the payload
// is permanently unusable).
func (s *Store) applyProjectsMutationTx(tx *sql.Tx, mutation SyncMutation) error {
	payload := []byte(mutation.Payload)
	switch strings.TrimSpace(mutation.Entity) {
	case SyncEntityProjectCard:
		return s.applyProjectCardMutationTx(tx, mutation, payload)
	case SyncEntityTask:
		return s.applyTaskMutationTx(tx, mutation, payload)
	case SyncEntityEvidence:
		return s.applyEvidenceMutationTx(tx, mutation, payload)
	case SyncEntityTaskLink:
		return s.applyTaskLinkMutationTx(tx, mutation, payload)
	case SyncEntityObservationRef:
		return s.applyObservationRefMutationTx(tx, mutation, payload)
	case SyncEntityProjectAlias:
		return s.applyProjectAliasMutationTx(tx, mutation, payload)
	case SyncEntityBenchmark:
		return s.applyBenchmarkMutationTx(tx, mutation, payload)
	default:
		return fmt.Errorf("unknown engram-projects entity %q", mutation.Entity)
	}
}

// checkPayloadProject enforces the project scoping mirror of
// ErrCrossProjectRelation: a payload must name its project, and when the
// journal entry also carries one they must agree.
func checkPayloadProject(mutation SyncMutation, payloadProject string) error {
	payloadProject = strings.TrimSpace(payloadProject)
	if payloadProject == "" {
		return fmt.Errorf("%w: %s payload project is required", ErrApplyDead, mutation.Entity)
	}
	mutationProject := strings.TrimSpace(mutation.Project)
	if mutationProject == "" {
		return nil
	}
	normalizedPayload, _ := NormalizeProject(payloadProject)
	normalizedMutation, _ := NormalizeProject(mutationProject)
	if normalizedPayload != normalizedMutation {
		return fmt.Errorf("%w: %s payload project %q does not match mutation project %q",
			ErrApplyDead, mutation.Entity, payloadProject, mutationProject)
	}
	return nil
}

// clearDeferredTx removes the parked row for exactly this payload once it
// applied, so `engram conflicts replay` stops retrying it. Other payloads
// parked under the same entity are left alone: each of them still carries
// state this replica has not merged yet.
func (s *Store) clearDeferredTx(tx *sql.Tx, entityKey, payload string) error {
	if strings.TrimSpace(entityKey) == "" {
		return nil
	}
	if _, err := s.execHook(tx,
		`DELETE FROM sync_apply_deferred WHERE sync_id = ?`, deferredRowKey(entityKey, payload),
	); err != nil {
		return fmt.Errorf("engram-projects: clear deferred row: %w", err)
	}
	return nil
}

// ─── project_card ────────────────────────────────────────────────────────────

// applyProjectCardMutationTx merges a card with two independent clocks: the
// descriptive fields follow last-writer-wins on updated_at, and the code-graph
// group (graph_commit, graph_built_at, graph_summary) only ever moves forward
// on graph_built_at. Keeping them apart is what stops a stale descriptive
// update from dragging a newer graph stamp backwards — the card would then
// claim facts about a commit it no longer points at, which is exactly what
// ADR-026 forbids.
func (s *Store) applyProjectCardMutationTx(tx *sql.Tx, mutation SyncMutation, payload []byte) error {
	var incoming syncProjectCardPayload
	if err := decodeSyncPayload(payload, &incoming); err != nil {
		return fmt.Errorf("%w: decode project_card payload: %v", ErrApplyDead, err)
	}
	incoming.Slug = strings.TrimSpace(incoming.Slug)
	incoming.SyncID = strings.TrimSpace(incoming.SyncID)
	if incoming.Project == "" {
		incoming.Project = incoming.Slug
	}
	if incoming.Slug == "" || incoming.SyncID == "" {
		return fmt.Errorf("%w: project_card payload requires slug and sync_id", ErrApplyDead)
	}
	if strings.TrimSpace(incoming.DisplayName) == "" {
		return fmt.Errorf("%w: project_card payload requires display_name", ErrApplyDead)
	}
	if err := checkPayloadProject(mutation, incoming.Project); err != nil {
		return err
	}
	// Mirror of the card's two DDL invariants. Checking them here turns a
	// malformed payload into a dead row the operator can see, instead of a
	// constraint violation that would fail the whole chunk it travelled in.
	incoming.GraphCommit = trimPtr(incoming.GraphCommit)
	incoming.GraphBuiltAt = trimPtr(incoming.GraphBuiltAt)
	incoming.GraphSummary = trimPtr(incoming.GraphSummary)
	if (incoming.GraphCommit == nil) != (incoming.GraphBuiltAt == nil) {
		return fmt.Errorf("%w: project_card graph_commit and graph_built_at must be set together", ErrApplyDead)
	}
	if incoming.GraphSummary != nil && incoming.GraphCommit == nil {
		return fmt.Errorf("%w: project_card graph_summary requires graph_commit", ErrApplyDead)
	}
	if strings.TrimSpace(incoming.DefaultBranch) == "" {
		incoming.DefaultBranch = "master"
	}
	if strings.TrimSpace(incoming.JiraProject) == "" {
		incoming.JiraProject = DefaultJiraProject()
	}
	if strings.TrimSpace(incoming.GraphPath) == "" {
		incoming.GraphPath = "graphify-out/graph.json"
	}

	if strings.TrimSpace(incoming.Kind) == "" {
		incoming.Kind = "repo"
	}
	incoming.ParentSlug = trimPtr(incoming.ParentSlug)

	local, err := scanProjectCardSyncPayload(
		tx.QueryRow(`SELECT `+projectCardSyncSelect+` FROM project_cards WHERE slug = ?`, incoming.Slug))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		parentSlug, depth, hierarchyErr := s.resolveReplicatedParentTx(tx, incoming.Slug, incoming.ParentSlug)
		if hierarchyErr != nil {
			return hierarchyErr
		}
		if _, err := s.execHook(tx, `
			INSERT INTO project_cards
				(slug, sync_id, display_name, repo_url, default_branch, jira_project, jira_component,
				 knowledge_hub_path, graph_path, graph_commit, graph_built_at, graph_summary, owner,
				 created_at, updated_at, deleted_at, parent_slug, depth, kind, description, icon, color, tags)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			incoming.Slug, incoming.SyncID, incoming.DisplayName, nullableStr(incoming.RepoURL),
			incoming.DefaultBranch, incoming.JiraProject, nullableStr(incoming.JiraComponent),
			nullableStr(incoming.KnowledgeHubPath), incoming.GraphPath, nullableStr(incoming.GraphCommit),
			nullableStr(incoming.GraphBuiltAt), nullableStr(incoming.GraphSummary), nullableStr(incoming.Owner),
			incoming.CreatedAt, incoming.UpdatedAt, nullableStr(incoming.DeletedAt),
			nullableStr(parentSlug), depth, incoming.Kind, nullableStr(incoming.Description),
			nullableStr(incoming.Icon), nullableStr(incoming.Color), nullableStr(incoming.Tags),
		); err != nil {
			return fmt.Errorf("engram-projects: insert replicated project card: %w", err)
		}
		if parentSlug == nil && incoming.ParentSlug != nil {
			if err := s.recordCardHierarchyCycleTx(tx, incoming); err != nil {
				return err
			}
		}
		return s.clearDeferredTx(tx, incoming.SyncID, mutation.Payload)
	case err != nil:
		return fmt.Errorf("engram-projects: read local project card: %w", err)
	}

	merged := local
	if incomingWinsLWW(local.UpdatedAt, incoming.UpdatedAt,
		cardDescriptiveDigest(local), cardDescriptiveDigest(incoming)) {
		merged.SyncID = incoming.SyncID
		merged.DisplayName = incoming.DisplayName
		merged.RepoURL = incoming.RepoURL
		merged.DefaultBranch = incoming.DefaultBranch
		merged.JiraProject = incoming.JiraProject
		merged.JiraComponent = incoming.JiraComponent
		merged.KnowledgeHubPath = incoming.KnowledgeHubPath
		merged.GraphPath = incoming.GraphPath
		merged.Owner = incoming.Owner
		merged.UpdatedAt = incoming.UpdatedAt
		merged.DeletedAt = incoming.DeletedAt
		merged.ParentSlug = incoming.ParentSlug
		merged.Kind = incoming.Kind
		merged.Description = incoming.Description
		merged.Icon = incoming.Icon
		merged.Color = incoming.Color
		merged.Tags = incoming.Tags
	}

	// The parent is checked against this replica's tree, not the sender's:
	// two cards reparented on two machines can be individually legal and
	// together form a loop, and the replica that ends up holding both is the
	// only one that can see it.
	parentSlug, depth, hierarchyErr := s.resolveReplicatedParentTx(tx, merged.Slug, merged.ParentSlug)
	if hierarchyErr != nil {
		return hierarchyErr
	}
	brokeHierarchy := parentSlug == nil && merged.ParentSlug != nil
	merged.ParentSlug = parentSlug
	merged.Depth = depth
	// created_at is the earliest known creation, never a later replica's guess.
	if timestampLess(incoming.CreatedAt, merged.CreatedAt) {
		merged.CreatedAt = incoming.CreatedAt
	}
	// Monotone graph group: a newer build wins, a stale one is ignored whole.
	// Two builds stamped at the same instant are separated by the digest of
	// the group itself, so the winner does not depend on arrival order.
	if incoming.GraphBuiltAt != nil && (local.GraphBuiltAt == nil ||
		incomingWinsLWW(*local.GraphBuiltAt, *incoming.GraphBuiltAt,
			cardGraphDigest(local), cardGraphDigest(incoming))) {
		merged.GraphCommit = incoming.GraphCommit
		merged.GraphBuiltAt = incoming.GraphBuiltAt
		merged.GraphSummary = incoming.GraphSummary
	}

	if _, err := s.execHook(tx, `
		UPDATE project_cards SET
			sync_id = ?, display_name = ?, repo_url = ?, default_branch = ?, jira_project = ?,
			jira_component = ?, knowledge_hub_path = ?, graph_path = ?, graph_commit = ?,
			graph_built_at = ?, graph_summary = ?, owner = ?, created_at = ?, updated_at = ?,
			deleted_at = ?, parent_slug = ?, depth = ?, kind = ?, description = ?, icon = ?,
			color = ?, tags = ?
		WHERE slug = ?`,
		merged.SyncID, merged.DisplayName, nullableStr(merged.RepoURL), merged.DefaultBranch,
		merged.JiraProject, nullableStr(merged.JiraComponent), nullableStr(merged.KnowledgeHubPath),
		merged.GraphPath, nullableStr(merged.GraphCommit), nullableStr(merged.GraphBuiltAt),
		nullableStr(merged.GraphSummary), nullableStr(merged.Owner), merged.CreatedAt,
		merged.UpdatedAt, nullableStr(merged.DeletedAt), nullableStr(merged.ParentSlug), merged.Depth,
		merged.Kind, nullableStr(merged.Description), nullableStr(merged.Icon),
		nullableStr(merged.Color), nullableStr(merged.Tags), merged.Slug,
	); err != nil {
		return fmt.Errorf("engram-projects: update replicated project card: %w", err)
	}
	if brokeHierarchy {
		if err := s.recordCardHierarchyCycleTx(tx, incoming); err != nil {
			return err
		}
	}
	if err := s.rewriteSubtreeDepthTx(tx, merged.Slug, merged.Depth); err != nil {
		return err
	}
	return s.clearDeferredTx(tx, incoming.SyncID, mutation.Payload)
}

// cardHierarchyCycleReason is the last_error recorded on the dead row of a card
// whose replicated parent could not be honoured. `engram doctor` surfaces it;
// the card stays usable at the top of the tree until somebody decides where it
// really belongs.
const cardHierarchyCycleReason = "project_hierarchy_cycle"

// resolveReplicatedParentTx decides what parent a pulled card may actually
// have here. It returns (nil, 0, nil) when the parent has to be dropped —
// because it would close a loop or push the card past the depth limit — and
// ErrProjectsFKMissing when the parent card simply has not arrived yet, which
// is a wait rather than a verdict.
//
// Dropping the parent instead of refusing the card is the only convergent
// option: the alternative leaves one replica holding a card the others have
// and calling it invalid, which is exactly the divergence replication exists
// to prevent.
func (s *Store) resolveReplicatedParentTx(tx *sql.Tx, slug string, parent *string) (*string, int, error) {
	if parent == nil {
		return nil, 0, nil
	}
	if *parent == slug {
		return nil, 0, nil
	}
	var parentDepth int
	var above *string
	err := tx.QueryRow(
		`SELECT depth, parent_slug FROM project_cards WHERE slug = ?`, *parent,
	).Scan(&parentDepth, &above)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, 0, fmt.Errorf("%w: project_card %s waits for parent %q", ErrProjectsFKMissing, slug, *parent)
	}
	if err != nil {
		return nil, 0, fmt.Errorf("engram-projects: read replicated parent card: %w", err)
	}
	if cycleErr := s.ancestorsExclude(tx, above, slug); cycleErr != nil {
		return nil, 0, nil
	}
	depth := parentDepth + 1
	if depth > maxProjectDepth {
		return nil, 0, nil
	}
	return parent, depth, nil
}

// recordCardHierarchyCycleTx parks the payload whose parent was dropped, so the
// decision is visible rather than silent.
func (s *Store) recordCardHierarchyCycleTx(tx *sql.Tx, card syncProjectCardPayload) error {
	encoded, err := json.Marshal(card)
	if err != nil {
		return fmt.Errorf("engram-projects: encode card with a rejected parent: %w", err)
	}
	message := fmt.Sprintf("%s: parent %q was dropped from card %q",
		cardHierarchyCycleReason, ptrOrEmpty(card.ParentSlug), card.Slug)
	if _, err := s.execHook(tx, `
		INSERT INTO sync_apply_deferred
			(sync_id, entity, payload, apply_status, retry_count, first_seen_at, last_error, last_attempted_at)
		VALUES (?, ?, ?, 'dead', 0, datetime('now'), ?, datetime('now'))
		ON CONFLICT(sync_id) DO UPDATE SET
			payload           = excluded.payload,
			apply_status      = 'dead',
			last_error        = excluded.last_error,
			last_attempted_at = datetime('now')`,
		card.SyncID, SyncEntityProjectCard, string(encoded), message,
	); err != nil {
		return fmt.Errorf("engram-projects: record project hierarchy cycle: %w", err)
	}
	return nil
}

// ─── task ────────────────────────────────────────────────────────────────────

// taskKeyConflictReason is the last_error recorded on the sync_apply_deferred
// row of a task that lost a jira_key race. `engram doctor` surfaces it and the
// reconciliation is manual (RFC section 10.5 step 4).
const taskKeyConflictReason = "task_key_conflict"

// quarantinedJiraKey is the deterministic key a losing task carries so the
// UNIQUE index frees the real key for the winner. It keeps the DDL's
// `jira_key GLOB '[A-Z]*-[0-9]*'` shape and is derived only from values both
// replicas already have, so both compute the same string.
func quarantinedJiraKey(jiraKey, syncID string) string {
	return strings.TrimSpace(jiraKey) + "-conflict-" + strings.TrimSpace(syncID)
}

// taskKeyWinner returns true when candidate A owns the contested jira_key:
// the older created_at wins, ties broken by the smaller sync_id. Total,
// deterministic, and evaluated identically on both replicas.
func taskKeyWinner(createdA, syncA, createdB, syncB string) bool {
	if timestampLess(createdA, createdB) {
		return true
	}
	if timestampLess(createdB, createdA) {
		return false
	}
	return strings.TrimSpace(syncA) < strings.TrimSpace(syncB)
}

func (s *Store) applyTaskMutationTx(tx *sql.Tx, mutation SyncMutation, payload []byte) error {
	var incoming syncTaskPayload
	if err := decodeSyncPayload(payload, &incoming); err != nil {
		return fmt.Errorf("%w: decode task payload: %v", ErrApplyDead, err)
	}
	incoming.SyncID = strings.TrimSpace(incoming.SyncID)
	incoming.Project = strings.TrimSpace(incoming.Project)
	if incoming.SyncID == "" {
		return fmt.Errorf("%w: task payload requires sync_id", ErrApplyDead)
	}
	if strings.TrimSpace(incoming.Title) == "" || strings.TrimSpace(incoming.Kind) == "" ||
		strings.TrimSpace(incoming.State) == "" {
		return fmt.Errorf("%w: task payload requires title, kind and state", ErrApplyDead)
	}
	if err := checkPayloadProject(mutation, incoming.Project); err != nil {
		return err
	}

	// tasks.project is a RESTRICT foreign key onto project_cards.slug: a task
	// that arrives before its card is deferred, never dropped.
	var cardExists int
	err := tx.QueryRow(`SELECT 1 FROM project_cards WHERE slug = ?`, incoming.Project).Scan(&cardExists)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: task %s waits for project card %q", ErrProjectsFKMissing, incoming.SyncID, incoming.Project)
	}
	if err != nil {
		return fmt.Errorf("engram-projects: check project card for task: %w", err)
	}

	if err := s.resolveTaskKeyConflictTx(tx, &incoming); err != nil {
		return err
	}

	local, err := scanTaskSyncPayload(
		tx.QueryRow(`SELECT `+taskSyncSelect+` FROM tasks WHERE sync_id = ?`, incoming.SyncID))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if err := s.insertReplicatedTaskTx(tx, incoming); err != nil {
			return err
		}
		return s.clearDeferredTx(tx, incoming.SyncID, mutation.Payload)
	case err != nil:
		return fmt.Errorf("engram-projects: read local task: %w", err)
	}

	merged := mergeTaskPayloads(local, incoming)
	parentTaskID, err := s.replicatedParentTaskIDTx(tx, merged.SyncID, merged.ParentTaskSyncID)
	if err != nil {
		return err
	}
	if _, err := s.execHook(tx, `
		UPDATE tasks SET
			project = ?, jira_key = ?, sdd_change = ?, title = ?, kind = ?, state = ?,
			jira_status = ?, jira_status_category = ?, state_synced_at = ?, branch = ?, pr_url = ?,
			knowledge_ref = ?, assignee = ?, created_at = ?, updated_at = ?, closed_at = ?, deleted_at = ?,
			slug = ?, summary = ?, pending_note = ?, vault_path = ?, parent_task_id = ?,
			parent_task_sync_id = ?
		WHERE sync_id = ?`,
		merged.Project, nullableStr(merged.JiraKey), nullableStr(merged.SDDChange), merged.Title,
		merged.Kind, merged.State, nullableStr(merged.JiraStatus), nullableStr(merged.JiraStatusCategory),
		nullableStr(merged.StateSyncedAt), nullableStr(merged.Branch), nullableStr(merged.PRUrl),
		nullableStr(merged.KnowledgeRef), nullableStr(merged.Assignee), merged.CreatedAt,
		merged.UpdatedAt, nullableStr(merged.ClosedAt), nullableStr(merged.DeletedAt),
		nullableStr(merged.Slug), nullableStr(merged.Summary), nullableStr(merged.PendingNote),
		nullableStr(merged.VaultPath), nullableInt64(parentTaskID), nullableStr(merged.ParentTaskSyncID),
		merged.SyncID,
	); err != nil {
		return fmt.Errorf("engram-projects: update replicated task: %w", err)
	}
	return s.clearDeferredTx(tx, incoming.SyncID, mutation.Payload)
}

// replicatedParentTaskIDTx resolves the parent a payload names to a local row
// id. A parent that has not arrived leaves the link recorded by sync_id alone:
// the pair is what replicates, and the numeric id is a local convenience that
// can be filled in when the parent turns up. A payload naming itself is
// dropped rather than deferred — the CHECK would refuse it forever.
func (s *Store) replicatedParentTaskIDTx(tx *sql.Tx, syncID string, parentSyncID *string) (*int64, error) {
	parent := trimPtr(parentSyncID)
	if parent == nil || *parent == strings.TrimSpace(syncID) {
		return nil, nil
	}
	var id int64
	err := tx.QueryRow(`SELECT id FROM tasks WHERE sync_id = ?`, *parent).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("engram-projects: resolve replicated parent task: %w", err)
	}
	return &id, nil
}

func (s *Store) insertReplicatedTaskTx(tx *sql.Tx, t syncTaskPayload) error {
	// The DDL only accepts closed_at on a closed state, and a first write must
	// land on exactly the value a later merge would compute for the same
	// payload — otherwise the row a replica inserts differs from the row
	// another replica reaches by merging.
	t.ClosedAt = taskClosedAt(t)
	if parent := trimPtr(t.ParentTaskSyncID); parent != nil && *parent == strings.TrimSpace(t.SyncID) {
		t.ParentTaskSyncID = nil
	}
	parentTaskID, err := s.replicatedParentTaskIDTx(tx, t.SyncID, t.ParentTaskSyncID)
	if err != nil {
		return err
	}
	if _, err := s.execHook(tx, `
		INSERT INTO tasks (sync_id, project, jira_key, sdd_change, title, kind, state, jira_status,
			jira_status_category, state_synced_at, branch, pr_url, knowledge_ref, assignee,
			created_at, updated_at, closed_at, deleted_at, slug, summary, pending_note, vault_path,
			parent_task_id, parent_task_sync_id)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.SyncID, t.Project, nullableStr(t.JiraKey), nullableStr(t.SDDChange), t.Title, t.Kind, t.State,
		nullableStr(t.JiraStatus), nullableStr(t.JiraStatusCategory), nullableStr(t.StateSyncedAt),
		nullableStr(t.Branch), nullableStr(t.PRUrl), nullableStr(t.KnowledgeRef), nullableStr(t.Assignee),
		t.CreatedAt, t.UpdatedAt, nullableStr(t.ClosedAt), nullableStr(t.DeletedAt),
		nullableStr(t.Slug), nullableStr(t.Summary), nullableStr(t.PendingNote), nullableStr(t.VaultPath),
		nullableInt64(parentTaskID), nullableStr(t.ParentTaskSyncID),
	); err != nil {
		return fmt.Errorf("engram-projects: insert replicated task: %w", err)
	}
	return nil
}

// mergeTaskPayloads merges two versions of the same task (same sync_id) under
// three independent clocks, so the result does not depend on which version
// arrived first: updated_at for the fields a person edits, state_synced_at for
// the two fields copied from Jira, and taskStateClock for `state` itself,
// which both writers touch. deleted_at is monotone and created_at keeps the
// earliest value any replica knows.
func mergeTaskPayloads(local, incoming syncTaskPayload) syncTaskPayload {
	merged := local

	if incomingWinsLWW(local.UpdatedAt, incoming.UpdatedAt,
		taskDescriptiveDigest(local), taskDescriptiveDigest(incoming)) {
		merged.Project = incoming.Project
		merged.JiraKey = incoming.JiraKey
		merged.SDDChange = incoming.SDDChange
		merged.Title = incoming.Title
		merged.Kind = incoming.Kind
		merged.Branch = incoming.Branch
		merged.PRUrl = incoming.PRUrl
		merged.KnowledgeRef = incoming.KnowledgeRef
		merged.Assignee = incoming.Assignee
		merged.UpdatedAt = incoming.UpdatedAt
		merged.Slug = incoming.Slug
		merged.Summary = incoming.Summary
		merged.PendingNote = incoming.PendingNote
		merged.VaultPath = incoming.VaultPath
		merged.ParentTaskSyncID = incoming.ParentTaskSyncID
	}
	if parent := trimPtr(merged.ParentTaskSyncID); parent != nil && *parent == strings.TrimSpace(merged.SyncID) {
		merged.ParentTaskSyncID = nil
	}

	localMirror := ptrOrEmpty(trimPtr(local.StateSyncedAt))
	incomingMirror := ptrOrEmpty(trimPtr(incoming.StateSyncedAt))
	if incomingMirror != "" && (localMirror == "" ||
		incomingWinsLWW(localMirror, incomingMirror, taskMirrorDigest(local), taskMirrorDigest(incoming))) {
		merged.JiraStatus = incoming.JiraStatus
		merged.JiraStatusCategory = incoming.JiraStatusCategory
		merged.StateSyncedAt = incoming.StateSyncedAt
	}

	if incomingWinsLWW(taskStateClock(local), taskStateClock(incoming),
		taskStateDigest(local), taskStateDigest(incoming)) {
		merged.State = incoming.State
		merged.ClosedAt = taskClosedAt(incoming)
	} else {
		merged.ClosedAt = taskClosedAt(local)
	}

	merged.DeletedAt = earliestTimestamp(local.DeletedAt, incoming.DeletedAt)
	if timestampLess(incoming.CreatedAt, merged.CreatedAt) {
		merged.CreatedAt = incoming.CreatedAt
	}
	return merged
}

// resolveTaskKeyConflictTx enforces the jira_key rule of RFC section 10.3.
// jira_key is globally UNIQUE, so two replicas that each opened a task for the
// same ticket produce two sync_ids claiming one key. The winner is fixed by
// (created_at, sync_id) — the older task keeps the ticket — and the loser
// keeps its row under a quarantined key derived from its own sync_id, plus a
// dead sync_apply_deferred row carrying its original payload for manual
// reconciliation (RFC section 10.5, step 4).
//
// Two details of that shape are deliberate. The loser's row is quarantined on
// BOTH replicas, including the one that never held the key locally, because
// anything else leaves the two databases with a different number of rows. And
// the loser is quarantined rather than removed: deleting it would cascade
// through its evidence and its observation links, destroying rows whose own
// mutations may already be acked — a conflict over a ticket number is not a
// reason to lose captured evidence.
func (s *Store) resolveTaskKeyConflictTx(tx *sql.Tx, incoming *syncTaskPayload) error {
	key := ptrOrEmpty(trimPtr(incoming.JiraKey))
	if key == "" {
		return nil
	}

	var holderSyncID, holderCreatedAt string
	err := tx.QueryRow(
		`SELECT sync_id, created_at FROM tasks WHERE jira_key = ? AND sync_id <> ?`,
		key, incoming.SyncID,
	).Scan(&holderSyncID, &holderCreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("engram-projects: check jira_key holder: %w", err)
	}

	if taskKeyWinner(holderCreatedAt, holderSyncID, incoming.CreatedAt, incoming.SyncID) {
		// The local holder keeps the ticket; the incoming task is quarantined.
		if err := s.recordTaskKeyConflictTx(tx, incoming.SyncID, key, holderSyncID, *incoming); err != nil {
			return err
		}
		quarantined := quarantinedJiraKey(key, incoming.SyncID)
		incoming.JiraKey = &quarantined
		return nil
	}

	// The incoming task wins: quarantine the local holder in place, keeping
	// its own sync_id so any evidence already pointing at it stays valid.
	holder, err := scanTaskSyncPayload(
		tx.QueryRow(`SELECT `+taskSyncSelect+` FROM tasks WHERE sync_id = ?`, holderSyncID))
	if err != nil {
		return fmt.Errorf("engram-projects: read jira_key holder: %w", err)
	}
	if err := s.recordTaskKeyConflictTx(tx, holderSyncID, key, incoming.SyncID, holder); err != nil {
		return err
	}
	if _, err := s.execHook(tx,
		`UPDATE tasks SET jira_key = ? WHERE sync_id = ?`,
		quarantinedJiraKey(key, holderSyncID), holderSyncID,
	); err != nil {
		return fmt.Errorf("engram-projects: quarantine jira_key holder: %w", err)
	}
	return nil
}

func (s *Store) recordTaskKeyConflictTx(tx *sql.Tx, loserSyncID, jiraKey, winnerSyncID string, loser syncTaskPayload) error {
	encoded, err := json.Marshal(loser)
	if err != nil {
		return fmt.Errorf("engram-projects: encode conflicted task: %w", err)
	}
	message := fmt.Sprintf("%s: jira_key %q kept by task %s", taskKeyConflictReason, jiraKey, winnerSyncID)
	if _, err := s.execHook(tx, `
		INSERT INTO sync_apply_deferred
			(sync_id, entity, payload, apply_status, retry_count, first_seen_at, last_error, last_attempted_at)
		VALUES (?, ?, ?, 'dead', 0, datetime('now'), ?, datetime('now'))
		ON CONFLICT(sync_id) DO UPDATE SET
			payload           = excluded.payload,
			apply_status      = 'dead',
			last_error        = excluded.last_error,
			last_attempted_at = datetime('now')`,
		loserSyncID, SyncEntityTask, string(encoded), message,
	); err != nil {
		return fmt.Errorf("engram-projects: record task key conflict: %w", err)
	}
	return nil
}

// ─── evidence ────────────────────────────────────────────────────────────────

// applyEvidenceMutationTx treats an evidence row as immutable once created.
// Only three things can still move, and each moves in one direction only:
// attached_jira from 0 to 1, attached_confluence_url from unset to a value
// (later disagreements settled by occurred_at), and deleted_at to the earliest
// deletion any replica saw. Everything else keeps whatever the row already
// holds, which is why the byte-level facts (path, sha256, captured_at) can
// never be rewritten by a late replica.
func (s *Store) applyEvidenceMutationTx(tx *sql.Tx, mutation SyncMutation, payload []byte) error {
	var incoming syncEvidencePayload
	if err := decodeSyncPayload(payload, &incoming); err != nil {
		return fmt.Errorf("%w: decode evidence payload: %v", ErrApplyDead, err)
	}
	incoming.SyncID = strings.TrimSpace(incoming.SyncID)
	incoming.Project = strings.TrimSpace(incoming.Project)
	incoming.TaskSyncID = strings.TrimSpace(incoming.TaskSyncID)
	if incoming.SyncID == "" || incoming.TaskSyncID == "" {
		return fmt.Errorf("%w: evidence payload requires sync_id and task_sync_id", ErrApplyDead)
	}
	if strings.TrimSpace(incoming.Path) == "" || strings.TrimSpace(incoming.SHA256) == "" ||
		strings.TrimSpace(incoming.Kind) == "" || strings.TrimSpace(incoming.Proves) == "" {
		return fmt.Errorf("%w: evidence payload requires path, sha256, kind and proves", ErrApplyDead)
	}
	if err := checkPayloadProject(mutation, incoming.Project); err != nil {
		return err
	}

	var taskID int64
	err := tx.QueryRow(`SELECT id FROM tasks WHERE sync_id = ?`, incoming.TaskSyncID).Scan(&taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: evidence %s waits for task %s", ErrProjectsFKMissing, incoming.SyncID, incoming.TaskSyncID)
	}
	if err != nil {
		return fmt.Errorf("engram-projects: resolve evidence task: %w", err)
	}

	if strings.TrimSpace(incoming.Category) == "" {
		incoming.Category = DefaultEvidenceCategory
	}

	local, err := scanEvidenceSyncPayload(
		tx.QueryRow(`SELECT `+evidenceSyncSelect+` FROM evidence WHERE sync_id = ?`, incoming.SyncID))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		attachedJira := 0
		if incoming.AttachedJira {
			attachedJira = 1
		}
		if _, err := s.execHook(tx, `
			INSERT INTO evidence (sync_id, project, task_id, task_sync_id, path, sha256, category, kind, proves,
				config_stamp, captured_at, attached_jira, attached_confluence_url, size_bytes,
				manifest_path, location_set_at, created_at, deleted_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			incoming.SyncID, incoming.Project, taskID, incoming.TaskSyncID, incoming.Path, incoming.SHA256,
			incoming.Category, incoming.Kind, incoming.Proves, nullableStr(incoming.ConfigStamp),
			incoming.CapturedAt, attachedJira, nullableStr(incoming.AttachedConfluenceURL),
			nullableInt64(incoming.SizeBytes), nullableStr(incoming.ManifestPath),
			evidenceLocationClock(incoming), incoming.CreatedAt, nullableStr(incoming.DeletedAt),
		); err != nil {
			return fmt.Errorf("engram-projects: insert replicated evidence: %w", err)
		}
		return s.clearDeferredTx(tx, incoming.SyncID, mutation.Payload)
	case err != nil:
		return fmt.Errorf("engram-projects: read local evidence: %w", err)
	}

	attachedJira := 0
	if local.AttachedJira || incoming.AttachedJira {
		attachedJira = 1
	}
	confluenceURL := local.AttachedConfluenceURL
	switch {
	case trimPtr(confluenceURL) == nil:
		confluenceURL = incoming.AttachedConfluenceURL
	case trimPtr(incoming.AttachedConfluenceURL) == nil:
		// keep local
	case *confluenceURL != *incoming.AttachedConfluenceURL:
		if timestampLess(local.OccurredAt, incoming.OccurredAt) {
			confluenceURL = incoming.AttachedConfluenceURL
		} else if local.OccurredAt == incoming.OccurredAt && *incoming.AttachedConfluenceURL > *confluenceURL {
			confluenceURL = incoming.AttachedConfluenceURL
		}
	}
	deletedAt := earliestTimestamp(local.DeletedAt, incoming.DeletedAt)

	// Where the file lives is the one group of an evidence row that moves.
	// A rescan that finds the same bytes under a new path or a new category is
	// reporting a move, not a second capture, and the later report wins.
	if strings.TrimSpace(local.Category) == "" {
		local.Category = DefaultEvidenceCategory
	}
	path, category := local.Path, local.Category
	locationSetAt := local.LocationSetAt
	if incomingWinsLWW(ptrOrEmpty(local.LocationSetAt), evidenceLocationClock(incoming),
		evidenceLocationDigest(local), evidenceLocationDigest(incoming)) {
		path, category = incoming.Path, incoming.Category
		clock := evidenceLocationClock(incoming)
		locationSetAt = &clock
	}

	if _, err := s.execHook(tx, `
		UPDATE evidence SET attached_jira = ?, attached_confluence_url = ?, deleted_at = ?,
			path = ?, category = ?, location_set_at = ?
		WHERE sync_id = ?`,
		attachedJira, nullableStr(confluenceURL), nullableStr(deletedAt), path, category,
		nullableStr(trimPtr(locationSetAt)), incoming.SyncID,
	); err != nil {
		return fmt.Errorf("engram-projects: update replicated evidence: %w", err)
	}
	return s.clearDeferredTx(tx, incoming.SyncID, mutation.Payload)
}

// evidenceLocationClock is the timestamp a payload claims for where the file
// lives, falling back to when the row was created for a sender that predates
// the column.
func evidenceLocationClock(e syncEvidencePayload) string {
	if set := trimPtr(e.LocationSetAt); set != nil {
		return *set
	}
	return strings.TrimSpace(e.CreatedAt)
}

// ─── task_link ───────────────────────────────────────────────────────────────

// applyTaskLinkMutationTx keeps task_observations as an LWW-element-set: an
// unlink is remembered in task_link_tombstones with its clock, so a link
// mutation that was created before that unlink can never resurrect the pair,
// no matter which of the two arrives first. On an exact tie the delete wins,
// which is the rule RFC section 10.3 states.
func (s *Store) applyTaskLinkMutationTx(tx *sql.Tx, mutation SyncMutation, payload []byte) error {
	var incoming syncTaskLinkPayload
	if err := decodeSyncPayload(payload, &incoming); err != nil {
		return fmt.Errorf("%w: decode task_link payload: %v", ErrApplyDead, err)
	}
	incoming.TaskSyncID = strings.TrimSpace(incoming.TaskSyncID)
	incoming.ObservationSyncID = strings.TrimSpace(incoming.ObservationSyncID)
	incoming.Role = strings.TrimSpace(incoming.Role)
	incoming.LinkedAt = strings.TrimSpace(incoming.LinkedAt)
	if incoming.TaskSyncID == "" || incoming.ObservationSyncID == "" {
		return fmt.Errorf("%w: task_link payload requires task_sync_id and observation_sync_id", ErrApplyDead)
	}
	if incoming.LinkedAt == "" {
		return fmt.Errorf("%w: task_link payload requires linked_at", ErrApplyDead)
	}
	if err := checkPayloadProject(mutation, incoming.Project); err != nil {
		return err
	}
	key := taskLinkKey(incoming.TaskSyncID, incoming.ObservationSyncID)

	if deletedAt := trimPtr(incoming.DeletedAt); deletedAt != nil {
		return s.applyTaskLinkDeleteTx(tx, incoming, key, *deletedAt, mutation.Payload)
	}
	if incoming.Role == "" {
		incoming.Role = "context"
	}

	var tombstone string
	err := tx.QueryRow(
		`SELECT deleted_at FROM task_link_tombstones WHERE task_sync_id = ? AND observation_sync_id = ?`,
		incoming.TaskSyncID, incoming.ObservationSyncID,
	).Scan(&tombstone)
	switch {
	case err == nil:
		if !timestampLess(tombstone, incoming.LinkedAt) {
			// The unlink is at least as recent as this link: delete wins.
			return s.clearDeferredTx(tx, key, mutation.Payload)
		}
		if _, err := s.execHook(tx,
			`DELETE FROM task_link_tombstones WHERE task_sync_id = ? AND observation_sync_id = ?`,
			incoming.TaskSyncID, incoming.ObservationSyncID,
		); err != nil {
			return fmt.Errorf("engram-projects: clear task link tombstone: %w", err)
		}
	case errors.Is(err, sql.ErrNoRows):
		// no tombstone, proceed
	default:
		return fmt.Errorf("engram-projects: read task link tombstone: %w", err)
	}

	var taskID int64
	err = tx.QueryRow(`SELECT id FROM tasks WHERE sync_id = ?`, incoming.TaskSyncID).Scan(&taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: task_link %s waits for task %s", ErrProjectsFKMissing, key, incoming.TaskSyncID)
	}
	if err != nil {
		return fmt.Errorf("engram-projects: resolve task link task: %w", err)
	}
	var observationID int64
	err = tx.QueryRow(`SELECT id FROM observations WHERE sync_id = ?`, incoming.ObservationSyncID).Scan(&observationID)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: task_link %s waits for observation %s", ErrProjectsFKMissing, key, incoming.ObservationSyncID)
	}
	if err != nil {
		return fmt.Errorf("engram-projects: resolve task link observation: %w", err)
	}

	var localRole, localLinkedAt string
	err = tx.QueryRow(
		`SELECT role, linked_at FROM task_observations WHERE task_sync_id = ? AND observation_sync_id = ?`,
		incoming.TaskSyncID, incoming.ObservationSyncID,
	).Scan(&localRole, &localLinkedAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if _, err := s.execHook(tx, `
			INSERT INTO task_observations (task_id, observation_id, task_sync_id, observation_sync_id, role, linked_at)
			VALUES (?, ?, ?, ?, ?, ?)`,
			taskID, observationID, incoming.TaskSyncID, incoming.ObservationSyncID, incoming.Role, incoming.LinkedAt,
		); err != nil {
			return fmt.Errorf("engram-projects: insert replicated task link: %w", err)
		}
	case err != nil:
		return fmt.Errorf("engram-projects: read local task link: %w", err)
	default:
		role, linkedAt := localRole, localLinkedAt
		if timestampLess(localLinkedAt, incoming.LinkedAt) ||
			(localLinkedAt == incoming.LinkedAt && incoming.Role > localRole) {
			role, linkedAt = incoming.Role, incoming.LinkedAt
		}
		if _, err := s.execHook(tx,
			`UPDATE task_observations SET role = ?, linked_at = ?
			 WHERE task_sync_id = ? AND observation_sync_id = ?`,
			role, linkedAt, incoming.TaskSyncID, incoming.ObservationSyncID,
		); err != nil {
			return fmt.Errorf("engram-projects: update replicated task link: %w", err)
		}
	}
	return s.clearDeferredTx(tx, key, mutation.Payload)
}

func (s *Store) applyTaskLinkDeleteTx(tx *sql.Tx, incoming syncTaskLinkPayload, key, deletedAt, rawPayload string) error {
	if _, err := s.execHook(tx, `
		INSERT INTO task_link_tombstones (task_sync_id, observation_sync_id, deleted_at)
		VALUES (?, ?, ?)
		ON CONFLICT(task_sync_id, observation_sync_id) DO UPDATE SET
			deleted_at = CASE WHEN excluded.deleted_at > task_link_tombstones.deleted_at
			                  THEN excluded.deleted_at ELSE task_link_tombstones.deleted_at END`,
		incoming.TaskSyncID, incoming.ObservationSyncID, deletedAt,
	); err != nil {
		return fmt.Errorf("engram-projects: record task link tombstone: %w", err)
	}

	var localLinkedAt string
	err := tx.QueryRow(
		`SELECT linked_at FROM task_observations WHERE task_sync_id = ? AND observation_sync_id = ?`,
		incoming.TaskSyncID, incoming.ObservationSyncID,
	).Scan(&localLinkedAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return s.clearDeferredTx(tx, key, rawPayload)
	case err != nil:
		return fmt.Errorf("engram-projects: read task link for delete: %w", err)
	}
	if timestampLess(deletedAt, localLinkedAt) {
		// A newer link already superseded this unlink.
		return s.clearDeferredTx(tx, key, rawPayload)
	}
	if _, err := s.execHook(tx,
		`DELETE FROM task_observations WHERE task_sync_id = ? AND observation_sync_id = ?`,
		incoming.TaskSyncID, incoming.ObservationSyncID,
	); err != nil {
		return fmt.Errorf("engram-projects: delete replicated task link: %w", err)
	}
	return s.clearDeferredTx(tx, key, rawPayload)
}

// ─── observation_ref ─────────────────────────────────────────────────────────

// applyObservationRefMutationTx is a grow-only set: the UNIQUE index makes a
// re-apply a no-op, and there is no delete in v1, so ordering cannot matter.
func (s *Store) applyObservationRefMutationTx(tx *sql.Tx, mutation SyncMutation, payload []byte) error {
	var incoming syncObservationRefPayload
	if err := decodeSyncPayload(payload, &incoming); err != nil {
		return fmt.Errorf("%w: decode observation_ref payload: %v", ErrApplyDead, err)
	}
	incoming.ObservationSyncID = strings.TrimSpace(incoming.ObservationSyncID)
	incoming.RefKind = strings.TrimSpace(incoming.RefKind)
	incoming.Ref = strings.TrimSpace(incoming.Ref)
	if incoming.ObservationSyncID == "" || incoming.RefKind == "" || incoming.Ref == "" {
		return fmt.Errorf("%w: observation_ref payload requires observation_sync_id, ref_kind and ref", ErrApplyDead)
	}
	if incoming.RefKind == "graph" && trimPtr(incoming.GraphCommit) == nil {
		return fmt.Errorf("%w: observation_ref of kind graph requires graph_commit", ErrApplyDead)
	}
	if err := checkPayloadProject(mutation, incoming.Project); err != nil {
		return err
	}
	createdAt := strings.TrimSpace(incoming.CreatedAt)
	if createdAt == "" {
		createdAt = s.nowUTC()
	}
	if _, err := s.execHook(tx, `
		INSERT OR IGNORE INTO observation_refs (observation_sync_id, ref_kind, ref, graph_commit, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		incoming.ObservationSyncID, incoming.RefKind, incoming.Ref,
		nullableStr(trimPtr(incoming.GraphCommit)), createdAt,
	); err != nil {
		return fmt.Errorf("engram-projects: insert replicated observation ref: %w", err)
	}
	return s.clearDeferredTx(tx,
		observationRefKey(incoming.ObservationSyncID, incoming.RefKind, incoming.Ref), mutation.Payload)
}

// ─── project_alias ───────────────────────────────────────────────────────────

// applyProjectAliasMutationTx keeps project_aliases as an LWW-element-set on
// updated_at, with deletion monotone: once any replica has retired an alias,
// none of them brings it back, and two replicas that saw different retirement
// times settle on the earlier one.
//
// An alias whose target card has not arrived is deferred rather than dropped.
// The foreign key would refuse it, and an alias that quietly disappears is a
// name that starts reporting unknown on one machine and resolving on another.
func (s *Store) applyProjectAliasMutationTx(tx *sql.Tx, mutation SyncMutation, payload []byte) error {
	var incoming syncProjectAliasPayload
	if err := decodeSyncPayload(payload, &incoming); err != nil {
		return fmt.Errorf("%w: decode project_alias payload: %v", ErrApplyDead, err)
	}
	incoming.Alias = strings.TrimSpace(incoming.Alias)
	incoming.SyncID = strings.TrimSpace(incoming.SyncID)
	incoming.Slug = strings.TrimSpace(incoming.Slug)
	if incoming.Project == "" {
		incoming.Project = incoming.Slug
	}
	if incoming.Alias == "" || incoming.SyncID == "" || incoming.Slug == "" {
		return fmt.Errorf("%w: project_alias payload requires alias, sync_id and slug", ErrApplyDead)
	}
	if incoming.Alias == incoming.Slug {
		return fmt.Errorf("%w: project_alias %q cannot point at itself", ErrApplyDead, incoming.Alias)
	}
	if strings.TrimSpace(incoming.Source) == "" {
		incoming.Source = "manual"
	}
	if err := checkPayloadProject(mutation, incoming.Project); err != nil {
		return err
	}

	var cardExists int
	err := tx.QueryRow(`SELECT 1 FROM project_cards WHERE slug = ?`, incoming.Slug).Scan(&cardExists)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: project_alias %s waits for project card %q",
			ErrProjectsFKMissing, incoming.Alias, incoming.Slug)
	}
	if err != nil {
		return fmt.Errorf("engram-projects: check project card for alias: %w", err)
	}

	var local syncProjectAliasPayload
	err = tx.QueryRow(
		`SELECT `+projectAliasSyncSelect+` FROM project_aliases WHERE alias = ?`, incoming.Alias,
	).Scan(&local.Alias, &local.SyncID, &local.Slug, &local.Source, &local.CreatedAt,
		&local.UpdatedAt, &local.DeletedAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if _, err := s.execHook(tx, `
			INSERT INTO project_aliases (alias, sync_id, slug, source, created_at, updated_at, deleted_at)
			VALUES (?,?,?,?,?,?,?)`,
			incoming.Alias, incoming.SyncID, incoming.Slug, incoming.Source,
			incoming.CreatedAt, incoming.UpdatedAt, nullableStr(trimPtr(incoming.DeletedAt)),
		); err != nil {
			return fmt.Errorf("engram-projects: insert replicated project alias: %w", err)
		}
		return s.clearDeferredTx(tx, incoming.Alias, mutation.Payload)
	case err != nil:
		return fmt.Errorf("engram-projects: read local project alias: %w", err)
	}

	merged := local
	if incomingWinsLWW(local.UpdatedAt, incoming.UpdatedAt, aliasDigest(local), aliasDigest(incoming)) {
		merged.SyncID = incoming.SyncID
		merged.Slug = incoming.Slug
		merged.Source = incoming.Source
		merged.UpdatedAt = incoming.UpdatedAt
	}
	merged.DeletedAt = earliestTimestamp(local.DeletedAt, incoming.DeletedAt)
	if timestampLess(incoming.CreatedAt, merged.CreatedAt) {
		merged.CreatedAt = incoming.CreatedAt
	}

	if _, err := s.execHook(tx, `
		UPDATE project_aliases SET sync_id = ?, slug = ?, source = ?, created_at = ?, updated_at = ?,
			deleted_at = ?
		WHERE alias = ?`,
		merged.SyncID, merged.Slug, merged.Source, merged.CreatedAt, merged.UpdatedAt,
		nullableStr(merged.DeletedAt), merged.Alias,
	); err != nil {
		return fmt.Errorf("engram-projects: update replicated project alias: %w", err)
	}
	return s.clearDeferredTx(tx, incoming.Alias, mutation.Payload)
}

// ─── benchmark ───────────────────────────────────────────────────────────────

// applyBenchmarkMutationTx treats a measurement as immutable: it was taken at a
// moment, from a run, and no later delivery makes it a different number. Only
// two things still move — which row of a metric is the baseline, and whether
// the row was deleted.
//
// The baseline is decided before the row is written rather than reconciled
// afterwards, because the partial unique index refuses the intermediate state
// where two rows of one metric both claim it. Both replicas compare the same
// two (baseline_set_at, sync_id) pairs, so both reach the same verdict whatever
// order the two measurements arrived in.
func (s *Store) applyBenchmarkMutationTx(tx *sql.Tx, mutation SyncMutation, payload []byte) error {
	var incoming syncBenchmarkPayload
	if err := decodeSyncPayload(payload, &incoming); err != nil {
		return fmt.Errorf("%w: decode benchmark payload: %v", ErrApplyDead, err)
	}
	incoming.SyncID = strings.TrimSpace(incoming.SyncID)
	incoming.Project = strings.TrimSpace(incoming.Project)
	incoming.TaskSyncID = strings.TrimSpace(incoming.TaskSyncID)
	incoming.Name = strings.TrimSpace(incoming.Name)
	incoming.Metric = strings.TrimSpace(incoming.Metric)
	incoming.Unit = strings.TrimSpace(incoming.Unit)
	if incoming.SyncID == "" || incoming.TaskSyncID == "" || incoming.Name == "" || incoming.Metric == "" {
		return fmt.Errorf("%w: benchmark payload requires sync_id, task_sync_id, name and metric", ErrApplyDead)
	}
	direction, ok := resolveBenchmarkDirection(incoming.Unit, incoming.Direction)
	if !ok {
		return fmt.Errorf("%w: benchmark unit %q is not one this store accepts", ErrApplyDead, incoming.Unit)
	}
	incoming.Direction = direction
	if strings.TrimSpace(incoming.Source) == "" {
		incoming.Source = "manual"
	}
	if strings.TrimSpace(incoming.CapturedAt) == "" {
		return fmt.Errorf("%w: benchmark payload requires captured_at", ErrApplyDead)
	}
	if incoming.Baseline && trimPtr(incoming.BaselineSetAt) == nil {
		return fmt.Errorf("%w: a benchmark baseline must say when it became one", ErrApplyDead)
	}
	if err := checkPayloadProject(mutation, incoming.Project); err != nil {
		return err
	}

	var taskID int64
	err := tx.QueryRow(`SELECT id FROM tasks WHERE sync_id = ?`, incoming.TaskSyncID).Scan(&taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: benchmark %s waits for task %s", ErrProjectsFKMissing, incoming.SyncID, incoming.TaskSyncID)
	}
	if err != nil {
		return fmt.Errorf("engram-projects: resolve benchmark task: %w", err)
	}

	local, err := scanBenchmarkSyncPayload(
		tx.QueryRow(`SELECT `+benchmarkSyncSelect+` FROM benchmarks WHERE sync_id = ?`, incoming.SyncID))
	known := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("engram-projects: read local benchmark: %w", err)
	}

	baseline, baselineSetAt := incoming.Baseline, trimPtr(incoming.BaselineSetAt)
	if known && !incomingWinsLWW(ptrOrEmpty(local.BaselineSetAt), ptrOrEmpty(incoming.BaselineSetAt),
		benchmarkBaselineDigest(local), benchmarkBaselineDigest(incoming)) {
		baseline, baselineSetAt = local.Baseline, trimPtr(local.BaselineSetAt)
	}
	if baseline {
		kept, err := s.settleBenchmarkBaselineTx(tx, incoming.SyncID, incoming.TaskSyncID,
			incoming.Metric, ptrOrEmpty(baselineSetAt))
		if err != nil {
			return err
		}
		if !kept {
			baseline, baselineSetAt = false, nil
		}
	}
	baselineFlag := 0
	if baseline {
		baselineFlag = 1
	}

	if !known {
		if _, err := s.execHook(tx, `
			INSERT INTO benchmarks (sync_id, project, task_id, task_sync_id, name, metric, unit,
				direction, value, baseline, baseline_set_at, run_path, sha256, config_stamp,
				captured_at, notes, source, created_at, deleted_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			incoming.SyncID, incoming.Project, taskID, incoming.TaskSyncID, incoming.Name,
			incoming.Metric, incoming.Unit, incoming.Direction, incoming.Value, baselineFlag,
			nullableStr(baselineSetAt), nullableStr(trimPtr(incoming.RunPath)),
			nullableStr(trimPtr(incoming.SHA256)), nullableStr(trimPtr(incoming.ConfigStamp)),
			incoming.CapturedAt, nullableStr(trimPtr(incoming.Notes)), incoming.Source,
			incoming.CreatedAt, nullableStr(trimPtr(incoming.DeletedAt)),
		); err != nil {
			return fmt.Errorf("engram-projects: insert replicated benchmark: %w", err)
		}
		return s.clearDeferredTx(tx, incoming.SyncID, mutation.Payload)
	}

	if _, err := s.execHook(tx, `
		UPDATE benchmarks SET baseline = ?, baseline_set_at = ?, deleted_at = ? WHERE sync_id = ?`,
		baselineFlag, nullableStr(baselineSetAt),
		nullableStr(earliestTimestamp(local.DeletedAt, incoming.DeletedAt)), incoming.SyncID,
	); err != nil {
		return fmt.Errorf("engram-projects: update replicated benchmark: %w", err)
	}
	return s.clearDeferredTx(tx, incoming.SyncID, mutation.Payload)
}

// settleBenchmarkBaselineTx decides whether syncID may hold the baseline of a
// metric, demoting the incumbent when it may. The comparison is on
// (baseline_set_at, sync_id), which both replicas hold for both rows.
func (s *Store) settleBenchmarkBaselineTx(tx *sql.Tx, syncID, taskSyncID, metric, setAt string) (bool, error) {
	var holderSyncID string
	var holderSetAt sql.NullString
	err := tx.QueryRow(
		`SELECT sync_id, baseline_set_at FROM benchmarks
		 WHERE task_sync_id = ? AND metric = ? AND baseline = 1 AND deleted_at IS NULL AND sync_id <> ?`,
		taskSyncID, metric, syncID,
	).Scan(&holderSyncID, &holderSetAt)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("engram-projects: read replicated baseline: %w", err)
	}

	if !benchmarkBaselineWinner(setAt, syncID, holderSetAt.String, holderSyncID) {
		return false, nil
	}
	if _, err := s.execHook(tx,
		`UPDATE benchmarks SET baseline = 0, baseline_set_at = NULL WHERE sync_id = ?`, holderSyncID,
	); err != nil {
		return false, fmt.Errorf("engram-projects: demote replicated baseline: %w", err)
	}
	return true, nil
}

// benchmarkBaselineWinner reports whether candidate A owns the baseline: the
// later stamp wins, ties broken by the greater sync_id. Total and deterministic,
// so both replicas break the tie the same way.
func benchmarkBaselineWinner(setAtA, syncA, setAtB, syncB string) bool {
	if timestampLess(setAtB, setAtA) {
		return true
	}
	if timestampLess(setAtA, setAtB) {
		return false
	}
	return strings.TrimSpace(syncA) > strings.TrimSpace(syncB)
}

// ─── Parking ─────────────────────────────────────────────────────────────────

// parkProjectsMutationTx writes a failed engram-projects mutation to
// sync_apply_deferred instead of failing the chunk it arrived in. It reports
// whether it took ownership of the error: false means the caller must still
// treat the failure as fatal, which is what keeps upstream entities on their
// existing strict behavior.
func (s *Store) parkProjectsMutationTx(tx *sql.Tx, mutation SyncMutation, applyErr error) (bool, error) {
	if !isProjectsEntity(mutation.Entity) {
		return false, nil
	}
	status := ""
	switch {
	case errors.Is(applyErr, ErrRelationFKMissing):
		status = "deferred"
	case errors.Is(applyErr, ErrApplyDead):
		status = "dead"
	default:
		return false, nil
	}

	key := strings.TrimSpace(mutation.EntityKey)
	if key == "" {
		derived, err := ProjectsEntityKey(mutation.Entity, []byte(mutation.Payload))
		if err != nil || strings.TrimSpace(derived) == "" {
			return false, nil
		}
		key = derived
	}
	key = deferredRowKey(key, mutation.Payload)

	if _, err := s.execHook(tx, `
		INSERT INTO sync_apply_deferred
			(sync_id, entity, payload, apply_status, retry_count, first_seen_at, last_error, last_attempted_at)
		VALUES (?, ?, ?, ?, 0, datetime('now'), ?, datetime('now'))
		ON CONFLICT(sync_id) DO UPDATE SET
			payload           = excluded.payload,
			apply_status      = excluded.apply_status,
			last_error        = excluded.last_error,
			last_attempted_at = datetime('now')`,
		key, mutation.Entity, mutation.Payload, status, applyErr.Error(),
	); err != nil {
		return false, fmt.Errorf("engram-projects: park mutation: %w", err)
	}
	return true, nil
}

// ─── Doctor read model ───────────────────────────────────────────────────────

// ProjectsSyncEntityStatus reports the replication backlog of one
// engram-projects entity.
type ProjectsSyncEntityStatus struct {
	Entity   string `json:"entity"`
	Pending  int    `json:"pending"`
	Deferred int    `json:"deferred"`
	Dead     int    `json:"dead"`
}

// ProjectsSyncStatus is the read model behind the `projects_sync` check of
// `engram doctor` (RFC section 10.5 step 1).
type ProjectsSyncStatus struct {
	Enabled       bool                       `json:"enabled"`
	Entities      []ProjectsSyncEntityStatus `json:"entities"`
	TotalPending  int                        `json:"total_pending"`
	TotalDeferred int                        `json:"total_deferred"`
	TotalDead     int                        `json:"total_dead"`
	KeyConflicts  int                        `json:"task_key_conflicts"`
}

// ProjectsSyncStatus counts, per engram-projects entity, the mutations still
// waiting to be pushed and the pulled ones parked in sync_apply_deferred.
// Project scoping is optional: an empty project counts every project.
func (s *Store) ProjectsSyncStatus(project string) (ProjectsSyncStatus, error) {
	status := ProjectsSyncStatus{Enabled: ProjectsSyncEnabled()}
	project, _ = NormalizeProject(strings.TrimSpace(project))

	entities := ProjectsSyncEntities()
	placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(entities)), ", ")
	entityArgs := make([]any, 0, len(entities))
	for _, entity := range entities {
		entityArgs = append(entityArgs, entity)
	}

	pending := map[string]int{}
	pendingQuery := `SELECT entity, COUNT(*) FROM sync_mutations
	                 WHERE acked_at IS NULL AND entity IN (` + placeholders + `)`
	args := append([]any{}, entityArgs...)
	if project != "" {
		pendingQuery += ` AND project = ?`
		args = append(args, project)
	}
	pendingQuery += ` GROUP BY entity`
	rows, err := s.db.Query(pendingQuery, args...)
	if err != nil {
		return status, fmt.Errorf("engram-projects: count pending mutations: %w", err)
	}
	for rows.Next() {
		var entity string
		var n int
		if err := rows.Scan(&entity, &n); err != nil {
			rows.Close()
			return status, fmt.Errorf("engram-projects: scan pending mutations: %w", err)
		}
		pending[entity] = n
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return status, fmt.Errorf("engram-projects: count pending mutations: %w", err)
	}

	type parkedCount struct{ deferred, dead int }
	parked := map[string]parkedCount{}
	deferredRows, err := s.db.Query(`
		SELECT entity, apply_status, COUNT(*) FROM sync_apply_deferred
		WHERE entity IN (`+placeholders+`) AND apply_status IN ('deferred', 'dead')
		GROUP BY entity, apply_status`, entityArgs...)
	if err != nil {
		return status, fmt.Errorf("engram-projects: count deferred mutations: %w", err)
	}
	for deferredRows.Next() {
		var entity, applyStatus string
		var n int
		if err := deferredRows.Scan(&entity, &applyStatus, &n); err != nil {
			deferredRows.Close()
			return status, fmt.Errorf("engram-projects: scan deferred mutations: %w", err)
		}
		current := parked[entity]
		if applyStatus == "dead" {
			current.dead = n
		} else {
			current.deferred = n
		}
		parked[entity] = current
	}
	deferredRows.Close()
	if err := deferredRows.Err(); err != nil {
		return status, fmt.Errorf("engram-projects: count deferred mutations: %w", err)
	}

	for _, entity := range ProjectsSyncEntities() {
		item := ProjectsSyncEntityStatus{
			Entity:   entity,
			Pending:  pending[entity],
			Deferred: parked[entity].deferred,
			Dead:     parked[entity].dead,
		}
		status.Entities = append(status.Entities, item)
		status.TotalPending += item.Pending
		status.TotalDeferred += item.Deferred
		status.TotalDead += item.Dead
	}

	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM sync_apply_deferred
		 WHERE entity = ? AND apply_status = 'dead' AND last_error LIKE ?`,
		SyncEntityTask, taskKeyConflictReason+"%",
	).Scan(&status.KeyConflicts); err != nil {
		return status, fmt.Errorf("engram-projects: count task key conflicts: %w", err)
	}
	return status, nil
}
