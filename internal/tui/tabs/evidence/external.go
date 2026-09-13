package evidence

import (
	"os/exec"
	"runtime"
)

// openFile is injectable so a test can assert on what would have been opened
// without spawning a real viewer — the same seam tabs/tasks/external.go's
// openURL uses. rfc-tui.md §3.1 wires it to S6/S7's "o" (open the evidence
// file) and S7's "m" (open its manifest.json).
var openFile = defaultOpenFile

// defaultOpenFile shells out to the OS's registered handler for a path:
// `open` on macOS, `xdg-open` everywhere else (rfc-tui.md §9.3: "`o` abre el
// archivo con `open`/`xdg-open`"). It only starts the process — it does not
// wait for the viewer to exit — and reports whether that process could even
// be launched; rfc-tui.md §9.3 documents that a terminal with neither binary
// (an SSH session with no desktop) must degrade to "show the path and let
// the user copy it", which is exactly what a non-nil error here drives in
// update.go instead of silently doing nothing.
func defaultOpenFile(path string) error {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	return exec.Command(name, path).Start()
}
