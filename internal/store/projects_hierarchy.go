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
		"repo_url":       func(c ProjectCard) bool { return trimPtr(c.RepoURL) == nil },
		"default_branch": func(c ProjectCard) bool { return c.DefaultBranch == "" || c.DefaultBranch == "master" },
		"jira_project": func(c ProjectCard) bool {
			return c.JiraProject == "" || c.JiraProject == defaultJira || c.JiraProject == "PROJ"
		},
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
	return prefixColumns(projectCardSelectColumns, alias)
}

// ProjectTree returns the cards under root in preorder, or the whole forest
// when root is empty. Counters are computed only when includeCounts is set;
// they cost four grouped aggregates for the whole tree, the same price
// ListProjectCards pays for the selector.
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
	rows, err := s.queryHook(s.readDB(), query, args...)
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
	// The cursor is closed before the counters are read: they are four more
	// queries, and holding a read connection across them buys nothing.
	rows.Close()

	if includeCounts {
		slugs := make([]string, 0, len(nodes))
		for i := range nodes {
			slugs = append(slugs, nodes[i].Slug)
		}
		counts, err := s.ProjectCardCountsBatch(slugs)
		if err != nil {
			return nil, err
		}
		for i := range nodes {
			nodeCounts := counts[nodes[i].Slug]
			nodes[i].Counts = &nodeCounts
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
	rows, err := s.readDB().Query(`
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

// instanceSuffix matches the tail segment that numbers one instance of a
// product: "00", "02", "i0002", "v2". A slug ending in one is read as a member
// of the family its prefix names.
var instanceSuffix = regexp.MustCompile(`^[a-z]?[0-9]+$`)

// projectFamilyStem returns the family a slug belongs to, or "" when the slug
// names no family. It is the folded slug without its instance suffix.
func projectFamilyStem(slug string) string {
	folded := FoldProjectSeparators(slug)
	idx := strings.LastIndex(folded, "-")
	if idx <= 0 {
		return ""
	}
	if !instanceSuffix.MatchString(folded[idx+1:]) {
		return ""
	}
	return folded[:idx]
}

// Why a group was proposed.
const (
	// SuggestReasonExistingPrefix is a family whose parent is a project the
	// store already answers to.
	SuggestReasonExistingPrefix = "existing_prefix_project"
	// SuggestReasonSharedPrefix is a family of numbered instances whose common
	// stem names no project yet.
	SuggestReasonSharedPrefix = "shared_prefix"
	// SuggestReasonSharedPrefixAndRepo is the same, with every member pointing
	// at one git remote.
	SuggestReasonSharedPrefixAndRepo = "shared_prefix_and_repo_url"
	// SuggestReasonSharedFirstSegment is a family that shares the first segment
	// of its name and nothing else — no numbering, and no project answering to
	// the segment yet.
	SuggestReasonSharedFirstSegment = "shared_first_segment"
)

// The number of members a family needs before it is worth proposing.
const (
	// minFamilyMembers is enough when the parent is already a project, or when
	// the members are numbered instances of it: both are strong signals on
	// their own.
	minFamilyMembers = 2
	// minSegmentFamilyMembers is what an invented parent costs. Two names that
	// merely start with the same word are a coincidence as often as a family —
	// "web-angular-skeleton" and "web-metronic-angular" share nothing but the
	// word — so a synthesized segment has to hold a third member before it is
	// offered.
	minSegmentFamilyMembers = 3
)

// knownProject is one name this store answers to, however it came to: a project
// card, or a name observations were saved under and nothing else.
type knownProject struct {
	slug    string
	folded  string
	parent  *string
	repoURL *string
	hasCard bool
}

// knownProjects returns every project the store answers to, in slug order:
// the live cards, plus the names only observations mention.
//
// Reading cards alone would see almost nothing on a real database. A card is
// something a person makes on purpose, and most projects are just the name a
// session saved its memory under — so a grouping that ignores them groups the
// handful of projects that least need it.
func (s *Store) knownProjects() ([]knownProject, error) {
	byFolded := map[string]*knownProject{}
	order := []string{}

	rows, err := s.readDB().Query(
		`SELECT slug, parent_slug, repo_url FROM project_cards WHERE deleted_at IS NULL ORDER BY slug`)
	if err != nil {
		return nil, fmt.Errorf("engram-projects: read project cards for suggestions: %w", err)
	}
	for rows.Next() {
		var k knownProject
		if err := rows.Scan(&k.slug, &k.parent, &k.repoURL); err != nil {
			rows.Close()
			return nil, fmt.Errorf("engram-projects: scan project card for suggestions: %w", err)
		}
		k.hasCard = true
		k.folded = FoldProjectSeparators(k.slug)
		if _, seen := byFolded[k.slug]; !seen {
			order = append(order, k.slug)
		}
		byFolded[k.slug] = &k
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("engram-projects: read project cards for suggestions: %w", err)
	}
	rows.Close()

	obsRows, err := s.readDB().Query(
		`SELECT DISTINCT lower(project) FROM observations
		 WHERE project IS NOT NULL AND trim(project) <> '' AND deleted_at IS NULL
		 ORDER BY 1`)
	if err != nil {
		return nil, fmt.Errorf("engram-projects: read observation projects for suggestions: %w", err)
	}
	for obsRows.Next() {
		var slug string
		if err := obsRows.Scan(&slug); err != nil {
			obsRows.Close()
			return nil, fmt.Errorf("engram-projects: scan observation project for suggestions: %w", err)
		}
		if _, seen := byFolded[slug]; seen {
			continue
		}
		byFolded[slug] = &knownProject{slug: slug, folded: FoldProjectSeparators(slug)}
		order = append(order, slug)
	}
	if err := obsRows.Err(); err != nil {
		obsRows.Close()
		return nil, fmt.Errorf("engram-projects: read observation projects for suggestions: %w", err)
	}
	obsRows.Close()

	sort.Strings(order)
	out := make([]knownProject, 0, len(order))
	for _, slug := range order {
		out = append(out, *byFolded[slug])
	}
	return out, nil
}

// longestKnownPrefix returns the longest project in known that slug reads as a
// child of — "koi-garden" for "koi-garden-pond-01" — or "" when none does.
// Longest wins so a "koi-garden-pond" that does exist takes its own instances
// back from "koi-garden".
func longestKnownPrefix(known []knownProject, slug, folded string) string {
	best := ""
	bestLen := 0
	for _, candidate := range known {
		if candidate.slug == slug || candidate.folded == folded {
			continue
		}
		if !strings.HasPrefix(folded, candidate.folded+"-") {
			continue
		}
		if len(candidate.folded) > bestLen {
			best, bestLen = candidate.slug, len(candidate.folded)
		}
	}
	return best
}

// firstNameSegment returns the leading segment of a folded slug — "clarodrive"
// for "clarodrive-sysops-utils" — or "" when the name has no second segment to
// be the prefix of.
func firstNameSegment(folded string) string {
	idx := strings.Index(folded, "-")
	if idx <= 0 || idx == len(folded)-1 {
		return ""
	}
	return folded[:idx]
}

// SuggestProjectTree proposes a parent for every group of projects that reads
// as members of one product, folding the separator each name happens to use so
// "nextcloud_00" and "nextcloud-02" land in the same family.
//
// Three groupings, in order of how much each one is worth believing. A project
// the store already answers to always wins: a family of "koi-garden-pond-01"
// and "koi-garden-pond-02" belongs under the "koi-garden" that is already
// there, not under a "koi-garden-pond" this would have to create next to it.
// Failing that, a numbered instance is filed under the stem its numbering
// implies. Failing that too, names that share their first segment are offered a
// parent named after it — the weakest signal, so it needs a third member.
//
// It writes nothing: the proposal is for a person to accept, and a name that
// looks like an instance is not proof that it is one.
func (s *Store) SuggestProjectTree() ([]ProjectTreeSuggestion, error) {
	known, err := s.knownProjects()
	if err != nil {
		return nil, err
	}

	cards := make(map[string]bool, len(known))
	for _, k := range known {
		if k.hasCard {
			cards[k.slug] = true
		}
	}

	type family struct {
		members []knownProject
		reason  string
		minimum int
	}
	families := map[string]*family{}
	for _, k := range known {
		if k.parent != nil {
			// Already placed: a suggestion would second-guess a decision
			// somebody already made.
			continue
		}
		parent, reason, minimum := "", "", minFamilyMembers
		switch {
		case longestKnownPrefix(known, k.slug, k.folded) != "":
			parent, reason = longestKnownPrefix(known, k.slug, k.folded), SuggestReasonExistingPrefix
		case projectFamilyStem(k.slug) != "":
			parent, reason = projectFamilyStem(k.slug), SuggestReasonSharedPrefix
		default:
			parent, reason, minimum = firstNameSegment(k.folded), SuggestReasonSharedFirstSegment, minSegmentFamilyMembers
		}
		if parent == "" || parent == k.slug {
			continue
		}
		f, ok := families[parent]
		if !ok {
			f = &family{reason: reason, minimum: minimum}
			families[parent] = f
		}
		f.members = append(f.members, k)
	}

	parents := make([]string, 0, len(families))
	for parent := range families {
		parents = append(parents, parent)
	}
	sort.Strings(parents)

	suggestions := make([]ProjectTreeSuggestion, 0, len(parents))
	for _, parent := range parents {
		f := families[parent]
		if len(f.members) < f.minimum {
			continue
		}
		children := make([]string, 0, len(f.members))
		sharedRepo := ""
		sameRepo := true
		for i, m := range f.members {
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

		reason := f.reason
		if reason == SuggestReasonSharedPrefix && sameRepo && sharedRepo != "" {
			reason = SuggestReasonSharedPrefixAndRepo
		}
		suggestions = append(suggestions, ProjectTreeSuggestion{
			Parent:       parent,
			Children:     children,
			Reason:       reason,
			ParentExists: cards[parent],
		})
	}
	return suggestions, nil
}

// ProjectSeparatorPair is a set of project names that differ only in the
// character somebody typed between the words.
type ProjectSeparatorPair struct {
	// Folded is the single comparison form all the names collapse to.
	Folded string `json:"folded"`
	// Names are the spellings in use, in name order.
	Names []string `json:"names"`
}

// SuggestProjectSeparatorPairs reports the projects that are one project
// written more than one way.
//
// The tree cannot fix these: "ai_engram" is not a child of "ai-engram", it is
// the same project, and reparenting one under the other would leave the memory
// split across two names that both still resolve. Naming them is the whole
// contribution — the repair is `engram projects merge`, which is destructive
// enough that nothing here should reach for it.
func (s *Store) SuggestProjectSeparatorPairs() ([]ProjectSeparatorPair, error) {
	known, err := s.knownProjects()
	if err != nil {
		return nil, err
	}

	byFolded := map[string][]string{}
	for _, k := range known {
		byFolded[k.folded] = append(byFolded[k.folded], k.slug)
	}

	folded := make([]string, 0, len(byFolded))
	for f, names := range byFolded {
		if len(names) < 2 {
			continue
		}
		folded = append(folded, f)
	}
	sort.Strings(folded)

	pairs := make([]ProjectSeparatorPair, 0, len(folded))
	for _, f := range folded {
		names := byFolded[f]
		sort.Strings(names)
		pairs = append(pairs, ProjectSeparatorPair{Folded: f, Names: names})
	}
	return pairs, nil
}
