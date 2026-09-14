package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// ProjectsSchemaVersion is the current version of the engram-projects
// extension schema. It is stamped into PRAGMA user_version by
// migrateProjects. The upstream store does not use user_version for
// anything else, so this pragma is reserved exclusively for this
// extension and is safe to read from outside the store package (see
// ProjectsSchemaStatus).
//
// Version 3 adds the workspace shape on top of version 2: the project
// hierarchy, project aliases, vault-aware tasks, categorised evidence and
// task-scoped benchmarks. The stamp is a diagnostic label, not a precondition:
// which steps still have to run is decided by the migration ledger, one row per
// step, so a database that stops halfway resumes exactly where it stopped.
const ProjectsSchemaVersion = 3

// projectsMigrationIDs lists the ledger ids of the engram-projects steps that
// take a version-2 database to version 3, in the order they must run.
const (
	// projCardsHierarchyID adds the parent/kind/appearance columns to
	// project_cards, plus the three local columns that record the last graph
	// staleness check.
	projCardsHierarchyID = "proj-0001-cards-hierarchy"
	// projAliasesID adds the table that lets one project answer to more than
	// one name without any historical row being renamed.
	projAliasesID = "proj-0002-project-aliases"
	// projTasksRebuildID reshapes tasks around the vault: a slug of its own, a
	// summary, a note for what is left, the folder it lives in, a parent task,
	// and the three states the vault README already uses.
	projTasksRebuildID = "proj-0003-tasks-rebuild"
	// projEvidenceRebuildID gives evidence the category the vault files it
	// under and widens kind to the file types people actually capture.
	projEvidenceRebuildID = "proj-0004-evidence-rebuild"
	// projBenchmarksID adds the measurements a task was justified by, so a
	// number that argued for a change is kept next to the change.
	projBenchmarksID = "proj-0005-benchmarks"
)

// projectsHierarchyDDL is the proj-0001-cards-hierarchy step. It is written as
// ALTER TABLE rather than folded into projectsSchemaDDL because an existing
// database must gain the columns without its rows being rewritten: a card is
// the anchor of every task, evidence row and runbook, and a rebuild here would
// cost a full-file backup for nine columns that all have a default.
//
// The CHECK on icon and color is spelled as a pair of GLOBs rather than one,
// because GLOB's `*` matches any run of characters rather than repeating the
// class before it. `name GLOB '[a-z]*'` fixes the first character and
// `name NOT GLOB '*[^a-z0-9-]*'` rejects every character outside the set, which
// together say what a single regular expression would.
const projectsHierarchyDDL = `
ALTER TABLE project_cards ADD COLUMN parent_slug TEXT
    REFERENCES project_cards(slug) ON DELETE RESTRICT ON UPDATE CASCADE;
ALTER TABLE project_cards ADD COLUMN depth INTEGER NOT NULL DEFAULT 0
    CHECK (depth BETWEEN 0 AND 3);
ALTER TABLE project_cards ADD COLUMN kind TEXT NOT NULL DEFAULT 'repo'
    CHECK (kind IN ('umbrella','repo','instance','service','dataset','knowledge'));
ALTER TABLE project_cards ADD COLUMN description TEXT
    CHECK (description IS NULL OR length(description) <= 1000);
ALTER TABLE project_cards ADD COLUMN icon TEXT
    CHECK (icon IS NULL OR (icon GLOB '[a-z]*' AND icon NOT GLOB '*[^a-z0-9-]*'));
ALTER TABLE project_cards ADD COLUMN color TEXT
    CHECK (color IS NULL
           OR color GLOB '#[0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]'
           OR (color GLOB '[a-z]*' AND color NOT GLOB '*[^a-z0-9-]*'));
ALTER TABLE project_cards ADD COLUMN tags TEXT
    CHECK (tags IS NULL OR (json_valid(tags) AND json_type(tags) = 'array'));
ALTER TABLE project_cards ADD COLUMN graph_stale_reason TEXT;
ALTER TABLE project_cards ADD COLUMN graph_changed_files INTEGER;
ALTER TABLE project_cards ADD COLUMN graph_checked_at TEXT;

CREATE INDEX IF NOT EXISTS idx_project_cards_parent ON project_cards(parent_slug, slug);
CREATE INDEX IF NOT EXISTS idx_project_cards_kind   ON project_cards(kind, updated_at DESC);

-- Safety net for a parent written by raw SQL. The real cycle check lives in
-- SetProjectParent: SQLite has no WITH RECURSIVE inside a trigger, so a trigger
-- can only see one level and would let a longer cycle through.
CREATE TRIGGER IF NOT EXISTS project_cards_depth_ck
BEFORE UPDATE OF parent_slug, depth ON project_cards
BEGIN
    SELECT RAISE(ABORT, 'project_cards: a card cannot be its own parent')
    WHERE new.parent_slug IS NOT NULL AND new.parent_slug = new.slug;

    SELECT RAISE(ABORT, 'project_cards: a card without a parent is at depth 0')
    WHERE new.parent_slug IS NULL AND new.depth <> 0;

    SELECT RAISE(ABORT, 'project_cards: depth must be the parent depth plus one')
    WHERE new.parent_slug IS NOT NULL
      AND new.depth <> (SELECT parent.depth + 1 FROM project_cards parent
                        WHERE parent.slug = new.parent_slug);
END;
`

