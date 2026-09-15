// Package store: project identity.
//
// One project answers to several names. A repository checked out twice, a tool
// configured with the old spelling, a shell that turned a hyphen into an
// underscore — each produces a name that means the existing project and does
// not equal its slug. This file resolves those names without renaming anything:
// history stays under the spelling it was written with, and only the lookup
// knows the spellings are the same thing.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ErrAliasOwnsRows marks an alias that is a project in its own right. See
// AliasOwnsRowsError for the count that comes with it.
var ErrAliasOwnsRows = errors.New("alias already owns memories of its own")

// AliasOwnsRowsError carries how many live observations the proposed alias
// holds, so the caller can tell the difference between a stray name and a real
// project somebody wants folded away.
type AliasOwnsRowsError struct {
	Alias string
	Slug  string
	Rows  int
}

func (e *AliasOwnsRowsError) Error() string {
	return fmt.Sprintf("%q holds %d observation(s) of its own and cannot become an alias of %q; merge the projects instead",
		e.Alias, e.Rows, e.Slug)
}

func (e *AliasOwnsRowsError) Unwrap() error { return ErrAliasOwnsRows }

// How a project name was resolved, most authoritative first.
const (
	// ProjectResolvedViaCard means the name is a project with a card.
	ProjectResolvedViaCard = "card"
	// ProjectResolvedViaObservations means the name has memories under it but
	// no card yet.
	ProjectResolvedViaObservations = "observations"
	// ProjectResolvedViaAlias means an alias row redirected the name.
	ProjectResolvedViaAlias = "alias"
	// ProjectResolvedViaFolded means the name matched a real project once the
	// separators were folded together.
	ProjectResolvedViaFolded = "folded"
	// ProjectResolvedViaUnresolved means nothing claimed the name.
	ProjectResolvedViaUnresolved = "unresolved"
)

// ProjectAlias mirrors a project_aliases row.
type ProjectAlias struct {
	Alias     string `json:"alias"`
	SyncID    string `json:"sync_id"`
	Slug      string `json:"slug"`
	Source    string `json:"source"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// ProjectResolution is the answer to "what project does this name mean?".
// Slug is empty exactly when Via is unresolved.
type ProjectResolution struct {
	Input       string `json:"input"`
	Slug        string `json:"slug"`
	Via         string `json:"via"`
	AliasSource string `json:"alias_source,omitempty"`
}

// projectSeparators are the characters people type where a slug uses a hyphen.
// A project written as "ai_engram", "ai engram" or "ai.engram" names the same
// thing as "ai-engram" — but only to a reader. Nothing is ever stored under the
// folded spelling.
var projectSeparators = strings.NewReplacer("_", "-", " ", "-", ".", "-")

// FoldProjectSeparators returns the comparison form of a project name. It is
// deliberately not part of NormalizeProject: normalization decides what gets
// written, and folding a separator on the way in would silently merge two
// projects a user meant to keep apart.
func FoldProjectSeparators(name string) string {
	folded := strings.TrimSpace(strings.ToLower(name))
	folded = projectSeparators.Replace(folded)
	for strings.Contains(folded, "--") {
		folded = strings.ReplaceAll(folded, "--", "-")
	}
	return strings.Trim(folded, "-")
}

// UpsertProjectAlias points alias at slug, creating the row or re-pointing an
// existing one.
//
// Two names are refused. An alias equal to its own target is a no-op dressed as
// a rule. An alias that already holds observations is a project: redirecting it
// would leave those memories reachable under a name the resolver no longer
// returns, which is data loss with extra steps — that case wants a merge.
func (s *Store) UpsertProjectAlias(alias, slug, source string) error {
	alias, _ = NormalizeProject(strings.TrimSpace(alias))
	slug, _ = NormalizeProject(strings.TrimSpace(slug))
	source = strings.TrimSpace(source)
	if alias == "" || slug == "" {
		return fmt.Errorf("engram-projects: an alias needs both a name and a target")
	}
	if alias == slug {
		return fmt.Errorf("engram-projects: %q cannot be an alias of itself", alias)
	}

	var rows int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM observations WHERE `+observationsByProjectPredicate, alias,
	).Scan(&rows); err != nil {
		return fmt.Errorf("engram-projects: count alias observations: %w", err)
	}
	if rows > 0 {
		return &AliasOwnsRowsError{Alias: alias, Slug: slug, Rows: rows}
	}

	// The alias points at a card, so a target that only has observations needs
	// one before the foreign key will accept it.
	known, err := s.ProjectKnown(slug)
	if err != nil {
		return err
	}
	if !known {
		return ErrNoProjectCard
	}
	if _, err := s.ensureMinimalProjectCard(slug); err != nil {
		return err
	}

	now := s.nowUTC()
	return s.withTx(func(tx *sql.Tx) error {
		if _, err := s.execHook(tx, `
			INSERT INTO project_aliases (alias, sync_id, slug, source, created_at, updated_at, deleted_at)
			VALUES (?, ?, ?, ?, ?, ?, NULL)
			ON CONFLICT(alias) DO UPDATE SET
				slug       = excluded.slug,
				source     = excluded.source,
				updated_at = excluded.updated_at,
				deleted_at = NULL`,
			alias, newSyncID("alias"), slug, source, now, now,
		); err != nil {
			return fmt.Errorf("engram-projects: upsert project alias: %w", err)
		}
		return s.enqueueProjectAliasTx(tx, alias)
	})
}

