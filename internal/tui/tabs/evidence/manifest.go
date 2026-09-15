package evidence

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// manifestFileName is the sibling file the controls are read from: when a
// manifest.json sits next to a captured file, positive_control and
// negative_control come from it. It is one file per task directory — not one
// per captured file — carrying one entry per file.
const manifestFileName = "manifest.json"

// ManifestEntry is one captured file's row inside a task's manifest.json:
// the config_stamp and positive/negative control pair that make a capture
// self-certifying, matched by filename against the evidence row the detail
// screen is showing.
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
// through exists=false so the detail screen can say so plainly instead of
// silently showing nothing. A manifest.json that exists but carries no entry
// for this file is likewise not an error: exists is true, entry is nil.
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