// projectsSchemaTriggers lists every FTS5 sync trigger created by
// projectsSchemaDDL. migrateProjects verifies each one exists after
// applying the DDL, as a defensive check against a SQLite build that
// silently failed to register a trigger.
var projectsSchemaTriggers = []string{
	"tasks_fts_insert", "tasks_fts_delete", "tasks_fts_update",
	"runbook_fts_insert", "runbook_fts_delete", "runbook_fts_update",
	"evidence_fts_insert", "evidence_fts_delete", "evidence_fts_update",
}

// projectsSchemaDDL creates the engram-projects extension schema: the five
// contract tables (project_cards, tasks, evidence, runbook_index,
// task_observations), the observation_refs and task_link_tombstones
// auxiliary tables, and the tasks_fts / runbook_index_fts external-content
// FTS5 tables with their sync triggers. Every statement is idempotent (IF NOT EXISTS,
// including on triggers), so re-running it against an already-migrated
// database creates nothing and touches no existing row. No upstream table
// is modified.
const projectsSchemaDDL = `
PRAGMA foreign_keys = ON;

-- 1. Project card: 1:1 with the existing project value (same slug).
CREATE TABLE IF NOT EXISTS project_cards (
    slug               TEXT    PRIMARY KEY
                       CHECK (slug = lower(trim(slug)) AND length(slug) BETWEEN 1 AND 64
                              AND slug NOT IN ('migrate', 'current')),
    sync_id            TEXT    NOT NULL UNIQUE,
    display_name       TEXT    NOT NULL CHECK (length(trim(display_name)) > 0),
    repo_url           TEXT,
    default_branch     TEXT    NOT NULL DEFAULT 'master',
    jira_project       TEXT    NOT NULL DEFAULT 'PROJ',
    jira_component     TEXT,
    knowledge_hub_path TEXT,
    graph_path         TEXT    NOT NULL DEFAULT 'graphify-out/graph.json',
    graph_commit       TEXT    CHECK (graph_commit IS NULL OR length(graph_commit) = 40),
    graph_built_at     TEXT,
    graph_summary      TEXT    CHECK (graph_summary IS NULL OR json_valid(graph_summary)),
    owner              TEXT,
    created_at         TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_at         TEXT    NOT NULL DEFAULT (datetime('now')),
    deleted_at         TEXT,
    -- A graph fact never exists without the commit it was built from.
    CHECK ((graph_commit IS NULL) = (graph_built_at IS NULL)),
    CHECK (graph_summary IS NULL OR graph_commit IS NOT NULL)
);
CREATE INDEX IF NOT EXISTS idx_project_cards_updated ON project_cards(updated_at DESC);

-- 2. Tasks: one row per Jira ticket or SDD change inside a project.
CREATE TABLE IF NOT EXISTS tasks (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    sync_id              TEXT    NOT NULL UNIQUE,
    project              TEXT    NOT NULL REFERENCES project_cards(slug)
                                 ON DELETE RESTRICT ON UPDATE CASCADE,
    jira_key             TEXT    UNIQUE
                         CHECK (jira_key IS NULL OR jira_key GLOB '[A-Z]*-[0-9]*'),
    sdd_change           TEXT    CHECK (sdd_change IS NULL OR sdd_change = lower(sdd_change)),
    title                TEXT    NOT NULL CHECK (length(trim(title)) > 0),
    kind                 TEXT    NOT NULL
                         CHECK (kind IN ('feature','bugfix','refactor','incident','migration','spike')),
    state                TEXT    NOT NULL DEFAULT 'open'
                         CHECK (state IN ('open','analysis','in_progress','review','verified',
                                          'done','blocked','cancelled')),
    jira_status          TEXT,
    jira_status_category TEXT    CHECK (jira_status_category IS NULL
                                        OR jira_status_category IN ('new','indeterminate','done')),
    state_synced_at      TEXT,
    branch               TEXT,
    pr_url               TEXT,
    knowledge_ref        TEXT,
    assignee             TEXT,
    created_at           TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_at           TEXT    NOT NULL DEFAULT (datetime('now')),
    closed_at            TEXT,
    deleted_at           TEXT,
    CHECK (closed_at IS NULL OR state IN ('done','cancelled')),
    CHECK (jira_key IS NOT NULL OR sdd_change IS NOT NULL)
);
CREATE INDEX IF NOT EXISTS idx_tasks_project_state ON tasks(project, state, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_tasks_sdd_change    ON tasks(project, sdd_change);
CREATE INDEX IF NOT EXISTS idx_tasks_deleted       ON tasks(deleted_at);

-- 3. Evidence: one row per captured file (metadata only, never the bytes).
CREATE TABLE IF NOT EXISTS evidence (
    id                      INTEGER PRIMARY KEY AUTOINCREMENT,
    sync_id                 TEXT    NOT NULL UNIQUE,
    project                 TEXT    NOT NULL REFERENCES project_cards(slug)
                                    ON DELETE RESTRICT ON UPDATE CASCADE,
    task_id                 INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    task_sync_id            TEXT    NOT NULL REFERENCES tasks(sync_id) ON DELETE CASCADE,
    path                    TEXT    NOT NULL
                            CHECK (length(trim(path)) > 0 AND path NOT LIKE '/%' AND path NOT LIKE '~%'),
    sha256                  TEXT    NOT NULL
                            CHECK (length(sha256) = 64 AND sha256 = lower(sha256)),
    kind                    TEXT    NOT NULL CHECK (kind IN ('png','gif','mp4','json','log','txt')),
    proves                  TEXT    NOT NULL CHECK (length(trim(proves)) > 0),
    config_stamp            TEXT,
    captured_at             TEXT    NOT NULL,
    attached_jira           INTEGER NOT NULL DEFAULT 0 CHECK (attached_jira IN (0, 1)),
    attached_confluence_url TEXT,
    size_bytes              INTEGER CHECK (size_bytes IS NULL OR size_bytes >= 0),
    manifest_path           TEXT,
    created_at              TEXT    NOT NULL DEFAULT (datetime('now')),
    deleted_at              TEXT,
    UNIQUE (task_sync_id, sha256)
);
CREATE INDEX IF NOT EXISTS idx_evidence_task    ON evidence(task_id, captured_at DESC);
CREATE INDEX IF NOT EXISTS idx_evidence_project ON evidence(project, captured_at DESC);

-- 4. Runbook index: derived from the vault, rebuilt per machine, never replicated.
--    seq is the stable INTEGER rowid required by the external-content FTS5 table;
--    id (RB-NNN) stays the business key (UNIQUE).
CREATE TABLE IF NOT EXISTS runbook_index (
    seq              INTEGER PRIMARY KEY AUTOINCREMENT,
    id               TEXT    NOT NULL UNIQUE CHECK (id GLOB 'RB-[0-9][0-9][0-9]'),
    project          TEXT    NOT NULL REFERENCES project_cards(slug)
                             ON DELETE RESTRICT ON UPDATE CASCADE,
    vault_path       TEXT    NOT NULL UNIQUE CHECK (vault_path NOT LIKE '/%'),
    title            TEXT    NOT NULL,
    category         TEXT    NOT NULL
                     CHECK (category IN ('auth','database','queue','network','performance',
                                         'data-integrity','registration')),
    pattern          TEXT    CHECK (pattern IS NULL OR pattern IN ('missing-files','auth-access',
                                    'file-save-failure','sync-upload','registration-subscription','other')),
    severity         TEXT    CHECK (severity IS NULL OR severity IN ('P1','P2','P3','P4')),
    status           TEXT    NOT NULL CHECK (status IN ('draft','verified','outdated')),
    symptoms         TEXT    NOT NULL DEFAULT '',
    owner            TEXT,
    automation_level TEXT    CHECK (automation_level IS NULL
                                    OR automation_level IN ('manual','assisted','autonomous-with-gate')),
    last_updated     TEXT,
    last_verified    TEXT,
    stale            INTEGER NOT NULL DEFAULT 0 CHECK (stale IN (0, 1)),
    age_days         INTEGER CHECK (age_days IS NULL OR age_days >= 0),
    exec_count       INTEGER NOT NULL DEFAULT 0 CHECK (exec_count >= 0),
    last_exec_at     TEXT,
    synced_at        TEXT    NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_runbook_project_status   ON runbook_index(project, status, stale);
CREATE INDEX IF NOT EXISTS idx_runbook_category_pattern ON runbook_index(category, pattern);

-- 5. Task <-> observation link (N:M). Local integer keys for integrity,
--    sync_id pairs for cross-machine replication.
CREATE TABLE IF NOT EXISTS task_observations (
    task_id             INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    observation_id      INTEGER NOT NULL REFERENCES observations(id) ON DELETE CASCADE,
    task_sync_id        TEXT    NOT NULL,
    observation_sync_id TEXT    NOT NULL,
    role                TEXT    NOT NULL DEFAULT 'context'
                        CHECK (role IN ('context','decision','root_cause','evidence','summary')),
    linked_at           TEXT    NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (task_id, observation_id),
    UNIQUE (task_sync_id, observation_sync_id)
);
CREATE INDEX IF NOT EXISTS idx_task_obs_observation ON task_observations(observation_id);

-- 6. Auxiliary: external references of an observation (knowledge_ref for observations,
--    graph facts stamped with graph_commit, runbook and Jira pointers). Keyed by
--    observation sync_id without FK, like memory_relations.source_id/target_id.
CREATE TABLE IF NOT EXISTS observation_refs (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    observation_sync_id TEXT    NOT NULL,
    ref_kind            TEXT    NOT NULL CHECK (ref_kind IN ('knowledge','graph','runbook','jira')),
    ref                 TEXT    NOT NULL CHECK (length(trim(ref)) > 0),
    graph_commit        TEXT    CHECK (graph_commit IS NULL OR length(graph_commit) = 40),
    created_at          TEXT    NOT NULL DEFAULT (datetime('now')),
    UNIQUE (observation_sync_id, ref_kind, ref),
    CHECK (ref_kind <> 'graph' OR graph_commit IS NOT NULL)
);
CREATE INDEX IF NOT EXISTS idx_obs_refs_kind_ref ON observation_refs(ref_kind, ref);
CREATE INDEX IF NOT EXISTS idx_obs_refs_obs      ON observation_refs(observation_sync_id);

-- 6b. Tombstones for replicated task<->observation unlinks. task_observations
--     carries no deleted_at, so a pulled task_link delete has nowhere to
--     record that the pair was removed; without that record a concurrent
--     upsert that arrives afterwards silently resurrects the link and two
--     replicas that saw the same mutations in different orders end up with
--     different rows. The tombstone keeps the delete clock so the apply rule
--     stays a convergent LWW-element-set (delete wins on an exact tie).
CREATE TABLE IF NOT EXISTS task_link_tombstones (
    task_sync_id        TEXT NOT NULL,
    observation_sync_id TEXT NOT NULL,
    deleted_at          TEXT NOT NULL,
    PRIMARY KEY (task_sync_id, observation_sync_id)
);

-- 7. FTS5 over tasks (external content, same pattern as observations_fts).
CREATE VIRTUAL TABLE IF NOT EXISTS tasks_fts USING fts5(
    title, jira_key, sdd_change, branch, project,
    content='tasks', content_rowid='id'
);
CREATE TRIGGER IF NOT EXISTS tasks_fts_insert AFTER INSERT ON tasks BEGIN
    INSERT INTO tasks_fts(rowid, title, jira_key, sdd_change, branch, project)
    VALUES (new.id, new.title, new.jira_key, new.sdd_change, new.branch, new.project);
END;
CREATE TRIGGER IF NOT EXISTS tasks_fts_delete AFTER DELETE ON tasks BEGIN
    INSERT INTO tasks_fts(tasks_fts, rowid, title, jira_key, sdd_change, branch, project)
    VALUES ('delete', old.id, old.title, old.jira_key, old.sdd_change, old.branch, old.project);
END;
CREATE TRIGGER IF NOT EXISTS tasks_fts_update AFTER UPDATE ON tasks BEGIN
    INSERT INTO tasks_fts(tasks_fts, rowid, title, jira_key, sdd_change, branch, project)
    VALUES ('delete', old.id, old.title, old.jira_key, old.sdd_change, old.branch, old.project);
    INSERT INTO tasks_fts(rowid, title, jira_key, sdd_change, branch, project)
    VALUES (new.id, new.title, new.jira_key, new.sdd_change, new.branch, new.project);
END;

-- 8. FTS5 over the runbook index (symptoms are the main retrieval signal).
CREATE VIRTUAL TABLE IF NOT EXISTS runbook_index_fts USING fts5(
    title, symptoms, category, pattern, project, id,
    content='runbook_index', content_rowid='seq'
);
CREATE TRIGGER IF NOT EXISTS runbook_fts_insert AFTER INSERT ON runbook_index BEGIN
    INSERT INTO runbook_index_fts(rowid, title, symptoms, category, pattern, project, id)
    VALUES (new.seq, new.title, new.symptoms, new.category, new.pattern, new.project, new.id);
END;
CREATE TRIGGER IF NOT EXISTS runbook_fts_delete AFTER DELETE ON runbook_index BEGIN
    INSERT INTO runbook_index_fts(runbook_index_fts, rowid, title, symptoms, category, pattern, project, id)
    VALUES ('delete', old.seq, old.title, old.symptoms, old.category, old.pattern, old.project, old.id);
END;
CREATE TRIGGER IF NOT EXISTS runbook_fts_update AFTER UPDATE ON runbook_index BEGIN
    INSERT INTO runbook_index_fts(runbook_index_fts, rowid, title, symptoms, category, pattern, project, id)
    VALUES ('delete', old.seq, old.title, old.symptoms, old.category, old.pattern, old.project, old.id);
    INSERT INTO runbook_index_fts(rowid, title, symptoms, category, pattern, project, id)
    VALUES (new.seq, new.title, new.symptoms, new.category, new.pattern, new.project, new.id);
END;
`

