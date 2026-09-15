package workspace

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/vault"
)

// TestImportBenchmarksV1AndForeignWithMap pins both halves of the import: a
// run carrying the marker is read directly, and the harness's own output is
// read only through the pointer map that sits beside it.
func TestImportBenchmarksV1AndForeignWithMap(t *testing.T) {
	s := newStore(t)
	root := newVault(t)
	t.Setenv(VaultRootEnv, root)
	task := seedTask(t, s, "koi-garden", "KOI-1099", "lookup-timeout", "koi-garden/KOI-1099-lookup-timeout")
	benchDir := filepath.Join(root, "koi-garden", "KOI-1099-lookup-timeout", "benchmarks")

	native, err := ImportBenchmarks(s, "KOI-1099", filepath.Join(benchDir, "baseline-run1.json"), nil, false, false)
	if err != nil {
		t.Fatalf("ImportBenchmarks(v1): %v", err)
	}
	if native.Format != vault.FormatV1 {
		t.Errorf("format %q, want %q", native.Format, vault.FormatV1)
	}
	if native.Imported != 2 {
		t.Errorf("imported %d metrics, want 2", native.Imported)
	}
	if native.Name != "lookup-timeout" {
		t.Errorf("name %q, want the run's own name", native.Name)
	}

	foreign, err := ImportBenchmarks(s, "KOI-1099", filepath.Join(benchDir, "harness-run.json"), nil, false, false)
	if err != nil {
		t.Fatalf("ImportBenchmarks(foreign): %v", err)
	}
	if foreign.Format != vault.FormatForeign {
		t.Errorf("format %q, want %q", foreign.Format, vault.FormatForeign)
	}
	if foreign.Imported != 2 {
		t.Errorf("imported %d metrics through the map, want 2", foreign.Imported)
	}

	page, err := s.ListBenchmarks(store.BenchmarkListFilter{Task: task.SyncID})
	if err != nil {
		t.Fatalf("ListBenchmarks: %v", err)
	}
	if page.Total != 4 {
		t.Fatalf("store holds %d measurements, want 4", page.Total)
	}
}

// TestImportBenchmarksRefusesAForeignRunWithNoMap pins that nothing guesses
// metrics out of a shape it does not recognise.
func TestImportBenchmarksRefusesAForeignRunWithNoMap(t *testing.T) {
	s := newStore(t)
	root := newVault(t)
	t.Setenv(VaultRootEnv, root)
	seedTask(t, s, "koi-garden", "KOI-1099", "lookup-timeout", "koi-garden/KOI-1099-lookup-timeout")

	orphan := filepath.Join(t.TempDir(), "harness-run.json")
	writeFile(t, orphan, `{"wallClockMs": 942}`)

	_, err := ImportBenchmarks(s, "KOI-1099", orphan, nil, false, false)
	if !errors.Is(err, ErrNotEngramBenchmarkV1) {
		t.Fatalf("got %v, want ErrNotEngramBenchmarkV1", err)
	}
	if code := CodeOf(err); code != "not_engram_benchmark_v1" {
		t.Fatalf("CodeOf = %q, want not_engram_benchmark_v1", code)
	}
}

// TestImportBenchmarksReportsAStalePointer pins that a map that no longer
// matches its harness fails loudly instead of importing a shorter list than
// the user wrote.
func TestImportBenchmarksReportsAStalePointer(t *testing.T) {
	s := newStore(t)
	root := newVault(t)
	t.Setenv(VaultRootEnv, root)
	seedTask(t, s, "koi-garden", "KOI-1099", "lookup-timeout", "koi-garden/KOI-1099-lookup-timeout")
	run := filepath.Join(root, "koi-garden", "KOI-1099-lookup-timeout", "benchmarks", "harness-run.json")

	_, err := ImportBenchmarks(s, "KOI-1099", run,
		[]vault.PointerMap{{Metric: "gone", Unit: "ms", Pointer: "/nope/missing"}}, false, false)
	if !errors.Is(err, ErrPointerUnresolved) {
		t.Fatalf("got %v, want ErrPointerUnresolved", err)
	}
	if code := CodeOf(err); code != "pointer_unresolved" {
		t.Fatalf("CodeOf = %q, want pointer_unresolved", code)
	}
}

// TestImportBenchmarksMissingRunCarriesItsCode pins the failure for a run file
// that is not there.
func TestImportBenchmarksMissingRunCarriesItsCode(t *testing.T) {
	s := newStore(t)
	root := newVault(t)
	t.Setenv(VaultRootEnv, root)
	seedTask(t, s, "koi-garden", "KOI-1099", "lookup-timeout", "koi-garden/KOI-1099-lookup-timeout")

	_, err := ImportBenchmarks(s, "KOI-1099", filepath.Join(root, "no-such-run.json"), nil, false, false)
	if !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("got %v, want ErrRunNotFound", err)
	}
	if code := CodeOf(err); code != "run_not_found" {
		t.Fatalf("CodeOf = %q, want run_not_found", code)
	}
}

// TestImportBenchmarksDryRunWritesNothing pins that the preview of an import
// is a preview.
func TestImportBenchmarksDryRunWritesNothing(t *testing.T) {
	s := newStore(t)
	root := newVault(t)
	t.Setenv(VaultRootEnv, root)
	task := seedTask(t, s, "koi-garden", "KOI-1099", "lookup-timeout", "koi-garden/KOI-1099-lookup-timeout")
	run := filepath.Join(root, "koi-garden", "KOI-1099-lookup-timeout", "benchmarks", "baseline-run1.json")

	report, err := ImportBenchmarks(s, "KOI-1099", run, nil, true, true)
	if err != nil {
		t.Fatalf("ImportBenchmarks: %v", err)
	}
	if report.Imported != 2 {
		t.Fatalf("planned %d metrics, want 2", report.Imported)
	}

	page, err := s.ListBenchmarks(store.BenchmarkListFilter{Task: task.SyncID})
	if err != nil {
		t.Fatalf("ListBenchmarks: %v", err)
	}
	if page.Total != 0 {
		t.Fatalf("dry run wrote %d measurements", page.Total)
	}
}
