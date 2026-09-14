package store

import (
	"errors"
	"math"
	"testing"
)

func addBenchmark(t *testing.T, s *Store, task Task, p AddBenchmarkParams) AddBenchmarkResult {
	t.Helper()
	p.Task = task
	res, err := s.AddBenchmark(p)
	if err != nil {
		t.Fatalf("AddBenchmark(%s/%s): %v", p.Name, p.Metric, err)
	}
	return res
}

// TestBenchmarkSingleBaselinePerMetric pins that exactly one measurement per
// metric is the reference, and that the handover happens in one step: never two
// baselines, never none.
func TestBenchmarkSingleBaselinePerMetric(t *testing.T) {
	s := newTestStore(t)
	task := newTaskForEvidence(t, s, "acme", "tuning")

	first := addBenchmark(t, s, task, AddBenchmarkParams{
		Name: "login", Metric: "p95", Unit: "ms", Value: 400, Baseline: true,
		CapturedAt: "2026-01-01 00:00:00",
	})
	if !first.Created || first.DemotedBaseline != nil {
		t.Fatalf("the first baseline demotes nothing: %+v", first)
	}
	if first.Benchmark.BaselineSetAt == nil {
		t.Fatal("a baseline must record when it became one")
	}

	second := addBenchmark(t, s, task, AddBenchmarkParams{
		Name: "login", Metric: "p95", Unit: "ms", Value: 250, Baseline: true,
		CapturedAt: "2026-02-01 00:00:00",
	})
	if second.DemotedBaseline == nil || *second.DemotedBaseline != first.Benchmark.SyncID {
		t.Fatalf("the previous baseline must be named as demoted: %+v", second.DemotedBaseline)
	}

	var baselines int
	if err := s.DB().QueryRow(
		`SELECT COUNT(*) FROM benchmarks WHERE task_sync_id = ? AND metric = 'p95' AND baseline = 1`,
		task.SyncID).Scan(&baselines); err != nil {
		t.Fatalf("count baselines: %v", err)
	}
	if baselines != 1 {
		t.Fatalf("baselines for one metric: %d, want 1", baselines)
	}

	// Another metric keeps its own baseline, untouched.
	other := addBenchmark(t, s, task, AddBenchmarkParams{
		Name: "login", Metric: "rss", Unit: "mib", Value: 120, Baseline: true,
		CapturedAt: "2026-02-01 00:00:00",
	})
	if other.DemotedBaseline != nil {
		t.Fatalf("a different metric must not lose its baseline: %v", *other.DemotedBaseline)
	}

	// The database refuses a second baseline written straight through SQL.
	if _, err := s.DB().Exec(
		`UPDATE benchmarks SET baseline = 1, baseline_set_at = datetime('now') WHERE sync_id = ?`,
		first.Benchmark.SyncID); err == nil {
		t.Fatal("two baselines for one metric must be impossible")
	}
}

func TestBenchmarkDeltaPct(t *testing.T) {
	s := newTestStore(t)
	task := newTaskForEvidence(t, s, "acme", "tuning")

	addBenchmark(t, s, task, AddBenchmarkParams{
		Name: "login", Metric: "p95", Unit: "ms", Value: 400, Baseline: true,
		CapturedAt: "2026-01-01 00:00:00",
	})
	addBenchmark(t, s, task, AddBenchmarkParams{
		Name: "login", Metric: "p95", Unit: "ms", Value: 300,
		CapturedAt: "2026-02-01 00:00:00",
	})

	page, err := s.ListBenchmarks(BenchmarkListFilter{Task: task.SyncID, Metric: "p95"})
	if err != nil {
		t.Fatalf("ListBenchmarks: %v", err)
	}
	if page.Total != 2 || len(page.Items) != 2 {
		t.Fatalf("items %d of %d, want 2 of 2", len(page.Items), page.Total)
	}
	// Newest first.
	newest := page.Items[0]
	if newest.Value != 300 {
		t.Fatalf("first item value %v, want the newest measurement", newest.Value)
	}
	if newest.BaselineValue == nil || *newest.BaselineValue != 400 {
		t.Fatalf("baseline_value %v, want 400", newest.BaselineValue)
	}
	if newest.DeltaPct == nil || math.Abs(*newest.DeltaPct-(-25)) > 1e-9 {
		t.Fatalf("delta_pct %v, want -25", newest.DeltaPct)
	}
	// The baseline is zero away from itself.
	base := page.Items[1]
	if base.DeltaPct == nil || *base.DeltaPct != 0 {
		t.Fatalf("the baseline delta %v, want 0", base.DeltaPct)
	}
}