// migrateProjects applies the engram-projects schema (EP-001). It is
// invoked once from Store.migrate() and is safe to run on every startup:
// every statement in projectsSchemaDDL is idempotent, so re-running it
// against an already-migrated database creates nothing and touches no
// existing row.
//
// After applying the DDL it verifies that every FTS5 sync trigger was
// actually registered, then stamps PRAGMA user_version forward-only: an
// already-migrated database (user_version >= ProjectsSchemaVersion) is left
// untouched. user_version is a diagnostic label here, not a migration
// precondition — the DDL's own idempotency is what makes re-runs safe.
func (s *Store) migrateProjects() error {
	if _, err := s.execHook(s.db, projectsSchemaDDL); err != nil {
		return fmt.Errorf("engram-projects: apply schema: %w", err)
	}

	if err := s.migrateProjectsToV3(); err != nil {
		return err
	}

	for _, trigger := range projectsSchemaTriggers {
		var name string
		err := s.db.QueryRow(
			`SELECT name FROM sqlite_master WHERE type='trigger' AND name=?`, trigger,
		).Scan(&name)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("engram-projects: trigger %q missing after migration", trigger)
			}
			return fmt.Errorf("engram-projects: verify trigger %q: %w", trigger, err)
		}
	}

	var current int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&current); err != nil {
		return fmt.Errorf("engram-projects: read user_version: %w", err)
	}
	if current < ProjectsSchemaVersion {
		if _, err := s.execHook(s.db, fmt.Sprintf("PRAGMA user_version = %d", ProjectsSchemaVersion)); err != nil {
			return fmt.Errorf("engram-projects: stamp user_version: %w", err)
		}
	}
	return nil
}

