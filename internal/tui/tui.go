// Package tui is the entry point of the engram terminal UI.
//
// It exists so cmd/engram depends on one stable name while the workspace
// underneath — the root model, the tabs, the theme and the data adapters —
// is free to move between packages.
package tui

import (
	"os"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/app"
	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs/memory"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"
)

// Model is the root Bubble Tea model of the TUI.
type Model = app.Model

// New creates the TUI bound to the given store. project, when non-empty,
// opens the workspace straight on that project's Home tab instead of the
// Memory tab; pass "" when none was resolved. Deciding which project that is —
// the explicit flag, then ENGRAM_PROJECT, then detection from the working
// directory — is cmd/engram's job, not this facade's.
// palette is the resolved theme — --theme, then ENGRAM_TUI_THEME, then
// settings['tui.theme'], then config.json, then the default — and resolving it
// is likewise cmd/engram's job (theme.Selection.Resolve), not this facade's;
// pass theme.Default() when nothing else applies (as tests that do not care
// about theming do).
//
// Every reader the workspace consumes is derived from s here, in one place, so
// each screen is backed by the same store cmd/engram opened.
func New(s *store.Store, version string, project string, palette theme.Palette) Model {
	return app.New(
		data.NewMemorySource(s),
		data.NewProjectReader(s),
		data.NewTaskSource(s),
		data.NewEvidenceSource(s),
		data.NewRunbookSource(s),
		version,
		theme.New(palette),
		project,
	).WithThemePicker(
		data.NewThemeReader(s),
		data.NewSettingsWriter(s),
	).WithProjectTree(
		data.NewProjectTreeReader(s),
	).WithGraph(
		data.NewGraphReader(s),
		// The graph is read and rebuilt against a git checkout, and the TUI
		// detects none of its own: whoever launched it is standing in the
		// repository they mean, the same assumption `engram project graph
		// sync` makes when no --repo-dir is given. A working directory that
		// cannot be read leaves the syncer pointed at "", which reports a
		// missing checkout rather than syncing the wrong one.
		data.NewGraphSyncer(s, workingDir()),
	).WithBenchmarks(
		data.NewBenchmarkReader(s),
	).WithSearch(
		data.NewGlobalSearcher(s),
		data.NewSettingsWriter(s),
	).WithSearchHistory(
		searchHistory(data.NewSettingsReader(s)),
	).WithSettingsStore(
		data.NewSettingsWriter(s),
		data.NewSettingsReader(s),
	).WithMemoryScope(
		memoryScope(data.NewSettingsReader(s)),
	)
}

// searchHistory reads the queries the workspace last remembered. It runs once,
// here, rather than every time the palette opens: a settings read is cheap but
// a read on the render path is a read nobody can see failing. A store that
// will not answer yields no history, which is what a fresh install has anyway.
func searchHistory(reader data.SettingsReader) []string {
	if reader == nil {
		return nil
	}
	raw, _, err := reader.Setting(app.SearchHistoryKey)
	if err != nil {
		return nil
	}
	return app.DecodeSearchHistory(raw)
}

// memoryScope reads the width Memory was last left at, the same way and for
// the same reason searchHistory reads the queries it last remembered. A store
// that will not answer opens the tab workspace-wide, which is what a fresh
// install gets anyway and is the answer that can never be wrong for the wrong
// reason: too much memory, never too little.
func memoryScope(reader data.SettingsReader) memory.Scope {
	if reader == nil {
		return memory.ScopeAll
	}
	raw, _, err := reader.Setting(memory.ScopeSettingKey)
	if err != nil {
		return memory.ScopeAll
	}
	return memory.ParseScope(raw)
}

// workingDir is the checkout the graph syncer resolves graph.json and git HEAD
// against. os.Getwd already answers "" when it cannot say, which is exactly
// the "no checkout" the syncer reports on, so the error needs no second
// translation here.
func workingDir() string {
	dir, _ := os.Getwd()
	return dir
}