func TestBenchmarkDeltaNilWithoutBaseline(t *testing.T) {
	s := newTestStore(t)
	task := newTaskForEvidence(t, s, "acme", "tuning")

	addBenchmark(t, s, task, AddBenchmarkParams{
		Name: "login", Metric: "p95", Unit: "ms", Value: 400,
		CapturedAt: "2026-01-01 00:00:00",
	})

	page, err := s.ListBenchmarks(BenchmarkListFilter{Task: task.SyncID})
	if err != nil {
		t.Fatalf("ListBenchmarks: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("items %d, want 1", len(page.Items))
	}
	if page.Items[0].BaselineValue != nil || page.Items[0].DeltaPct != nil {
		t.Fatalf("a metric with no baseline has no delta: %+v", page.Items[0])
	}
}

func TestBenchmarkDirectionDefaultsByUnit(t *testing.T) {
	s := newTestStore(t)
	task := newTaskForEvidence(t, s, "acme", "tuning")

	cases := []struct {
		unit, requested, want string
	}{
		{"ms", "", BenchmarkDirectionLower},
		{"s", "", BenchmarkDirectionLower},
		{"bytes", "", BenchmarkDirectionLower},
		{"kib", "", BenchmarkDirectionLower},
		{"mib", "", BenchmarkDirectionLower},
		{"usd", "", BenchmarkDirectionLower},
		{"ops", "", BenchmarkDirectionHigher},
		{"rps", "", BenchmarkDirectionHigher},
		{"score", "", BenchmarkDirectionHigher},
		{"count", "", BenchmarkDirectionLower},
		{"pct", "", BenchmarkDirectionLower},
		// A caller who knows better overrides the default of the unit.
		{"pct", BenchmarkDirectionHigher, BenchmarkDirectionHigher},
		{"count", BenchmarkDirectionHigher, BenchmarkDirectionHigher},
		{"ops", BenchmarkDirectionLower, BenchmarkDirectionLower},
	}
	for i, c := range cases {
		res := addBenchmark(t, s, task, AddBenchmarkParams{
			Name:      "run",
			Metric:    c.unit + c.requested,
			Unit:      c.unit,
			Direction: c.requested,
			Value:     float64(i + 1),
		})
		if res.Benchmark.Direction != c.want {
			t.Fatalf("unit %q with direction %q: got %q, want %q",
				c.unit, c.requested, res.Benchmark.Direction, c.want)
		}
	}
}

func TestBenchmarkRejectsInvalidUnit(t *testing.T) {
	s := newTestStore(t)
	task := newTaskForEvidence(t, s, "acme", "tuning")

	_, err := s.AddBenchmark(AddBenchmarkParams{
		Task: task, Name: "run", Metric: "p95", Unit: "fortnights", Value: 1,
	})
	if !errors.Is(err, ErrInvalidBenchmarkUnit) {
		t.Fatalf("want ErrInvalidBenchmarkUnit, got %v", err)
	}

	page, err := s.ListBenchmarks(BenchmarkListFilter{Task: task.SyncID})
	if err != nil {
		t.Fatalf("ListBenchmarks: %v", err)
	}
	if page.Total != 0 {
		t.Fatalf("a rejected unit must write nothing, got %d rows", page.Total)
	}

	// The same run registered twice is one measurement, not two.
	first := addBenchmark(t, s, task, AddBenchmarkParams{
		Name: "run", Metric: "p95", Unit: "ms", Value: 10, CapturedAt: "2026-01-01 00:00:00",
	})
	again := addBenchmark(t, s, task, AddBenchmarkParams{
		Name: "run", Metric: "p95", Unit: "ms", Value: 10, CapturedAt: "2026-01-01 00:00:00",
	})
	if again.Created {
		t.Fatal("the same run must not be recorded twice")
	}
	if again.Benchmark.SyncID != first.Benchmark.SyncID {
		t.Fatalf("duplicate returned %q, want %q", again.Benchmark.SyncID, first.Benchmark.SyncID)
	}
}
