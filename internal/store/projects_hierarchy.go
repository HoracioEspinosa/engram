// Package store: the engram-projects hierarchy.
//
// A project card points at its parent and carries the depth that parent
// implies. The pair is redundant on purpose: depth is what lets a reader render
// an indented tree, and a query order it, without walking the chain for every
// row. Redundancy has to be maintained, so this file owns the only write path
// that touches either column and rewrites the whole subtree whenever one moves.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// maxProjectDepth is the deepest a card may sit below a root. Three levels hold
// the shapes the tree exists for — an umbrella over a repository over its
// instances — and stop the structure from becoming a filesystem nobody can see
// the bottom of.
const maxProjectDepth = 3

var (
	// ErrProjectCycle is returned when a parent would make a card its own
	// ancestor.
	ErrProjectCycle = errors.New("project parent would close a cycle")
	// ErrProjectDepthExceeded is returned when a parent would push a card, or
	// anything below it, past maxProjectDepth.
	ErrProjectDepthExceeded = errors.New("project parent exceeds the maximum depth")
)

// ProjectTreeNode is one card in a preorder walk of the tree: the card itself,
// how far below its root it sits, how many children hang from it, and the
// counters when the caller asked for them.
type ProjectTreeNode struct {
	ProjectCard
	Children int                `json:"children"`
	Counts   *ProjectCardCounts `json:"counts,omitempty"`
}

// ProjectTreeSuggestion is a proposed parent for a group of cards that look
// like instances of one thing. It is never applied: renaming or reparenting
// someone's projects on a guess is not a decision this package gets to make.
type ProjectTreeSuggestion struct {
	Parent       string   `json:"parent"`
	Children     []string `json:"children"`
	Reason       string   `json:"reason"`
	ParentExists bool     `json:"parent_exists"`
}

// ResolvedProjectCard is a card read through its ancestors: every pointer the
// card left at its default is filled from the nearest ancestor that set one,
// and Inherited names, per column, which ancestor it came from.
type ResolvedProjectCard struct {
	ProjectCard
	Inherited map[string]string `json:"inherited,omitempty"`
}

// ResolveProjectCard returns the card for slug with the pointers an ancestor
// already answered. Inheriting on read rather than copying on write is what
// keeps one edit of an umbrella true for everything under it; a copy would
// leave every instance holding a stale answer nobody remembers to refresh.
//
// The code-graph group is deliberately excluded. graph_commit, graph_built_at
// and graph_summary describe one checkout on one machine, so borrowing the
// umbrella's graph would tell an instance it has a graph it never built.
func (s *Store) ResolveProjectCard(slug string) (ResolvedProjectCard, error) {
	card, err := s.GetProjectCard(slug)
	if err != nil {
		return ResolvedProjectCard{}, err
	}
	resolved := ResolvedProjectCard{ProjectCard: card, Inherited: map[string]string{}}

	defaultJira := DefaultJiraProject()
	// A field is open while the card still holds the value the schema would
	// have given it on its own; take returns false once it has been answered.
	open := map[string]func(ProjectCard) bool{
		"repo_url":           func(c ProjectCard) bool { return trimPtr(c.RepoURL) == nil },
		"default_branch":     func(c ProjectCard) bool { return c.DefaultBranch == "" || c.DefaultBranch == "master" },
		"jira_project":       func(c ProjectCard) bool { return c.JiraProject == "" || c.JiraProject == defaultJira || c.JiraProject == "PROJ" },
		"jira_component":     func(c ProjectCard) bool { return trimPtr(c.JiraComponent) == nil },
		"knowledge_hub_path": func(c ProjectCard) bool { return trimPtr(c.KnowledgeHubPath) == nil },
		"graph_path":         func(c ProjectCard) bool { return c.GraphPath == "" || c.GraphPath == "graphify-out/graph.json" },
		"owner":              func(c ProjectCard) bool { return trimPtr(c.Owner) == nil },
	}
	adopt := map[string]func(*ResolvedProjectCard, ProjectCard){
		"repo_url":           func(r *ResolvedProjectCard, a ProjectCard) { r.RepoURL = a.RepoURL },
		"default_branch":     func(r *ResolvedProjectCard, a ProjectCard) { r.DefaultBranch = a.DefaultBranch },
		"jira_project":       func(r *ResolvedProjectCard, a ProjectCard) { r.JiraProject = a.JiraProject },
		"jira_component":     func(r *ResolvedProjectCard, a ProjectCard) { r.JiraComponent = a.JiraComponent },
		"knowledge_hub_path": func(r *ResolvedProjectCard, a ProjectCard) { r.KnowledgeHubPath = a.KnowledgeHubPath },
		"graph_path":         func(r *ResolvedProjectCard, a ProjectCard) { r.GraphPath = a.GraphPath },
		"owner":              func(r *ResolvedProjectCard, a ProjectCard) { r.Owner = a.Owner },
	}

	ancestor := card
	for hop := 0; hop < maxProjectDepth; hop++ {
		parentSlug := trimPtr(ancestor.ParentSlug)
		if parentSlug == nil {
			break
		}
		parent, err := s.GetProjectCard(*parentSlug)
		if errors.Is(err, ErrNoProjectCard) {
			break
		}
		if err != nil {
			return ResolvedProjectCard{}, err
		}
		for field, isOpen := range open {
			if !isOpen(resolved.ProjectCard) || isOpen(parent) {
				continue
			}
			adopt[field](&resolved, parent)
			resolved.Inherited[field] = parent.Slug
		}
		ancestor = parent
	}

	if len(resolved.Inherited) == 0 {
		resolved.Inherited = nil
	}
	return resolved, nil
}