// migrateProjectsToV3 applies the steps that take the version-2 schema to
// version 3. Each one is guarded by the migration ledger, so a fresh database
// (which projectsSchemaDDL just created in its version-2 shape) and an existing
// one both walk the same path exactly once.
func (s *Store) migrateProjectsToV3() error {
	// A step that rewrites a table goes through s.rebuild, which copies the
	// whole file first: the old table is gone by the time anything downstream
	// can fail, so without the copy there would be nothing to go back to.
	steps := []struct {
		id      string
		ddl     string
		rebuild bool
	}{
		{id: projCardsHierarchyID, ddl: projectsHierarchyDDL},
		{id: projAliasesID, ddl: projectAliasesDDL},
		{id: projTasksRebuildID, ddl: tasksRebuildDDL, rebuild: true},
		{id: projEvidenceRebuildID, ddl: evidenceRebuildDDL, rebuild: true},
		{id: projBenchmarksID, ddl: benchmarksDDL},
	}
	for _, step := range steps {
		ddl := step.ddl
		var err error
		if step.rebuild {
			err = s.rebuild(step.id, func() error { return s.rebuildTable(ddl) })
		} else {
			err = s.once(step.id, func() error {
				_, execErr := s.execHook(s.db, ddl)
				return execErr
			})
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// tasksRebuildDDL replaces tasks with its version-3 shape. A rebuild rather
// than a column-by-column ALTER because three of the changes cannot be
// expressed as additions: the state CHECK has to accept three more values, the
// identity CHECK has to accept a slug where it used to demand a Jira key or an
// SDD change, and closed_at has to be legal on an archived task.
//
// Everything the old table held is carried over verbatim. The new columns land
// as NULL, which is what "we do not know yet" looks like for a task written
// before the vault had a say in any of it.
const tasksRebuildDDL = `
CREATE TABLE tasks_rebuild (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    sync_id              TEXT    NOT NULL UNIQUE,
    project              TEXT    NOT NULL REFERENCES project_cards(slug)
                                 ON DELETE RESTRICT ON UPDATE CASCADE,
    jira_key             TEXT    UNIQUE
                         CHECK (jira_key IS NULL OR jira_key GLOB '[A-Z]*-[0-9]*'),
    sdd_change           TEXT    CHECK (sdd_change IS NULL OR sdd_change = lower(sdd_change)),
    slug                 TEXT    CHECK (slug IS NULL
                                        OR (slug = lower(trim(slug)) AND length(slug) BETWEEN 1 AND 80)),
    title                TEXT    NOT NULL CHECK (length(trim(title)) > 0),
    summary              TEXT    CHECK (summary IS NULL OR length(summary) <= 1000),
    pending_note         TEXT,
    vault_path           TEXT    CHECK (vault_path IS NULL
                                        OR (length(trim(vault_path)) > 0
                                            AND vault_path NOT LIKE '/%'
                                            AND vault_path NOT LIKE '~%'
                                            AND vault_path NOT LIKE '%..%')),
    kind                 TEXT    NOT NULL
                         CHECK (kind IN ('feature','bugfix','refactor','incident','migration','spike')),
    state                TEXT    NOT NULL DEFAULT 'open'
                         CHECK (state IN ('open','analysis','in_progress','review','verified',
                                          'done','blocked','cancelled','pending','archived','unverified')),
    jira_status          TEXT,
    jira_status_category TEXT    CHECK (jira_status_category IS NULL
                                        OR jira_status_category IN ('new','indeterminate','done')),
    state_synced_at      TEXT,
    branch               TEXT,
    pr_url               TEXT,
    knowledge_ref        TEXT,
    assignee             TEXT,
    parent_task_id       INTEGER REFERENCES tasks(id) ON DELETE SET NULL,
    parent_task_sync_id  TEXT,
    created_at           TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_at           TEXT    NOT NULL DEFAULT (datetime('now')),
    closed_at            TEXT,
    deleted_at           TEXT,
    CHECK (closed_at IS NULL OR state IN ('done','cancelled','archived')),
    CHECK (jira_key IS NOT NULL OR sdd_change IS NOT NULL OR slug IS NOT NULL),
    CHECK (parent_task_id IS NULL OR parent_task_id <> id)
);

INSERT INTO tasks_rebuild
    (id, sync_id, project, jira_key, sdd_change, slug, title, summary, pending_note, vault_path,
     kind, state, jira_status, jira_status_category, state_synced_at, branch, pr_url,
     knowledge_ref, assignee, parent_task_id, parent_task_sync_id,
     created_at, updated_at, closed_at, deleted_at)
SELECT id, sync_id, project, jira_key, sdd_change, NULL, title, NULL, NULL, NULL,
       kind, state, jira_status, jira_status_category, state_synced_at, branch, pr_url,
       knowledge_ref, assignee, NULL, NULL,
       created_at, updated_at, closed_at, deleted_at
FROM tasks;

DROP TABLE tasks;
ALTER TABLE tasks_rebuild RENAME TO tasks;

CREATE INDEX IF NOT EXISTS idx_tasks_project_state ON tasks(project, state, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_tasks_sdd_change    ON tasks(project, sdd_change);
CREATE INDEX IF NOT EXISTS idx_tasks_deleted       ON tasks(deleted_at);
CREATE INDEX IF NOT EXISTS idx_tasks_parent        ON tasks(parent_task_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_tasks_project_slug
    ON tasks(project, slug) WHERE slug IS NOT NULL AND deleted_at IS NULL;

CREATE TRIGGER IF NOT EXISTS tasks_fts_insert AFTER INSERT ON tasks BEGIN
    INSERT INTO tasks_fts(rowid, title, jira_key, sdd_change, branch, project)
    VALUES (new.id, new.title, new.jira_key, new.sdd_change, new.branch, new.project);
END;
CREATE TRIGGER IF NOT EXISTS tasks_fts_delete AFTER DELETE ON tasks BEGIN
    INSERT INTO tasks_fts(tasks_fts, rowid, title, jira_key, sdd_change, branch, project)
    VALUES ('delete', old.id, old.title, old.jira_key, old.sdd_change, old.branch, old.project);
END;
CREATE TRIGGER IF NOT EXISTS tasks_fts_update AFTER UPDATE ON tasks BEGIN
    INSERT INTO tasks_fts(tasks_fts, rowid, title, jira_key, sdd_change, branch, project)
    VALUES ('delete', old.id, old.title, old.jira_key, old.sdd_change, old.branch, old.project);
    INSERT INTO tasks_fts(rowid, title, jira_key, sdd_change, branch, project)
    VALUES (new.id, new.title, new.jira_key, new.sdd_change, new.branch, new.project);
END;

INSERT INTO tasks_fts(tasks_fts) VALUES('rebuild');
`

// evidenceRebuildDDL replaces evidence with its version-3 shape.
//
// Two closed lists change at once. category did not exist, and the vault has
// been filing captures under eleven of them all along — without the column, a
// scan can register what it found but not where it belongs. And kind accepted
// six file types, which is fewer than a capture session produces in an
// afternoon: a .webp screenshot or a .har trace had to be logged as something
// it is not, or not logged at all. `other` is the escape hatch, so an unusual
// extension degrades to a truthful label instead of a wrong one.
//
// Existing rows take the default category. `evidences` is the right default
// rather than a guess: it is the category the capture flow has always written
// into, and inferring anything else from a path would be inventing history.
const evidenceRebuildDDL = `
CREATE TABLE evidence_rebuild (
    id                      INTEGER PRIMARY KEY AUTOINCREMENT,
    sync_id                 TEXT    NOT NULL UNIQUE,
    project                 TEXT    NOT NULL REFERENCES project_cards(slug)
                                    ON DELETE RESTRICT ON UPDATE CASCADE,
    task_id                 INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    task_sync_id            TEXT    NOT NULL REFERENCES tasks(sync_id) ON DELETE CASCADE,
    path                    TEXT    NOT NULL
                            CHECK (length(trim(path)) > 0 AND path NOT LIKE '/%' AND path NOT LIKE '~%'),
    sha256                  TEXT    NOT NULL
                            CHECK (length(sha256) = 64 AND sha256 = lower(sha256)),
    category                TEXT    NOT NULL DEFAULT 'evidences'
                            CHECK (category IN ('analysis','plans','runbooks','reports','patches',
                                                'evidences','evidences-qa','benchmarks','scripts',
                                                'assets','exports')),
    kind                    TEXT    NOT NULL
                            CHECK (kind IN ('png','jpg','gif','webp','svg','mp4','webm','json','csv',
                                            'log','txt','md','patch','diff','pdf','html','zip','har','other')),
    proves                  TEXT    NOT NULL CHECK (length(trim(proves)) > 0),
    config_stamp            TEXT,
    captured_at             TEXT    NOT NULL,
    attached_jira           INTEGER NOT NULL DEFAULT 0 CHECK (attached_jira IN (0, 1)),
    attached_confluence_url TEXT,
    size_bytes              INTEGER CHECK (size_bytes IS NULL OR size_bytes >= 0),
    manifest_path           TEXT,
    -- When the path and category last moved. A relocation is the one thing an
    -- otherwise immutable row can report, and two replicas need a clock they
    -- both hold to agree on which report is the later one.
    location_set_at         TEXT,
    created_at              TEXT    NOT NULL DEFAULT (datetime('now')),
    deleted_at              TEXT,
    UNIQUE (task_sync_id, sha256)
);

INSERT INTO evidence_rebuild
    (id, sync_id, project, task_id, task_sync_id, path, sha256, category, kind, proves,
     config_stamp, captured_at, attached_jira, attached_confluence_url, size_bytes,
     manifest_path, location_set_at, created_at, deleted_at)
SELECT id, sync_id, project, task_id, task_sync_id, path, sha256, 'evidences', kind, proves,
       config_stamp, captured_at, attached_jira, attached_confluence_url, size_bytes,
       manifest_path, created_at, created_at, deleted_at
FROM evidence;

DROP TABLE evidence;
ALTER TABLE evidence_rebuild RENAME TO evidence;

CREATE INDEX IF NOT EXISTS idx_evidence_task     ON evidence(task_id, captured_at DESC);
CREATE INDEX IF NOT EXISTS idx_evidence_project  ON evidence(project, captured_at DESC);
CREATE INDEX IF NOT EXISTS idx_evidence_category ON evidence(project, category, captured_at DESC);

CREATE VIRTUAL TABLE IF NOT EXISTS evidence_fts USING fts5(
    path, proves, category, kind, project,
    content='evidence', content_rowid='id'
);
CREATE TRIGGER IF NOT EXISTS evidence_fts_insert AFTER INSERT ON evidence BEGIN
    INSERT INTO evidence_fts(rowid, path, proves, category, kind, project)
    VALUES (new.id, new.path, new.proves, new.category, new.kind, new.project);
END;
CREATE TRIGGER IF NOT EXISTS evidence_fts_delete AFTER DELETE ON evidence BEGIN
    INSERT INTO evidence_fts(evidence_fts, rowid, path, proves, category, kind, project)
    VALUES ('delete', old.id, old.path, old.proves, old.category, old.kind, old.project);
END;
CREATE TRIGGER IF NOT EXISTS evidence_fts_update AFTER UPDATE ON evidence BEGIN
    INSERT INTO evidence_fts(evidence_fts, rowid, path, proves, category, kind, project)
    VALUES ('delete', old.id, old.path, old.proves, old.category, old.kind, old.project);
    INSERT INTO evidence_fts(rowid, path, proves, category, kind, project)
    VALUES (new.id, new.path, new.proves, new.category, new.kind, new.project);
END;

INSERT INTO evidence_fts(evidence_fts) VALUES('rebuild');
`

// projectAliasesDDL is the proj-0002-project-aliases step. An alias is a name
// that resolves to a project, never a rename: nothing historical moves, so a
// tool configured years ago against the old spelling keeps working while the
// rows stay under the name they were written with.
const projectAliasesDDL = `
CREATE TABLE IF NOT EXISTS project_aliases (
    alias      TEXT    PRIMARY KEY
               CHECK (alias = lower(trim(alias)) AND length(alias) BETWEEN 1 AND 96),
    sync_id    TEXT    NOT NULL UNIQUE,
    slug       TEXT    NOT NULL REFERENCES project_cards(slug)
                       ON DELETE CASCADE ON UPDATE CASCADE,
    source     TEXT    NOT NULL
               CHECK (source IN ('git_remote','dir','env','manual','normalizer')),
    created_at TEXT    NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT    NOT NULL DEFAULT (datetime('now')),
    deleted_at TEXT,
    CHECK (alias <> slug)
);
CREATE INDEX IF NOT EXISTS idx_project_aliases_slug ON project_aliases(slug);
`

// benchmarksDDL is the proj-0005-benchmarks step. A measurement is what
// justified a change, and it was being kept as an attached file nobody could
// query: the number, its unit and which run it came from all lived inside a
// JSON blob. Here they are columns, so "is this better than before" is a
// comparison rather than a reading exercise.
//
// A measurement is immutable — it was taken at a moment, from a run, and no
// later run makes it untrue. The one thing that moves is which row is the
// baseline, and idx_benchmarks_one_baseline makes the database enforce that
// only one row per metric claims it.
const benchmarksDDL = `
CREATE TABLE IF NOT EXISTS benchmarks (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    sync_id         TEXT    NOT NULL UNIQUE,
    project         TEXT    NOT NULL REFERENCES project_cards(slug)
                            ON DELETE RESTRICT ON UPDATE CASCADE,
    task_id         INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    task_sync_id    TEXT    NOT NULL REFERENCES tasks(sync_id) ON DELETE CASCADE,
    name            TEXT    NOT NULL CHECK (length(trim(name)) > 0),
    metric          TEXT    NOT NULL CHECK (length(trim(metric)) > 0),
    unit            TEXT    NOT NULL
                    CHECK (unit IN ('ms','s','count','bytes','kib','mib','pct','ops','rps','usd','score')),
    direction       TEXT    NOT NULL DEFAULT 'lower' CHECK (direction IN ('lower','higher')),
    value           REAL    NOT NULL,
    baseline        INTEGER NOT NULL DEFAULT 0 CHECK (baseline IN (0, 1)),
    baseline_set_at TEXT,
    run_path        TEXT    CHECK (run_path IS NULL
                                   OR (length(trim(run_path)) > 0
                                       AND run_path NOT LIKE '/%'
                                       AND run_path NOT LIKE '~%'
                                       AND run_path NOT LIKE '%..%')),
    sha256          TEXT    CHECK (sha256 IS NULL OR (length(sha256) = 64 AND sha256 = lower(sha256))),
    config_stamp    TEXT,
    captured_at     TEXT    NOT NULL,
    notes           TEXT,
    source          TEXT    NOT NULL DEFAULT 'manual' CHECK (source IN ('manual','json')),
    created_at      TEXT    NOT NULL DEFAULT (datetime('now')),
    deleted_at      TEXT,
    UNIQUE (task_sync_id, name, metric, captured_at),
    CHECK (baseline = 0 OR baseline_set_at IS NOT NULL)
);
CREATE INDEX IF NOT EXISTS idx_benchmarks_task ON benchmarks(task_id, captured_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS idx_benchmarks_one_baseline
    ON benchmarks(task_sync_id, metric) WHERE baseline = 1 AND deleted_at IS NULL;
`

// projectsSchemaDropDDL removes every engram-projects object in dependency
// order: FTS5 sync triggers first, then the FTS5 virtual tables, then the
// contract and auxiliary tables (children before parents so foreign keys
// never block the drop). No upstream table is touched.
const projectsSchemaDropDDL = `
DROP TRIGGER IF EXISTS project_cards_depth_ck;
DROP TRIGGER IF EXISTS evidence_fts_update;
DROP TRIGGER IF EXISTS evidence_fts_delete;
DROP TRIGGER IF EXISTS evidence_fts_insert;
DROP TABLE IF EXISTS evidence_fts;
DROP TRIGGER IF EXISTS runbook_fts_update;
DROP TRIGGER IF EXISTS runbook_fts_delete;
DROP TRIGGER IF EXISTS runbook_fts_insert;
DROP TRIGGER IF EXISTS tasks_fts_update;
DROP TRIGGER IF EXISTS tasks_fts_delete;
DROP TRIGGER IF EXISTS tasks_fts_insert;
DROP TABLE IF EXISTS runbook_index_fts;
DROP TABLE IF EXISTS tasks_fts;
DROP TABLE IF EXISTS task_link_tombstones;
DROP TABLE IF EXISTS project_aliases;
DROP TABLE IF EXISTS benchmarks;
DROP TABLE IF EXISTS observation_refs;
DROP TABLE IF EXISTS task_observations;
DROP TABLE IF EXISTS evidence;
DROP TABLE IF EXISTS runbook_index;
DROP TABLE IF EXISTS tasks;
DROP TABLE IF EXISTS project_cards;
`

// DropProjectsSchema is the explicit rollback path for the engram-projects
// extension: it drops every trigger, FTS5 table, and contract table it
// created, then resets PRAGMA user_version to 0. Callers (e.g. the
// `engram projects schema drop --yes` CLI command) are responsible for
// requiring an export/confirmation before invoking this — it performs no
// backup itself. It never touches any upstream table (sessions,
// observations, memory_relations, sync_*, ...).
func (s *Store) DropProjectsSchema() error {
	if _, err := s.execHook(s.db, projectsSchemaDropDDL); err != nil {
		return fmt.Errorf("engram-projects: drop schema: %w", err)
	}
	// The ledger rows go with the tables they describe. Leaving them behind
	// would tell the next migration that a step whose table no longer exists
	// has already run, and the schema would come back in its version-2 shape
	// with none of the columns the code expects.
	if _, err := s.execHook(s.db, `DELETE FROM schema_migrations WHERE id LIKE 'proj-%'`); err != nil {
		return fmt.Errorf("engram-projects: clear migration ledger: %w", err)
	}
	if _, err := s.execHook(s.db, "PRAGMA user_version = 0"); err != nil {
		return fmt.Errorf("engram-projects: reset user_version: %w", err)
	}
	return nil
}

// ProjectsSchemaStatus reports whether the engram-projects schema is
// present, its PRAGMA user_version stamp, and row counts for the five
// contract tables. It is the read model `engram doctor` uses to confirm the
// schema is up to date (see ProjectsSchemaCheck in internal/diagnostic)
// without that package running raw SQL against the store.
type ProjectsSchemaStatus struct {
	Present          bool `json:"present"`
	UserVersion      int  `json:"user_version"`
	ProjectCards     int  `json:"project_cards"`
	Tasks            int  `json:"tasks"`
	Evidence         int  `json:"evidence"`
	RunbookIndex     int  `json:"runbook_index"`
	TaskObservations int  `json:"task_observations"`
	ProjectAliases   int  `json:"project_aliases"`
	Benchmarks       int  `json:"benchmarks"`
}

// ProjectsSchemaStatus reads the current state of the engram-projects
// schema. When the schema has not been created yet (Present == false), the
// row counts are left at zero rather than erroring.
func (s *Store) ProjectsSchemaStatus() (ProjectsSchemaStatus, error) {
	var status ProjectsSchemaStatus

	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return status, fmt.Errorf("engram-projects: read user_version: %w", err)
	}
	status.UserVersion = version

	var name string
	err := s.db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='project_cards'`,
	).Scan(&name)
	switch {
	case err == nil:
		status.Present = true
	case errors.Is(err, sql.ErrNoRows):
		return status, nil
	default:
		return status, fmt.Errorf("engram-projects: check schema presence: %w", err)
	}

	counts := []struct {
		table string
		dest  *int
	}{
		{"project_cards", &status.ProjectCards},
		{"tasks", &status.Tasks},
		{"evidence", &status.Evidence},
		{"runbook_index", &status.RunbookIndex},
		{"task_observations", &status.TaskObservations},
		{"project_aliases", &status.ProjectAliases},
		{"benchmarks", &status.Benchmarks},
	}
	for _, c := range counts {
		if err := s.db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", c.table)).Scan(c.dest); err != nil {
			return status, fmt.Errorf("engram-projects: count %s: %w", c.table, err)
		}
	}
	return status, nil
}