// DeleteProjectAlias retires an alias. The row is kept with a deletion stamp
// rather than removed: a replica that never saw the alias would otherwise have
// no way to learn it is gone.
func (s *Store) DeleteProjectAlias(alias string) error {
	alias, _ = NormalizeProject(strings.TrimSpace(alias))
	if alias == "" {
		return fmt.Errorf("engram-projects: an alias to delete needs a name")
	}
	now := s.nowUTC()
	return s.withTx(func(tx *sql.Tx) error {
		res, err := s.execHook(tx,
			`UPDATE project_aliases SET deleted_at = ?, updated_at = ? WHERE alias = ? AND deleted_at IS NULL`,
			now, now, alias)
		if err != nil {
			return fmt.Errorf("engram-projects: delete project alias: %w", err)
		}
		if affected, _ := res.RowsAffected(); affected == 0 {
			return nil
		}
		return s.enqueueProjectAliasTx(tx, alias)
	})
}

const projectAliasSelectColumns = `alias, sync_id, slug, source, created_at, updated_at`

// ListProjectAliases returns the live aliases of one project, or every live
// alias when slug is empty.
func (s *Store) ListProjectAliases(slug string) ([]ProjectAlias, error) {
	slug, _ = NormalizeProject(strings.TrimSpace(slug))
	query := `SELECT ` + projectAliasSelectColumns + ` FROM project_aliases WHERE deleted_at IS NULL`
	var args []any
	if slug != "" {
		query += ` AND slug = ?`
		args = append(args, slug)
	}
	query += ` ORDER BY alias`

	rows, err := s.readDB().Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("engram-projects: list project aliases: %w", err)
	}
	defer rows.Close()

	var aliases []ProjectAlias
	for rows.Next() {
		var a ProjectAlias
		if err := rows.Scan(&a.Alias, &a.SyncID, &a.Slug, &a.Source, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, fmt.Errorf("engram-projects: scan project alias: %w", err)
		}
		aliases = append(aliases, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("engram-projects: list project aliases: %w", err)
	}
	return aliases, nil
}

// ResolveProjectSlug answers what project a name means, in a fixed order: the
// name itself when it is a real project, then an alias somebody declared, then
// a real project that differs only in which separator was typed.
//
// The order is the whole point. A real project is never redirected, so an alias
// can never shadow live data; and the folded match comes last, so a deliberate
// alias always beats a coincidence of spelling.
func (s *Store) ResolveProjectSlug(raw string) (ProjectResolution, error) {
	input := strings.TrimSpace(raw)
	out := ProjectResolution{Input: input, Via: ProjectResolvedViaUnresolved}
	if input == "" {
		return out, nil
	}
	normalized, _ := NormalizeProject(input)

	hasCard, err := s.ProjectCardExists(normalized)
	if err == nil && hasCard {
		out.Slug, out.Via = normalized, ProjectResolvedViaCard
		return out, nil
	}
	hasRows, err := s.ProjectExists(normalized)
	if err != nil {
		return out, err
	}
	if hasRows {
		out.Slug, out.Via = normalized, ProjectResolvedViaObservations
		return out, nil
	}

	var aliasSlug, aliasSource string
	err = s.readDB().QueryRow(
		`SELECT slug, source FROM project_aliases WHERE alias = ? AND deleted_at IS NULL`, normalized,
	).Scan(&aliasSlug, &aliasSource)
	switch {
	case err == nil:
		out.Slug, out.Via, out.AliasSource = aliasSlug, ProjectResolvedViaAlias, aliasSource
		return out, nil
	case errors.Is(err, sql.ErrNoRows):
		// fall through to folding
	default:
		return out, fmt.Errorf("engram-projects: read project alias: %w", err)
	}

	folded, err := s.foldedProjectMatch(normalized)
	if err != nil {
		return out, err
	}
	if folded != "" {
		out.Slug, out.Via = folded, ProjectResolvedViaFolded
	}
	return out, nil
}

// foldedProjectMatch returns the single known project whose folded spelling
// equals the folded input, or "" when none or more than one does. More than one
// is reported as no match on purpose: two projects that fold together are a
// question for a person, and picking one of them silently would write memories
// into whichever happened to sort first.
func (s *Store) foldedProjectMatch(normalized string) (string, error) {
	target := FoldProjectSeparators(normalized)
	if target == "" {
		return "", nil
	}

	candidates := map[string]bool{}
	collect := func(query string) error {
		rows, err := s.readDB().Query(query)
		if err != nil {
			return fmt.Errorf("engram-projects: read projects for folding: %w", err)
		}
		var names []string
		for rows.Next() {
			var name sql.NullString
			if err := rows.Scan(&name); err != nil {
				rows.Close()
				return fmt.Errorf("engram-projects: scan project for folding: %w", err)
			}
			if name.Valid {
				names = append(names, name.String)
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return fmt.Errorf("engram-projects: read projects for folding: %w", err)
		}
		rows.Close()
		for _, name := range names {
			canonical, _ := NormalizeProject(name)
			if canonical != "" && FoldProjectSeparators(canonical) == target {
				candidates[canonical] = true
			}
		}
		return nil
	}

	if err := collect(`SELECT slug FROM project_cards WHERE deleted_at IS NULL`); err != nil {
		return "", err
	}
	if err := collect(
		`SELECT DISTINCT project FROM observations WHERE project IS NOT NULL AND deleted_at IS NULL`); err != nil {
		return "", err
	}
	if len(candidates) != 1 {
		return "", nil
	}
	names := make([]string, 0, 1)
	for name := range candidates {
		names = append(names, name)
	}
	sort.Strings(names)
	return names[0], nil
}
