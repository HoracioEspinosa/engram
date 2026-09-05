package obsidian

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Gentleman-Programming/engram/internal/store"
)

// refMockStore is the mock StoreReader plus the optional engram-projects
// capability, so the exporter takes the ProjectRefReader branch.
type refMockStore struct {
	mockStore
	refs    map[string]store.ObservationExportRefs
	refsErr error
	asked   string
	calls   int
}

func (m *refMockStore) ObservationExportRefs(project string) (map[string]store.ObservationExportRefs, error) {
	m.asked = project
	m.calls++
	return m.refs, m.refsErr
}

func exportedNote(t *testing.T, vault, project, obsType, name string) string {
	t.Helper()
	path := filepath.Join(vault, "engram", project, obsType, name)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read exported note %s: %v", path, err)
	}
	return string(body)
}

func TestObservationToMarkdownWithRefs(t *testing.T) {
	obs := store.Observation{
		ID: 42, Type: "decision", Title: "Preview cap", Content: "body",
		Scope: "project", SessionID: "s1", CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
	}

	t.Run("without refs the note is unchanged", func(t *testing.T) {
		plain := ObservationToMarkdown(obs)
		for _, key := range []string{"knowledge_ref:", "jira_key:", "runbook_id:", "graph_commit:"} {
			if strings.Contains(plain, key) {
				t.Fatalf("an observation with no engram-projects refs emitted %q:\n%s", key, plain)
			}
		}
	})

	t.Run("each ref is emitted only when present", func(t *testing.T) {
		out := ObservationToMarkdownWithRefs(obs, store.ObservationExportRefs{
			KnowledgeRef: "Services/Nextcloud/Architecture.md#Object Store",
			JiraKey:      "CDBS-10336",
		})
		if !strings.Contains(out, `knowledge_ref: "Services/Nextcloud/Architecture.md#Object Store"`) {
			t.Fatalf("knowledge_ref missing or unquoted:\n%s", out)
		}
		if !strings.Contains(out, "jira_key: CDBS-10336\n") {
			t.Fatalf("jira_key missing:\n%s", out)
		}
		if strings.Contains(out, "runbook_id:") || strings.Contains(out, "graph_commit:") {
			t.Fatalf("absent refs were emitted anyway:\n%s", out)
		}
	})

	t.Run("the four keys land inside the frontmatter fence", func(t *testing.T) {
		out := ObservationToMarkdownWithRefs(obs, store.ObservationExportRefs{
			KnowledgeRef: "Services/Lookup/Lookup.md", JiraKey: "CDBS-1", RunbookID: "RB-003",
			GraphCommit: "3f9c2a7d1b8e4c6f0a2d9e1b7c5f3a8d2e6b4c1a",
		})
		header, _, found := strings.Cut(strings.TrimPrefix(out, "---\n"), "\n---\n")
		if !found {
			t.Fatalf("no frontmatter fence:\n%s", out)
		}
		for _, key := range []string{"knowledge_ref:", "jira_key:", "runbook_id:", "graph_commit:"} {
			if !strings.Contains(header, key) {
				t.Fatalf("%q is outside the frontmatter:\n%s", key, out)
			}
		}
	})
}

func TestExport_StampsProjectRefs(t *testing.T) {
	vault := t.TempDir()
	obs := store.Observation{
		ID: 7, SyncID: "obs-00000000000000ab", Type: "decision", Title: "Preview cap",
		Content: "body", Scope: "project", SessionID: "s1",
		CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
	}
	project := "nextcloud"
	obs.Project = &project

	ms := &refMockStore{
		mockStore: mockStore{exportData: &store.ExportData{Observations: []store.Observation{obs}}},
		refs: map[string]store.ObservationExportRefs{
			"obs-00000000000000ab": {KnowledgeRef: "Services/Nextcloud/Architecture.md", RunbookID: "RB-003"},
		},
	}
	if _, err := NewExporter(ms, ExportConfig{VaultPath: vault, Project: project}).Export(); err != nil {
		t.Fatalf("Export: %v", err)
	}
	if ms.calls != 1 || ms.asked != project {
		t.Fatalf("ObservationExportRefs called %d time(s) with %q, want once with %q", ms.calls, ms.asked, project)
	}
	note := exportedNote(t, vault, project, "decision", "preview-cap-7.md")
	if !strings.Contains(note, "knowledge_ref: \"Services/Nextcloud/Architecture.md\"") ||
		!strings.Contains(note, "runbook_id: RB-003") {
		t.Fatalf("exported note is missing its refs:\n%s", note)
	}
}

// A StoreReader without the projects capability must still export: the
// exporter predates these tables and has to keep working on a store that
// never created them.
func TestExport_WithoutProjectRefReader(t *testing.T) {
	vault := t.TempDir()
	project := "nextcloud"
	obs := store.Observation{
		ID: 7, SyncID: "obs-00000000000000ab", Type: "decision", Title: "Preview cap",
		Content: "body", Scope: "project", SessionID: "s1", Project: &project,
		CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
	}
	ms := &mockStore{exportData: &store.ExportData{Observations: []store.Observation{obs}}}
	if _, err := NewExporter(ms, ExportConfig{VaultPath: vault, Project: project}).Export(); err != nil {
		t.Fatalf("Export: %v", err)
	}
	note := exportedNote(t, vault, project, "decision", "preview-cap-7.md")
	if strings.Contains(note, "knowledge_ref:") {
		t.Fatalf("a plain StoreReader produced projects frontmatter:\n%s", note)
	}
}
