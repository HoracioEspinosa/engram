// Package tui is the entry point of the engram terminal UI.
//
// It exists so cmd/engram depends on one stable name while the workspace
// underneath — the root model, the tabs, the theme and the data adapters —
// is free to move between packages.
package tui

import (
	"github.com/Gentleman-Programming/engram/internal/store"
	"github.com/Gentleman-Programming/engram/internal/tui/app"
)

// Model is the root Bubble Tea model of the TUI.
type Model = app.Model

// New creates the TUI bound to the given store.
func New(s *store.Store, version string) Model {
	return app.New(s, version)
}
