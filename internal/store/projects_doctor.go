package store

import (
	"database/sql"
	"fmt"
	"strings"
)

// RunbookIndexFreshness is the read model behind the `runbook_index_age`
// doctor check (RFC §10.5): how many rows the index holds, how many of them
// are already flagged stale by their own source, and when the oldest and
// newest row were last synced.
type RunbookIndexFreshness struct {
	Project     string `json:"project,omitempty"`
	Rows        int    `json:"rows"`
	StaleRows   int    `json:"stale_rows"`
	OldestSync  string `json:"oldest_synced_at,omitempty"`
	NewestSync  string `json:"newest_synced_at,omitempty"`
	OldestAgeD  int    `json:"oldest_age_days"`
	SchemaSetUp bool   `json:"schema_present"`
}

// RunbookIndexAge reports the freshness of the runbook index. An empty
// project covers every service. A database without the engram-projects
// schema answers SchemaSetUp=false and no error: the check turns that into an
// informational result instead of a failure.
func (s *Store) RunbookIndexAge(project string) (RunbookIndexFreshness, error) {
	var out RunbookIndexFreshness
	schema, err := s.ProjectsSchemaStatus()
	if err != nil {
		return out, err
	}
	if !schema.Present {
		return out, nil
	}
	out.SchemaSetUp = true

	project = strings.TrimSpace(project)
	if project != "" {
		project, _ = NormalizeProject(project)
		out.Project = project
	}

	query := `
		SELECT COUNT(*), COALESCE(SUM(stale), 0), COALESCE(MIN(synced_at), ''), COALESCE(MAX(synced_at), ''),
		       COALESCE(CAST(julianday('now') - julianday(MIN(synced_at)) AS INTEGER), 0)
		FROM runbook_index`
	var args []any
	if project != "" {
		query += ` WHERE project = ?`
		args = append(args, project)
	}
	if err := s.db.QueryRow(query, args...).Scan(
		&out.Rows, &out.StaleRows, &out.OldestSync, &out.NewestSync, &out.OldestAgeD,
	); err != nil {
		return out, fmt.Errorf("engram-projects: read runbook index age: %w", err)
	}
	if out.OldestAgeD < 0 {
		out.OldestAgeD = 0
	}
	return out, nil
}

// VaultPointer is one stored path into the knowledge vault, with enough
// context for `engram doctor` to name what would break if the file is gone.
type VaultPointer struct {
	// Kind is the column the pointer came from: knowledge_ref (a task),
	// observation_ref (an observation), knowledge_hub_path (a project card)
	// or runbook_vault_path (a runbook index row).
	Kind string `json:"kind"`
	// Owner identifies the row: a task key, an observation sync_id, a project
	// slug or a runbook id.
	Owner string `json:"owner"`
	// Path is the vault-relative path, anchor already removed.
	Path string `json:"path"`
	// Project is the project the row belongs to, when it has one.
	Project string `json:"project,omitempty"`
}

// KnowledgeVaultPointers returns every stored path into the vault, so a
// caller can check them against a checkout. An empty project covers every
// project; a database without the engram-projects schema returns nothing.
//
// The anchor is stripped here: `Doc.md#Heading` and `Doc.md` are the same
// file, and only the file can be checked for existence.
func (s *Store) KnowledgeVaultPointers(project string) ([]VaultPointer, error) {
	schema, err := s.ProjectsSchemaStatus()
	if err != nil {
		return nil, err
	}
	if !schema.Present {
		return nil, nil
	}

	project = strings.TrimSpace(project)
	if project != "" {
		project, _ = NormalizeProject(project)
	}

	type source struct {
		kind  string
		query string
	}
	sources := []source{
		{"knowledge_ref", `
			SELECT COALESCE(jira_key, sdd_change, sync_id), knowledge_ref, project
			FROM tasks
			WHERE knowledge_ref IS NOT NULL AND trim(knowledge_ref) <> '' AND deleted_at IS NULL`},
		{"observation_ref", `
			SELECT r.observation_sync_id, r.ref, COALESCE(o.project, '')
			FROM observation_refs r
			LEFT JOIN observations o ON o.sync_id = r.observation_sync_id
			WHERE r.ref_kind = 'knowledge' AND (o.deleted_at IS NULL OR o.sync_id IS NULL)`},
		{"knowledge_hub_path", `
			SELECT slug, knowledge_hub_path, slug
			FROM project_cards
			WHERE knowledge_hub_path IS NOT NULL AND trim(knowledge_hub_path) <> ''`},
		{"runbook_vault_path", `
			SELECT id, vault_path, project
			FROM runbook_index
			WHERE trim(vault_path) <> ''`},
	}

	var out []VaultPointer
	for _, src := range sources {
		query := src.query
		var args []any
		if project != "" {
			column := "project"
			switch src.kind {
			case "observation_ref":
				column = "o.project"
			case "knowledge_hub_path":
				column = "slug"
			}
			query += " AND " + column + " = ?"
			args = append(args, project)
		}
		rows, err := s.db.Query(query, args...)
		if err != nil {
			return nil, fmt.Errorf("engram-projects: list %s pointers: %w", src.kind, err)
		}
		for rows.Next() {
			var owner, path string
			var rowProject sql.NullString
			if err := rows.Scan(&owner, &path, &rowProject); err != nil {
				rows.Close()
				return nil, fmt.Errorf("engram-projects: scan %s pointer: %w", src.kind, err)
			}
			out = append(out, VaultPointer{
				Kind:    src.kind,
				Owner:   owner,
				Path:    vaultPathWithoutAnchor(path),
				Project: rowProject.String,
			})
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	return out, nil
}

// vaultPathWithoutAnchor drops a trailing `#Heading` from a vault pointer.
func vaultPathWithoutAnchor(path string) string {
	path = strings.TrimSpace(path)
	if hash := strings.Index(path, "#"); hash >= 0 {
		path = strings.TrimSpace(path[:hash])
	}
	return path
}
