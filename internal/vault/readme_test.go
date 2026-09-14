package vault

import (
	"path/filepath"
	"testing"
)

func TestParseProjectReadmeMapsTheFourStates(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "README.md")
	writeFile(t, path, vaultReadme)

	tasks, err := ParseProjectReadme(path)
	if err != nil {
		t.Fatalf("ParseProjectReadme: %v", err)
	}
	if len(tasks) != 5 {
		t.Fatalf("expected 5 rows, got %d: %+v", len(tasks), tasks)
	}

	cases := []struct {
		idx         int
		title       string
		path        string
		state       string
		closedAt    string
		pendingNote string
		count       int
	}{
		{idx: 0, title: "Hardening del autologin", path: "koi-garden/KOI-1042-hardening-autologin",
			state: StateDone, closedAt: "2026-08-14", count: 11},
		{idx: 1, title: "Timeout de lookup", path: "koi-garden/KOI-1099-lookup-timeout",
			state: StatePending, pendingNote: "falta validar en la instancia 02", count: 12},
		{idx: 2, title: "División del filesharing", path: "koi-garden/split-pond-filesharing",
			state: StateUnverified, count: 3},
		{idx: 3, title: "Mantenimiento del estanque", path: "koi-garden/mantenimiento-del-estanque",
			state: StateArchived, count: 3},
		{idx: 4, title: "Migración de miniaturas", path: "tsukimi-bridge/TSU-204-migracion-thumbnails",
			state: "", count: 6},
	}
	for _, tc := range cases {
		got := tasks[tc.idx]
		if got.Title != tc.title {
			t.Errorf("row %d: title %q, want %q", tc.idx, got.Title, tc.title)
		}
		if got.Path != tc.path {
			t.Errorf("row %d: path %q, want %q", tc.idx, got.Path, tc.path)
		}
		if got.State != tc.state {
			t.Errorf("row %d: state %q, want %q", tc.idx, got.State, tc.state)
		}
		if got.ClosedAt != tc.closedAt {
			t.Errorf("row %d: closed_at %q, want %q", tc.idx, got.ClosedAt, tc.closedAt)
		}
		if got.PendingNote != tc.pendingNote {
			t.Errorf("row %d: pending note %q, want %q", tc.idx, got.PendingNote, tc.pendingNote)
		}
		if got.Count != tc.count {
			t.Errorf("row %d: count %d, want %d", tc.idx, got.Count, tc.count)
		}
		if got.StateRaw == "" {
			t.Errorf("row %d: the raw state cell must always be preserved", tc.idx)
		}
		if got.What == "" {
			t.Errorf("row %d: the description cell must be preserved", tc.idx)
		}
	}

	// An unrecognised state keeps its raw cell so nothing downstream guesses.
	if tasks[4].StateRaw != "En veremos" {
		t.Fatalf("unknown state must be preserved verbatim, got %q", tasks[4].StateRaw)
	}
}

func TestParseProjectReadmeIgnoresTablesOutsideTheTaskMap(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "README.md")
	writeFile(t, path, `# Vault

## Instancias

| Instancia | Rol |
| --- | --- |
| [otra](./koi-garden/OTRA-1-x/README.md) | no es una tarea |

## Mapa de tareas

| Tarea | Qué es | Estado | Archivos |
| --- | --- | --- | --- |
| [Real](./koi-garden/KOI-1-real/README.md) | la única | Histórico | 2 |

## Proyectos

| Proyecto | Rol |
| --- | --- |
| [tampoco](./x/Y-1-z/README.md) | fuera de la tabla |
`)

	tasks, err := ParseProjectReadme(path)
	if err != nil {
		t.Fatalf("ParseProjectReadme: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected only the task map rows, got %d: %+v", len(tasks), tasks)
	}
	if tasks[0].Path != "koi-garden/KOI-1-real" {
		t.Fatalf("unexpected path %q", tasks[0].Path)
	}
}

func TestParseProjectReadmeFailsWithoutTheTaskMap(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "README.md")
	writeFile(t, path, "# Vault\n\nSin tabla.\n")

	if _, err := ParseProjectReadme(path); err == nil {
		t.Fatal("expected an error when the task map is absent")
	}
}

func TestParseTaskDirNameSplitsTicketAndSlug(t *testing.T) {
	cases := []struct {
		name     string
		jiraKey  string
		slug     string
		wantName string
	}{
		{name: "KOI-1042-hardening-autologin", jiraKey: "KOI-1042", slug: "hardening-autologin"},
		{name: "TSU-204-migracion-thumbnails", jiraKey: "TSU-204", slug: "migracion-thumbnails"},
		{name: "CDBS-10555-hardening-autologin", jiraKey: "CDBS-10555", slug: "hardening-autologin"},
		{name: "split-pond-filesharing", jiraKey: "", slug: "split-pond-filesharing"},
		{name: "mantenimiento-del-estanque", jiraKey: "", slug: "mantenimiento-del-estanque"},
		{name: "koi-1042-minusculas", jiraKey: "", slug: "koi-1042-minusculas"},
		{name: "KOI-1042", jiraKey: "", slug: "KOI-1042"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			jira, slug := ParseTaskDirName(tc.name)
			if jira != tc.jiraKey || slug != tc.slug {
				t.Fatalf("got (%q, %q), want (%q, %q)", jira, slug, tc.jiraKey, tc.slug)
			}
		})
	}
}
