package vault

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Run formats a benchmark JSON file can carry.
const (
	// FormatV1 is Engram's own run format, recognised by the
	// "engram_benchmark": "v1" marker at the top level.
	FormatV1 = "engram.benchmark.v1"
	// FormatForeign is every other JSON file under benchmarks/. Those are the
	// harness's own output: they are recorded as evidence, never read as
	// metrics, until someone maps their pointers in benchmark_map.json.
	FormatForeign = "foreign"
)

// Directions a metric can improve in.
const (
	DirectionLower  = "lower"
	DirectionHigher = "higher"
)

// benchmarkMarker is the key whose presence promotes a JSON file from evidence
// to a parsed run.
const benchmarkMarker = "engram_benchmark"

// defaultDirection maps a unit to the direction that means "better" for it.
// Units absent from this table default to lower unless the run says otherwise.
var defaultDirection = map[string]string{
	"ms":    DirectionLower,
	"s":     DirectionLower,
	"bytes": DirectionLower,
	"kib":   DirectionLower,
	"mib":   DirectionLower,
	"usd":   DirectionLower,
	"ops":   DirectionHigher,
	"rps":   DirectionHigher,
	"score": DirectionHigher,
	"count": DirectionLower,
	"pct":   DirectionLower,
}

// knownUnits is the closed unit enum the store's benchmarks table accepts.
var knownUnits = map[string]bool{
	"ms": true, "s": true, "count": true, "bytes": true, "kib": true,
	"mib": true, "pct": true, "ops": true, "rps": true, "usd": true, "score": true,
}

// Metric is one measurement of a run.
type Metric struct {
	Metric    string  `json:"metric"`
	Unit      string  `json:"unit"`
	Value     float64 `json:"value"`
	Direction string  `json:"direction,omitempty"`
}

// Run is a parsed engram.benchmark.v1 file.
type Run struct {
	Task        string   `json:"task"`
	Name        string   `json:"name"`
	CapturedAt  string   `json:"captured_at"`
	ConfigStamp string   `json:"config_stamp"`
	Baseline    bool     `json:"baseline"`
	Notes       string   `json:"notes"`
	Metrics     []Metric `json:"metrics"`
}

// RunFile is a JSON file found under a task's benchmarks/ directory.
type RunFile struct {
	// RelPath is the file relative to the task directory when the RunFile came
	// from ScanTask, and the file's base name when ParseRunJSON was called
	// directly.
	RelPath string
	// Format is FormatV1 or FormatForeign.
	Format string
	// Run is nil for a foreign file. Nothing guesses metrics out of a format
	// it does not recognise.
	Run *Run
}

// PointerMap maps one JSON Pointer of a foreign run onto a metric. It is the
// content of the benchmark_map.json file that sits beside the runs.
type PointerMap struct {
	Metric    string `json:"metric"`
	Unit      string `json:"unit"`
	Pointer   string `json:"pointer"`
	Direction string `json:"direction,omitempty"`
}

// ParseRunJSON reads one JSON file from a benchmarks directory. A file
// carrying the v1 marker is parsed and validated; anything else comes back as
// FormatForeign with no metrics, which is what keeps a harness's own output
// from being mistaken for measurements Engram can compare.
func ParseRunJSON(path string) (RunFile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return RunFile{}, fmt.Errorf("vault: read %s: %w", path, err)
	}
	out := RunFile{RelPath: filepath.Base(path), Format: FormatForeign}

	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		// Not a JSON object at all: an array or a scalar is still a foreign
		// artefact, but a syntactically broken file is a problem to report.
		var scalar any
		if err := json.Unmarshal(raw, &scalar); err != nil {
			return RunFile{}, fmt.Errorf("vault: parse %s: %w", path, err)
		}
		return out, nil
	}
	marker, ok := probe[benchmarkMarker]
	if !ok {
		return out, nil
	}
	var version string
	if err := json.Unmarshal(marker, &version); err != nil {
		return RunFile{}, fmt.Errorf("vault: parse %s: %s must be a string", path, benchmarkMarker)
	}
	if strings.TrimSpace(version) != "v1" {
		return RunFile{}, fmt.Errorf("vault: parse %s: unsupported %s %q", path, benchmarkMarker, version)
	}

	var run Run
	if err := json.Unmarshal(raw, &run); err != nil {
		return RunFile{}, fmt.Errorf("vault: parse %s: %w", path, err)
	}
	if err := validateRun(&run); err != nil {
		return RunFile{}, fmt.Errorf("vault: parse %s: %w", path, err)
	}
	out.Format = FormatV1
	out.Run = &run
	return out, nil
}