// SetProjectParent moves a card under parent, or to the top of the tree when
// parent is nil, and rewrites the depth of everything below it.
//
// The cycle check lives here rather than in a trigger because SQLite has no
// WITH RECURSIVE inside a trigger: a trigger sees one level and would happily
// accept a card whose grandparent is itself. The trigger that does exist
// (project_cards_depth_ck) is the safety net for a write that bypasses this
// function, not the rule.
func (s *Store) SetProjectParent(slug string, parent *string) error {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return ErrNoProjectCard
	}
	if _, err := s.GetProjectCard(slug); err != nil {
		return err
	}

	newParent := ""
	if parent != nil {
		newParent = strings.TrimSpace(*parent)
	}

	return s.withTx(func(tx *sql.Tx) error {
		depth := 0
		if newParent != "" {
			if newParent == slug {
				return fmt.Errorf("%w: %q cannot be its own parent", ErrProjectCycle, slug)
			}
			parentDepth, err := s.projectAncestorDepthTx(tx, slug, newParent)
			if err != nil {
				return err
			}
			depth = parentDepth + 1
		}

		// The subtree travels with the card, so the card's own new depth is
		// not the only one that has to fit.
		height, err := s.subtreeHeightTx(tx, slug)
		if err != nil {
			return err
		}
		if depth+height > maxProjectDepth {
			return fmt.Errorf("%w: %q would sit at depth %d with %d level(s) below it, and the limit is %d",
				ErrProjectDepthExceeded, slug, depth, height, maxProjectDepth)
		}

		if _, err := s.execHook(tx,
			`UPDATE project_cards SET parent_slug = ?, depth = ?, updated_at = ? WHERE slug = ?`,
			nullableStr(trimToNil(newParent)), depth, s.nowUTC(), slug,
		); err != nil {
			return fmt.Errorf("engram-projects: set project parent: %w", err)
		}
		if err := s.rewriteSubtreeDepthTx(tx, slug, depth); err != nil {
			return err
		}
		return s.enqueueProjectCardTx(tx, slug)
	})
}

