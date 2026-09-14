package vault

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

const runV1 = `{
  "engram_benchmark": "v1",
  "task": "KOI-1099",
  "name": "lookup-timeout",
  "captured_at": "2026-08-20T10:00:00Z",
  "config_stamp": "pond-02 / cache off",
  "baseline": true,
  "notes": "corrida base",
  "metrics": [
    { "metric": "lookup.p95", "unit": "ms", "value": 1512 },
    { "metric": "lookup.errors", "unit": "count", "value": 3 },
    { "metric": "lookup.throughput", "unit": "rps", "value": 42.5 },
    { "metric": "lookup.budget", "unit": "pct", "value": 91, "direction": "higher" }
  ]
}`

const runForeign = `{
  "scenarios": { "warm": { "total": { "ops": { "resolve": 2 } } } },
  "wallClockMs": 2594.9
}`

func TestParseRunJSONAcceptsV1AndFlagsForeign(t *testing.T) {
	dir := t.TempDir()

	v1Path := filepath.Join(dir, "baseline-run1.json")
	writeFile(t, v1Path, runV1)
	got, err := ParseRunJSON(v1Path)
	if err != nil {
		t.Fatalf("ParseRunJSON: %v", err)
	}
	if got.Format != FormatV1 {
		t.Fatalf("expected format %q, got %q", FormatV1, got.Format)
	}
	if got.Run == nil {
		t.Fatal("a v1 run must be parsed")
	}
	if got.Run.Task != "KOI-1099" || got.Run.Name != "lookup-timeout" {
		t.Fatalf("unexpected identity: %+v", got.Run)
	}
	if !got.Run.Baseline || got.Run.ConfigStamp != "pond-02 / cache off" {
		t.Fatalf("unexpected header: %+v", got.Run)
	}
	if len(got.Run.Metrics) != 4 {
		t.Fatalf("expected 4 metrics, got %d", len(got.Run.Metrics))
	}
	wantDirection := map[string]string{
		"lookup.p95":        DirectionLower,
		"lookup.errors":     DirectionLower,
		"lookup.throughput": DirectionHigher,
		"lookup.budget":     DirectionHigher, // explicit, overriding the default for pct
	}
	for _, m := range got.Run.Metrics {
		if want := wantDirection[m.Metric]; m.Direction != want {
			t.Errorf("%s: direction %q, want %q", m.Metric, m.Direction, want)
		}
	}

	foreignPath := filepath.Join(dir, "baseline-run3.json")
	writeFile(t, foreignPath, runForeign)
	foreign, err := ParseRunJSON(foreignPath)
	if err != nil {
		t.Fatalf("ParseRunJSON(foreign): %v", err)
	}
	if foreign.Format != FormatForeign {
		t.Fatalf("expected format %q, got %q", FormatForeign, foreign.Format)
	}
	if foreign.Run != nil {
		t.Fatalf("a run without the marker must not be parsed as metrics, got %+v", foreign.Run)
	}
}