// validateRun rejects a v1 file that cannot be persisted as benchmarks, and
// fills in each metric's direction from its unit when the file leaves it out.
func validateRun(run *Run) error {
	if strings.TrimSpace(run.CapturedAt) == "" {
		return fmt.Errorf("captured_at is required")
	}
	if len(run.Metrics) == 0 {
		return fmt.Errorf("metrics is empty")
	}
	for i := range run.Metrics {
		if err := normalizeMetric(&run.Metrics[i]); err != nil {
			return err
		}
	}
	return nil
}

// normalizeMetric validates one metric and resolves its direction.
func normalizeMetric(m *Metric) error {
	m.Metric = strings.TrimSpace(m.Metric)
	if m.Metric == "" {
		return fmt.Errorf("a metric has no name")
	}
	m.Unit = strings.ToLower(strings.TrimSpace(m.Unit))
	if !knownUnits[m.Unit] {
		return fmt.Errorf("metric %s has unknown unit %q", m.Metric, m.Unit)
	}
	m.Direction = strings.ToLower(strings.TrimSpace(m.Direction))
	switch m.Direction {
	case "":
		m.Direction = defaultDirection[m.Unit]
		if m.Direction == "" {
			m.Direction = DirectionLower
		}
	case DirectionLower, DirectionHigher:
	default:
		return fmt.Errorf("metric %s has unknown direction %q", m.Metric, m.Direction)
	}
	return nil
}

// LoadPointerMap reads a benchmark_map.json file: the list of pointers that
// turns a foreign run into metrics. An empty map is an error, because an
// import that would produce nothing is a mistake worth surfacing at the file
// rather than as a silent zero.
func LoadPointerMap(path string) ([]PointerMap, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("vault: read %s: %w", path, err)
	}
	var mapping []PointerMap
	if err := json.Unmarshal(raw, &mapping); err != nil {
		return nil, fmt.Errorf("vault: parse %s: %w", path, err)
	}
	if len(mapping) == 0 {
		return nil, fmt.Errorf("vault: %s maps no metrics", path)
	}
	return mapping, nil
}

// ExtractMetrics pulls metrics out of a foreign run using a pointer map. Every
// pointer must resolve to a number: a map that no longer matches the harness's
// output fails loudly instead of importing a shorter list than the user wrote.
func ExtractMetrics(doc []byte, mapping []PointerMap) ([]Metric, error) {
	var decoded any
	if err := json.Unmarshal(doc, &decoded); err != nil {
		return nil, fmt.Errorf("vault: parse benchmark run: %w", err)
	}
	metrics := make([]Metric, 0, len(mapping))
	for _, entry := range mapping {
		value, err := JSONPointer(decoded, entry.Pointer)
		if err != nil {
			return nil, err
		}
		number, ok := value.(float64)
		if !ok {
			return nil, fmt.Errorf("vault: %s resolves to %T, not a number", entry.Pointer, value)
		}
		metric := Metric{
			Metric:    entry.Metric,
			Unit:      entry.Unit,
			Value:     number,
			Direction: entry.Direction,
		}
		if err := normalizeMetric(&metric); err != nil {
			return nil, fmt.Errorf("vault: %s: %w", entry.Pointer, err)
		}
		metrics = append(metrics, metric)
	}
	return metrics, nil
}
