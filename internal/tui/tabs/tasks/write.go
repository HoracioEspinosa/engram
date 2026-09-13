package tasks

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/shared"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs"

	tea "github.com/charmbracelet/bubbletea"
)

// evidenceDirEnv and evidenceDirDefault mirror
// cmd/engram/project_cmd_ops.go's projEvidenceDir (D-06): the two packages
// cannot share the constant directly (cmd depends on internal/tui, so the
// reverse import is not available), but both must resolve the same root or
// a context pack written from S5 would land somewhere `engram project
// evidence add` never looks.
const (
	evidenceDirEnv     = "CD_EVIDENCE_DIR"
	evidenceDirDefault = ".clarodrive/evidence"
)

// evidenceRoot resolves ${CD_EVIDENCE_DIR:-~/.clarodrive/evidence}.
func evidenceRoot() string {
	if v := strings.TrimSpace(os.Getenv(evidenceDirEnv)); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return evidenceDirDefault
	}
	return filepath.Join(home, evidenceDirDefault)
}

// writeContextPack saves the loaded context pack to
// ${CD_EVIDENCE_DIR:-~/.clarodrive/evidence}/<project>/<KEY>/context-pack.md
// (rfc-tui.md §3.1 S5, key "w"), creating the task's evidence directory if it
// does not exist yet.
func (m Model) writeContextPack() (tabs.Tab, tea.Cmd) {
	if m.Detail == nil || m.ContextPack == "" {
		return m, nil
	}
	dir := filepath.Join(evidenceRoot(), m.project, data.TaskKey(m.Detail.Task))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		m.ErrorMsg = "could not create " + dir + ": " + err.Error()
		return m, nil
	}
	path := filepath.Join(dir, "context-pack.md")
	if err := os.WriteFile(path, []byte(m.ContextPack), 0o644); err != nil {
		m.ErrorMsg = "could not write " + path + ": " + err.Error()
		return m, nil
	}
	m.CopyFeedback = "✓ Saved " + path
	return m, shared.ClearFeedbackAfter(2 * time.Second)
}
