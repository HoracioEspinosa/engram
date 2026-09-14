package store

import (
	"errors"
	"fmt"
	"strings"
)

// A workspace holds six kinds of thing and, until now, six ways to look for
// them. SearchWorkspace asks all six at once so "where did I see this" is one
// question rather than a tour of the tabs.
//
// Five arms are FTS5 indexes. The sixth, benchmarks, is a LIKE scan: the table
// holds one row per measurement of one task, it is the smallest thing here by
// two orders of magnitude, and an index over it would cost more to keep than
// the scan costs to run.
const (
	WorkspaceKindObservation = "observation"
	WorkspaceKindTask        = "task"
	WorkspaceKindEvidence    = "evidence"
	WorkspaceKindRunbook     = "runbook"
	WorkspaceKindCard        = "card"
	WorkspaceKindBenchmark   = "benchmark"
)

// workspaceKindOrder fixes the order results are grouped in. It is the order
// the workspace itself reads in: what was written, what is being worked on,
// what proves it, what documents it, what holds it, what measures it.
var workspaceKindOrder = []string{
	WorkspaceKindObservation,
	WorkspaceKindTask,
	WorkspaceKindEvidence,
	WorkspaceKindRunbook,
	WorkspaceKindCard,
	WorkspaceKindBenchmark,
}

// Workspace search bounds. A per-kind cap is what keeps one loud kind from
// filling a result set six kinds are meant to share.
const (
	defaultWorkspacePerKind = 5
	maxWorkspacePerKind     = 25
	minWorkspaceQuery       = 2
)

// ErrWorkspaceQueryTooShort is returned for a query no index can narrow with.
// One character matches most of a workspace, which is not a search result, it
// is a listing with extra steps.
var ErrWorkspaceQueryTooShort = errors.New("workspace search needs at least two characters")

// ErrUnknownWorkspaceKind is returned when a caller narrows the search to a
// kind that does not exist. Silently returning nothing would read as "no
// matches" for a question that was never asked.
var ErrUnknownWorkspaceKind = errors.New("unknown workspace kind")

// SearchWorkspaceParams is the input of SearchWorkspace.
type SearchWorkspaceParams struct {
	// Query is matched as a prefix on its last token, so a search narrows as
	// it is typed instead of only answering on a whole word.
	Query string
	// Project scopes the search to one project. Empty searches everything.
	Project string
	// Subtree widens Project to the project and everything under it.
	Subtree bool
	// Kinds narrows the search to some of the six. Empty asks all of them.
	Kinds []string
	// PerKind caps each kind's hits, 1 to 25, defaulting to 5.
	PerKind int
}

// WorkspaceHit is one result, in the shape every kind is flattened into.
type WorkspaceHit struct {
	Kind      string  `json:"kind"`
	ID        int64   `json:"id"`
	Ref       string  `json:"ref"`
	Project   string  `json:"project"`
	Title     string  `json:"title"`
	Snippet   string  `json:"snippet"`
	UpdatedAt string  `json:"updated_at"`
	Rank      float64 `json:"rank"`
}

// WorkspaceResults carries the hits and, per kind, how many there were before
// the per-kind cap. The totals are what lets a caller say "5 of 40" instead of
// implying there were only five.
type WorkspaceResults struct {
	Hits   []WorkspaceHit `json:"hits"`
	Totals map[string]int `json:"totals"`
}

// workspaceArm is one kind's half of the search. The pieces are kept apart
// because each arm is asked two questions off the same tables — which rows to
// show and how many there are — and FTS5 refuses to evaluate bm25() in a query
// that also carries a window function, so the count cannot ride along with the
// page.
type workspaceArm struct {
	kind string
	// order is the position this kind takes in the grouped result.
	order int
	// projection is the seven value columns, in the order every arm reports
	// them: id, ref, project, title, snippet, updated_at, rank.
	projection string
	// from is the table expression. It is a CROSS JOIN, and deliberately so:
	// SQLite is free to reorder an ordinary join, and given a filter on the
	// source table it picks that table as the outer loop and re-runs the MATCH
	// once per row — measured at 25ms against 169µs for the same query driven
	// from the index. CROSS JOIN fixes the order without changing the result.
	from string
	// where is the search predicate, placeholders included.
	where string
	// orderBy is what "best first" means for this arm.
	orderBy string
	// projectFilter renders the scope predicate for a rendered placeholder list.
	projectFilter func(placeholders string) string
	// searchArgs are the values `where` binds, before the project slugs.
	searchArgs func(q workspaceQuery) []any
}

