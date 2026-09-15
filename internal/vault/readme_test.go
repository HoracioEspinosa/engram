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

// realShapedReadme mirrors the shape of the root README a real vault keeps: a
// "Mapa de tareas" section split into one subheading and one table per project,
// rows whose first cell is a link whose text leads with the ticket, rows with
// no ticket at all, and state cells that continue past the state word.
const realShapedReadme = "# Vault de conocimiento\n" + `
## Mapa de tareas

### ` + "`koi-garden/`" + ` — ingeniería del estanque

| Tarea | Qué resuelve | Estado | Archivos |
|---|---|---|---|
| [KOI-1042 — hardening autologin](./koi-garden/KOI-1042-hardening-autologin/README.md) | Endurece la cadena de autologin. | Cerrado (2026-08-14) | 11 |
| [KOI-1099 — timeout de lookup](./koi-garden/KOI-1099-lookup-timeout/README.md) | Mitiga el timeout al buscar entre estanques. | Con pendientes — falta validar en pond-02 | 12 |
| [mantenimiento del estanque](./koi-garden/mantenimiento-del-estanque/README.md) | Bitácora de tareas puntuales. | Histórico | 3 |
| [split del filesharing](./koi-garden/split-pond-filesharing/README.md) | Evalúa separar el filesharing. | Sin confirmar | 1 |

### ` + "`tsukimi-bridge/`" + ` — puente de miniaturas

| Tarea | Qué resuelve | Estado | Archivos |
|---|---|---|---|
| [TSU-204 — migración de miniaturas](./tsukimi-bridge/TSU-204-migracion-thumbnails/README.md) | Migra la generación de miniaturas. | Cerrado (2-jul-2026) | 6 |
| [TSU-311 — reintento de subida](./tsukimi-bridge/TSU-311-reintento-subida/README.md) | Reintenta la subida fallida. | Histórico — el pipeline viejo ya no existe | 4 |
| [catálogo de miniaturas](./tsukimi-bridge/catalogo-de-miniaturas/README.md) | Inventario de formatos. | Sin confirmar cuál corre hoy en producción | 2 |

## Cómo encontrar algo
`

// TestParseProjectReadmeCorrelatesRowsByLinkPath pins the correlation key: the
// folder the row's link points at, never the prose of its text. A row is only
// useful to an importer if it names the task folder the importer is walking.
func TestParseProjectReadmeCorrelatesRowsByLinkPath(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "README.md")
	writeFile(t, path, realShapedReadme)

	tasks, err := ParseProjectReadme(path)
	if err != nil {
		t.Fatalf("ParseProjectReadme: %v", err)
	}
	if len(tasks) != 7 {
		t.Fatalf("expected 7 rows across the two projects, got %d: %+v", len(tasks), tasks)
	}

	cases := []struct {
		project     string
		dir         string
		jiraKey     string
		title       string
		state       string
		closedAt    string
		pendingNote string
		count       int
	}{
		{project: "koi-garden", dir: "KOI-1042-hardening-autologin", jiraKey: "KOI-1042",
			title: "hardening autologin", state: StateDone, closedAt: "2026-08-14", count: 11},
		{project: "koi-garden", dir: "KOI-1099-lookup-timeout", jiraKey: "KOI-1099",
			title: "timeout de lookup", state: StatePending,
			pendingNote: "falta validar en pond-02", count: 12},
		{project: "koi-garden", dir: "mantenimiento-del-estanque",
			title: "mantenimiento del estanque", state: StateArchived, count: 3},
		{project: "koi-garden", dir: "split-pond-filesharing",
			title: "split del filesharing", state: StateUnverified, count: 1},
		{project: "tsukimi-bridge", dir: "TSU-204-migracion-thumbnails", jiraKey: "TSU-204",
			title: "migración de miniaturas", state: StateDone, closedAt: "2-jul-2026", count: 6},
		{project: "tsukimi-bridge", dir: "TSU-311-reintento-subida", jiraKey: "TSU-311",
			title: "reintento de subida", state: StateArchived, count: 4},
		{project: "tsukimi-bridge", dir: "catalogo-de-miniaturas",
			title: "catálogo de miniaturas", state: StateUnverified, count: 2},
	}
	for i, tc := range cases {
		got := tasks[i]
		if got.Project != tc.project {
			t.Errorf("row %d: project %q, want %q", i, got.Project, tc.project)
		}
		if got.Dir != tc.dir {
			t.Errorf("row %d: dir %q, want %q", i, got.Dir, tc.dir)
		}
		if want := tc.project + "/" + tc.dir; got.Path != want {
			t.Errorf("row %d: path %q, want %q", i, got.Path, want)
		}
		if got.JiraKey != tc.jiraKey {
			t.Errorf("row %d: jira key %q, want %q", i, got.JiraKey, tc.jiraKey)
		}
		if got.Title != tc.title {
			t.Errorf("row %d: title %q, want %q", i, got.Title, tc.title)
		}
		if got.State != tc.state {
			t.Errorf("row %d: state %q, want %q", i, got.State, tc.state)
		}
		if got.ClosedAt != tc.closedAt {
			t.Errorf("row %d: closed_at %q, want %q", i, got.ClosedAt, tc.closedAt)
		}
		if got.PendingNote != tc.pendingNote {
			t.Errorf("row %d: pending note %q, want %q", i, got.PendingNote, tc.pendingNote)
		}
		if got.StateRaw == "" {
			t.Errorf("row %d: the raw state cell must always be preserved", i)
		}
		if got.What == "" {
			t.Errorf("row %d: the description cell must be preserved", i)
		}
		if got.Count != tc.count {
			t.Errorf("row %d: count %d, want %d", i, got.Count, tc.count)
		}
	}
}

// TestParseProjectReadmeHandlesProjectSubheadings pins where the task map ends.
// The section is split by subheadings and interrupted by prose, neither of
// which closes it; the next section at the map's own heading level does, so the
// lookup tables below it never turn into tasks.
func TestParseProjectReadmeHandlesProjectSubheadings(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "README.md")
	writeFile(t, path, "# Vault\n"+`
## Mapa de tareas

### `+"`koi-garden/`"+` — el estanque

| Tarea | Qué resuelve | Estado | Archivos |
|---|---|---|---|
| [KOI-1042 — hardening autologin](./KOI-1042-hardening-autologin/README.md) | Endurece. | Cerrado (2026-08-14) | 11 |

### `+"`tsukimi-bridge/`"+` — el puente

Cada tarea del puente vive en su propia carpeta.

| Tarea | Qué resuelve | Estado | Archivos |
|---|---|---|---|
| [TSU-204 — migración](./tsukimi-bridge/TSU-204-migracion-thumbnails/README.md) | Migra. | Histórico | 6 |

## Cómo encontrar algo

### Por tema

| Síntoma | Ruta | Nota |
|---|---|---|
| [KOI-9999 — trampa](./koi-garden/KOI-9999-trampa/README.md) | autologin, JWT | Sin confirmar |
`)

	tasks, err := ParseProjectReadme(path)
	if err != nil {
		t.Fatalf("ParseProjectReadme: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected only the task map rows, got %d: %+v", len(tasks), tasks)
	}
	// The first link names no project, so the subheading above it supplies one.
	if tasks[0].Path != "koi-garden/KOI-1042-hardening-autologin" {
		t.Errorf("row 0: path %q, want the project from its subheading", tasks[0].Path)
	}
	if tasks[1].Path != "tsukimi-bridge/TSU-204-migracion-thumbnails" {
		t.Errorf("row 1: path %q", tasks[1].Path)
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
