package evidence

import (
	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/data"

	tea "github.com/charmbracelet/bubbletea"
)

// loadEvidence returns the command that lists project's evidence under f.
//
// It asks for the page rather than the bare slice: the store counts the
// whole match — rows and bytes — in the same round trip, and the footer has
// no honest total to report without it.
func loadEvidence(r data.EvidenceSource, project string, f store.EvidenceListFilter) tea.Cmd {
	return func() tea.Msg {
		page, err := r.ListEvidencePage(project, f)
		return evidenceLoadedMsg{project: project, filter: f, page: page, err: err}
	}
}

// loadManifest returns the command that reads item's sibling manifest.json,
// if one exists — what the detail screen's manifest section renders.
func loadManifest(item store.EvidenceListItem) tea.Cmd {
	return func() tea.Msg {
		entry, exists, err := readManifestEntry(absolutePath(item.Path))
		return manifestLoadedMsg{evidenceID: item.ID, entry: entry, exists: exists, err: err}
	}
}
