package benchmarks

import (
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/HoracioEspinosa/engram/internal/tui/shared"

	tea "github.com/charmbracelet/bubbletea"
)

// baselineDoc is the file "b" opens: the project's own record of what its
// numbers are supposed to be, which lives in the vault beside the runbooks
// rather than in the store.
const baselineDoc = "BASELINE.md"

// openFile is injectable so a test can assert on what would have been opened
// without spawning a real viewer — the same seam tabs/evidence/external.go
// uses.
var openFile = defaultOpenFile

// openerFor picks the OS's registered-handler binary for goos: `open` on
// macOS, `xdg-open` everywhere else. It takes goos as a parameter rather than
// reading runtime.GOOS itself so a test can exercise both branches on any
// single platform.
func openerFor(goos string) string {
	if goos == "darwin" {
		return "open"
	}
	return "xdg-open"
}

// defaultOpenFile starts the OS viewer for path without waiting for it. A
// terminal with neither binary — an SSH session with no desktop — reports the
// failure, which is what puts the path on screen for the reader to copy.
//
// This body is not covered by any test: exercising it would spawn a real
// viewer as a side effect of `go test`. openerFor carries every branch it
// has; this is the one line of process-spawning glue on top.
func defaultOpenFile(path string) error {
	return exec.Command(openerFor(runtime.GOOS), path).Start()
}

// openBaseline opens the project's BASELINE.md with the system viewer.
//
// The vault root is resolved the same way every other vault path in the TUI
// is; a workspace with none configured says so rather than opening whatever
// relative path the process happens to be standing next to.
func openBaseline(project string) tea.Cmd {
	return func() tea.Msg {
		root, ok := shared.VaultRoot()
		if !ok {
			return baselineOpenedMsg{
				path: baselineDoc,
				err:  errNoVaultRoot,
			}
		}
		path := filepath.Join(root, "Projects", project, baselineDoc)
		return baselineOpenedMsg{path: path, err: openFile(path)}
	}
}
