// Package store: engram-projects data access.
//
// This file implements the CRUD and query methods backing the 10 MCP tools
// registered under the `projects` profile (internal/mcp/projects_tools.go):
// project cards, tasks, evidence, the runbook index, and the task<->
// observation link with its external references. The schema itself lives in
// projects_schema.go (EP-001); this file never issues DDL.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// ─── Sentinel errors ─────────────────────────────────────────────────────────
//
// These map 1:1 to the error `code` values documented in RFC §5.0. Callers in
// internal/mcp translate them (via errors.As/Is) into the structured
// {"error", "code", ...} envelope; store methods never format that envelope
// themselves so the same data layer can back the HTTP API (T-04.03) later.
var (
	ErrNoProjectCard      = errors.New("no project card")
	ErrGraphNotFound      = errors.New("graph.json not found")
	ErrGraphMissingCommit = errors.New("graph.json missing built_at_commit")
	ErrUnknownTask        = errors.New("unknown task")
	// ErrAmbiguousTask is returned when a reference resolved without a project
	// answers in more than one of them.
	ErrAmbiguousTask       = errors.New("task reference matches more than one project")
	ErrUnknownObservation  = errors.New("unknown observation")
	ErrCrossProjectLink    = errors.New("link rejected: observation and task belong to different projects")
	ErrGraphCommitRequired = errors.New("graph_ref requires graph_commit")
	// ErrGraphCommitNotFullSHA rejects an abbreviated commit before it reaches
	// the schema's 40-character CHECK, where the failure would otherwise be
	// indistinguishable from a duplicate reference.
	ErrGraphCommitNotFullSHA = errors.New("graph_commit must be a full 40-character commit sha")
)

// TaskKeyConflictError is returned by UpsertTask when the incoming jira_key
// already belongs to a task in a different project.
type TaskKeyConflictError struct {
	JiraKey         string
	ExistingProject string
}

func (e *TaskKeyConflictError) Error() string {
	return fmt.Sprintf("jira_key %q already belongs to project %q", e.JiraKey, e.ExistingProject)
}

// ─── Project card ────────────────────────────────────────────────────────────

// ProjectCard mirrors the project_cards row (RFC §5.1).
type ProjectCard struct {
	Slug             string  `json:"slug"`
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

	// ParentSlug and Depth place the card in the project tree. Only
	// SetProjectParent writes them, so the pair stays consistent with the
	// subtree below it.
	ParentSlug *string `json:"parent_slug,omitempty"`
	Depth      int     `json:"depth"`
	// Kind, Description, Icon, Color and Tags are what a person chooses about
	// a project. Colour is a role token or an #rrggbb triple; the renderer
	// resolves the token against whichever palette is active.
	Kind        string  `json:"kind"`
	Description *string `json:"description,omitempty"`
	Icon        *string `json:"icon,omitempty"`
	Color       *string `json:"color,omitempty"`
	Tags        *string `json:"tags,omitempty"`
	// GraphStaleReason, GraphChangedFiles and GraphCheckedAt record the last
	// staleness verdict for this checkout. They never replicate: a graph is
	// fresh or stale relative to the working copy on this machine, and another
	// replica's answer says nothing about this one.
	GraphStaleReason  *string `json:"graph_stale_reason,omitempty"`
	GraphChangedFiles *int    `json:"graph_changed_files,omitempty"`
	GraphCheckedAt    *string `json:"graph_checked_at,omitempty"`
}

// ProjectCardCounts backs the `counts` section of mem_project_card.
type ProjectCardCounts struct {
	Observations       int `json:"observations"`
	Pinned             int `json:"pinned"`
	TasksActive        int `json:"tasks_active"`
	TasksTotal         int `json:"tasks_total"`
	Evidence           int `json:"evidence"`
	EvidenceUnattached int `json:"evidence_unattached"`
	Runbooks           int `json:"runbooks"`
	RunbooksStale      int `json:"runbooks_stale"`
}

// ProjectSyncSummary backs the `sync` section of mem_project_card. Full
// project-scoped mutation sync is T-04.05; today this only reports whether
// the project is enrolled for cloud sync and the shared sync_state lifecycle
// engram already tracks for it.
type ProjectSyncSummary struct {
	Enrolled     bool   `json:"enrolled"`
	LastAckedSeq int64  `json:"last_acked_seq"`
	Lifecycle    string `json:"lifecycle"`
}

