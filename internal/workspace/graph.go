package workspace

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	projectpkg "github.com/HoracioEspinosa/engram/internal/project"
	"github.com/HoracioEspinosa/engram/internal/store"
)

// GraphCheck answers "does this project's code graph still describe the
// checkout?" and records the verdict on the card.
//
// The repository is the current working directory. A project card records
// where the graph lives inside a repository, not where the repository is: a
// checkout is a fact about this machine, and a path copied between machines
// points at nothing. Callers that know better use GraphCheckIn.
func GraphCheck(s *store.Store, slug string) (projectpkg.GraphStaleness, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return projectpkg.GraphStaleness{}, fmt.Errorf("engram-workspace: resolve working directory: %w", err)
	}
	return GraphCheckIn(s, slug, cwd)
}

// GraphCheckIn is GraphCheck against a named repository directory.
//
// The card is read through its ancestors, so an instance that never set a
// graph path uses its umbrella's. The graph commit is not inherited: it
// describes one build on one checkout, and borrowing it would tell an instance
// it has a graph nobody built there.
func GraphCheckIn(s *store.Store, slug, repoDir string) (projectpkg.GraphStaleness, error) {
	slug = strings.TrimSpace(slug)
	card, err := s.ResolveProjectCard(slug)
	if errors.Is(err, store.ErrNoProjectCard) {
		return projectpkg.GraphStaleness{}, fmt.Errorf("%w: %s", ErrUnknownProject, slug)
	}
	if err != nil {
		return projectpkg.GraphStaleness{}, err
	}

	graphCommit := ""
	if card.GraphCommit != nil {
		graphCommit = strings.TrimSpace(*card.GraphCommit)
	}

	staleness, err := projectpkg.CheckStaleness(repoDir, graphCommit, card.GraphPath, time.Now())
	if err != nil {
		return projectpkg.GraphStaleness{}, err
	}
	if err := s.StampGraphStaleness(card.Slug, staleness.Reason, staleness.ChangedFiles, staleness.CheckedAt); err != nil {
		return projectpkg.GraphStaleness{}, err
	}
	return staleness, nil
}
