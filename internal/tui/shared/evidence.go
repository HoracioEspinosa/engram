package shared

import (
	"os"
	"path/filepath"
	"strings"
)

// EvidenceDirEnv and EvidenceDirDefault mirror
// cmd/engram/project_cmd_ops.go's projEvidenceDir (D-06): the two packages
// cannot share the constant directly (cmd depends on internal/tui, so the
// reverse import is not available), but both must resolve the same root or
// a file the TUI writes or opens would land somewhere `engram project
// evidence add` never looks.
//
// Within internal/tui, though, every tab that touches captured evidence —
// Tasks (S5's "w" writes context-pack.md here) and Evidence (S6/S7 resolve
// a row's stored path against this same root) — shares this one copy rather
// than each keeping its own, which is exactly what shared exists for.
const (
	EvidenceDirEnv     = "CD_EVIDENCE_DIR"
	EvidenceDirDefault = ".clarodrive/evidence"
)

// EvidenceRoot resolves ${CD_EVIDENCE_DIR:-~/.clarodrive/evidence}.
func EvidenceRoot() string {
	if v := strings.TrimSpace(os.Getenv(EvidenceDirEnv)); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return EvidenceDirDefault
	}
	return filepath.Join(home, EvidenceDirDefault)
}