const cloudSyncTargetKeyPrefix = "cloud"

// UpsertProjectCardParams holds the optional fields of mem_project_upsert.
// A nil pointer means "omitted": UpsertProjectCard leaves that column
// untouched on update, or applies its schema default on create.
// The parent is deliberately absent: it is set through SetProjectParent, which
// is the only path that can walk the ancestors, reject a cycle and rewrite the
// depth of everything below the card.
type UpsertProjectCardParams struct {
	Slug             string
	DisplayName      *string
	RepoURL          *string
	DefaultBranch    *string
	JiraProject      *string
	JiraComponent    *string
	KnowledgeHubPath *string
	Owner            *string
	GraphPath        *string
	Kind             *string
	Description      *string
	Icon             *string
	Color            *string
	Tags             *string
}

// DefaultJiraProject is the Jira project key applied when a card does not
// carry one. It is read from the environment so a deployment is not tied to
// one Jira project: set ENGRAM_JIRA_PROJECT to your own key.
func DefaultJiraProject() string {
	if v := strings.TrimSpace(os.Getenv("ENGRAM_JIRA_PROJECT")); v != "" {
		return v
	}
	return "PROJ"
}

// UpsertProjectCard creates or updates a project_cards row. It is idempotent
// in both directions: a field the caller omits is never overwritten, and a
// field whose value the card already carries is not rewritten either, so a
// repeated upsert leaves updated_at where it was and journals nothing.
func (s *Store) UpsertProjectCard(p UpsertProjectCardParams) (ProjectCard, bool, error) {
	existing, err := s.GetProjectCard(p.Slug)
	created := false
	switch {
	case err == nil:
		// update path below
	case errors.Is(err, ErrNoProjectCard):
		created = true
	default:
		return ProjectCard{}, false, err
	}

	now := s.nowUTC()
	if created {
		displayName := p.Slug
		if p.DisplayName != nil && strings.TrimSpace(*p.DisplayName) != "" {
			displayName = strings.TrimSpace(*p.DisplayName)
		}
		defaultBranch := "master"
		if p.DefaultBranch != nil {
			defaultBranch = *p.DefaultBranch
		}
		jiraProject := DefaultJiraProject()
		if p.JiraProject != nil {
			jiraProject = *p.JiraProject
		}
		graphPath := "graphify-out/graph.json"
		if p.GraphPath != nil {
			graphPath = *p.GraphPath
		}
		// The row and its sync mutation are written in one transaction, so a
		// card can never exist locally without the mutation that replicates it
		// (nor the other way round). This is the same atomicity contract
		// mem_save already gives observations.
		kind := "repo"
		if p.Kind != nil && strings.TrimSpace(*p.Kind) != "" {
			kind = strings.TrimSpace(*p.Kind)
		}
		if err := s.withTx(func(tx *sql.Tx) error {
			if _, err := s.execHook(tx, `
				INSERT INTO project_cards
					(slug, sync_id, display_name, repo_url, default_branch, jira_project,
					 jira_component, knowledge_hub_path, graph_path, owner, created_at, updated_at,
					 kind, description, icon, color, tags)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				p.Slug, newSyncID("proj"), displayName, nullableStr(p.RepoURL), defaultBranch, jiraProject,
				nullableStr(p.JiraComponent), nullableStr(p.KnowledgeHubPath), graphPath, nullableStr(p.Owner), now, now,
				kind, nullableStr(p.Description), nullableStr(p.Icon), nullableStr(p.Color), nullableStr(p.Tags),
			); err != nil {
				return fmt.Errorf("engram-projects: insert project card: %w", err)
			}
			return s.enqueueProjectCardTx(tx, p.Slug)
		}); err != nil {
			return ProjectCard{}, false, err
		}
	} else {
		// Only the columns whose value actually differs are written. An
		// upsert that restates what the card already says is a no-op, and a
		// no-op must not move updated_at or journal a mutation: under
		// last-writer-wins a fresh updated_at carrying no change outranks a
		// real edit another replica made a moment earlier, and the rollout
		// scripts that re-run the same upsert would keep producing cloud
		// traffic for cards nobody touched.
		var sets []string
		var args []any
		addSet := func(col string, v *string, current string) {
			if v == nil || *v == current {
				return
			}
			sets = append(sets, col+" = ?")
			args = append(args, *v)
		}
		addSet("display_name", p.DisplayName, existing.DisplayName)
		addSet("repo_url", p.RepoURL, derefString(existing.RepoURL))
		addSet("default_branch", p.DefaultBranch, existing.DefaultBranch)
		addSet("jira_project", p.JiraProject, existing.JiraProject)
		addSet("jira_component", p.JiraComponent, derefString(existing.JiraComponent))
		addSet("knowledge_hub_path", p.KnowledgeHubPath, derefString(existing.KnowledgeHubPath))
		addSet("graph_path", p.GraphPath, existing.GraphPath)
		addSet("owner", p.Owner, derefString(existing.Owner))
		addSet("kind", p.Kind, existing.Kind)
		addSet("description", p.Description, derefString(existing.Description))
		addSet("icon", p.Icon, derefString(existing.Icon))
		addSet("color", p.Color, derefString(existing.Color))
		addSet("tags", p.Tags, derefString(existing.Tags))
		if len(sets) == 0 {
			return existing, false, nil
		}
		sets = append([]string{"updated_at = ?"}, sets...)
		args = append([]any{now}, args...)
		args = append(args, p.Slug)
		if err := s.withTx(func(tx *sql.Tx) error {
			if _, err := s.execHook(tx,
				`UPDATE project_cards SET `+strings.Join(sets, ", ")+` WHERE slug = ?`, args...,
			); err != nil {
				return fmt.Errorf("engram-projects: update project card: %w", err)
			}
			return s.enqueueProjectCardTx(tx, p.Slug)
		}); err != nil {
			return ProjectCard{}, false, err
		}
	}

	card, err := s.GetProjectCard(p.Slug)
	if err != nil {
		return ProjectCard{}, false, err
	}
	return card, created, nil
}

// projectCardSelectColumns is the read projection of a card, spelled once so a
// column added to the row cannot reach one reader and miss another.
const projectCardSelectColumns = `slug, display_name, repo_url, default_branch, jira_project,
	jira_component, knowledge_hub_path, graph_path, graph_commit, graph_built_at, graph_summary,
	owner, created_at, updated_at, parent_slug, depth, kind, description, icon, color, tags,
	graph_stale_reason, graph_changed_files, graph_checked_at`

// prefixColumns qualifies every column of a comma-separated projection with a
// table alias, so a query that joins or correlates keeps the list unambiguous
// without writing it out a second time.
func prefixColumns(columns, alias string) string {
	parts := strings.Split(columns, ",")
	for i, part := range parts {
		parts[i] = alias + "." + strings.TrimSpace(part)
	}
	return strings.Join(parts, ", ")
}

func scanProjectCard(row interface{ Scan(dest ...any) error }) (ProjectCard, error) {
	var c ProjectCard
	err := row.Scan(&c.Slug, &c.DisplayName, &c.RepoURL, &c.DefaultBranch, &c.JiraProject,
		&c.JiraComponent, &c.KnowledgeHubPath, &c.GraphPath, &c.GraphCommit, &c.GraphBuiltAt,
		&c.GraphSummary, &c.Owner, &c.CreatedAt, &c.UpdatedAt, &c.ParentSlug, &c.Depth, &c.Kind,
		&c.Description, &c.Icon, &c.Color, &c.Tags, &c.GraphStaleReason, &c.GraphChangedFiles,
		&c.GraphCheckedAt)
	return c, err
}

// GetProjectCard returns ErrNoProjectCard when the slug has no card yet.
//
// The read goes through the read pool, which ResolveProjectCard depends on:
// it walks up to maxProjectDepth ancestors through this function on every
// card read, and queueing each hop behind the writer is what the pool exists
// to avoid. Every caller runs outside a transaction — the write paths read
// here before they open one, or after they commit — so the pool always sees
// the rows they expect.
func (s *Store) GetProjectCard(slug string) (ProjectCard, error) {
	c, err := scanProjectCard(s.readDB().QueryRow(
		`SELECT `+projectCardSelectColumns+` FROM project_cards WHERE slug = ? AND deleted_at IS NULL`, slug))
	if errors.Is(err, sql.ErrNoRows) {
		return ProjectCard{}, ErrNoProjectCard
	}
	if err != nil {
		return ProjectCard{}, fmt.Errorf("engram-projects: get project card: %w", err)
	}
	return c, nil
}

// ProjectCardExists reports whether a project_cards row exists for slug,
// without erroring when it does not (unlike GetProjectCard).
func (s *Store) ProjectCardExists(slug string) (bool, error) {
	var exists int
	err := s.db.QueryRow(`SELECT 1 FROM project_cards WHERE slug = ? AND deleted_at IS NULL`, slug).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// ProjectKnown reports whether the store recognises a project at all: it has
// rows in observations, sessions, prompts or enrollment (ProjectExists), or it
// has a project card and nothing else yet. Read and save tools resolve an
// explicit project through this so a card created by mem_project_upsert is
// usable before its first observation lands, instead of being reported unknown
// until something happens to be written under it.
//
// project_cards belongs to the engram-projects extension, so a store that never
// created that schema falls back to ProjectExists alone rather than erroring.
func (s *Store) ProjectKnown(slug string) (bool, error) {
	exists, err := s.ProjectExists(slug)
	if err != nil {
		return false, err
	}
	if exists {
		return true, nil
	}
	cardExists, cardErr := s.ProjectCardExists(slug)
	if cardErr == nil {
		return cardExists, nil
	}
	if present, presentErr := s.projectCardsTableExists(); presentErr == nil && !present {
		return false, nil
	}
	return false, cardErr
}

// projectCardsTableExists tells a store without the engram-projects schema apart
// from one whose project_cards query failed for any other reason.
func (s *Store) projectCardsTableExists() (bool, error) {
	var name string
	err := s.db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='project_cards'`).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// ensureMinimalProjectCard creates a card with display_name = slug when none
// exists yet. Returns cardCreated=true when it had to create one. Used by
// UpsertTask and AddEvidence, whose parent RFC sections require a task's
// project to always resolve to a real card.
func (s *Store) ensureMinimalProjectCard(slug string) (bool, error) {
	exists, err := s.ProjectCardExists(slug)
	if err != nil {
		return false, err
	}
	if exists {
		return false, nil
	}
	_, _, err = s.UpsertProjectCard(UpsertProjectCardParams{Slug: slug})
	if err != nil {
		return false, err
	}
	return true, nil
}

// observationsByProjectPredicate narrows observations to one project. It is
// spelled once because idx_obs_project_lower is built on exactly this
// expression: SQLite only uses a functional index when the query repeats the
// expression the index was created with.
const observationsByProjectPredicate = `lower(project) = ? AND deleted_at IS NULL`

// taskActivePredicate is the state test that separates work still open from
// work that is finished, cancelled or filed away. It is spelled once so the
// per-card counter and the batch cannot drift apart on what "active" means.
const taskActivePredicate = `state NOT IN ('done','cancelled','archived')`

// evidenceUnattachedPredicate marks a capture nobody has filed anywhere yet.
const evidenceUnattachedPredicate = `attached_jira = 0 AND attached_confluence_url IS NULL`

// cardObservationCountsQuery reads the two observation counters in one pass.
// It is kept apart from the rest because it is the only counter that matches
// on lower(project): rows written before project names were lowercased are
// still under their original spelling, and idx_obs_project_lower is built on
// exactly this expression.
const cardObservationCountsQuery = `
SELECT COUNT(*), COALESCE(SUM(pinned), 0)
FROM observations WHERE ` + observationsByProjectPredicate

// cardScalarCountsQuery reads the remaining six counters as scalar subqueries
// against a single row. Each subquery is an independent aggregate over its own
// index, so the database walks the same rows it used to — once per counter
// rather than once per counter per round trip.
const cardScalarCountsQuery = `
SELECT (SELECT COUNT(*) FROM tasks WHERE project = ?1 AND deleted_at IS NULL),
       (SELECT COUNT(*) FROM tasks WHERE project = ?1 AND deleted_at IS NULL AND ` + taskActivePredicate + `),
       (SELECT COUNT(*) FROM evidence WHERE project = ?1 AND deleted_at IS NULL),
       (SELECT COUNT(*) FROM evidence WHERE project = ?1 AND deleted_at IS NULL AND ` + evidenceUnattachedPredicate + `),
       (SELECT COUNT(*) FROM runbook_index WHERE project = ?1),
       (SELECT COUNT(*) FROM runbook_index WHERE project = ?1 AND stale = 1)`

// ProjectCardCounts computes the dashboard counters for mem_project_card.
//
// Eight aggregates, two round trips: the observation pair on its own because it
// filters on lower(project), and the other six as scalar subqueries in a single
// row. A card read is on the path of every project screen, and seven of the
// eight numbers used to arrive one connection round trip at a time.
func (s *Store) ProjectCardCounts(slug string) (ProjectCardCounts, error) {
	var c ProjectCardCounts
	rdb := s.readDB()
	if err := s.queryRowHook(rdb, cardObservationCountsQuery, slug).Scan(&c.Observations, &c.Pinned); err != nil {
		return c, fmt.Errorf("engram-projects: count project observations: %w", err)
	}
	if err := s.queryRowHook(rdb, cardScalarCountsQuery, slug).Scan(
		&c.TasksTotal, &c.TasksActive, &c.Evidence, &c.EvidenceUnattached,
		&c.Runbooks, &c.RunbooksStale,
	); err != nil {
		return c, fmt.Errorf("engram-projects: count project rows: %w", err)
	}
	return c, nil
}

// batchCountQueries are the four grouped aggregates behind
// ProjectCardCountsBatch: one per source table, each returning the project and
// the pair of counters that table feeds.
var batchCountQueries = []struct {
	// sql takes the slug placeholders as its only argument list.
	sql func(placeholders string) string
	// assign writes the row's two counters into the card's counts.
	assign func(c *ProjectCardCounts, total, subset int)
}{
	{
		sql: func(p string) string {
			return `SELECT lower(project), COUNT(*), COALESCE(SUM(pinned), 0)
				FROM observations WHERE lower(project) IN (` + p + `) AND deleted_at IS NULL
				GROUP BY lower(project)`
		},
		assign: func(c *ProjectCardCounts, total, subset int) { c.Observations, c.Pinned = total, subset },
	},
	{
		sql: func(p string) string {
			return `SELECT project, COUNT(*), SUM(CASE WHEN ` + taskActivePredicate + ` THEN 1 ELSE 0 END)
				FROM tasks WHERE project IN (` + p + `) AND deleted_at IS NULL
				GROUP BY project`
		},
		assign: func(c *ProjectCardCounts, total, subset int) { c.TasksTotal, c.TasksActive = total, subset },
	},
	{
		sql: func(p string) string {
			return `SELECT project, COUNT(*), SUM(CASE WHEN ` + evidenceUnattachedPredicate + ` THEN 1 ELSE 0 END)
				FROM evidence WHERE project IN (` + p + `) AND deleted_at IS NULL
				GROUP BY project`
		},
		assign: func(c *ProjectCardCounts, total, subset int) { c.Evidence, c.EvidenceUnattached = total, subset },
	},
	{
		sql: func(p string) string {
			return `SELECT project, COUNT(*), SUM(CASE WHEN stale = 1 THEN 1 ELSE 0 END)
				FROM runbook_index WHERE project IN (` + p + `)
				GROUP BY project`
		},
		assign: func(c *ProjectCardCounts, total, subset int) { c.Runbooks, c.RunbooksStale = total, subset },
	},
}

// ProjectCardCountsBatch computes the same counters as ProjectCardCounts for a
// whole set of slugs at once: four grouped aggregates rather than eight per
// card. The selector and the project tree both draw every card's counters on
// every repaint, which is where the per-card version turned a screen into
// hundreds of round trips.
//
// Every requested slug is present in the result, zeroed when nothing under it
// exists, so a caller never has to decide what a missing key meant.
func (s *Store) ProjectCardCountsBatch(slugs []string) (map[string]ProjectCardCounts, error) {
	counts := make(map[string]ProjectCardCounts, len(slugs))
	args := make([]any, 0, len(slugs))
	for _, slug := range slugs {
		if _, seen := counts[slug]; seen {
			continue
		}
		counts[slug] = ProjectCardCounts{}
		args = append(args, slug)
	}
	if len(args) == 0 {
		return counts, nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(args)), ",")
	rdb := s.readDB()
	for _, q := range batchCountQueries {
		if err := s.scanBatchCounts(rdb, q.sql(placeholders), args, counts, q.assign); err != nil {
			return nil, err
		}
	}
	return counts, nil
}

// scanBatchCounts runs one grouped aggregate and folds its rows into counts.
// A project the query reports but the caller did not ask for is ignored rather
// than added: the batch answers the question it was given.
func (s *Store) scanBatchCounts(
	rdb dbQueryer, query string, args []any,
	counts map[string]ProjectCardCounts,
	assign func(*ProjectCardCounts, int, int),
) error {
	rows, err := s.queryHook(rdb, query, args...)
	if err != nil {
		return fmt.Errorf("engram-projects: batch project counts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var slug string
		var total, subset int
		if err := rows.Scan(&slug, &total, &subset); err != nil {
			return fmt.Errorf("engram-projects: scan batch project counts: %w", err)
		}
		c, ok := counts[slug]
		if !ok {
			continue
		}
		assign(&c, total, subset)
		counts[slug] = c
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("engram-projects: batch project counts: %w", err)
	}
	return nil
}

// ProjectSyncSummary reports the cloud-sync enrollment and lifecycle for a
// project without mutating sync_state (unlike GetSyncState, which
// bootstraps a row on first read — not appropriate from a read-only tool).
func (s *Store) ProjectSyncSummary(slug string) (ProjectSyncSummary, error) {
	var out ProjectSyncSummary
	enrolled, err := s.IsProjectEnrolled(slug)
	if err != nil {
		return out, err
	}
	out.Enrolled = enrolled
	out.Lifecycle = "disabled"
	if !enrolled {
		return out, nil
	}
	targetKey := cloudSyncTargetKeyPrefix + ":" + slug
	var lifecycle string
	var lastAcked int64
	err = s.db.QueryRow(
		`SELECT lifecycle, last_acked_seq FROM sync_state WHERE target_key = ?`, targetKey,
	).Scan(&lifecycle, &lastAcked)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		out.Lifecycle = "idle"
		return out, nil
	case err != nil:
		return out, err
	default:
		out.Lifecycle = lifecycle
		out.LastAckedSeq = lastAcked
		return out, nil
	}
}

// GraphSyncResult backs the `graph` section of mem_project_upsert and
// POST /projects/{slug}/graph/sync.
//
// The graph_summary blob itself (god nodes, labeled communities,
// GRAPH_REPORT.md parsing — RFC §8) is computed by
// internal/project.SyncGraph, which calls StampProjectGraph below to persist
// it; that function returns this same struct so callers don't see a
// difference in shape.
type GraphSyncResult struct {
	Synced         bool   `json:"synced"`
	GraphCommit    string `json:"graph_commit"`
	HeadCommit     string `json:"head_commit"`
	Stale          bool   `json:"stale"`
	NodeCount      int    `json:"node_count"`
	EdgeCount      int    `json:"edge_count"`
	CommunityCount int    `json:"community_count"`
}

// StampProjectGraph persists the code-graph pointer (graph_commit,
// graph_built_at) and, when non-nil, the graph_summary blob for slug, in a
// single UPDATE. It is the only write path for these three columns.
//
// The parsing and aggregation that produces graphSummary lives in
// internal/project (see graph_summary.go there), not here: internal/project
// already depends on internal/store for card/task/evidence access, so store
// cannot depend back on project without an import cycle. This method is
// store's half of that split — plain persistence, no graphify-specific
// logic — which is also why it takes the summary pre-rendered as a JSON
// string rather than a struct.
//
// The DDL constraint `CHECK (graph_summary IS NULL OR graph_commit IS NOT
// NULL)` guarantees a summary is never stored without the commit it was
// computed from (D-02); this method's signature makes that pairing the only
// thing you can call it with in the first place.
func (s *Store) StampProjectGraph(slug, graphCommit, graphBuiltAt string, graphSummary *string) error {
	return s.withTx(func(tx *sql.Tx) error {
		if _, err := s.execHook(tx,
			`UPDATE project_cards SET graph_commit = ?, graph_built_at = ?, graph_summary = ?, updated_at = ? WHERE slug = ?`,
			graphCommit, graphBuiltAt, nullableStr(graphSummary), s.nowUTC(), slug,
		); err != nil {
			return fmt.Errorf("engram-projects: stamp graph commit: %w", err)
		}
		return s.enqueueProjectCardTx(tx, slug)
	})
}

// StampGraphStaleness persists the verdict of the last graph staleness check:
// why the graph is or is not current, how many code files changed since it was
// built, and when the question was asked.
//
// The three columns never leave this machine, so unlike StampProjectGraph this
// writes no sync mutation and does not move updated_at: a local check is not an
// edit of the card, and replicating it would let one checkout's answer overwrite
// another's.
//
// It takes the fields rather than a GraphStaleness value for the same reason
// StampProjectGraph takes a pre-rendered summary: the type that computes them
// lives in internal/project, which already imports this package.
func (s *Store) StampGraphStaleness(slug, reason string, changedFiles int, checkedAt string) error {
	if _, err := s.execHook(s.db,
		`UPDATE project_cards SET graph_stale_reason = ?, graph_changed_files = ?, graph_checked_at = ?
		 WHERE slug = ?`,
		nullableStr(trimToNil(reason)), changedFiles, nullableStr(trimToNil(checkedAt)), slug,
	); err != nil {
		return fmt.Errorf("engram-projects: stamp graph staleness: %w", err)
	}
	return nil
}

// trimToNil turns a blank string into a NULL column rather than an empty one,
// so "not checked" and "checked, no reason" stay distinguishable.
func trimToNil(v string) *string {
	trimmed := strings.TrimSpace(v)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// nowUTC returns the current UTC time formatted like SQLite's datetime('now'),
// for Go-side timestamps that must match store column formatting exactly.
func (s *Store) nowUTC() string {
	return time.Now().UTC().Format("2006-01-02 15:04:05")
}

func nullableStr(v *string) any {
	if v == nil {
		return nil
	}
	return *v
}

// ─── Card listing (backs GET /projects) ──────────────────────────────────────

// ProjectCardListItem is one row of the project-card listing, optionally
// carrying the same counters mem_project_card reports for a single card.
type ProjectCardListItem struct {
	ProjectCard
	Counts *ProjectCardCounts `json:"counts,omitempty"`
}

// ListProjectCards returns every live project card, most recently updated
// first. Counters are computed only when includeCounts is set: they cost four
// grouped aggregates, which the TUI selector wants and a plain pointer lookup
// does not.
func (s *Store) ListProjectCards(includeCounts bool) ([]ProjectCardListItem, int, error) {
	rows, err := s.queryHook(s.readDB(), `SELECT `+projectCardSelectColumns+`
		FROM project_cards WHERE deleted_at IS NULL ORDER BY updated_at DESC, slug ASC`)
	if err != nil {
		return nil, 0, fmt.Errorf("engram-projects: list project cards: %w", err)
	}
	// The cursor is drained and closed before the counters are read: they are
	// four more queries, and a cursor left open across them owns one of the
	// read pool's connections for no reason.
	var cards []ProjectCard
	for rows.Next() {
		c, err := scanProjectCard(rows)
		if err != nil {
			rows.Close()
			return nil, 0, fmt.Errorf("engram-projects: scan project card: %w", err)
		}
		cards = append(cards, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, 0, fmt.Errorf("engram-projects: list project cards: %w", err)
	}
	rows.Close()

	var counts map[string]ProjectCardCounts
	if includeCounts {
		slugs := make([]string, 0, len(cards))
		for _, c := range cards {
			slugs = append(slugs, c.Slug)
		}
		if counts, err = s.ProjectCardCountsBatch(slugs); err != nil {
			return nil, 0, err
		}
	}

	items := make([]ProjectCardListItem, 0, len(cards))
	for _, c := range cards {
		item := ProjectCardListItem{ProjectCard: c}
		if includeCounts {
			cardCounts := counts[c.Slug]
			item.Counts = &cardCounts
		}
		items = append(items, item)
	}
	return items, len(items), nil
}
