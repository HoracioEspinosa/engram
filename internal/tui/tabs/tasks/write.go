package tasks

import (
	"os"
	"path/filepath"
	"time"

	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/shared"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs"

	tea "github.com/charmbracelet/bubbletea"
)

// writeContextPack saves the loaded context pack to
// ${CD_EVIDENCE_DIR:-~/.clarodrive/evidence}/<project>/<KEY>/context-pack.md
// when "w" is pressed on the context pack, creating the task's evidence
// directory if it does not exist yet.
func (m Model) writeContextPack() (tabs.Tab, tea.Cmd) {
	if m.Detail == nil || m.ContextPack == "" {
		return m, nil
	}
	dir := filepath.Join(shared.EvidenceRoot(), m.project, data.TaskKey(m.Detail.Task))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		m.ErrorMsg = "could not create " + dir + ": " + err.Error()
		return m, nil
	}
	path := filepath.Join(dir, "context-pack.md")
	if err := os.WriteFile(path, []byte(m.ContextPack), 0o644); err != nil {
		m.ErrorMsg = "could not write " + path + ": " + err.Error()
		return m, nil
	}
	m.CopyFeedback = "Saved " + path
	return m, shared.ClearFeedbackAfter(2 * time.Second)
}