func TestParseRunJSONRejectsInvalidV1(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{name: "no metrics", body: `{"engram_benchmark":"v1","task":"T","name":"n","captured_at":"2026-01-01T00:00:00Z","metrics":[]}`},
		{name: "unknown unit", body: `{"engram_benchmark":"v1","task":"T","name":"n","captured_at":"2026-01-01T00:00:00Z","metrics":[{"metric":"m","unit":"furlongs","value":1}]}`},
		{name: "unknown direction", body: `{"engram_benchmark":"v1","task":"T","name":"n","captured_at":"2026-01-01T00:00:00Z","metrics":[{"metric":"m","unit":"ms","value":1,"direction":"sideways"}]}`},
		{name: "nameless metric", body: `{"engram_benchmark":"v1","task":"T","name":"n","captured_at":"2026-01-01T00:00:00Z","metrics":[{"unit":"ms","value":1}]}`},
		{name: "no captured_at", body: `{"engram_benchmark":"v1","task":"T","name":"n","metrics":[{"metric":"m","unit":"ms","value":1}]}`},
		{name: "wrong version", body: `{"engram_benchmark":"v2","metrics":[]}`},
		{name: "broken json", body: `{`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "run.json")
			writeFile(t, path, tc.body)
			if _, err := ParseRunJSON(path); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestJSONPointerResolvesEscapesAndIndexes(t *testing.T) {
	doc := map[string]any{
		"scenarios": map[string]any{
			"warm": map[string]any{"total": map[string]any{"ops": map[string]any{"resolve": 2.0}}},
		},
		"wallClockMs": 2594.9,
		"a/b":         "slash",
		"m~n":         "tilde",
		"runs":        []any{map[string]any{"ms": 10.0}, map[string]any{"ms": 20.0}},
		"":            "empty key",
	}
	cases := []struct {
		pointer string
		want    any
	}{
		{pointer: "", want: doc},
		{pointer: "/wallClockMs", want: 2594.9},
		{pointer: "/scenarios/warm/total/ops/resolve", want: 2.0},
		{pointer: "/a~1b", want: "slash"},
		{pointer: "/m~0n", want: "tilde"},
		{pointer: "/runs/1/ms", want: 20.0},
		{pointer: "/", want: "empty key"},
	}
	for _, tc := range cases {
		t.Run(tc.pointer, func(t *testing.T) {
			got, err := JSONPointer(doc, tc.pointer)
			if err != nil {
				t.Fatalf("JSONPointer(%q): %v", tc.pointer, err)
			}
			if tc.pointer != "" && got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}

	bad := []string{"missing", "/nope", "/runs/9/ms", "/runs/-/ms", "/runs/01/ms", "/wallClockMs/deeper", "/runs/x"}
	for _, pointer := range bad {
		t.Run("bad "+pointer, func(t *testing.T) {
			_, err := JSONPointer(doc, pointer)
			if err == nil {
				t.Fatalf("expected an error for %q", pointer)
			}
			if !errors.Is(err, ErrPointerUnresolved) {
				t.Fatalf("expected ErrPointerUnresolved, got %v", err)
			}
		})
	}
}

func TestExtractMetricsFailsOnUnresolvedPointer(t *testing.T) {
	mapping := []PointerMap{
		{Metric: "warm.resolve", Unit: "count", Pointer: "/scenarios/warm/total/ops/resolve"},
		{Metric: "cold.wallClockMs", Unit: "ms", Pointer: "/wallClockMs"},
	}
	metrics, err := ExtractMetrics([]byte(runForeign), mapping)
	if err != nil {
		t.Fatalf("ExtractMetrics: %v", err)
	}
	if len(metrics) != 2 {
		t.Fatalf("expected 2 metrics, got %d", len(metrics))
	}
	if metrics[0].Value != 2 || metrics[0].Direction != DirectionLower {
		t.Fatalf("unexpected first metric: %+v", metrics[0])
	}
	if metrics[1].Value != 2594.9 || metrics[1].Unit != "ms" {
		t.Fatalf("unexpected second metric: %+v", metrics[1])
	}

	broken := append([]PointerMap{}, mapping...)
	broken = append(broken, PointerMap{Metric: "gone", Unit: "ms", Pointer: "/scenarios/cold/total"})
	_, err = ExtractMetrics([]byte(runForeign), broken)
	if err == nil {
		t.Fatal("expected an error for an unresolved pointer")
	}
	if !errors.Is(err, ErrPointerUnresolved) {
		t.Fatalf("expected ErrPointerUnresolved, got %v", err)
	}
	if got := err.Error(); !strings.Contains(got, "/scenarios/cold/total") {
		t.Fatalf("the error must name the failing pointer, got %q", got)
	}
}

func TestExtractMetricsRejectsNonNumericAndUnknownUnits(t *testing.T) {
	doc := []byte(`{"label":"fast","ms":12}`)
	if _, err := ExtractMetrics(doc, []PointerMap{{Metric: "m", Unit: "ms", Pointer: "/label"}}); err == nil {
		t.Fatal("expected an error for a non-numeric pointer target")
	}
	if _, err := ExtractMetrics(doc, []PointerMap{{Metric: "m", Unit: "furlongs", Pointer: "/ms"}}); err == nil {
		t.Fatal("expected an error for an unknown unit")
	}
	if _, err := ExtractMetrics(doc, []PointerMap{{Unit: "ms", Pointer: "/ms"}}); err == nil {
		t.Fatal("expected an error for a nameless metric")
	}
	if _, err := ExtractMetrics([]byte(`{`), []PointerMap{{Metric: "m", Unit: "ms", Pointer: "/ms"}}); err == nil {
		t.Fatal("expected an error for a broken document")
	}
}

func TestLoadPointerMapReadsTheBenchmarkMap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "benchmark_map.json")
	writeFile(t, path, `[
  { "metric": "warm.resolve", "unit": "count", "pointer": "/scenarios/warm/total/ops/resolve" },
  { "metric": "cold.wallClockMs", "unit": "ms", "pointer": "/wallClockMs" }
]`)
	mapping, err := LoadPointerMap(path)
	if err != nil {
		t.Fatalf("LoadPointerMap: %v", err)
	}
	if len(mapping) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(mapping))
	}
	if mapping[1].Pointer != "/wallClockMs" || mapping[1].Unit != "ms" {
		t.Fatalf("unexpected entry: %+v", mapping[1])
	}

	empty := filepath.Join(t.TempDir(), "benchmark_map.json")
	writeFile(t, empty, `[]`)
	if _, err := LoadPointerMap(empty); err == nil {
		t.Fatal("expected an error for an empty map")
	}

	broken := filepath.Join(t.TempDir(), "benchmark_map.json")
	writeFile(t, broken, `{"metric":"m"}`)
	if _, err := LoadPointerMap(broken); err == nil {
		t.Fatal("expected an error for a map that is not a list")
	}

	if _, err := LoadPointerMap(filepath.Join(t.TempDir(), "ausente.json")); err == nil {
		t.Fatal("expected an error for a missing map")
	}
}

