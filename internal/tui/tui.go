// Package tui is the entry point of the engram terminal UI.
//
// It exists so cmd/engram depends on one stable name while the workspace
// underneath — the root model, the tabs, the theme and the data adapters —
// is free to move between packages.
package tui

import (
	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/app"
	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"
)

// Model is the root Bubble Tea model of the TUI.
type Model = app.Model

// New creates the TUI bound to the given store. project, when non-empty,
// opens the workspace straight on that project's Dashboard instead of the
// Memory tab; pass "" when none was resolved (rfc-tui.md §9.1's
// "Semántica de --project": explicit flag, then ENGRAM_PROJECT, then cwd
// detection — that precedence is cmd/engram's job, not this facade's).
//
// Every reader the workspace consumes is derived from s here, in one place, so
// each screen is backed by the same store cmd/engram opened.
func New(s *store.Store, version string, project string) Model {
	return app.New(
		data.NewMemoryReader(s),
		data.NewProjectReader(s),
		data.NewTaskReader(s),
		data.NewEvidenceReader(s),
		data.NewRunbookReader(s),
		version,
		theme.Default(),
		project,
	)
}
