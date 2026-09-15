package runbooks

import (
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// editorEnv is the environment variable both editor keys resolve through:
// "e" opens the runbook's own file and "o" opens the service hub.
const editorEnv = "EDITOR"

// defaultEditorFallback is used when $EDITOR is unset: the same baseline
// `git commit` falls back to on a POSIX shell, so a fresh environment with
// no $EDITOR configured still opens something instead of a dead keypress.
const defaultEditorFallback = "vi"

// execEditor is injectable so a test can capture what would have been
// executed without suspending the terminal for a real editor — the same
// seam tabs/evidence/external.go's openFile and tabs/tasks/external.go's
// openURL use, adapted for $EDITOR: unlike a GUI viewer opened with
// `open`/`xdg-open`, an editor needs the terminal, so this returns the
// tea.Cmd that suspends the Bubble Tea renderer (tea.ExecProcess) instead of
// a fire-and-forget error like those two.
var execEditor = defaultExecEditor

// defaultExecEditor opens path in resolveEditor() via tea.ExecProcess. The
// process's outcome (a missing binary, a non-zero exit, or a clean close)
// surfaces through editorClosedMsg rather than being swallowed — the same
// "report it" contract the system-viewer opens elsewhere in the TUI follow.
func defaultExecEditor(path string) tea.Cmd {
	cmd := exec.Command(resolveEditor(), path)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editorClosedMsg{err: err}
	})
}

// resolveEditor reads $EDITOR, falling back to defaultEditorFallback when
// unset or blank.
func resolveEditor() string {
	if e := strings.TrimSpace(os.Getenv(editorEnv)); e != "" {
		return e
	}
	return defaultEditorFallback
}
