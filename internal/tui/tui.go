// Package tui is the entry point of the engram terminal UI.
//
// It exists so cmd/engram depends on one stable name while the workspace
// underneath — the root model, the tabs, the theme and the data adapters —
// is free to move between packages.
package tui

import (
	"github.com/Gentleman-Programming/engram/internal/store"
	"github.com/Gentleman-Programming/engram/internal/tui/app"
	"github.com/Gentleman-Programming/engram/internal/tui/data"
	"github.com/Gentleman-Programming/engram/internal/tui/theme"
)

// Model is the root Bubble Tea model of the TUI.
type Model = app.Model

// New creates the TUI bound to the given store.
//
// Every reader the workspace consumes is derived from s here, in one place, so
// each screen is backed by the same store cmd/engram opened.
func New(s *store.Store, version string) Model {
	return app.New(
		data.NewMemoryReader(s),
		data.NewProjectReader(s),
		version,
		theme.Default(),
		"",
	)
}
