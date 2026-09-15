package shared

import (
	"os"
	"strings"
)

// AbbreviateHome shortens path to a leading ~ when it lives under the
// current user's home directory, the way a shell prompt does. A path outside
// the home directory is returned unchanged, and so is any path when the home
// directory itself cannot be resolved.
//
// The workspace resolves rows like the settings tab's vault root and
// evidence dir by joining $HOME with a fixed suffix (see EvidenceRoot), so
// without this the settings list would print whichever machine rendered the
// screen's literal home directory — /root/... inside a dev container,
// /home/runner/... in CI — instead of a portable ~/... A golden screenshot
// built from the raw path would then depend on whose machine recorded it.
func AbbreviateHome(path string) string {
	if path == "" {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(path, home+string(os.PathSeparator)); ok {
		return "~" + string(os.PathSeparator) + rest
	}
	return path
}
