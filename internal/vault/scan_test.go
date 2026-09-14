package vault

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// writeFile creates path and every missing parent directory.
func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// newVault lays out a minimal <project>/<task>/<category> tree and returns its
// root. It mirrors the shape of the development fixtures without copying their
// binary blobs.
func newVault(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "README.md"), vaultReadme)
	writeFile(t, filepath.Join(root, "koi-garden", "README.md"),
		"# koi-garden (ficticio)\n\nPanel y API de un estanque de peces koi.\n")
	task := filepath.Join(root, "koi-garden", "KOI-1042-hardening-autologin")
	writeFile(t, filepath.Join(task, "README.md"), taskReadme)
	writeFile(t, filepath.Join(task, "analysis", "causa-raiz.md"), "# Causa raiz\n\ncuerpo\n")
	writeFile(t, filepath.Join(task, "evidences", "01-login-antes", "README.md"),
		"# Login falla con 401 antes del fix\n\ncaptura y respuesta cruda\n")
	writeFile(t, filepath.Join(task, "evidences", "01-login-antes", "01-pantalla-login.png"), "not-a-real-png")
	writeFile(t, filepath.Join(task, "evidences", "02-login-despues", "manifest.json"),
		`{"01-panel.png":{"proves":"el panel abre directo tras el fix","captured_at":"2026-08-14T10:00:00Z"}}`)
	writeFile(t, filepath.Join(task, "evidences", "02-login-despues", "01-panel.png"), "not-a-real-png")
	writeFile(t, filepath.Join(task, "evidences", "02-login-despues", "02-sin-readme.png"), "not-a-real-png")
	writeFile(t, filepath.Join(task, "patches", "autologin-token-ttl.patch"), "--- a\n+++ b\n")
	return root
}

const vaultReadme = `# Vault de conocimiento (ficticio)

## Mapa de tareas

| Tarea | Qué es | Estado | Archivos |
| --- | --- | --- | --- |
| [Hardening del autologin](./koi-garden/KOI-1042-hardening-autologin/README.md) | Endurecer el autologin | Cerrado (2026-08-14) | 11 |
| [Timeout de lookup](./koi-garden/KOI-1099-lookup-timeout/README.md) | Mitigar el timeout | Con pendientes — falta validar en la instancia 02 | 12 |
| [División del filesharing](./koi-garden/split-pond-filesharing/README.md) | Evaluar la separación | Sin confirmar | 3 |
| [Mantenimiento del estanque](./koi-garden/mantenimiento-del-estanque/README.md) | Bitácora | Histórico | 3 |
| [Migración de miniaturas](./tsukimi-bridge/TSU-204-migracion-thumbnails/README.md) | Migrar miniaturas | En veremos | 6 |

## Proyectos
`

const taskReadme = `# KOI-1042 — Hardening del autologin

**Estado:** Cerrado (2026-08-14)
**Proyecto:** koi-garden

Endurecer el autologin de koi-garden tras detectar que una parte de los
usuarios con sesión recordada caía en la pantalla de acceso.

## Contenido
`

func TestScanRejectsRestrictedRealpath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevated privileges on windows")
	}
	root := newVault(t)
	restricted := t.TempDir()
	writeFile(t, filepath.Join(restricted, "secreto.txt"), "credenciales")

	// A symlink inside the vault pointing at a directory under a restricted
	// root. Its own path shares no prefix with the restricted root, so only a
	// realpath comparison can catch it.
	link := filepath.Join(root, "koi-garden", "KOI-1042-hardening-autologin", "evidences", "03-fuga")
	if err := os.Symlink(restricted, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	project, err := ScanProject(root, "koi-garden", Options{Restricted: []string{restricted}})
	if err != nil {
		t.Fatalf("ScanProject: %v", err)
	}
	if len(project.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(project.Tasks))
	}

	var skipped *File
	for i := range project.Tasks[0].Files {
		f := &project.Tasks[0].Files[i]
		if strings.Contains(f.RelPath, "03-fuga") {
			skipped = f
		}
		if strings.Contains(f.RelPath, "secreto.txt") {
			t.Fatalf("restricted file leaked into the scan: %s", f.RelPath)
		}
	}
	if skipped == nil {
		t.Fatal("the restricted symlink was not reported as a skipped entry")
	}
	if !skipped.Skipped || skipped.SkipReason != SkipRestricted {
		t.Fatalf("expected a %q skip, got skipped=%v reason=%q", SkipRestricted, skipped.Skipped, skipped.SkipReason)
	}
	if skipped.SHA256 != "" {
		t.Fatalf("a restricted entry must not be hashed, got %q", skipped.SHA256)
	}

	var warned bool
	for _, w := range project.Warnings {
		if strings.Contains(w, "03-fuga") {
			warned = true
		}
	}
	if !warned {
		t.Fatalf("expected a warning naming the restricted entry, got %v", project.Warnings)
	}
}

