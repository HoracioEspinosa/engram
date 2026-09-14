package workspace

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/vault"
)

// ScanReport is the outcome of a scan: what it registered, what it refused to
// read, and what it noticed but left for a different command.
type ScanReport struct {
	Project    string `json:"project"`
	TaskSyncID string `json:"task_sync_id"`
	// Root and TaskDir name the tree the scan walked, so a report that found
	// nothing still says where it looked.
	Root    string `json:"root"`
	TaskDir string `json:"task_dir"`
	DryRun  bool   `json:"dry_run"`
	// Added counts rows a scan created, Updated rows whose file merely moved
	// or was refiled. In a dry run they count what would happen.
	Added   int    `json:"added"`
	Updated int    `json:"updated"`
	Skipped []Skip `json:"skipped"`
	// BenchmarkCandidates are the JSON runs found under benchmarks/. They are
	// registered as evidence like any other file; the list is what tells the
	// caller there are numbers here worth importing as measurements.
	BenchmarkCandidates []string `json:"benchmark_candidates"`
	// TotalBytes is the size of everything the scan actually read.
	TotalBytes int64 `json:"total_bytes"`
}

// ScanEvidence walks a task's vault folder and registers what it finds as
// evidence, one category or all eleven.
//
// Dry run is the default everywhere this is reachable from, and it is a real
// dry run: the same walk, the same hashes, the same decisions, and not one
// write. The plan a caller approves is therefore the plan that runs, which is
// the only way "review before applying" means anything.
//
// Registration is idempotent by (task, sha256): the same bytes found under a
// new name update the row's path and category instead of duplicating it.
func ScanEvidence(s *store.Store, taskRef, category string, dryRun bool, o vault.Options) (ScanReport, error) {
	task, err := ResolveTask(s, taskRef)
	if err != nil {
		return ScanReport{}, err
	}

	categories, err := scanCategories(category)
	if err != nil {
		return ScanReport{}, err
	}

	root, err := VaultRoot(s, "", task.Project)
	if err != nil {
		return ScanReport{}, err
	}
	taskDir, err := TaskDir(root, task, o)
	if err != nil {
		return ScanReport{}, err
	}

	report := ScanReport{
		Project:             task.Project,
		TaskSyncID:          task.SyncID,
		Root:                root,
		TaskDir:             taskDir,
		DryRun:              dryRun,
		Skipped:             []Skip{},
		BenchmarkCandidates: []string{},
	}

	for _, c := range categories {
		files, err := vault.ScanCategory(taskDir, c, o)
		if err != nil {
			return ScanReport{}, err
		}
		if err := recordFiles(s, task, root, taskDir, files, dryRun, &report); err != nil {
			return ScanReport{}, err
		}
	}
	return report, nil
}

// scanCategories resolves the category argument: one named category, or the
// closed set of eleven when the caller named none.
func scanCategories(category string) ([]vault.Category, error) {
	if strings.TrimSpace(category) == "" {
		return vault.Categories(), nil
	}
	c, err := vault.ParseCategory(category)
	if err != nil {
		return nil, err
	}
	return []vault.Category{c}, nil
}

// recordFiles turns a category's scan into evidence rows, accumulating the
// report. It is shared with the vault import, which walks the whole tree once
// and hands the files over rather than scanning each task twice.
func recordFiles(s *store.Store, task store.Task, root, taskDir string, files []vault.File, dryRun bool, report *ScanReport) error {
	for _, f := range files {
		full := filepath.Join(taskDir, filepath.FromSlash(f.RelPath))
		inside, err := withinRoot(root, full)
		if err != nil {
			return err
		}
		if !inside {
			report.Skipped = append(report.Skipped, Skip{Path: f.RelPath, Reason: "path_escapes_vault"})
			continue
		}
		if f.Skipped {
			report.Skipped = append(report.Skipped, Skip{Path: f.RelPath, Reason: f.SkipReason})
			continue
		}
		if isBenchmarkRun(f) {
			report.BenchmarkCandidates = append(report.BenchmarkCandidates, f.RelPath)
		}

		report.TotalBytes += f.Size
		if dryRun {
			// The plan still has to distinguish a new row from one that only
			// moves, so the duplicate check runs; the write does not.
			if known, err := evidenceExists(s, task.SyncID, f.SHA256); err != nil {
				return err
			} else if known {
				report.Updated++
			} else {
				report.Added++
			}
			continue
		}

		size := f.Size
		capturedAt := f.ModTime.UTC().Format(time.RFC3339)
		_, duplicate, _, err := s.AddEvidence(store.AddEvidenceParams{
			Task:       task,
			Path:       vaultRelative(root, full),
			SHA256:     f.SHA256,
			Category:   string(f.Category),
			Kind:       string(f.Kind),
			Proves:     f.Proves,
			CapturedAt: &capturedAt,
			SizeBytes:  &size,
		})
		if err != nil {
			return fmt.Errorf("engram-workspace: register %s: %w", f.RelPath, err)
		}
		if duplicate {
			report.Updated++
			continue
		}
		report.Added++
	}
	return nil
}

// isBenchmarkRun reports whether a scanned file is a benchmark run worth
// offering to the import command. The map that explains a foreign run is not
// itself a run.
func isBenchmarkRun(f vault.File) bool {
	return f.Category == vault.CategoryBenchmarks &&
		f.Kind == vault.KindJSON &&
		!strings.EqualFold(filepath.Base(f.RelPath), "benchmark_map.json")
}

// evidenceExists reports whether a task already holds these exact bytes. It is
// the read half of AddEvidence's idempotency key, which a dry run needs in
// order to say "update" where an apply would.
func evidenceExists(s *store.Store, taskSyncID, sha string) (bool, error) {
	var one int
	err := s.DB().QueryRow(
		`SELECT 1 FROM evidence WHERE task_sync_id = ? AND sha256 = ? AND deleted_at IS NULL LIMIT 1`,
		taskSyncID, sha).Scan(&one)
	if isNoRows(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("engram-workspace: check evidence: %w", err)
	}
	return true, nil
}
