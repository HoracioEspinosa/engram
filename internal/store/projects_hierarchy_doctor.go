package store

import "fmt"

// MaxProjectDepth is the deepest a card may sit below its root. The write path
// enforces it on every reparenting; a reader needs the number to name a row
// that got past it — a restore, or a sync from a peer whose schema predates the
// trigger that refuses one.
const MaxProjectDepth = maxProjectDepth

// The faults a hierarchy can be in.
const (
	// TreeFaultOrphan is a card pointing at a parent no live query returns.
	TreeFaultOrphan = "orphan"
	// TreeFaultCycle is a card that is its own ancestor.
	TreeFaultCycle = "cycle"
	// TreeFaultDepth is a card whose recorded depth disagrees with its chain,
	// or which sits deeper than the tree allows.
	TreeFaultDepth = "depth"
)

// ProjectTreeFault is one card whose parent pointer or depth does not hold up.
type ProjectTreeFault struct {
	Slug   string `json:"slug"`
	Fault  string `json:"fault"`
	Detail string `json:"detail"`
	// Repaired reports that the card was detached to the top of the tree.
	Repaired bool `json:"repaired"`
	// RepairError names why a detach was refused, empty when none was tried or
	// it succeeded.
	RepairError string `json:"repair_error,omitempty"`
}

// TreeDoctorReport is what one pass over the hierarchy found.
type TreeDoctorReport struct {
	Faults []ProjectTreeFault `json:"faults"`
	// Cards counts the live cards examined, so a clean report still says how
	// much was looked at rather than only that nothing was wrong.
	Cards int `json:"cards"`
}

// ProjectTreeDoctor reports every card whose parent pointer or depth does not
// hold up. It writes nothing.
func (s *Store) ProjectTreeDoctor() (TreeDoctorReport, error) {
	return s.diagnoseProjectTree(false)
}

// RepairProjectTree reports the same faults and detaches each broken card to
// the top of the tree.
//
// Detaching is the only repair. Where a broken card belongs is a question about
// intent, and guessing an answer would replace a visible fault with an
// invisible wrong one; a card at the top of the tree is at least somewhere a
// person can see it and move it on purpose.
func (s *Store) RepairProjectTree() (TreeDoctorReport, error) {
	return s.diagnoseProjectTree(true)
}

// treeCardRow is the pair of columns a tree's integrity is decided by.
type treeCardRow struct {
	parent *string
	depth  int
}

func (s *Store) diagnoseProjectTree(fix bool) (TreeDoctorReport, error) {
	rows, err := s.readDB().Query(
		`SELECT slug, parent_slug, depth FROM project_cards WHERE deleted_at IS NULL ORDER BY slug`)
	if err != nil {
		return TreeDoctorReport{}, fmt.Errorf("engram-projects: read project cards for the doctor: %w", err)
	}
	cards := map[string]treeCardRow{}
	var slugs []string
	for rows.Next() {
		var slug string
		var c treeCardRow
		if err := rows.Scan(&slug, &c.parent, &c.depth); err != nil {
			rows.Close()
			return TreeDoctorReport{}, fmt.Errorf("engram-projects: scan project card for the doctor: %w", err)
		}
		cards[slug] = c
		slugs = append(slugs, slug)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return TreeDoctorReport{}, fmt.Errorf("engram-projects: read project cards for the doctor: %w", err)
	}
	// The cursor is closed before the first repair: detaching writes, and
	// holding a read cursor across a write is how a single-connection store
	// deadlocks itself.
	rows.Close()

	report := TreeDoctorReport{Faults: []ProjectTreeFault{}, Cards: len(slugs)}
	for _, slug := range slugs {
		fault, ok := diagnoseProjectCard(cards, slug)
		if !ok {
			continue
		}
		if fix {
			if err := s.SetProjectParent(slug, nil); err != nil {
				fault.RepairError = err.Error()
			} else {
				fault.Repaired = true
			}
		}
		report.Faults = append(report.Faults, fault)
	}
	return report, nil
}

// diagnoseProjectCard walks one card's ancestry and reports the first fault it
// finds.
func diagnoseProjectCard(cards map[string]treeCardRow, slug string) (ProjectTreeFault, bool) {
	c := cards[slug]
	if c.parent == nil {
		if c.depth != 0 {
			return ProjectTreeFault{Slug: slug, Fault: TreeFaultDepth,
				Detail: fmt.Sprintf("has no parent but records depth %d", c.depth)}, true
		}
		return ProjectTreeFault{}, false
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
			return ProjectTreeFault{Slug: slug, Fault: TreeFaultOrphan,
				Detail: fmt.Sprintf("parent %q has no live card", *parent)}, true
		}
		if seen[*parent] {
			return ProjectTreeFault{Slug: slug, Fault: TreeFaultCycle,
				Detail: fmt.Sprintf("is its own ancestor through %q", *parent)}, true
		}
		seen[*parent] = true
		hops++
		if hops > len(cards) {
			return ProjectTreeFault{Slug: slug, Fault: TreeFaultCycle,
				Detail: "ancestry does not terminate"}, true
		}
		current = *parent
	}
	if hops != c.depth {
		return ProjectTreeFault{Slug: slug, Fault: TreeFaultDepth,
			Detail: fmt.Sprintf("records depth %d but sits %d level(s) below its root", c.depth, hops)}, true
	}
	if hops >= MaxProjectDepth {
		return ProjectTreeFault{Slug: slug, Fault: TreeFaultDepth,
			Detail: fmt.Sprintf("sits %d level(s) below its root, past the %d the tree allows", hops, MaxProjectDepth-1)}, true
	}
	return ProjectTreeFault{}, false
}