// projectAncestorDepthTx returns the depth of parent after proving that slug is
// not one of its ancestors.
func (s *Store) projectAncestorDepthTx(tx *sql.Tx, slug, parent string) (int, error) {
	var depth int
	var above *string
	err := tx.QueryRow(
		`SELECT depth, parent_slug FROM project_cards WHERE slug = ? AND deleted_at IS NULL`, parent,
	).Scan(&depth, &above)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNoProjectCard
	}
	if err != nil {
		return 0, fmt.Errorf("engram-projects: read project parent: %w", err)
	}
	if err := s.ancestorsExclude(tx, above, slug); err != nil {
		return 0, err
	}
	return depth, nil
}

// ancestorsExclude reports a cycle when slug appears anywhere above start.
func (s *Store) ancestorsExclude(tx *sql.Tx, start *string, slug string) error {
	current := start
	for hop := 0; hop <= maxProjectDepth+1; hop++ {
		if current == nil {
			return nil
		}
		if *current == slug {
			return fmt.Errorf("%w: %q is already an ancestor of the parent", ErrProjectCycle, slug)
		}
		var next *string
		err := tx.QueryRow(`SELECT parent_slug FROM project_cards WHERE slug = ?`, *current).Scan(&next)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("engram-projects: walk project ancestors: %w", err)
		}
		current = next
	}
	return fmt.Errorf("%w: the ancestors of %q do not terminate", ErrProjectCycle, slug)
}

// subtreeHeightTx returns how many levels hang below slug (0 for a leaf).
func (s *Store) subtreeHeightTx(tx *sql.Tx, slug string) (int, error) {
	var height int
	err := tx.QueryRow(`
		WITH RECURSIVE below(slug, level) AS (
			SELECT slug, 0 FROM project_cards WHERE slug = ? AND deleted_at IS NULL
			UNION ALL
			SELECT child.slug, below.level + 1
			FROM project_cards child
			JOIN below ON child.parent_slug = below.slug
			WHERE child.deleted_at IS NULL AND below.level < ?
		)
		SELECT COALESCE(MAX(level), 0) FROM below`, slug, maxProjectDepth+1,
	).Scan(&height)
	if err != nil {
		return 0, fmt.Errorf("engram-projects: measure project subtree: %w", err)
	}
	return height, nil
}

// rewriteSubtreeDepthTx walks the children of slug top-down and stamps each one
// with its parent's depth plus one. Top-down is not a preference: the depth
// trigger compares a row against its parent, so a child updated before its
// parent would be compared against a depth that is about to change.
func (s *Store) rewriteSubtreeDepthTx(tx *sql.Tx, slug string, depth int) error {
	type pending struct {
		slug  string
		depth int
	}
	queue := []pending{{slug: slug, depth: depth}}
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		if node.depth > maxProjectDepth {
			return fmt.Errorf("%w: %q would sit at depth %d", ErrProjectDepthExceeded, node.slug, node.depth)
		}

		rows, err := tx.Query(
			`SELECT slug FROM project_cards WHERE parent_slug = ? AND deleted_at IS NULL ORDER BY slug`, node.slug)
		if err != nil {
			return fmt.Errorf("engram-projects: read project children: %w", err)
		}
		var children []string
		for rows.Next() {
			var child string
			if err := rows.Scan(&child); err != nil {
				rows.Close()
				return fmt.Errorf("engram-projects: scan project child: %w", err)
			}
			children = append(children, child)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return fmt.Errorf("engram-projects: read project children: %w", err)
		}
		rows.Close()

		for _, child := range children {
			if _, err := s.execHook(tx,
				`UPDATE project_cards SET depth = ? WHERE slug = ?`, node.depth+1, child,
			); err != nil {
				return fmt.Errorf("engram-projects: rewrite project depth: %w", err)
			}
			queue = append(queue, pending{slug: child, depth: node.depth + 1})
		}
	}
	return nil
}

// projectTreeQuery walks the tree in preorder. The path column is what orders
// it: a parent's path is a strict prefix of every path below it, so sorting on
// the path puts each card immediately before its own descendants.
const projectTreeQuery = `
WITH RECURSIVE tree(slug, path) AS (
	SELECT slug, slug FROM project_cards
	WHERE deleted_at IS NULL AND %s
	UNION ALL
	SELECT child.slug, tree.path || '/' || child.slug
	FROM project_cards child
	JOIN tree ON child.parent_slug = tree.slug
	WHERE child.deleted_at IS NULL
)
SELECT %s,
       (SELECT COUNT(*) FROM project_cards kid
        WHERE kid.parent_slug = card.slug AND kid.deleted_at IS NULL)
FROM tree
JOIN project_cards card ON card.slug = tree.slug
ORDER BY tree.path`

