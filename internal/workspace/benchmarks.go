package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/vault"
)

// benchmarkMapName is the file that sits beside a harness's own runs and says
// which of their numbers are metrics.
const benchmarkMapName = "benchmark_map.json"

// benchmarkSourceJSON is the provenance every measurement this package records
// carries: it came out of a run file, not out of somebody typing it. It
// mirrors the benchmarks table's source enum, which the store does not export.
const benchmarkSourceJSON = "json"

// ImportReport is the outcome of reading one benchmark run into measurements.
type ImportReport struct {
	Project    string `json:"project"`
	TaskSyncID string `json:"task_sync_id"`
	// Format is engram.benchmark.v1 or foreign.
	Format string `json:"format"`
	// Name is the run's name, which every measurement from it carries.
	Name string `json:"name"`
	// RunPath is the file the numbers came from.
	RunPath string `json:"run_path"`
	DryRun  bool   `json:"dry_run"`
	// Imported counts the measurements written, Duplicates the ones already
	// recorded from the same run.
	Imported   int    `json:"imported"`
	Duplicates int    `json:"duplicates"`
	Skipped    []Skip `json:"skipped"`
	// Metrics names what was read, in file order.
	Metrics []string `json:"metrics"`
	// DemotedBaselines are the sync ids of measurements that lost the
	// baseline flag to one of these.
	DemotedBaselines []string `json:"demoted_baselines"`
}

// ImportBenchmarks reads one run file into a task's measurements.
//
// A run carrying the engram.benchmark.v1 marker is read directly. Anything
// else is the harness's own output, and nothing guesses metrics out of a shape
// it does not recognise: it needs a pointer map, given by the caller or found
// as benchmark_map.json beside the run. Without one the run stays what it
// already is — an attached file — and the caller is told why.
func ImportBenchmarks(s *store.Store, taskRef, runPath string, mapping []vault.PointerMap, baseline, dryRun bool) (ImportReport, error) {
	task, err := ResolveTask(s, taskRef)
	if err != nil {
		return ImportReport{}, err
	}

	runPath = strings.TrimSpace(runPath)
	restricted, err := vault.IsRestricted(runPath, vault.DefaultRestrictedRoots())
	if err != nil {
		return ImportReport{}, err
	}
	if restricted {
		return ImportReport{}, fmt.Errorf("%w: %s", ErrRestrictedPath, runPath)
	}
	info, err := os.Stat(runPath)
	if err != nil || info.IsDir() {
		return ImportReport{}, fmt.Errorf("%w: %s", ErrRunNotFound, runPath)
	}

	report := ImportReport{
		Project:          task.Project,
		TaskSyncID:       task.SyncID,
		RunPath:          runPath,
		DryRun:           dryRun,
		Skipped:          []Skip{},
		Metrics:          []string{},
		DemotedBaselines: []string{},
	}

	run, err := vault.ParseRunJSON(runPath)
	if err != nil {
		return ImportReport{}, err
	}
	report.Format = run.Format

	var (
		metrics     []vault.Metric
		name        string
		capturedAt  string
		configStamp *string
		notes       *string
		isBaseline  = baseline
	)

	switch run.Format {
	case vault.FormatV1:
		metrics = run.Run.Metrics
		name = strings.TrimSpace(run.Run.Name)
		capturedAt = run.Run.CapturedAt
		configStamp = optional(run.Run.ConfigStamp)
		notes = optional(run.Run.Notes)
		isBaseline = baseline || run.Run.Baseline
	default:
		mapping, err = pointerMapFor(runPath, mapping)
		if err != nil {
			return ImportReport{}, err
		}
		raw, err := os.ReadFile(runPath)
		if err != nil {
			return ImportReport{}, fmt.Errorf("engram-workspace: read %s: %w", runPath, err)
		}
		metrics, err = vault.ExtractMetrics(raw, mapping)
		if err != nil {
			if errors.Is(err, vault.ErrPointerUnresolved) {
				return ImportReport{}, fmt.Errorf("%w: %s", ErrPointerUnresolved, err)
			}
			return ImportReport{}, err
		}
		capturedAt = info.ModTime().UTC().Format(time.RFC3339)
	}

	if name == "" {
		name = strings.TrimSuffix(filepath.Base(runPath), filepath.Ext(runPath))
	}
	report.Name = name

	// The run is recorded relative to the vault so the same measurement
	// registered from two machines carries the same provenance. A vault root
	// nobody declared is not an error here: the file still has a name.
	vaultPath, _ := VaultRoot(s, "", task.Project)
	relPath := runPathFor(vaultPath, runPath)

	for _, m := range metrics {
		report.Metrics = append(report.Metrics, m.Metric)
		if dryRun {
			known, err := benchmarkExists(s, task.SyncID, name, m.Metric, capturedAt)
			if err != nil {
				return ImportReport{}, err
			}
			if known {
				report.Duplicates++
				continue
			}
			report.Imported++
			continue
		}
		result, err := s.AddBenchmark(store.AddBenchmarkParams{
			Task:        task,
			Name:        name,
			Metric:      m.Metric,
			Unit:        m.Unit,
			Direction:   m.Direction,
			Value:       m.Value,
			Baseline:    isBaseline,
			RunPath:     &relPath,
			ConfigStamp: configStamp,
			CapturedAt:  capturedAt,
			Notes:       notes,
			Source:      benchmarkSourceJSON,
		})
		if err != nil {
			return ImportReport{}, fmt.Errorf("engram-workspace: record %s: %w", m.Metric, err)
		}
		if !result.Created {
			report.Duplicates++
			continue
		}
		report.Imported++
		if result.DemotedBaseline != nil {
			report.DemotedBaselines = append(report.DemotedBaselines, *result.DemotedBaseline)
		}
	}
	return report, nil
}

// pointerMapFor returns the mapping a foreign run is read with: the caller's,
// or the benchmark_map.json beside the run. Neither being there is
// ErrNotEngramBenchmarkV1 — the run is fine, there is just nothing saying
// which of its numbers mean anything.
func pointerMapFor(runPath string, mapping []vault.PointerMap) ([]vault.PointerMap, error) {
	if len(mapping) > 0 {
		return mapping, nil
	}
	sibling := filepath.Join(filepath.Dir(runPath), benchmarkMapName)
	loaded, err := vault.LoadPointerMap(sibling)
	if err != nil {
		return nil, fmt.Errorf("%w: %s (no %s beside it either)",
			ErrNotEngramBenchmarkV1, runPath, benchmarkMapName)
	}
	return loaded, nil
}

// optional turns a blank string into a nil pointer, so a field the run left
// empty stays absent on the row instead of being stored as "".
func optional(v string) *string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return &v
}