// workspaceQuery holds the values every arm binds: the FTS prefix query, the
// LIKE pattern the benchmark arm uses instead, the project slugs and the cap.
type workspaceQuery struct {
	match string
	like  string
	slugs []any
	limit int
}

// ftsSearchArgs binds the one value an FTS arm's predicate spells.
func ftsSearchArgs(q workspaceQuery) []any { return []any{q.match} }

// benchmarkSearchArgs binds the LIKE pattern once per column it is compared
// against.
func benchmarkSearchArgs(q workspaceQuery) []any { return []any{q.like, q.like, q.like} }

var workspaceArms = []workspaceArm{
	{
		kind:  WorkspaceKindObservation,
		order: 1,
		projection: `o.id,
			COALESCE(NULLIF(o.topic_key, ''), '#' || o.id),
			o.project, o.title,
			snippet(observations_fts, -1, '', '', '…', 12),
			o.updated_at,
			bm25(observations_fts)`,
		from: `observations_fts
			CROSS JOIN observations o ON o.id = observations_fts.rowid`,
		where:         `observations_fts MATCH ? AND o.deleted_at IS NULL`,
		orderBy:       `bm25(observations_fts)`,
		projectFilter: func(p string) string { return ` AND lower(o.project) IN (` + p + `)` },
		searchArgs:    ftsSearchArgs,
	},
	{
		kind:  WorkspaceKindTask,
		order: 2,
		projection: `t.id,
			COALESCE(t.jira_key, t.sdd_change, t.slug, '#' || t.id),
			t.project, t.title,
			snippet(tasks_fts, -1, '', '', '…', 12),
			t.updated_at,
			bm25(tasks_fts)`,
		from: `tasks_fts
			CROSS JOIN tasks t ON t.id = tasks_fts.rowid`,
		where:         `tasks_fts MATCH ? AND t.deleted_at IS NULL`,
		orderBy:       `bm25(tasks_fts)`,
		projectFilter: func(p string) string { return ` AND t.project IN (` + p + `)` },
		searchArgs:    ftsSearchArgs,
	},
	{
		kind:  WorkspaceKindEvidence,
		order: 3,
		projection: `e.id,
			e.path,
			e.project, e.proves,
			snippet(evidence_fts, -1, '', '', '…', 12),
			e.captured_at,
			bm25(evidence_fts)`,
		from: `evidence_fts
			CROSS JOIN evidence e ON e.id = evidence_fts.rowid`,
		where:         `evidence_fts MATCH ? AND e.deleted_at IS NULL`,
		orderBy:       `bm25(evidence_fts)`,
		projectFilter: func(p string) string { return ` AND e.project IN (` + p + `)` },
		searchArgs:    ftsSearchArgs,
	},
	{
		kind:  WorkspaceKindRunbook,
		order: 4,
		projection: `ri.seq,
			ri.id,
			ri.project, ri.title,
			snippet(runbook_index_fts, -1, '', '', '…', 12),
			ri.synced_at,
			bm25(runbook_index_fts)`,
		from: `runbook_index_fts
			CROSS JOIN runbook_index ri ON ri.seq = runbook_index_fts.rowid`,
		where:         `runbook_index_fts MATCH ?`,
		orderBy:       `bm25(runbook_index_fts)`,
		projectFilter: func(p string) string { return ` AND ri.project IN (` + p + `)` },
		searchArgs:    ftsSearchArgs,
	},
	{
		kind:  WorkspaceKindCard,
		order: 5,
		projection: `card.rowid,
			card.slug,
			card.slug, card.display_name,
			snippet(project_cards_fts, -1, '', '', '…', 12),
			card.updated_at,
			bm25(project_cards_fts)`,
		from: `project_cards_fts
			CROSS JOIN project_cards card ON card.rowid = project_cards_fts.rowid`,
		where:         `project_cards_fts MATCH ? AND card.deleted_at IS NULL`,
		orderBy:       `bm25(project_cards_fts)`,
		projectFilter: func(p string) string { return ` AND card.slug IN (` + p + `)` },
		searchArgs:    ftsSearchArgs,
	},
	{
		kind:  WorkspaceKindBenchmark,
		order: 6,
		projection: `b.id,
			b.name || '/' || b.metric,
			b.project, b.name,
			substr(COALESCE(NULLIF(b.notes, ''), b.metric || ' = ' || b.value || ' ' || b.unit), 1, 120),
			b.captured_at,
			0.0`,
		from: `benchmarks b`,
		where: `b.deleted_at IS NULL
			AND (b.name LIKE ? ESCAPE '\'
			  OR b.metric LIKE ? ESCAPE '\'
			  OR COALESCE(b.notes, '') LIKE ? ESCAPE '\')`,
		orderBy:       `b.captured_at DESC`,
		projectFilter: func(p string) string { return ` AND b.project IN (` + p + `)` },
		searchArgs:    benchmarkSearchArgs,
	},
}