// prefixedProjectCardColumns qualifies the card projection with a table alias,
// so a join that also carries a `slug` column stays unambiguous without the
// column list being written a second time.
func prefixedProjectCardColumns(alias string) string {
	parts := strings.Split(projectCardSelectColumns, ",")
	for i, part := range parts {
		parts[i] = alias + "." + strings.TrimSpace(part)
	}
	return strings.Join(parts, ", ")
}

// ProjectTree returns the cards under root in preorder, or the whole forest
// when root is empty. Counters are computed only when includeCounts is set;
// they cost one round of aggregates per card, the same price ListProjectCards
// already pays for the selector.
func (s *Store) ProjectTree(root string, includeCounts bool) ([]ProjectTreeNode, error) {
	root = strings.TrimSpace(root)
	anchor := "parent_slug IS NULL"
	var args []any
	if root != "" {
		if _, err := s.GetProjectCard(root); err != nil {
			return nil, err
		}
		anchor = "slug = ?"
		args = append(args, root)
	}

	query := fmt.Sprintf(projectTreeQuery, anchor, prefixedProjectCardColumns("card"))
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("engram-projects: walk project tree: %w", err)
	}
	var nodes []ProjectTreeNode
	for rows.Next() {
		var node ProjectTreeNode
		if err := rows.Scan(&node.Slug, &node.DisplayName, &node.RepoURL, &node.DefaultBranch,
			&node.JiraProject, &node.JiraComponent, &node.KnowledgeHubPath, &node.GraphPath,
			&node.GraphCommit, &node.GraphBuiltAt, &node.GraphSummary, &node.Owner, &node.CreatedAt,
			&node.UpdatedAt, &node.ParentSlug, &node.Depth, &node.Kind, &node.Description, &node.Icon,
			&node.Color, &node.Tags, &node.GraphStaleReason, &node.GraphChangedFiles,
			&node.GraphCheckedAt, &node.Children); err != nil {
			rows.Close()
			return nil, fmt.Errorf("engram-projects: scan project tree node: %w", err)
		}
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("engram-projects: walk project tree: %w", err)
	}
	// The store pool holds a single connection, so the cursor has to be closed
	// before the counter queries below can get one.
	rows.Close()

	if includeCounts {
		for i := range nodes {
			counts, err := s.ProjectCardCounts(nodes[i].Slug)
			if err != nil {
				return nil, err
			}
			nodes[i].Counts = &counts
		}
	}
	return nodes, nil
}

// SubtreeSlugs returns root and every slug below it, in preorder. It is what a
// query scoped to "this project and its children" filters by.
func (s *Store) SubtreeSlugs(root string) ([]string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, ErrNoProjectCard
	}
	rows, err := s.db.Query(`
		WITH RECURSIVE tree(slug, path) AS (
			SELECT slug, slug FROM project_cards WHERE deleted_at IS NULL AND slug = ?
			UNION ALL
			SELECT child.slug, tree.path || '/' || child.slug
			FROM project_cards child
			JOIN tree ON child.parent_slug = tree.slug
			WHERE child.deleted_at IS NULL
		)
		SELECT slug FROM tree ORDER BY path`, root)
	if err != nil {
		return nil, fmt.Errorf("engram-projects: read project subtree: %w", err)
	}
	defer rows.Close()

	var slugs []string
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			return nil, fmt.Errorf("engram-projects: scan project subtree: %w", err)
		}
		slugs = append(slugs, slug)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("engram-projects: read project subtree: %w", err)
	}
	if len(slugs) == 0 {
		return nil, ErrNoProjectCard
	}
	return slugs, nil
}

