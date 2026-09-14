package evidence

import (
	"strings"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"
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
			m.CopyFeedback = "Copied!"
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

// ─── Pure helpers ────────────────────────────────────────────────────────────

func TestFormatBytesCoversEveryUnit(t *testing.T) {
	i64 := func(v int64) *int64 { return &v }

	tests := []struct {
		name string
		v    *int64
		want string
	}{
		{"nil", nil, "unknown"},
		{"bytes", i64(512), "512 B"},
		{"kilobytes", i64(2048), "2.0 KB"},
		{"megabytes", i64(3 * 1 << 20), "3.0 MB"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatBytes(tt.v); got != tt.want {
				t.Errorf("formatBytes(%v) = %q, want %q", tt.v, got, tt.want)
			}
		})
	}
}

func TestOrDash(t *testing.T) {
	m := New(nil)
	if got, want := m.orDash(""), m.styles.Icons.Glyph(theme.IconUnknown); got != want {
		t.Errorf("orDash(\"\") = %q, want the vocabulary's unknown mark %q", got, want)
	}
	if got := m.orDash("p"); got != "p" {
		t.Errorf("orDash(%q) = %q, want the value unchanged", "p", got)
	}
}

func TestOrEmptyStr(t *testing.T) {
	if got := orEmptyStr(nil); got != "" {
		t.Errorf("orEmptyStr(nil) = %q, want \"\"", got)
	}
	v := "2026-08-23"
	if got := orEmptyStr(&v); got != v {
		t.Errorf("orEmptyStr(&v) = %q, want %q", got, v)
	}
}

func TestFilepathBase(t *testing.T) {
	if got := filepathBase("ACME-9/a.png"); got != "a.png" {
		t.Errorf("filepathBase with a slash = %q, want %q", got, "a.png")
	}
	if got := filepathBase("a.png"); got != "a.png" {
		t.Errorf("filepathBase with no slash = %q, want the path unchanged", got)
	}
}

func TestTaskFilterLabel(t *testing.T) {
	withKey := sampleItem(1, 9, "ACME-9", "ACME-9/a.png", false)
	withoutKey := sampleItem(2, 10, "", "ACME-10/b.png", false)
	withoutKey.JiraKey = nil

	tests := []struct {
		name  string
		items []store.EvidenceListItem
		f     store.EvidenceListFilter
		want  string
	}{
		{"unfiltered", nil, store.EvidenceListFilter{}, "all"},
		{"matches a jira key", []store.EvidenceListItem{withKey}, store.EvidenceListFilter{TaskID: 9}, "ACME-9"},
		{"matches an sdd-only task", []store.EvidenceListItem{withoutKey}, store.EvidenceListFilter{TaskID: 10}, "task-sync"},
		{"no row carries the filtered task", []store.EvidenceListItem{withKey}, store.EvidenceListFilter{TaskID: 404}, "#404"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := taskFilterLabel(tt.items, tt.f); got != tt.want {
				t.Errorf("taskFilterLabel(...) = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAttachedFilterLabel(t *testing.T) {
	yes := true
	if got := attachedFilterLabel(store.EvidenceListFilter{}); got != "all attached states" {
		t.Errorf("attachedFilterLabel(unset) = %q, want %q", got, "all attached states")
	}
	if got := attachedFilterLabel(store.EvidenceListFilter{AttachedJira: &yes}); got != "attached only" {
		t.Errorf("attachedFilterLabel(attached) = %q, want %q", got, "attached only")
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
