package runbooks

import (
	"strings"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/data"
)

// TestViewIndexRendersEveryStateWithoutPanicking is the Runbooks-tab
// counterpart of tabs/evidence's and tabs/tasks's own
// TestViewRendersEveryScreenWithoutPanicking: viewIndex and
// viewRunbookPreview sat at 0% coverage because no existing test called
// m.View() with Screen left at its zero value (ScreenIndex).
func TestViewIndexRendersEveryStateWithoutPanicking(t *testing.T) {
	item := sampleRunbook("RB-900", "acme", "Stale runbook", true)
	item.Symptoms = []string{"returns HTTP 503", "cold generation is slow"}
	itemNoSymptoms := sampleRunbook("RB-901", "acme", "No symptoms recorded", false)

	scenes := []struct {
		name  string
		build func() Model
	}{
		{"empty", func() Model { return withHeight(newModel(&data.FakeRunbook{}, nil).WithProject("acme"), 30) }},
		{"populated-with-preview", func() Model {
			m := withHeight(newModel(&data.FakeRunbook{}, nil).WithProject("acme"), 30)
			m.Items = []store.RunbookIndexRow{item, itemNoSymptoms}
			m.Cursor = 0
			return m
		}},
		{"populated-cursor-on-row-without-symptoms", func() Model {
			m := withHeight(newModel(&data.FakeRunbook{}, nil).WithProject("acme"), 30)
			m.Items = []store.RunbookIndexRow{item, itemNoSymptoms}
			m.Cursor = 1
			return m
		}},
		{"searching", func() Model {
			m := withHeight(newModel(&data.FakeRunbook{}, nil).WithProject("acme"), 30)
			m.Items = []store.RunbookIndexRow{item}
			m.Searching = true
			m.SearchInput.Focus()
			return m
		}},
		{"all-projects-with-query", func() Model {
			m := withHeight(newModel(&data.FakeRunbook{}, nil).WithProject("acme"), 30)
			m.All = true
			m.Query = "503"
			m.Items = []store.RunbookIndexRow{item}
			return m
		}},
		{"error-and-copy-banner", func() Model {
			m := withHeight(newModel(&data.FakeRunbook{}, nil).WithProject("acme"), 30)
			m.ErrorMsg = "database is locked"
			m.CopyFeedback = "✓ Copied!"
			return m
		}},
	}

	for _, scene := range scenes {
		t.Run(scene.name, func(t *testing.T) {
			out := scene.build().View()
			if out == "" {
				t.Fatal("View() returned an empty string")
			}
		})
	}
}

func TestViewIndexShowsTheRunbookIdentity(t *testing.T) {
	item := sampleRunbook("RB-900", "acme", "Stale runbook", true)
	m := withHeight(newModel(&data.FakeRunbook{}, nil).WithProject("acme"), 30)
	m.Items = []store.RunbookIndexRow{item}

	out := m.View()
	if !strings.Contains(out, "RB-900") || !strings.Contains(out, "Stale runbook") {
		t.Fatalf("index view missing the runbook identity, got:\n%s", out)
	}
}

// TestViewRunbookPreviewShowsTheSymptoms pins the strip rfc-tui.md §5's S8
// wireframe puts under the cursor, the one viewRunbookPreview renders.
func TestViewRunbookPreviewShowsTheSymptoms(t *testing.T) {
	item := sampleRunbook("RB-900", "acme", "Stale runbook", true)
	item.Symptoms = []string{"returns HTTP 503"}
	m := withHeight(newModel(&data.FakeRunbook{}, nil).WithProject("acme"), 30)
	m.Items = []store.RunbookIndexRow{item}
	m.Cursor = 0

	out := m.View()
	if !strings.Contains(out, "returns HTTP 503") {
		t.Fatalf("preview missing the symptom line, got:\n%s", out)
	}
}