// projectSeparators are the characters people use where a slug uses a hyphen.
// A project typed as "ai_engram", "ai engram" or "ai.engram" names the same
// thing as "ai-engram", and only the reader folds them together — nothing is
// ever stored under the folded spelling.
var projectSeparators = strings.NewReplacer("_", "-", " ", "-", ".", "-")

// foldProjectSeparators returns the comparison form of a project name.
func foldProjectSeparators(name string) string {
	folded := strings.TrimSpace(strings.ToLower(name))
	folded = projectSeparators.Replace(folded)
	for strings.Contains(folded, "--") {
		folded = strings.ReplaceAll(folded, "--", "-")
	}
	return strings.Trim(folded, "-")
}

// instanceSuffix matches the tail segment that numbers one instance of a
// product: "00", "02", "i0002", "v2". A slug ending in one is read as a member
// of the family its prefix names.
var instanceSuffix = regexp.MustCompile(`^[a-z]?[0-9]+$`)

// projectFamilyStem returns the family a slug belongs to, or "" when the slug
// names no family. It is the folded slug without its instance suffix.
func projectFamilyStem(slug string) string {
	folded := foldProjectSeparators(slug)
	idx := strings.LastIndex(folded, "-")
	if idx <= 0 {
		return ""
	}
	if !instanceSuffix.MatchString(folded[idx+1:]) {
		return ""
	}
	return folded[:idx]
}

// SuggestProjectTree proposes a parent for every group of cards that reads as
// numbered instances of one product, folding the separator each slug happens to
// use so "nextcloud_00" and "nextcloud-02" land in the same family. It writes
// nothing: the proposal is for a person to accept, and a slug that looks like
// an instance is not proof that it is one.
func (s *Store) SuggestProjectTree() ([]ProjectTreeSuggestion, error) {
	rows, err := s.db.Query(
		`SELECT slug, parent_slug, repo_url FROM project_cards WHERE deleted_at IS NULL ORDER BY slug`)
	if err != nil {
		return nil, fmt.Errorf("engram-projects: read project cards for suggestions: %w", err)
	}
	type card struct {
		slug    string
		parent  *string
		repoURL *string
	}
	var cards []card
	for rows.Next() {
		var c card
		if err := rows.Scan(&c.slug, &c.parent, &c.repoURL); err != nil {
			rows.Close()
			return nil, fmt.Errorf("engram-projects: scan project card for suggestions: %w", err)
		}
		cards = append(cards, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("engram-projects: read project cards for suggestions: %w", err)
	}
	rows.Close()

	existing := make(map[string]bool, len(cards))
	for _, c := range cards {
		existing[c.slug] = true
	}

	families := map[string][]card{}
	for _, c := range cards {
		if c.parent != nil {
			// Already placed: a suggestion would second-guess a decision
			// somebody already made.
			continue
		}
		stem := projectFamilyStem(c.slug)
		if stem == "" {
			continue
		}
		families[stem] = append(families[stem], c)
	}

	stems := make([]string, 0, len(families))
	for stem := range families {
		stems = append(stems, stem)
	}
	sort.Strings(stems)

	suggestions := make([]ProjectTreeSuggestion, 0, len(stems))
	for _, stem := range stems {
		members := families[stem]
		if len(members) < 2 {
			continue
		}
		children := make([]string, 0, len(members))
		sharedRepo := ""
		sameRepo := true
		for i, m := range members {
			children = append(children, m.slug)
			repo := strings.TrimSpace(ptrOrEmpty(m.repoURL))
			if i == 0 {
				sharedRepo = repo
				continue
			}
			if repo == "" || repo != sharedRepo {
				sameRepo = false
			}
		}
		sort.Strings(children)
		reason := "shared_prefix"
		if sameRepo && sharedRepo != "" {
			reason = "shared_prefix_and_repo_url"
		}
		suggestions = append(suggestions, ProjectTreeSuggestion{
			Parent:       stem,
			Children:     children,
			Reason:       reason,
			ParentExists: existing[stem],
		})
	}
	return suggestions, nil
}
