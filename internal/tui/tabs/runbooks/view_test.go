package runbooks

import (
	"strings"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/data"
)

// TestIndexRowShowsTheStaleBadgeOnlyWhenStale pins the criterion T-10.05
// closes on: a runbook flagged stale in runbook_index must render with a
// visible stale marker, and one that is not must not — the badge must track
// the real Stale field, never assume it.
func TestIndexRowShowsTheStaleBadgeOnlyWhenStale(t *testing.T) {
	stale := sampleRunbook("RB-003", "nextcloud", "Preview endpoint slow", true)
	fresh := sampleRunbook("RB-005", "nextcloud", "Push notifications", false)

	m := withHeight(newModel(&data.FakeRunbook{}, nil), 20).WithProject("nextcloud")
	m.Items = []store.RunbookIndexRow{stale, fresh}

	staleRow := m.viewRunbookRow(stale, false)
	if !strings.Contains(staleRow, "⚠") {
		t.Fatalf("stale row = %q, want the stale marker rendered", staleRow)
	}

	freshRow := m.viewRunbookRow(fresh, false)
	if strings.Contains(freshRow, "⚠") {
		t.Fatalf("fresh row = %q, want no stale marker", freshRow)
	}
}

// TestMarkdownViewTitleShowsTheStaleBadgeOnlyWhenStale is S9's counterpart:
// the exact screen the closing criterion opens ("RB-003 se abre desde la TUI
// con su bandera stale").
func TestMarkdownViewTitleShowsTheStaleBadgeOnlyWhenStale(t *testing.T) {
	stale := sampleRunbook("RB-003", "nextcloud", "Preview endpoint slow", true)
	m := newModel(&data.FakeRunbook{}, nil).WithProject("nextcloud")
	m.Screen = ScreenView
	m.Selected = &stale
	m.FileExists = true
	m.Rendered = "body"

	view := m.View()
	if !strings.Contains(view, "stale") || !strings.Contains(view, "⚠") {
		t.Fatalf("view = %q, want the stale badge visible in the title line", view)
	}

	fresh := sampleRunbook("RB-005", "nextcloud", "Push notifications", false)
	m.Selected = &fresh
	view = m.View()
	if strings.Contains(view, "⚠") {
		t.Fatalf("view = %q, want no stale badge for a fresh runbook", view)
	}
}
