package store

import (
	"fmt"
	"strings"
)

// ProjectJournalGapReport counts the rows of one enrolled project that have no
// sync_mutations row, split by what the cloud upsert contract can do with them.
//
// The split matters because `pending mutations = 0` is not the same as
// "replicated": a row with no journal entry at all never enters a push, and the
// per-project counters report nothing about it. Journalable rows are the ones a
// backfill pass can enqueue; blocked rows are live rows that the cloud would
// reject on sight (an upsert needs session_id, type, title, content and scope
// for an observation, session_id and content for a prompt), so the backfill
// skips them rather than poisoning the journal with undeliverable work. Those
// used to vanish silently — counted here instead, so the upgrade doctor can
// refuse to call the project ready while any remain.
type ProjectJournalGapReport struct {
	Project      string
	Sessions     int
	Observations int
	Prompts      int
	Relations    int
	Blocked      int
}

// Journalable is the number of rows a backfill pass would enqueue.
func (r ProjectJournalGapReport) Journalable() int {
	return r.Sessions + r.Observations + r.Prompts + r.Relations
}

// Total is every row of the project that has no journal entry, deliverable or
// not.
func (r ProjectJournalGapReport) Total() int {
	return r.Journalable() + r.Blocked
}

// Summary renders the report for the CLI in a single line.
func (r ProjectJournalGapReport) Summary() string {
	return fmt.Sprintf(
		"sessions=%d observations=%d prompts=%d relations=%d blocked=%d",
		r.Sessions, r.Observations, r.Prompts, r.Relations, r.Blocked,
	)
}

// observationUpsertFieldPredicate is the set of columns the cloud requires on an
// observation upsert. It is shared by the journalable and blocked counts so the
// two can never overlap or leave a row uncounted.
const observationUpsertFieldPredicate = `
	trim(ifnull(o.session_id, '')) != ''
	AND trim(ifnull(o.type, '')) != ''
	AND trim(ifnull(o.title, '')) != ''
	AND trim(ifnull(o.content, '')) != ''
	AND trim(ifnull(o.scope, '')) != ''`

// promptUpsertFieldPredicate is the same contract for a prompt upsert.
const promptUpsertFieldPredicate = `
	trim(ifnull(p.session_id, '')) != ''
	AND trim(ifnull(p.content, '')) != ''`

