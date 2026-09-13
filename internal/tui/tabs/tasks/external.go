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

// defaultOpenURL shells out to the OS's registered handler for a URL: `open`
// on macOS, `xdg-open` everywhere else (rfc-tui.md §9.3's fallback list,
// applied here to Tasks' two link-opening actions). It only starts the
// process — it does not wait for the browser to exit — and reports whether
// that process could even be launched; rfc-tui.md §10.2 documents that a
// terminal with neither binary (an SSH session with no desktop) must degrade
// to "show the path/URL and let the user copy it", which is exactly what a
// non-nil error here drives in update.go instead of silently doing nothing.
func defaultOpenURL(url string) error {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	return exec.Command(name, url).Start()
}
