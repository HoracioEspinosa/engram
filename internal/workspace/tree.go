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