func TestScanTaskCollectsBenchmarkRuns(t *testing.T) {
	root := t.TempDir()
	task := filepath.Join(root, "koi-garden", "KOI-1099-lookup-timeout")
	writeFile(t, filepath.Join(task, "README.md"), "# KOI-1099 — Timeout\n\n**Estado:** Con pendientes — falta pond-02\n\nBuscar un pez falla.\n")
	writeFile(t, filepath.Join(task, "benchmarks", "baseline-run1.json"), runV1)
	writeFile(t, filepath.Join(task, "benchmarks", "baseline-run3.json"), runForeign)
	writeFile(t, filepath.Join(task, "benchmarks", "BASELINE.md"), "# Linea base\n")

	got, err := ScanTask(root, "koi-garden", "KOI-1099-lookup-timeout", Options{})
	if err != nil {
		t.Fatalf("ScanTask: %v", err)
	}
	if got.State != StatePending || got.PendingNote != "falta pond-02" {
		t.Fatalf("unexpected state: %q / %q", got.State, got.PendingNote)
	}
	if len(got.BenchmarkRuns) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(got.BenchmarkRuns))
	}
	if got.BenchmarkRuns[0].RelPath != "benchmarks/baseline-run1.json" {
		t.Fatalf("runs must carry their task-relative path, got %q", got.BenchmarkRuns[0].RelPath)
	}
	if got.BenchmarkRuns[0].Format != FormatV1 || got.BenchmarkRuns[1].Format != FormatForeign {
		t.Fatalf("unexpected formats: %q / %q", got.BenchmarkRuns[0].Format, got.BenchmarkRuns[1].Format)
	}
}
