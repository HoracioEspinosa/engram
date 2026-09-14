package workspace

import (
	"errors"
	"fmt"

	"github.com/HoracioEspinosa/engram/internal/store"
)

// umbrellaKind is what a parent invented to hold a family of instances is:
// a container, not a repository anybody checks out.
const umbrellaKind = "umbrella"

// TreeMove is one card that changed parents.
type TreeMove struct {
	Child  string `json:"child"`
	Parent string `json:"parent"`
	// ParentCreated reports that the parent card did not exist and was
	// created to hold the family.
	ParentCreated bool `json:"parent_created"`
}

// TreeConflict is one card that could not be moved, named by the code the
// refusal is reported under.
type TreeConflict struct {
	Child  string `json:"child"`
	Parent string `json:"parent"`
	Code   string `json:"code"`
	Reason string `json:"reason"`
}

// ApplyTreeReport is what applying a set of suggestions did.
type ApplyTreeReport struct {
	Applied   []TreeMove     `json:"applied"`
	Conflicts []TreeConflict `json:"conflicts"`
	// ParentsCreated names the umbrella cards the apply had to invent.
	ParentsCreated []string `json:"parents_created"`
}

// SuggestTree proposes a parent for each group of cards that reads as numbered
// instances of one product. It writes nothing, and neither does anything it
// returns: a slug that looks like an instance is not proof that it is one.
func SuggestTree(s *store.Store) ([]store.ProjectTreeSuggestion, error) {
	return s.SuggestProjectTree()
}

// ApplySuggestedTree carries out suggestions somebody accepted.
//
// A suggested parent that has no card is created — the whole point of the
// suggestion is that the family has no container yet — and the report says so,
// so an apply never silently adds a project nobody asked for. Every refusal is
// collected rather than returned: one card that would close a cycle must not
// stop the rest of a reorganisation somebody already approved.
func ApplySuggestedTree(s *store.Store, suggestions []store.ProjectTreeSuggestion) (ApplyTreeReport, error) {
	report := ApplyTreeReport{
		Applied:        []TreeMove{},
		Conflicts:      []TreeConflict{},
		ParentsCreated: []string{},
	}

	for _, suggestion := range suggestions {
		parent := suggestion.Parent
		created := false
		if !suggestion.ParentExists {
			if _, isNew, err := ensureParentCard(s, parent); err != nil {
				report.Conflicts = append(report.Conflicts, TreeConflict{
					Parent: parent, Code: "unknown_project", Reason: err.Error(),
				})
				continue
			} else if isNew {
				created = true
				report.ParentsCreated = append(report.ParentsCreated, parent)
			}
		}

		for _, child := range suggestion.Children {
			target := parent
			if err := s.SetProjectParent(child, &target); err != nil {
				report.Conflicts = append(report.Conflicts, TreeConflict{
					Child: child, Parent: parent, Code: treeConflictCode(err), Reason: err.Error(),
				})
				continue
			}
			report.Applied = append(report.Applied, TreeMove{
				Child: child, Parent: parent, ParentCreated: created,
			})
		}
	}
	return report, nil
}

// ensureParentCard creates the umbrella a family needs, and reports whether it
// had to.
func ensureParentCard(s *store.Store, slug string) (store.ProjectCard, bool, error) {
	kind := umbrellaKind
	card, created, err := s.UpsertProjectCard(store.UpsertProjectCardParams{Slug: slug, Kind: &kind})
	if err != nil {
		return store.ProjectCard{}, false, fmt.Errorf("engram-workspace: create umbrella %s: %w", slug, err)
	}
	return card, created, nil
}

// The faults a tree can be in.
const (
	// FaultOrphan is a card pointing at a parent that is gone.
	FaultOrphan = "orphan"
	// FaultCycle is a card that is its own ancestor. Only a write that
	// bypassed SetProjectParent can produce one.
	FaultCycle = "cycle"
	// FaultDepth is a card whose recorded depth disagrees with its chain, or
	// sits deeper than the tree allows.
	FaultDepth = "depth"
)

// TreeFault is one thing wrong with the hierarchy.
type TreeFault struct {
	Slug   string `json:"slug"`
	Fault  string `json:"fault"`
	Detail string `json:"detail"`
	// Fixed reports that the card was detached to the top of the tree.
	Fixed bool `json:"fixed"`
}

