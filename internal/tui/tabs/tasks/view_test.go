package tasks

import (
	"strings"
	"testing"
	"time"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/data"
)

// TestViewRendersEveryScreenWithoutPanicking is a smoke test, not a golden
// file: it only pins that each screen, in its loading/empty/populated/overlay
// states, renders to a string instead of panicking on a nil Detail or an
// empty slice.
func TestViewRendersEveryScreenWithoutPanicking(t *testing.T) {
	task := sampleTask(1, "ACME-1", "review")

	scenes := []struct {
		name  string
		build func() Model
	}{
		{"list-empty", func() Model { return withHeight(New(&data.FakeTask{}).WithProject("acme"), 30) }},
		{"list-populated", func() Model {
			m := withHeight(New(&data.FakeTask{}).WithProject("acme"), 30)
			m.Items = []store.TaskListItem{{Task: task, Observations: 6, Evidence: 3, StateStale: true}}
			return m
		}},
		{"list-searching", func() Model {
			m := withHeight(New(&data.FakeTask{}).WithProject("acme"), 30)
			m.Searching = true
			m.SearchInput.Focus()
			return m
		}},
		{"detail-loading", func() Model {
			m := New(&data.FakeTask{}).WithProject("acme")
			m.Screen = ScreenDetail
			return m
		}},
		{"detail-populated", func() Model {
			m := New(&data.FakeTask{}).WithProject("acme")
			m.Screen = ScreenDetail
			detail := sampleDetail(task, 1460, 1466)
			detail.Evidence = []store.EvidenceListItem{{Evidence: store.Evidence{Path: "01-cold-503.png", Proves: "3 of 15 GETs return 503", AttachedJira: true}}}
			m.Detail = &detail
			return m
		}},
		{"detail-state-picker", func() Model {
			m := New(&data.FakeTask{}).WithProject("acme")
			m.Screen = ScreenDetail
			detail := sampleDetail(task)
			m.Detail = &detail
			m.ChangingState = true
			return m
		}},
		{"detail-linking", func() Model {
			m := New(&data.FakeTask{}).WithProject("acme")
			m.Screen = ScreenDetail
			detail := sampleDetail(task)
			m.Detail = &detail
			m.Linking = true
			m.LinkInput.Focus()
			return m
		}},
		{"context-pack-loading", func() Model {
			m := New(&data.FakeTask{}).WithProject("acme")
			m.Screen = ScreenContextPack
			return m
		}},
		{"context-pack-loaded", func() Model {
			m := withHeight(New(&data.FakeTask{}).WithProject("acme"), 30)
			m.Screen = ScreenContextPack
			detail := sampleDetail(task)
			m.Detail = &detail
			m.ContextPack = "# Context pack: acme / ACME-1\n\n## Task\nACME-1 · bugfix · review\n"
			m.ContextPackBuilt = time.Now()
			return m
		}},
		{"error-and-copy-banner", func() Model {
			m := withHeight(New(&data.FakeTask{}).WithProject("acme"), 30)
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

// ─── Pure helpers ────────────────────────────────────────────────────────────

func TestFilterLabel(t *testing.T) {
	if got := filterLabel("", "active"); got != "active" {
		t.Errorf("filterLabel(\"\", \"active\") = %q, want %q", got, "active")
	}
	if got := filterLabel("review", "active"); got != "review" {
		t.Errorf("filterLabel(\"review\", \"active\") = %q, want the value unchanged", got)
	}
}

func TestOrEmpty(t *testing.T) {
	if got := orEmpty(nil); got != "" {
		t.Errorf("orEmpty(nil) = %q, want \"\"", got)
	}
	v := "fix/ACME-1"
	if got := orEmpty(&v); got != v {
		t.Errorf("orEmpty(&v) = %q, want %q", got, v)
	}
}

// TestViewTaskRowCoversTheNoBranchAndNoPRDefaults pins viewTaskRow's two
// fallback labels: TestViewRendersEveryScreenWithoutPanicking's fixture task
// always has a branch and never a PR, so neither default has ever rendered
// before this.
func TestViewTaskRowCoversTheNoBranchAndNoPRDefaults(t *testing.T) {
	m := New(&data.FakeTask{}).WithProject("acme")
	item := store.TaskListItem{Task: sampleTask(1, "ACME-1", "open")}
	item.Branch = nil
	item.PRUrl = nil

	out := m.viewTaskRow(item, false, taskColumns(m.masterWidth()))
	if !strings.Contains(out, "no branch") {
		t.Fatalf("row with no branch = %q, want it to say \"no branch\"", out)
	}
	if !strings.Contains(out, "no PR") {
		t.Fatalf("row with no PR = %q, want it to say \"no PR\"", out)
	}

	item.Branch = strp("fix/ACME-1")
	item.PRUrl = strp("https://github.com/acme/repo/pull/1")
	out = m.viewTaskRow(item, true, taskColumns(m.masterWidth()))
	if !strings.Contains(out, "PR linked") {
		t.Fatalf("row with a PR = %q, want it to say \"PR linked\"", out)
	}
}

func TestViewListShowsTheJiraKeyAndTitle(t *testing.T) {
	m := withHeight(New(&data.FakeTask{}).WithProject("acme"), 30)
	m.Items = []store.TaskListItem{{Task: sampleTask(1, "ACME-1", "review")}}

	out := m.View()
	if !strings.Contains(out, "ACME-1") || !strings.Contains(out, "Previews 503 on cold generation") {
		t.Fatalf("list view missing task identity, got:\n%s", out)
	}
}