// ProjectJournalGaps counts the rows of a project that carry no sync_mutations
// entry. Every project comparison is folded: enrollment normalizes the slug to
// lower case while the rows keep whatever case they were written with.
func (s *Store) ProjectJournalGaps(project string) (ProjectJournalGapReport, error) {
	project, _ = NormalizeProject(project)
	project = strings.TrimSpace(project)
	report := ProjectJournalGapReport{Project: project}
	if project == "" {
		return report, nil
	}

	counts := []struct {
		into *int
		q    string
		args []any
	}{
		{
			into: &report.Sessions,
			q: `SELECT COUNT(*) FROM sessions
			    WHERE lower(project) = ?
			      AND NOT EXISTS (
			        SELECT 1 FROM sync_mutations sm
			        WHERE sm.target_key = ? AND sm.entity = ? AND sm.entity_key = sessions.id AND sm.source = ? AND lower(sm.project) = ?
			      )`,
			args: []any{project, DefaultSyncTargetKey, SyncEntitySession, SyncSourceLocal, project},
		},
		{
			into: &report.Observations,
			q: `SELECT COUNT(*) FROM observations o
			    LEFT JOIN sessions s ON s.id = o.session_id
			    WHERE (lower(ifnull(o.project, '')) = ? OR (ifnull(o.project, '') = '' AND lower(ifnull(s.project, '')) = ?))
			      AND o.deleted_at IS NULL
			      AND` + observationUpsertFieldPredicate + `
			      AND NOT EXISTS (
			        SELECT 1 FROM sync_mutations sm
			        WHERE sm.target_key = ? AND sm.entity = ? AND sm.entity_key = o.sync_id AND sm.source = ? AND lower(sm.project) = ?
			      )`,
			args: []any{project, project, DefaultSyncTargetKey, SyncEntityObservation, SyncSourceLocal, project},
		},
		{
			// Soft-deleted observations need a delete mutation of their own.
			// They have no field contract to meet, so they are always
			// journalable — and the fast-path guard used to have no counterpart
			// for them at all, which let a project whose only gap was a delete
			// skip the backfill forever.
			into: &report.Observations,
			q: `SELECT COUNT(*) FROM observations o
			    LEFT JOIN sessions s ON s.id = o.session_id
			    WHERE (lower(ifnull(o.project, '')) = ? OR (ifnull(o.project, '') = '' AND lower(ifnull(s.project, '')) = ?))
			      AND o.deleted_at IS NOT NULL
			      AND NOT EXISTS (
			        SELECT 1 FROM sync_mutations sm
			        WHERE sm.target_key = ? AND sm.entity = ? AND sm.entity_key = o.sync_id AND sm.op = ? AND sm.source = ? AND lower(sm.project) = ?
			      )`,
			args: []any{project, project, DefaultSyncTargetKey, SyncEntityObservation, SyncOpDelete, SyncSourceLocal, project},
		},
		{
			into: &report.Prompts,
			q: `SELECT COUNT(*) FROM user_prompts p
			    LEFT JOIN sessions s ON s.id = p.session_id
			    WHERE (lower(ifnull(p.project, '')) = ? OR (ifnull(p.project, '') = '' AND lower(ifnull(s.project, '')) = ?))
			      AND` + promptUpsertFieldPredicate + `
			      AND NOT EXISTS (
			        SELECT 1 FROM sync_mutations sm
			        WHERE sm.target_key = ? AND sm.entity = ? AND sm.entity_key = p.sync_id AND sm.source = ? AND lower(sm.project) = ?
			      )`,
			args: []any{project, project, DefaultSyncTargetKey, SyncEntityPrompt, SyncSourceLocal, project},
		},
		{
			into: &report.Prompts,
			q: `SELECT COUNT(*) FROM prompt_tombstones t
			    LEFT JOIN sessions s ON s.id = t.session_id
			    WHERE (lower(ifnull(t.project, '')) = ? OR (ifnull(t.project, '') = '' AND lower(ifnull(s.project, '')) = ?))
			      AND NOT EXISTS (
			        SELECT 1 FROM sync_mutations sm
			        WHERE sm.target_key = ? AND sm.entity = ? AND sm.entity_key = t.sync_id AND sm.op = ? AND sm.source = ? AND lower(sm.project) = ?
			      )`,
			args: []any{project, project, DefaultSyncTargetKey, SyncEntityPrompt, SyncOpDelete, SyncSourceLocal, project},
		},
		{
			into: &report.Relations,
			q: `SELECT COUNT(*)
			    FROM memory_relations r
			    JOIN observations src ON src.sync_id = r.source_id AND src.deleted_at IS NULL
			    JOIN observations tgt ON tgt.sync_id = r.target_id AND tgt.deleted_at IS NULL
			    LEFT JOIN sessions src_s ON src_s.id = src.session_id
			    WHERE r.judgment_status NOT IN (?, ?)
			      AND ifnull(r.marked_by_actor, '') != ''
			      AND ifnull(r.marked_by_kind, '') != ''
			      AND lower(coalesce(nullif(src.project, ''), src_s.project, '')) = ?
			      AND NOT EXISTS (
			        SELECT 1 FROM sync_mutations sm
			        WHERE sm.target_key = ? AND sm.entity = ? AND sm.entity_key = r.sync_id AND sm.source = ? AND lower(sm.project) = ?
			      )`,
			args: []any{JudgmentStatusOrphaned, JudgmentStatusPending, project, DefaultSyncTargetKey, SyncEntityRelation, SyncSourceLocal, project},
		},
		{
			// Live observations the cloud contract would reject. They have no
			// journal entry and no backfill can give them one, so they only
			// leave the count when the row itself is completed or removed.
			into: &report.Blocked,
			q: `SELECT COUNT(*) FROM observations o
			    LEFT JOIN sessions s ON s.id = o.session_id
			    WHERE (lower(ifnull(o.project, '')) = ? OR (ifnull(o.project, '') = '' AND lower(ifnull(s.project, '')) = ?))
			      AND o.deleted_at IS NULL
			      AND NOT (` + observationUpsertFieldPredicate + `)
			      AND NOT EXISTS (
			        SELECT 1 FROM sync_mutations sm
			        WHERE sm.target_key = ? AND sm.entity = ? AND sm.entity_key = o.sync_id AND sm.source = ? AND lower(sm.project) = ?
			      )`,
			args: []any{project, project, DefaultSyncTargetKey, SyncEntityObservation, SyncSourceLocal, project},
		},
		{
			into: &report.Blocked,
			q: `SELECT COUNT(*) FROM user_prompts p
			    LEFT JOIN sessions s ON s.id = p.session_id
			    WHERE (lower(ifnull(p.project, '')) = ? OR (ifnull(p.project, '') = '' AND lower(ifnull(s.project, '')) = ?))
			      AND NOT (` + promptUpsertFieldPredicate + `)
			      AND NOT EXISTS (
			        SELECT 1 FROM sync_mutations sm
			        WHERE sm.target_key = ? AND sm.entity = ? AND sm.entity_key = p.sync_id AND sm.source = ? AND lower(sm.project) = ?
			      )`,
			args: []any{project, project, DefaultSyncTargetKey, SyncEntityPrompt, SyncSourceLocal, project},
		},
	}

	for _, c := range counts {
		var n int
		if err := s.db.QueryRow(c.q, c.args...).Scan(&n); err != nil {
			return ProjectJournalGapReport{}, fmt.Errorf("count journal gaps for project %q: %w", project, err)
		}
		*c.into += n
	}
	return report, nil
}

// EnrolledProjectJournalGaps reports the journal gaps of every enrolled project
// that still has one, ordered by project name.
func (s *Store) EnrolledProjectJournalGaps() ([]ProjectJournalGapReport, error) {
	projects, err := s.enrolledProjectNames()
	if err != nil {
		return nil, err
	}
	reports := make([]ProjectJournalGapReport, 0, len(projects))
	for _, project := range projects {
		report, err := s.ProjectJournalGaps(project)
		if err != nil {
			return nil, err
		}
		if report.Total() == 0 {
			continue
		}
		reports = append(reports, report)
	}
	return reports, nil
}

func (s *Store) enrolledProjectNames() ([]string, error) {
	rows, err := s.db.Query(`SELECT project FROM sync_enrolled_projects ORDER BY project ASC`)
	if err != nil {
		return nil, err
	}
	var projects []string
	for rows.Next() {
		var project string
		if err := rows.Scan(&project); err != nil {
			return nil, closeRowsWithError(rows, err)
		}
		projects = append(projects, project)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return projects, nil
}
