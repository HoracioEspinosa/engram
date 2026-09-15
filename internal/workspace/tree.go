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

// SuggestSeparatorPairs reports the projects that are one project written more
// than one way. They are reported next to the tree suggestions and never
// applied by them: a name spelled with an underscore is not a child of the same
// name spelled with a hyphen, it is the same project, and only a merge joins
// them back.
func SuggestSeparatorPairs(s *store.Store) ([]store.ProjectSeparatorPair, error) {
	return s.SuggestProjectSeparatorPairs()
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

// The faults a tree can be in. The store diagnoses them; these names are the
// spelling a surface publishes, kept equal to the store's by construction.
const (
	// FaultOrphan is a card pointing at a parent that is gone.
	FaultOrphan = store.TreeFaultOrphan
	// FaultCycle is a card that is its own ancestor. Only a write that
	// bypassed SetProjectParent can produce one.
	FaultCycle = store.TreeFaultCycle
	// FaultDepth is a card whose recorded depth disagrees with its chain, or
	// sits deeper than the tree allows.
	FaultDepth = store.TreeFaultDepth
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
	report, err := s.ProjectTreeDoctor()
	if fix {
		report, err = s.RepairProjectTree()
	}
	if err != nil {
		return nil, err
	}

	faults := make([]TreeFault, 0, len(report.Faults))
	for _, f := range report.Faults {
		fault := TreeFault{Slug: f.Slug, Fault: f.Fault, Detail: f.Detail, Fixed: f.Repaired}
		if f.RepairError != "" {
			fault.Detail += "; detaching failed: " + f.RepairError
		}
		faults = append(faults, fault)
	}
	return faults, nil
}

// MaxProjectDepth mirrors the deepest a card may sit below a root. The store
// owns the rule and enforces it on every write; a surface needs the number to
// explain a row that got past it.
const MaxProjectDepth = store.MaxProjectDepth

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
