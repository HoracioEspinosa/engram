package tasks

import (
	"os/exec"
	"runtime"
)

// openURL is injectable so a test can assert on what would have been opened
// without spawning a real browser — the same seam cmd/engram/main.go's
// detectProject var uses to keep project.DetectProject out of its tests.
// rfc-tui.md §3.1 wires it to two actions: S3/S4's "o" (open the task's Jira
// issue) and S4's "u" (open its pr_url).
var openURL = defaultOpenURL

// openerFor picks the OS's registered-handler binary for goos: `open` on
// macOS, `xdg-open` everywhere else (rfc-tui.md §9.3's fallback list,
// applied here to Tasks' two link-opening actions). It takes goos as a
// parameter rather than reading runtime.GOOS itself so a test can exercise
// both branches on any single platform — the coverage gate
// (scripts/tui-coverage-gate.sh) runs on every CI platform, and a branch
// keyed off the real runtime.GOOS could only ever show one of its two
// outcomes as covered on a given machine.
func openerFor(goos string) string {
	if goos == "darwin" {
		return "open"
	}
	return "xdg-open"
}

// defaultOpenURL shells out to openerFor(runtime.GOOS) for url. It only
// starts the process — it does not wait for the browser to exit — and
// reports whether that process could even be launched; rfc-tui.md §10.2
// documents that a terminal with neither binary (an SSH session with no
// desktop) must degrade to "show the path/URL and let the user copy it",
// which is exactly what a non-nil error here drives in update.go instead of
// silently doing nothing.
//
// This function itself is not covered by any test: exercising it for real
// would spawn an actual `open`/`xdg-open` process (a real browser launch on
// macOS, since "open" is always present there) as a side effect of running
// `go test`. openerFor carries every branch this function has; this body is
// the one line of unconditional process-spawning glue on top of it.
func defaultOpenURL(url string) error {
	return exec.Command(openerFor(runtime.GOOS), url).Start()
}