// SearchWorkspace searches every kind of thing a workspace holds: the arms are
// compound-selected together, each with its own cap, so the database is asked
// once for the page instead of once per tab.
//
// The totals take a second statement rather than a window function beside the
// hits: FTS5 refuses to evaluate bm25() in a query that also carries one, and
// ranking the page is worth more than saving a round trip on a count that needs
// neither ranking nor snippets.
func (s *Store) SearchWorkspace(p SearchWorkspaceParams) (WorkspaceResults, error) {
	if len([]rune(strings.TrimSpace(p.Query))) < minWorkspaceQuery {
		return WorkspaceResults{}, ErrWorkspaceQueryTooShort
	}

	arms, err := selectWorkspaceArms(p.Kinds)
	if err != nil {
		return WorkspaceResults{}, err
	}

	slugs, err := s.workspaceScope(p)
	if err != nil {
		return WorkspaceResults{}, err
	}

	query := workspaceQuery{
		match: prefixFTSQuery(p.Query),
		like:  "%" + escapeLikePattern(strings.TrimSpace(p.Query)) + "%",
		limit: clampWorkspacePerKind(p.PerKind),
	}
	for _, slug := range slugs {
		query.slugs = append(query.slugs, slug)
	}

	results := WorkspaceResults{Totals: make(map[string]int, len(arms))}
	for _, arm := range arms {
		results.Totals[arm.kind] = 0
	}
	// Every token was noise once the quotes came off, so there is nothing an
	// index could match. That is an empty result, not an error.
	if query.match == "" {
		return results, nil
	}

	if err := s.readWorkspaceHits(arms, query, &results); err != nil {
		return WorkspaceResults{}, err
	}
	if err := s.readWorkspaceTotals(arms, query, &results); err != nil {
		return WorkspaceResults{}, err
	}
	return results, nil
}

