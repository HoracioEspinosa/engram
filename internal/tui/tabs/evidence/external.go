package evidence

import (
	"os/exec"
	"runtime"
)

// openFile is injectable so a test can assert on what would have been opened
// without spawning a real viewer — the same seam tabs/tasks/external.go's
// openURL uses. It backs "o" (open the evidence file) on both screens and
// "m" (open its manifest.json) on the detail screen.
var openFile = defaultOpenFile

// openerFor picks the OS's registered-handler binary for goos: `open` on
// macOS, `xdg-open` everywhere else. It takes goos as a parameter rather
// than reading runtime.GOOS itself so a test can exercise both branches on
// any single platform — the coverage gate (scripts/tui-coverage-gate.sh)
// runs on every CI platform, and a branch keyed off the real runtime.GOOS
// could only ever show one of its two outcomes as covered on a given
// machine.
func openerFor(goos string) string {
	if goos == "darwin" {
		return "open"
	}
	return "xdg-open"
}

// defaultOpenFile shells out to openerFor(runtime.GOOS) for path. It only
// starts the process — it does not wait for the viewer to exit — and
// reports whether that process could even be launched: a terminal with
// neither binary (an SSH session with no desktop) must degrade to showing
// the path and letting the user copy it, which is exactly what a non-nil
// error here drives in update.go instead of silently doing nothing.
//
// This function itself is not covered by any test: exercising it for real
// would spawn an actual `open`/`xdg-open` process (a real GUI viewer on
// macOS, since "open" is always present there) as a side effect of running
// `go test`. openerFor carries every branch this function has; this body is
// the one line of unconditional process-spawning glue on top of it.
func defaultOpenFile(path string) error {
	return exec.Command(openerFor(runtime.GOOS), path).Start()
}
