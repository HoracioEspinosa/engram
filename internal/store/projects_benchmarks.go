// Package store: task-scoped benchmarks.
//
// A benchmark is the number a change was argued with. Kept as an attached file
// it is unqueryable — the value, its unit and the run it came from all sit
// inside a blob — so "is this better than before" needs somebody to open two
// files and do the arithmetic. Here each measurement is a row, one of them per
// metric is the baseline, and the comparison is a read.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Page is a slice of results with the size of the whole set it came from, so a
// caller can render "showing 20 of 340" without a second query.
type Page[T any] struct {
	Items  []T `json:"items"`
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// Benchmark directions: which way a metric has to move to be an improvement.
const (
	BenchmarkDirectionLower  = "lower"
	BenchmarkDirectionHigher = "higher"
)

// ErrInvalidBenchmarkUnit is returned when a measurement names a unit outside
// the closed list. Refusing it is the point: a unit nobody agreed on makes two
// numbers incomparable while still looking like a comparison.
var ErrInvalidBenchmarkUnit = errors.New("invalid benchmark unit")

// benchmarkUnitDirections fixes which way each unit improves. Milliseconds and
// bytes are costs, so less is better; operations per second and a score are
// yields, so more is. count and pct are neither on their own — an error count
// and a cache-hit percentage disagree — so they take the conservative default
// and expect the caller to say when it differs.
var benchmarkUnitDirections = map[string]string{
	"ms":    BenchmarkDirectionLower,
	"s":     BenchmarkDirectionLower,
	"bytes": BenchmarkDirectionLower,
	"kib":   BenchmarkDirectionLower,
	"mib":   BenchmarkDirectionLower,
	"usd":   BenchmarkDirectionLower,
	"ops":   BenchmarkDirectionHigher,
	"rps":   BenchmarkDirectionHigher,
	"score": BenchmarkDirectionHigher,
	"count": BenchmarkDirectionLower,
	"pct":   BenchmarkDirectionLower,
}

// Benchmark mirrors a benchmarks row.
type Benchmark struct {
	ID            int64   `json:"id"`
	SyncID        string  `json:"sync_id"`
	Project       string  `json:"project"`
	TaskID        int64   `json:"task_id"`
	TaskSyncID    string  `json:"task_sync_id"`
	Name          string  `json:"name"`
	Metric        string  `json:"metric"`
	Unit          string  `json:"unit"`
	Direction     string  `json:"direction"`
	Value         float64 `json:"value"`
	Baseline      bool    `json:"baseline"`
	BaselineSetAt *string `json:"baseline_set_at,omitempty"`
	RunPath       *string `json:"run_path,omitempty"`
	SHA256        *string `json:"sha256,omitempty"`
	ConfigStamp   *string `json:"config_stamp,omitempty"`
	CapturedAt    string  `json:"captured_at"`
	Notes         *string `json:"notes,omitempty"`
	Source        string  `json:"source"`
	CreatedAt     string  `json:"created_at"`
	DeletedAt     *string `json:"deleted_at,omitempty"`
}

// BenchmarkDelta is a measurement next to the baseline of its own metric.
// Both extra fields are nil when the metric has no baseline yet: a delta
// against nothing would be a number with no meaning.
type BenchmarkDelta struct {
	Benchmark
	BaselineValue *float64 `json:"baseline_value,omitempty"`
	DeltaPct      *float64 `json:"delta_pct,omitempty"`
}

// AddBenchmarkParams holds one measurement. Direction is optional: an empty
// value takes the default of its unit.
type AddBenchmarkParams struct {
	Task        Task
	Name        string
	Metric      string
	Unit        string
	Direction   string
	Value       float64
	Baseline    bool
	RunPath     *string
	SHA256      *string
	ConfigStamp *string
	CapturedAt  string
	Notes       *string
	Source      string
}

// AddBenchmarkResult is the outcome of AddBenchmark, in the shape
// UpsertTaskResult already established for this package.
type AddBenchmarkResult struct {
	Benchmark Benchmark
	Created   bool
	// DemotedBaseline is the sync_id of the measurement that lost the
	// baseline flag to this one, when there was one.
	DemotedBaseline *string
}

const benchmarkSelectColumns = `id, sync_id, project, task_id, task_sync_id, name, metric, unit,
	direction, value, baseline, baseline_set_at, run_path, sha256, config_stamp, captured_at,
	notes, source, created_at, deleted_at`

func scanBenchmark(row interface{ Scan(dest ...any) error }) (Benchmark, error) {
	var b Benchmark
	var baseline int
	err := row.Scan(&b.ID, &b.SyncID, &b.Project, &b.TaskID, &b.TaskSyncID, &b.Name, &b.Metric,
		&b.Unit, &b.Direction, &b.Value, &baseline, &b.BaselineSetAt, &b.RunPath, &b.SHA256,
		&b.ConfigStamp, &b.CapturedAt, &b.Notes, &b.Source, &b.CreatedAt, &b.DeletedAt)
	b.Baseline = baseline == 1
	return b, err
}

// FindBenchmark returns the measurement a task already holds under this
// (name, metric, captured_at), and whether there was one.
//
// It is the read half of AddBenchmark's idempotency key, exposed so a dry run
// can report a re-import as a no-op rather than as work.
func (s *Store) FindBenchmark(taskSyncID, name, metric, capturedAt string) (Benchmark, bool, error) {
	b, err := scanBenchmark(s.readDB().QueryRow(
		`SELECT `+benchmarkSelectColumns+` FROM benchmarks
		 WHERE task_sync_id = ? AND name = ? AND metric = ? AND captured_at = ? AND deleted_at IS NULL`,
		taskSyncID, strings.TrimSpace(name), strings.TrimSpace(metric), strings.TrimSpace(capturedAt)))
	if errors.Is(err, sql.ErrNoRows) {
		return Benchmark{}, false, nil
	}
	if err != nil {
		return Benchmark{}, false, fmt.Errorf("engram-projects: check duplicate benchmark: %w", err)
	}
	return b, true, nil
}

// AddBenchmark records one measurement, idempotent by
// (task_sync_id, name, metric, captured_at): the same run registered twice is
// the same number, not two.
//
// Marking a measurement as the baseline demotes the previous one for that
// metric in the same transaction. Doing it in two steps would leave a moment
// with two baselines — which the partial unique index refuses outright — or a
// moment with none, and a reader that arrived then would compute every delta
// against nothing.
func (s *Store) AddBenchmark(p AddBenchmarkParams) (AddBenchmarkResult, error) {
	var out AddBenchmarkResult

	name := strings.TrimSpace(p.Name)
	metric := strings.TrimSpace(p.Metric)
	unit := strings.TrimSpace(p.Unit)
	if name == "" || metric == "" {
		return out, &MissingFieldError{Field: "name and metric"}
	}
	direction, ok := resolveBenchmarkDirection(unit, p.Direction)
	if !ok {
		return out, fmt.Errorf("%w: %q", ErrInvalidBenchmarkUnit, unit)
	}
	source := strings.TrimSpace(p.Source)
	if source == "" {
		source = "manual"
	}
	capturedAt := strings.TrimSpace(p.CapturedAt)
	if capturedAt == "" {
		capturedAt = s.nowUTC()
	}

	existing, found, err := s.FindBenchmark(p.Task.SyncID, name, metric, capturedAt)
	if err != nil {
		return out, err
	}
	if found {
		out.Benchmark = existing
		return out, nil
	}

	now := s.nowUTC()
	syncID := newSyncID("bench")
	var baselineSetAt any
	baseline := 0
	if p.Baseline {
		baseline = 1
		baselineSetAt = now
	}

	if err := s.withTx(func(tx *sql.Tx) error {
		out.DemotedBaseline = nil
		if p.Baseline {
			demoted, err := s.demoteBaselineTx(tx, p.Task.SyncID, metric)
			if err != nil {
				return err
			}
			out.DemotedBaseline = demoted
		}
		if _, err := s.execHook(tx, `
			INSERT INTO benchmarks (sync_id, project, task_id, task_sync_id, name, metric, unit,
				direction, value, baseline, baseline_set_at, run_path, sha256, config_stamp,
				captured_at, notes, source, created_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			syncID, p.Task.Project, p.Task.ID, p.Task.SyncID, name, metric, unit, direction,
			p.Value, baseline, baselineSetAt, nullableStr(p.RunPath), nullableStr(p.SHA256),
			nullableStr(p.ConfigStamp), capturedAt, nullableStr(p.Notes), source, now,
		); err != nil {
			return fmt.Errorf("engram-projects: insert benchmark: %w", err)
		}
		return s.enqueueBenchmarkTx(tx, syncID)
	}); err != nil {
		return AddBenchmarkResult{}, err
	}

	saved, err := scanBenchmark(s.db.QueryRow(
		`SELECT `+benchmarkSelectColumns+` FROM benchmarks WHERE sync_id = ?`, syncID))
	if err != nil {
		return AddBenchmarkResult{}, fmt.Errorf("engram-projects: reload benchmark: %w", err)
	}
	out.Benchmark = saved
	out.Created = true
	return out, nil
}

// resolveBenchmarkDirection returns the direction a measurement improves in,
// preferring what the caller said over the default of the unit.
func resolveBenchmarkDirection(unit, requested string) (string, bool) {
	fallback, known := benchmarkUnitDirections[unit]
	if !known {
		return "", false
	}
	switch strings.TrimSpace(requested) {
	case BenchmarkDirectionLower:
		return BenchmarkDirectionLower, true
	case BenchmarkDirectionHigher:
		return BenchmarkDirectionHigher, true
	default:
		return fallback, true
	}
}

// demoteBaselineTx clears the baseline flag of the metric's current baseline
// and returns its sync_id, or nil when the metric had none.
func (s *Store) demoteBaselineTx(tx *sql.Tx, taskSyncID, metric string) (*string, error) {
	var syncID string
	err := tx.QueryRow(
		`SELECT sync_id FROM benchmarks
		 WHERE task_sync_id = ? AND metric = ? AND baseline = 1 AND deleted_at IS NULL`,
		taskSyncID, metric,
	).Scan(&syncID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("engram-projects: read current baseline: %w", err)
	}
	if _, err := s.execHook(tx,
		`UPDATE benchmarks SET baseline = 0, baseline_set_at = NULL WHERE sync_id = ?`, syncID,
	); err != nil {
		return nil, fmt.Errorf("engram-projects: demote baseline: %w", err)
	}
	// The demotion replicates too: a peer that only heard about the new
	// baseline would end up with two.
	if err := s.enqueueBenchmarkTx(tx, syncID); err != nil {
		return nil, err
	}
	return &syncID, nil
}

// BenchmarkListFilter narrows a benchmark listing.
type BenchmarkListFilter struct {
	// Task is a task sync_id. Empty lists every task of the project.
	Task string
	// Project scopes the listing when no task is named.
	Project string
	Metric  string
	// IncludeChildren widens the scope one way: to the child tasks of Task,
	// or to the project subtree of Project.
	IncludeChildren bool
	Limit           int
	Offset          int
}

// ListBenchmarks returns measurements newest first, each next to the baseline
// of its own metric.
func (s *Store) ListBenchmarks(f BenchmarkListFilter) (Page[BenchmarkDelta], error) {
	page := Page[BenchmarkDelta]{Limit: f.Limit, Offset: f.Offset}
	if page.Limit <= 0 {
		page.Limit = 50
	}

	where := []string{"b.deleted_at IS NULL"}
	var args []any

	switch {
	case strings.TrimSpace(f.Task) != "":
		taskSyncIDs, err := s.benchmarkTaskScope(f.Task, f.IncludeChildren)
		if err != nil {
			return page, err
		}
		placeholders := make([]string, 0, len(taskSyncIDs))
		for _, id := range taskSyncIDs {
			placeholders = append(placeholders, "?")
			args = append(args, id)
		}
		where = append(where, "b.task_sync_id IN ("+strings.Join(placeholders, ",")+")")
	case strings.TrimSpace(f.Project) != "":
		projects := []string{f.Project}
		if f.IncludeChildren {
			subtree, err := s.SubtreeSlugs(f.Project)
			if err != nil && !errors.Is(err, ErrNoProjectCard) {
				return page, err
			}
			if len(subtree) > 0 {
				projects = subtree
			}
		}
		placeholders := make([]string, 0, len(projects))
		for _, slug := range projects {
			placeholders = append(placeholders, "?")
			args = append(args, slug)
		}
		where = append(where, "b.project IN ("+strings.Join(placeholders, ",")+")")
	}
	if strings.TrimSpace(f.Metric) != "" {
		where = append(where, "b.metric = ?")
		args = append(args, f.Metric)
	}
	whereSQL := strings.Join(where, " AND ")

	rdb := s.readDB()

	if err := rdb.QueryRow(`SELECT COUNT(*) FROM benchmarks b WHERE `+whereSQL, args...).
		Scan(&page.Total); err != nil {
		return page, fmt.Errorf("engram-projects: count benchmarks: %w", err)
	}

	listArgs := append(append([]any{}, args...), page.Limit, page.Offset)
	rows, err := rdb.Query(`
		SELECT `+prefixedBenchmarkColumns("b")+`,
		       (SELECT base.value FROM benchmarks base
		        WHERE base.task_sync_id = b.task_sync_id AND base.metric = b.metric
		          AND base.baseline = 1 AND base.deleted_at IS NULL)
		FROM benchmarks b
		WHERE `+whereSQL+`
		ORDER BY b.captured_at DESC, b.id DESC
		LIMIT ? OFFSET ?`, listArgs...)
	if err != nil {
		return page, fmt.Errorf("engram-projects: list benchmarks: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var item BenchmarkDelta
		var baseline int
		var baselineValue sql.NullFloat64
		if err := rows.Scan(&item.ID, &item.SyncID, &item.Project, &item.TaskID, &item.TaskSyncID,
			&item.Name, &item.Metric, &item.Unit, &item.Direction, &item.Value, &baseline,
			&item.BaselineSetAt, &item.RunPath, &item.SHA256, &item.ConfigStamp, &item.CapturedAt,
			&item.Notes, &item.Source, &item.CreatedAt, &item.DeletedAt, &baselineValue); err != nil {
			return page, fmt.Errorf("engram-projects: scan benchmark: %w", err)
		}
		item.Baseline = baseline == 1
		if baselineValue.Valid {
			value := baselineValue.Float64
			item.BaselineValue = &value
			if value != 0 {
				delta := (item.Value - value) / value * 100
				item.DeltaPct = &delta
			}
		}
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return page, fmt.Errorf("engram-projects: list benchmarks: %w", err)
	}
	return page, nil
}

// benchmarkTaskScope resolves a task sync_id to itself, plus its child tasks
// when the caller asked for them.
func (s *Store) benchmarkTaskScope(taskSyncID string, includeChildren bool) ([]string, error) {
	scope := []string{taskSyncID}
	if !includeChildren {
		return scope, nil
	}
	rows, err := s.readDB().Query(
		`SELECT sync_id FROM tasks WHERE parent_task_sync_id = ? AND deleted_at IS NULL ORDER BY id`,
		taskSyncID)
	if err != nil {
		return nil, fmt.Errorf("engram-projects: read child tasks: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var child string
		if err := rows.Scan(&child); err != nil {
			return nil, fmt.Errorf("engram-projects: scan child task: %w", err)
		}
		scope = append(scope, child)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("engram-projects: read child tasks: %w", err)
	}
	return scope, nil
}

// prefixedBenchmarkColumns qualifies the benchmark projection with a table
// alias, so the correlated baseline subquery below it stays unambiguous.
func prefixedBenchmarkColumns(alias string) string {
	parts := strings.Split(benchmarkSelectColumns, ",")
	for i, part := range parts {
		parts[i] = alias + "." + strings.TrimSpace(part)
	}
	return strings.Join(parts, ", ")
}