// readWorkspaceHits runs the compound page query and appends its rows, already
// grouped by kind and ranked inside each group.
func (s *Store) readWorkspaceHits(arms []workspaceArm, q workspaceQuery, results *WorkspaceResults) error {
	sql, args := renderWorkspaceHits(arms, q)
	rows, err := s.queryHook(s.readDB(), sql, args...)
	if err != nil {
		return fmt.Errorf("engram-projects: search workspace: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var hit WorkspaceHit
		var order int
		if err := rows.Scan(&order, &hit.Kind, &hit.ID, &hit.Ref, &hit.Project, &hit.Title,
			&hit.Snippet, &hit.UpdatedAt, &hit.Rank); err != nil {
			return fmt.Errorf("engram-projects: scan workspace hit: %w", err)
		}
		results.Hits = append(results.Hits, hit)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("engram-projects: search workspace: %w", err)
	}
	return nil
}

// readWorkspaceTotals runs the compound count query, so a caller can say "5 of
// 40" rather than implying there were only five.
func (s *Store) readWorkspaceTotals(arms []workspaceArm, q workspaceQuery, results *WorkspaceResults) error {
	sql, args := renderWorkspaceTotals(arms, q)
	rows, err := s.queryHook(s.readDB(), sql, args...)
	if err != nil {
		return fmt.Errorf("engram-projects: count workspace matches: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var kind string
		var total int
		if err := rows.Scan(&kind, &total); err != nil {
			return fmt.Errorf("engram-projects: scan workspace total: %w", err)
		}
		results.Totals[kind] = total
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("engram-projects: count workspace matches: %w", err)
	}
	return nil
}

// renderWorkspaceHits joins the arms into one compound SELECT. Each arm is
// wrapped in a subquery because SQLite only accepts LIMIT on a compound term
// that way, and the cap is per kind by design.
func renderWorkspaceHits(arms []workspaceArm, q workspaceQuery) (string, []any) {
	placeholders := slugPlaceholders(len(q.slugs))
	bodies := make([]string, 0, len(arms))
	args := make([]any, 0, len(arms)*(len(q.slugs)+4))
	for _, arm := range arms {
		bodies = append(bodies, fmt.Sprintf(
			"SELECT * FROM (SELECT %d, '%s', %s\nFROM %s\nWHERE %s%s\nORDER BY %s LIMIT ?)",
			arm.order, arm.kind, arm.projection, arm.from, arm.where,
			arm.scopeFilter(placeholders), arm.orderBy))
		args = append(args, arm.searchArgs(q)...)
		args = append(args, q.slugs...)
		args = append(args, q.limit)
	}
	// Grouped by kind, best first inside each group, newest first on a tie.
	// The tiebreak matters for benchmarks, whose arm carries no rank at all.
	return strings.Join(bodies, "\nUNION ALL\n") + "\nORDER BY 1, 9, 8 DESC", args
}

// renderWorkspaceTotals counts each arm's matches with the same predicate the
// page used, minus the ranking and the cap.
func renderWorkspaceTotals(arms []workspaceArm, q workspaceQuery) (string, []any) {
	placeholders := slugPlaceholders(len(q.slugs))
	bodies := make([]string, 0, len(arms))
	args := make([]any, 0, len(arms)*(len(q.slugs)+3))
	for _, arm := range arms {
		bodies = append(bodies, fmt.Sprintf(
			"SELECT '%s', COUNT(*)\nFROM %s\nWHERE %s%s",
			arm.kind, arm.from, arm.where, arm.scopeFilter(placeholders)))
		args = append(args, arm.searchArgs(q)...)
		args = append(args, q.slugs...)
	}
	return strings.Join(bodies, "\nUNION ALL\n"), args
}

// scopeFilter renders the arm's project predicate, or nothing when the search
// covers the whole workspace.
func (a workspaceArm) scopeFilter(placeholders string) string {
	if placeholders == "" {
		return ""
	}
	return a.projectFilter(placeholders)
}

func slugPlaceholders(n int) string {
	if n == 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// selectWorkspaceArms returns the arms for the requested kinds, in the fixed
// grouping order, or every arm when none was named.
func selectWorkspaceArms(kinds []string) ([]workspaceArm, error) {
	if len(kinds) == 0 {
		return workspaceArms, nil
	}
	wanted := make(map[string]bool, len(kinds))
	for _, kind := range kinds {
		normalized := strings.ToLower(strings.TrimSpace(kind))
		if !isWorkspaceKind(normalized) {
			return nil, fmt.Errorf("%w: %q", ErrUnknownWorkspaceKind, kind)
		}
		wanted[normalized] = true
	}
	arms := make([]workspaceArm, 0, len(wanted))
	for _, arm := range workspaceArms {
		if wanted[arm.kind] {
			arms = append(arms, arm)
		}
	}
	return arms, nil
}

func isWorkspaceKind(kind string) bool {
	for _, known := range workspaceKindOrder {
		if known == kind {
			return true
		}
	}
	return false
}

// workspaceScope turns the project parameters into the list of slugs every arm
// filters by. An empty list means the whole workspace.
func (s *Store) workspaceScope(p SearchWorkspaceParams) ([]string, error) {
	project, _ := NormalizeProject(p.Project)
	if project == "" {
		return nil, nil
	}
	if !p.Subtree {
		return []string{project}, nil
	}
	slugs, err := s.SubtreeSlugs(project)
	if err != nil {
		return nil, err
	}
	return slugs, nil
}

func clampWorkspacePerKind(perKind int) int {
	switch {
	case perKind <= 0:
		return defaultWorkspacePerKind
	case perKind > maxWorkspacePerKind:
		return maxWorkspacePerKind
	default:
		return perKind
	}
}

// prefixFTSQuery quotes every token the way sanitizeFTS does and then makes the
// last one a prefix, so "cook" reaches "cookie-flags" while the tokens before
// it stay exact — a search narrows as it is typed rather than only answering on
// a whole word.
//
// A token that is nothing but quotes is dropped rather than quoted into an
// empty phrase, which FTS5 rejects as a syntax error.
func prefixFTSQuery(query string) string {
	fields := strings.Fields(query)
	tokens := make([]string, 0, len(fields))
	for _, field := range fields {
		token := strings.Trim(field, `"`)
		if token == "" {
			continue
		}
		tokens = append(tokens, `"`+strings.ReplaceAll(token, `"`, `""`)+`"`)
	}
	if len(tokens) == 0 {
		return ""
	}
	tokens[len(tokens)-1] += "*"
	return strings.Join(tokens, " ")
}

// escapeLikePattern neutralises the two characters LIKE reads as wildcards, so
// a query containing one searches for it instead of matching on it. The
// benchmark arm declares ESCAPE '\' for this.
func escapeLikePattern(query string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(query)
}
