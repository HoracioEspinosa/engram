package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/store"
	_ "modernc.org/sqlite"
)

// newStore opens a throwaway engram database with the projects schema on it.
func newStore(t *testing.T) *store.Store {
	t.Helper()
	cfg, err := store.DefaultConfig()
	if err != nil {
		t.Fatalf("DefaultConfig: %v", err)
	}
	cfg.DataDir = t.TempDir()
	s, err := store.New(cfg)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// writeFile creates path and every directory above it.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// newVault builds a knowledge tree shaped like docker/dev/fixtures/vault:
// <project>/<task>/<category>, a root README whose "Mapa de tareas" table
// carries the states, a Runbooks/ tree that is not a project, and both
// benchmark formats. It is built in code rather than copied so a test can bend
// one corner of it without a fixture file drifting away from what it asserts.
func newVault(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	writeFile(t, filepath.Join(root, "README.md"), `# Vault de conocimiento

## Mapa de tareas

| Tarea | Qué es | Estado | Archivos |
| --- | --- | --- | --- |
| [Hardening del autologin](./koi-garden/KOI-1042-hardening-autologin/README.md) | Endurecer el autologin | Cerrado (2026-08-14) | 3 |
| [Timeout de lookup](./koi-garden/KOI-1099-lookup-timeout/README.md) | Mitigar el timeout de lookup | Con pendientes — falta validar en pond-02 | 4 |
| [División del filesharing](./koi-garden/split-pond-filesharing/README.md) | Separar el filesharing | Sin confirmar | 1 |
| [Mantenimiento](./koi-garden/mantenimiento-del-estanque/README.md) | Bitácora de tareas puntuales | Histórico | 1 |
| [Migración de miniaturas](./tsukimi-bridge/TSU-204-migracion-thumbnails/README.md) | Migrar las miniaturas | Con pendientes | 2 |

## Proyectos

- koi-garden
- tsukimi-bridge
`)

	writeFile(t, filepath.Join(root, "koi-garden", "README.md"), "# koi-garden\n\nPanel de un estanque.\n")
	writeFile(t, filepath.Join(root, "tsukimi-bridge", "README.md"), "# tsukimi-bridge\n\nPuente de miniaturas.\n")

	// KOI-1042: analysis, a patch and two evidence bundles.
	koi1042 := filepath.Join(root, "koi-garden", "KOI-1042-hardening-autologin")
	writeFile(t, filepath.Join(koi1042, "README.md"),
		"# KOI-1042 — Hardening del autologin\n\n**Estado:** Cerrado (2026-08-14)\n\nEndurecer el autologin tras el incidente.\n")
	writeFile(t, filepath.Join(koi1042, "analysis", "causa-raiz.md"), "# Causa raíz\n\nEl token no expiraba.\n")
	writeFile(t, filepath.Join(koi1042, "patches", "autologin-token-ttl.patch"), "--- a/token.go\n+++ b/token.go\n")
	writeFile(t, filepath.Join(koi1042, "evidences", "01-login-antes", "captura.png"), "\x89PNG antes")

	// KOI-1099: both benchmark formats plus the map that reads the foreign one.
	koi1099 := filepath.Join(root, "koi-garden", "KOI-1099-lookup-timeout")
	writeFile(t, filepath.Join(koi1099, "README.md"),
		"# KOI-1099 — Timeout de lookup\n\n**Estado:** Con pendientes — falta validar en pond-02\n\nEl lookup falla con timeout.\n")
	writeFile(t, filepath.Join(koi1099, "analysis", "hipotesis.md"), "# Hipótesis\n\nEl cliente no fija timeout.\n")
	writeFile(t, filepath.Join(koi1099, "benchmarks", "baseline-run1.json"), `{
  "engram_benchmark": "v1",
  "task": "KOI-1099",
  "name": "lookup-timeout",
  "captured_at": "2026-08-20T10:00:00Z",
  "config_stamp": "pond-02 / cache off",
  "baseline": true,
  "notes": "corrida base",
  "metrics": [
    { "metric": "lookup.p95", "unit": "ms", "value": 1512 },
    { "metric": "lookup.errors", "unit": "count", "value": 3 }
  ]
}`)
	writeFile(t, filepath.Join(koi1099, "benchmarks", "harness-run.json"), `{
  "wallClockMs": 942,
  "scenarios": { "warm": { "total": { "ops": { "resolve": 2 } } } }
}`)
	writeFile(t, filepath.Join(koi1099, "benchmarks", benchmarkMapName), `[
  { "metric": "warm.resolve", "unit": "count", "pointer": "/scenarios/warm/total/ops/resolve" },
  { "metric": "cold.wallClockMs", "unit": "ms", "pointer": "/wallClockMs" }
]`)

	// Two task folders with no ticket at all: one refactor, one catch-all.
	split := filepath.Join(root, "koi-garden", "split-pond-filesharing")
	writeFile(t, filepath.Join(split, "README.md"),
		"# División del filesharing\n\n**Estado:** Sin confirmar\n\nEvaluar separar el filesharing.\n")
	writeFile(t, filepath.Join(split, "analysis", "alcance.md"), "# Alcance\n\nQué se separa.\n")

	mant := filepath.Join(root, "koi-garden", "mantenimiento-del-estanque")
	writeFile(t, filepath.Join(mant, "README.md"), "# Mantenimiento del estanque\n\nBitácora de tareas puntuales.\n")
	writeFile(t, filepath.Join(mant, "reports", "bitacora.md"), "# Bitácora\n\nLimpieza de caché.\n")

	tsu := filepath.Join(root, "tsukimi-bridge", "TSU-204-migracion-thumbnails")
	writeFile(t, filepath.Join(tsu, "README.md"),
		"# TSU-204 — Migración de miniaturas\n\n**Estado:** Con pendientes\n\nMigrar la generación de miniaturas.\n")
	writeFile(t, filepath.Join(tsu, "plans", "migracion.md"), "# Plan\n\nLotes de 100.\n")
	writeFile(t, filepath.Join(tsu, "benchmarks", "baseline-run1.json"), `{
  "engram_benchmark": "v1",
  "task": "TSU-204",
  "name": "thumbnail-generation",
  "captured_at": "2026-09-05T11:30:00Z",
  "baseline": true,
  "metrics": [ { "metric": "thumbnail.batch_ms", "unit": "ms", "value": 8420 } ]
}`)

	// Not a project: the runbook tree the vault keeps beside them, whose
	// folders hold no README and therefore no tasks.
	writeFile(t, filepath.Join(root, "Runbooks", "auth", "RB-001-autologin.md"), "# RB-001\n")
	// Not a task: the quarantine folder every vault carries.
	writeFile(t, filepath.Join(root, "koi-garden", "_cuarentena", "README.md"), "# Cuarentena\n")

	return root
}

// newRealShapedVault builds a tree whose root README carries the task map in
// the shape a real vault writes it: one subheading and one table per project,
// link texts that lead with the ticket, a task with no ticket, and state cells
// that continue past the state word. No task README carries an "**Estado:**"
// line, so every state an import reports can only have come from that table.
func newRealShapedVault(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	writeFile(t, filepath.Join(root, "README.md"), "# Vault de conocimiento\n"+`
## Mapa de tareas

### `+"`koi-garden/`"+` — ingeniería del estanque

| Tarea | Qué resuelve | Estado | Archivos |
|---|---|---|---|
| [KOI-1042 — hardening autologin](./koi-garden/KOI-1042-hardening-autologin/README.md) | Endurece la cadena de autologin. | Cerrado (2026-08-14) | 3 |
| [KOI-1099 — timeout de lookup](./koi-garden/KOI-1099-lookup-timeout/README.md) | Mitiga el timeout de lookup. | Con pendientes — falta validar en pond-02 | 2 |
| [mantenimiento del estanque](./koi-garden/mantenimiento-del-estanque/README.md) | Bitácora de tareas puntuales. | Histórico — el estanque viejo ya no existe | 1 |

### `+"`tsukimi-bridge/`"+` — puente de miniaturas

| Tarea | Qué resuelve | Estado | Archivos |
|---|---|---|---|
| [TSU-204 — migración de miniaturas](./tsukimi-bridge/TSU-204-migracion-thumbnails/README.md) | Migra la generación de miniaturas. | Sin confirmar cuál corre hoy en producción | 1 |

## Cómo encontrar algo

### Por ticket

| Ticket | Carpeta | Nota |
|---|---|---|
| [KOI-9999 — trampa](./koi-garden/KOI-9999-trampa/README.md) | autologin, JWT | Sin confirmar |
`)

	writeFile(t, filepath.Join(root, "koi-garden", "README.md"), "# koi-garden\n\nPanel de un estanque.\n")
	writeFile(t, filepath.Join(root, "tsukimi-bridge", "README.md"), "# tsukimi-bridge\n\nPuente de miniaturas.\n")

	koi1042 := filepath.Join(root, "koi-garden", "KOI-1042-hardening-autologin")
	writeFile(t, filepath.Join(koi1042, "README.md"), "# KOI-1042 — Hardening del autologin\n\nEndurecer el autologin.\n")
	writeFile(t, filepath.Join(koi1042, "analysis", "causa-raiz.md"), "# Causa raíz\n\nEl token no expiraba.\n")

	koi1099 := filepath.Join(root, "koi-garden", "KOI-1099-lookup-timeout")
	writeFile(t, filepath.Join(koi1099, "README.md"), "# KOI-1099 — Timeout de lookup\n\nEl lookup falla con timeout.\n")

	mant := filepath.Join(root, "koi-garden", "mantenimiento-del-estanque")
	writeFile(t, filepath.Join(mant, "README.md"), "# Mantenimiento del estanque\n\nBitácora de tareas puntuales.\n")

	tsu := filepath.Join(root, "tsukimi-bridge", "TSU-204-migracion-thumbnails")
	writeFile(t, filepath.Join(tsu, "README.md"), "# TSU-204 — Migración de miniaturas\n\nMigrar las miniaturas.\n")

	return root
}

// seedTask registers a task the way the store's own callers do, so a scan has
// a row to attach evidence to.
func seedTask(t *testing.T, s *store.Store, project, jiraKey, slug, vaultPath string) store.Task {
	t.Helper()
	params := store.UpsertTaskParams{Project: project}
	title := slug
	if title == "" {
		title = jiraKey
	}
	kind := "bugfix"
	params.Title = &title
	params.Kind = &kind
	if slug != "" {
		params.Slug = &slug
	}
	if vaultPath != "" {
		params.VaultPath = &vaultPath
	}
	if jiraKey != "" {
		params.JiraKey = &jiraKey
	}
	res, err := s.UpsertTask(params)
	if err != nil {
		t.Fatalf("UpsertTask(%s): %v", slug, err)
	}
	return res.Task
}
