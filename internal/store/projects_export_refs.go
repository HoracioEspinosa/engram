package store

import (
	"fmt"
	"strings"
)

// ObservationExportRefs are the engram-projects pointers the weekly bridge
// stamps on an exported note's frontmatter (RFC §9.5). Everything here is
// additive: a note whose observation carries none of them is exported exactly
// as before.
//
// Only these four travel. Tasks and evidence are operational and replicate
// through the cloud instead, and the runbook index is derived from the vault
// itself, so exporting it back would be a loop.
type ObservationExportRefs struct {
	// KnowledgeRef is the curated document that already describes the fact.
	// Its presence is the promotion mark of D-11: once stamped, the bridge
	// stops exporting the observation.
	KnowledgeRef string `json:"knowledge_ref,omitempty"`
	// JiraKey is the ticket of the task the observation is linked to.
	JiraKey string `json:"jira_key,omitempty"`
	// RunbookID is the RB-NNN the observation refers to.
	RunbookID string `json:"runbook_id,omitempty"`
	// GraphCommit is the commit the graph facts were read at.
	GraphCommit string `json:"graph_commit,omitempty"`
}

// Empty reports whether there is nothing to stamp.
func (r ObservationExportRefs) Empty() bool {
	return r.KnowledgeRef == "" && r.JiraKey == "" && r.RunbookID == "" && r.GraphCommit == ""
}

// ObservationExportRefs collects, per observation sync_id, the references the
// obsidian exporter adds to the frontmatter. An empty project means every
// project.
//
// A database without the engram-projects schema returns an empty map and no
// error: the exporter predates these tables and has to keep working on a
// store that never created them.
func (s *Store) ObservationExportRefs(project string) (map[string]ObservationExportRefs, error) {
	schema, err := s.ProjectsSchemaStatus()
	if err != nil {
		return nil, err
	}
	out := map[string]ObservationExportRefs{}
	if !schema.Present {
		return out, nil
	}

	project = strings.TrimSpace(project)
	if project != "" {
		project, _ = NormalizeProject(project)
	}

	// observation_refs holds knowledge, runbook and graph pointers. The first
	// one of each kind wins: refs are inserted in order, so that is the
	// earliest stamp, and a later contradicting one is a conflict for
	// mem_judge to settle, not for the exporter to pick a side on.
	refQuery := `
		SELECT r.observation_sync_id, r.ref_kind, r.ref, COALESCE(r.graph_commit, '')
		FROM observation_refs r
		JOIN observations o ON o.sync_id = r.observation_sync_id
		WHERE o.deleted_at IS NULL`
	var refArgs []any
	if project != "" {
		refQuery += ` AND o.project = ?`
		refArgs = append(refArgs, project)
	}
	refQuery += ` ORDER BY r.id ASC`

	rows, err := s.db.Query(refQuery, refArgs...)
	if err != nil {
		return nil, fmt.Errorf("engram-projects: read observation refs for export: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var syncID, kind, ref, graphCommit string
		if err := rows.Scan(&syncID, &kind, &ref, &graphCommit); err != nil {
			return nil, fmt.Errorf("engram-projects: scan observation ref for export: %w", err)
		}
		entry := out[syncID]
		switch kind {
		case "knowledge":
			if entry.KnowledgeRef == "" {
				entry.KnowledgeRef = ref
			}
		case "runbook":
			if entry.RunbookID == "" {
				entry.RunbookID = ref
			}
		case "graph":
			if entry.GraphCommit == "" {
				entry.GraphCommit = graphCommit
			}
		}
		out[syncID] = entry
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// jira_key is not a ref: it comes from the task the observation is linked
	// to. An observation linked to several tasks keeps the first ticket, by
	// link order.
	taskQuery := `
		SELECT tob.observation_sync_id, t.jira_key
		FROM task_observations tob
		JOIN tasks t ON t.sync_id = tob.task_sync_id
		JOIN observations o ON o.sync_id = tob.observation_sync_id
		WHERE t.jira_key IS NOT NULL AND t.jira_key <> ''
		  AND t.deleted_at IS NULL AND o.deleted_at IS NULL`
	var taskArgs []any
	if project != "" {
		taskQuery += ` AND o.project = ?`
		taskArgs = append(taskArgs, project)
	}
	taskQuery += ` ORDER BY tob.linked_at ASC, tob.task_id ASC`

	taskRows, err := s.db.Query(taskQuery, taskArgs...)
	if err != nil {
		return nil, fmt.Errorf("engram-projects: read task keys for export: %w", err)
	}
	defer taskRows.Close()
	for taskRows.Next() {
		var syncID, jiraKey string
		if err := taskRows.Scan(&syncID, &jiraKey); err != nil {
			return nil, fmt.Errorf("engram-projects: scan task key for export: %w", err)
		}
		entry := out[syncID]
		if entry.JiraKey == "" {
			entry.JiraKey = jiraKey
		}
		out[syncID] = entry
	}
	return out, taskRows.Err()
}