func TestScanSkipsOversizeWithoutHashing(t *testing.T) {
	root := newVault(t)
	task := filepath.Join(root, "koi-garden", "KOI-1042-hardening-autologin")
	writeFile(t, filepath.Join(task, "exports", "dump.csv"), strings.Repeat("a", 4096))

	files, err := ScanCategory(task, CategoryExports, Options{MaxBytes: 16})
	if err != nil {
		t.Fatalf("ScanCategory: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	got := files[0]
	if !got.Skipped || got.SkipReason != SkipOversize {
		t.Fatalf("expected a %q skip, got skipped=%v reason=%q", SkipOversize, got.Skipped, got.SkipReason)
	}
	if got.SHA256 != "" {
		t.Fatalf("an oversize file must not be hashed, got %q", got.SHA256)
	}
	if got.Size != 4096 {
		t.Fatalf("size must still be reported, got %d", got.Size)
	}

	// The same file under a generous budget is hashed.
	files, err = ScanCategory(task, CategoryExports, Options{})
	if err != nil {
		t.Fatalf("ScanCategory: %v", err)
	}
	if files[0].Skipped || files[0].SHA256 == "" {
		t.Fatalf("expected the file to be hashed under the default budget, got %+v", files[0])
	}
}

func TestScanIsIdempotent(t *testing.T) {
	root := newVault(t)
	first, err := ScanProject(root, "koi-garden", Options{})
	if err != nil {
		t.Fatalf("first ScanProject: %v", err)
	}
	second, err := ScanProject(root, "koi-garden", Options{})
	if err != nil {
		t.Fatalf("second ScanProject: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("two scans of the same tree disagree:\nfirst:  %+v\nsecond: %+v", first, second)
	}
}

func TestScanDerivesProvesFromManifestThenReadmeThenPath(t *testing.T) {
	root := newVault(t)
	task := filepath.Join(root, "koi-garden", "KOI-1042-hardening-autologin")

	files, err := ScanCategory(task, CategoryEvidences, Options{})
	if err != nil {
		t.Fatalf("ScanCategory: %v", err)
	}
	byPath := map[string]File{}
	for _, f := range files {
		byPath[f.RelPath] = f
		if f.Proves == "" {
			t.Fatalf("proves must never be empty, %s has none", f.RelPath)
		}
	}

	manifested := byPath["evidences/02-login-despues/01-panel.png"]
	if manifested.Proves != "el panel abre directo tras el fix" {
		t.Fatalf("expected the manifest entry to win, got %q", manifested.Proves)
	}

	fromReadme := byPath["evidences/01-login-antes/01-pantalla-login.png"]
	if fromReadme.Proves != "Login falla con 401 antes del fix" {
		t.Fatalf("expected the sibling README H1, got %q", fromReadme.Proves)
	}

	fromPath := byPath["evidences/02-login-despues/02-sin-readme.png"]
	if fromPath.Proves != "evidences/02-login-despues/02-sin-readme.png" {
		t.Fatalf("expected the relative path fallback, got %q", fromPath.Proves)
	}
}

func TestScanTaskReadsTheReadmeHeader(t *testing.T) {
	root := newVault(t)
	task, err := ScanTask(root, "koi-garden", "KOI-1042-hardening-autologin", Options{})
	if err != nil {
		t.Fatalf("ScanTask: %v", err)
	}
	if task.JiraKey != "KOI-1042" || task.Slug != "hardening-autologin" {
		t.Fatalf("unexpected identity: jira=%q slug=%q", task.JiraKey, task.Slug)
	}
	if task.Title != "KOI-1042 — Hardening del autologin" {
		t.Fatalf("unexpected title: %q", task.Title)
	}
	if task.State != StateDone {
		t.Fatalf("expected state %q, got %q", StateDone, task.State)
	}
	if !strings.HasPrefix(task.Summary, "Endurecer el autologin") {
		t.Fatalf("unexpected summary: %q", task.Summary)
	}
	if len(task.Files) == 0 {
		t.Fatal("expected the task scan to collect files from every category")
	}
}

func TestScanCategoryStopsAtTheDepthLimit(t *testing.T) {
	root := t.TempDir()
	task := filepath.Join(root, "koi-garden", "KOI-1")
	writeFile(t, filepath.Join(task, "evidences", "a", "b", "deep.png"), "x")
	writeFile(t, filepath.Join(task, "evidences", "a", "b", "c", "too-deep.png"), "x")

	files, err := ScanCategory(task, CategoryEvidences, Options{})
	if err != nil {
		t.Fatalf("ScanCategory: %v", err)
	}
	var paths []string
	for _, f := range files {
		paths = append(paths, f.RelPath)
	}
	want := []string{"evidences/a/b/deep.png"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("expected %v, got %v", want, paths)
	}
}

func TestScanCategorySkipsNoiseDirectories(t *testing.T) {
	root := t.TempDir()
	task := filepath.Join(root, "koi-garden", "KOI-1")
	writeFile(t, filepath.Join(task, "scripts", "run.sh"), "#!/bin/sh\n")
	writeFile(t, filepath.Join(task, "scripts", ".DS_Store"), "junk")
	writeFile(t, filepath.Join(task, "scripts", "node_modules", "dep.js"), "x")
	writeFile(t, filepath.Join(task, "scripts", "_borrador", "old.sh"), "x")
	writeFile(t, filepath.Join(task, "scripts", ".git", "HEAD"), "ref: x")

	files, err := ScanCategory(task, CategoryScripts, Options{})
	if err != nil {
		t.Fatalf("ScanCategory: %v", err)
	}
	var paths []string
	for _, f := range files {
		paths = append(paths, f.RelPath)
	}
	want := []string{"scripts/run.sh"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("expected %v, got %v", want, paths)
	}
}

func TestScanCategoryReturnsNothingForAnAbsentCategory(t *testing.T) {
	root := newVault(t)
	task := filepath.Join(root, "koi-garden", "KOI-1042-hardening-autologin")
	files, err := ScanCategory(task, CategoryRunbooks, Options{})
	if err != nil {
		t.Fatalf("ScanCategory: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("expected no files, got %v", files)
	}
}

func TestScanRefusesToStartInsideARestrictedRoot(t *testing.T) {
	restricted := t.TempDir()
	writeFile(t, filepath.Join(restricted, "koi-garden", "KOI-1", "README.md"), "# secreto\n")

	if _, err := ScanProject(restricted, "koi-garden", Options{Restricted: []string{restricted}}); !errors.Is(err, ErrRestrictedPath) {
		t.Fatalf("expected ErrRestrictedPath, got %v", err)
	}
	if _, err := ScanTask(restricted, "koi-garden", "KOI-1", Options{Restricted: []string{restricted}}); !errors.Is(err, ErrRestrictedPath) {
		t.Fatalf("expected ErrRestrictedPath, got %v", err)
	}
}

func TestScanProjectWarnsAboutAnUnreadableTask(t *testing.T) {
	root := newVault(t)
	// A task entry that is a file rather than a directory cannot be scanned.
	writeFile(t, filepath.Join(root, "koi-garden", "KOI-9-suelto"), "no soy una carpeta")
	project, err := ScanProject(root, "koi-garden", Options{})
	if err != nil {
		t.Fatalf("ScanProject: %v", err)
	}
	if len(project.Tasks) != 1 {
		t.Fatalf("expected the readable task only, got %d", len(project.Tasks))
	}
	if project.Title != "koi-garden (ficticio)" {
		t.Fatalf("unexpected project title: %q", project.Title)
	}
	if project.Description == "" {
		t.Fatal("the project README's first paragraph must become the description")
	}
}

func TestScanCategoryIgnoresACategoryThatIsAFile(t *testing.T) {
	root := t.TempDir()
	task := filepath.Join(root, "koi-garden", "KOI-1")
	writeFile(t, filepath.Join(task, "assets"), "no soy una carpeta")
	files, err := ScanCategory(task, CategoryAssets, Options{})
	if err != nil {
		t.Fatalf("ScanCategory: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("expected no files, got %v", files)
	}
}

func TestRootPrefersTheExplicitArgumentThenHubThenEnv(t *testing.T) {
	cases := []struct {
		name     string
		explicit string
		hub      string
		env      string
		want     string
		wantErr  bool
	}{
		{name: "explicit wins", explicit: "/a", hub: "/b", env: "/c", want: "/a"},
		{name: "hub is second", hub: "/b", env: "/c", want: "/b"},
		{name: "env is last", env: "/c", want: "/c"},
		{name: "blanks do not count", explicit: "  ", hub: "\t", env: "/c", want: "/c"},
		{name: "nothing resolves", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Root(tc.explicit, tc.hub, tc.env)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Root: %v", err)
			}
			if got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func TestIsRestrictedComparesRealPaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevated privileges on windows")
	}
	restricted := t.TempDir()
	writeFile(t, filepath.Join(restricted, "secreto.txt"), "x")
	outside := t.TempDir()
	link := filepath.Join(outside, "atajo")
	if err := os.Symlink(restricted, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	yes, err := IsRestricted(filepath.Join(link, "secreto.txt"), []string{restricted})
	if err != nil {
		t.Fatalf("IsRestricted: %v", err)
	}
	if !yes {
		t.Fatal("a symlink into a restricted root must be restricted")
	}

	no, err := IsRestricted(filepath.Join(outside, "libre.txt"), []string{restricted})
	if err != nil {
		t.Fatalf("IsRestricted: %v", err)
	}
	if no {
		t.Fatal("a path outside every restricted root must not be restricted")
	}

	// A sibling whose name merely starts with the restricted root's name is not
	// inside it: prefix comparison would say otherwise.
	sibling := restricted + "-publico"
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	near, err := IsRestricted(sibling, []string{restricted})
	if err != nil {
		t.Fatalf("IsRestricted: %v", err)
	}
	if near {
		t.Fatalf("%s is a sibling of the restricted root, not a child", sibling)
	}
}
