package evidence

import (
	"strings"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/data"
)

// TestViewRendersEveryScreenWithoutPanicking is a smoke test, not a golden
// file (teatest goldens are T-10.07's): it only pins that each screen, in
// its loading/empty/populated states, renders to a string instead of
// panicking on a nil Selected or an empty slice.
func TestViewRendersEveryScreenWithoutPanicking(t *testing.T) {
	item := sampleItem(1, 9, "ACME-9", "ACME-9/a.png", true)

	scenes := []struct {
		name  string
		build func() Model
	}{
		{"list-empty", func() Model { return withHeight(New(&data.FakeEvidence{}).WithProject("acme"), 30) }},
		{"list-populated", func() Model {
			m := withHeight(New(&data.FakeEvidence{}).WithProject("acme"), 30)
			m.Items = []store.EvidenceListItem{item}
			return m
		}},
		{"list-unattached", func() Model {
			m := withHeight(New(&data.FakeEvidence{}).WithProject("acme"), 30)
			m.Items = []store.EvidenceListItem{sampleItem(2, 9, "ACME-9", "ACME-9/b.png", false)}
			return m
		}},
		{"list-task-filtered", func() Model {
			m := withHeight(New(&data.FakeEvidence{}).WithProject("acme"), 30)
			m.Items = []store.EvidenceListItem{item}
			m.Filter = store.EvidenceListFilter{TaskID: 9}
			return m
		}},
		{"detail-loading", func() Model {
			m := New(&data.FakeEvidence{}).WithProject("acme")
			m.Screen = ScreenDetail
			return m
		}},
		{"detail-manifest-missing", func() Model {
			m := New(&data.FakeEvidence{}).WithProject("acme")
			m.Screen = ScreenDetail
			m.Selected = &item
			m.ManifestChecked = true
			m.ManifestExists = false
			return m
		}},
		{"detail-manifest-found", func() Model {
			m := New(&data.FakeEvidence{}).WithProject("acme")
			m.Screen = ScreenDetail
			m.Selected = &item
			m.ManifestChecked = true
			m.ManifestExists = true
			m.Manifest = &ManifestEntry{PositiveControl: "p", NegativeControl: "n"}
			return m
		}},
		{"detail-manifest-error", func() Model {
			m := New(&data.FakeEvidence{}).WithProject("acme")
			m.Screen = ScreenDetail
			m.Selected = &item
			m.ManifestChecked = true
			m.ManifestExists = true
			m.ManifestErr = "parse manifest.json: unexpected end of JSON input"
			return m
		}},
		{"with-error-and-copy-feedback", func() Model {
			m := New(&data.FakeEvidence{}).WithProject("acme")
			m.ErrorMsg = "database is locked"
			m.CopyFeedback = "✓ Copied!"
			return m
		}},
	}

	for _, scene := range scenes {
		t.Run(scene.name, func(t *testing.T) {
			m := scene.build()
			out := m.View()
			if out == "" {
				t.Fatal("View() returned an empty string")
			}
		})
	}
}

func TestViewListShowsTheFileAndTaskIdentity(t *testing.T) {
	m := withHeight(New(&data.FakeEvidence{}).WithProject("acme"), 30)
	m.Items = []store.EvidenceListItem{sampleItem(1, 9, "ACME-9", "ACME-9/a.png", true)}

	out := m.View()
	if !strings.Contains(out, "a.png") || !strings.Contains(out, "ACME-9") {
		t.Fatalf("list view missing file/task identity, got:\n%s", out)
	}
}

func TestViewDetailShowsThePathAndSha256(t *testing.T) {
	item := sampleItem(1, 9, "ACME-9", "ACME-9/a.png", true)
	m := New(&data.FakeEvidence{}).WithProject("acme")
	m.Screen = ScreenDetail
	m.Selected = &item
	m.ManifestChecked = true

	out := m.View()
	if !strings.Contains(out, item.SHA256) {
		t.Fatalf("detail view missing sha256, got:\n%s", out)
	}
	if !strings.Contains(out, "a.png") {
		t.Fatalf("detail view missing the filename, got:\n%s", out)
	}
}
