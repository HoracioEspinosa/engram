package evidence

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// manifestFileName is the sibling file rfc-tui.md §9.3 reads controls from:
// "si junto al archivo existe manifest.json se leen positive_control y
// negative_control". ADR-029 §2 and the capture-evidence SKILL.md (its
// envelope's `files[]`, which scripts/evidence-manifest.sh mirrors into the
// manifest itself per ADR-029's "Corregido" note) agree it is one file per
// task directory — not one per captured file — with one entry per file.
const manifestFileName = "manifest.json"

// ManifestEntry is one captured file's row inside a task's manifest.json
// (ADR-029 §2, D-06): the config_stamp and positive/negative control pair
// that make a capture self-certifying, matched by filename against the
// evidence row S7 is showing.
type ManifestEntry struct {
	File            string `json:"file"`
	SHA256          string `json:"sha256"`
	Kind            string `json:"kind"`
	Proves          string `json:"proves"`
	CapturedAt      string `json:"captured_at"`
	ConfigStamp     string `json:"config_stamp"`
	PositiveControl string `json:"positive_control"`
	NegativeControl string `json:"negative_control"`
	PIIMasked       bool   `json:"pii_masked"`
}

// manifestDocument is manifest.json's on-disk shape: one entry per file
// captured for the task, sibling to them in the same directory.
type manifestDocument struct {
	Files []ManifestEntry `json:"files"`
}

// manifestPath returns the manifest.json path sibling to evidencePath.
func manifestPath(evidencePath string) string {
	return filepath.Join(filepath.Dir(evidencePath), manifestFileName)
}

// readManifestEntry looks for manifest.json next to evidencePath and returns
// the entry whose "file" matches its basename.
//
// A missing manifest.json is not an error — registered evidence rows
// routinely have manifest_path NULL and no manifest.json on disk next to it,
// `.snapshot.json` sidecars with an unrelated shape instead — it is reported
// through exists=false so S7 can say so plainly instead of silently showing
// nothing. A manifest.json
// that exists but carries no entry for this file is likewise not an error:
// exists is true, entry is nil.
func readManifestEntry(evidencePath string) (entry *ManifestEntry, exists bool, err error) {
	raw, err := os.ReadFile(manifestPath(evidencePath))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}

	var doc manifestDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, true, fmt.Errorf("parse manifest.json: %w", err)
	}

	base := filepath.Base(evidencePath)
	for i := range doc.Files {
		if doc.Files[i].File == base {
			return &doc.Files[i], true, nil
		}
	}
	return nil, true, nil
}
