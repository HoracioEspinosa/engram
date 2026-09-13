package evidence

import (
	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/data"

	tea "github.com/charmbracelet/bubbletea"
)

// loadEvidence returns the command that lists project's evidence under f
// (rfc-tui.md §9.2's "S6 Evidence list" query).
func loadEvidence(r data.EvidenceReader, project string, f store.EvidenceListFilter) tea.Cmd {
	return func() tea.Msg {
		items, err := r.ListEvidence(project, f)
		return evidenceLoadedMsg{project: project, filter: f, items: items, err: err}
	}
}

// loadManifest returns the command that reads item's sibling manifest.json,
// if one exists (S7, rfc-tui.md §9.3).
func loadManifest(item store.EvidenceListItem) tea.Cmd {
	return func() tea.Msg {
		entry, exists, err := readManifestEntry(absolutePath(item.Path))
		return manifestLoadedMsg{evidenceID: item.ID, entry: entry, exists: exists, err: err}
	}
}