// TreeDoctor reports every card whose parent pointer or depth does not hold
// up, and detaches them when fix is set.
//
// The only repair is detaching to the root. Where a broken card belongs is a
// question about intent, and guessing an answer would replace a visible fault
// with an invisible wrong one; a card at the top of the tree is at least
// somewhere a person can see it and move it on purpose.
func TreeDoctor(s *store.Store, fix bool) ([]TreeFault, error) {
	rows, err := s.DB().Query(
		`SELECT slug, parent_slug, depth FROM project_cards WHERE deleted_at IS NULL ORDER BY slug`)
	if err != nil {
		return nil, fmt.Errorf("engram-workspace: read project cards: %w", err)
	}
	cards := map[string]treeCard{}
	var slugs []string
	for rows.Next() {
		var slug string
		var c treeCard
		if err := rows.Scan(&slug, &c.parent, &c.depth); err != nil {
			rows.Close()
			return nil, fmt.Errorf("engram-workspace: scan project card: %w", err)
		}
		cards[slug] = c
		slugs = append(slugs, slug)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("engram-workspace: read project cards: %w", err)
	}
	rows.Close()

	faults := []TreeFault{}
	for _, slug := range slugs {
		fault, ok := diagnoseCard(cards, slug)
		if !ok {
			continue
		}
		if fix {
			if err := s.SetProjectParent(slug, nil); err != nil {
				fault.Detail += "; detaching failed: " + err.Error()
			} else {
				fault.Fixed = true
			}
		}
		faults = append(faults, fault)
	}
	return faults, nil
}

// treeCard is the pair of columns a tree's integrity is decided by.
type treeCard struct {
	parent *string
	depth  int
}

// diagnoseCard walks one card's ancestry and reports the first fault it finds.
func diagnoseCard(cards map[string]treeCard, slug string) (TreeFault, bool) {
	c := cards[slug]
	if c.parent == nil {
		if c.depth != 0 {
			return TreeFault{Slug: slug, Fault: FaultDepth,
				Detail: fmt.Sprintf("has no parent but records depth %d", c.depth)}, true
		}
		return TreeFault{}, false
	}

	seen := map[string]bool{slug: true}
	hops := 0
	current := slug
	for {
		parent := cards[current].parent
		if parent == nil {
			break
		}
		if _, ok := cards[*parent]; !ok {
			return TreeFault{Slug: slug, Fault: FaultOrphan,
				Detail: fmt.Sprintf("parent %q has no live card", *parent)}, true
		}
		if seen[*parent] {
			return TreeFault{Slug: slug, Fault: FaultCycle,
				Detail: fmt.Sprintf("is its own ancestor through %q", *parent)}, true
		}
		seen[*parent] = true
		hops++
		if hops > len(cards) {
			return TreeFault{Slug: slug, Fault: FaultCycle,
				Detail: "ancestry does not terminate"}, true
		}
		current = *parent
	}
	if hops != c.depth {
		return TreeFault{Slug: slug, Fault: FaultDepth,
			Detail: fmt.Sprintf("records depth %d but sits %d level(s) below its root", c.depth, hops)}, true
	}
	if hops >= MaxProjectDepth {
		return TreeFault{Slug: slug, Fault: FaultDepth,
			Detail: fmt.Sprintf("sits %d level(s) below its root, past the %d the tree allows", hops, MaxProjectDepth-1)}, true
	}
	return TreeFault{}, false
}

// MaxProjectDepth mirrors the deepest a card may sit below a root. The store
// owns the rule and enforces it on every write; the doctor needs the number to
// name a row that got past it.
const MaxProjectDepth = 3

// treeConflictCode maps a reparenting refusal onto the envelope code it is
// published under.
func treeConflictCode(err error) string {
	switch {
	case errors.Is(err, store.ErrProjectCycle):
		return "project_cycle"
	case errors.Is(err, store.ErrProjectDepthExceeded):
		return "project_depth_exceeded"
	case errors.Is(err, store.ErrNoProjectCard):
		return "unknown_project"
	default:
		return "set_parent_failed"
	}
}
