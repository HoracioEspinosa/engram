package tui

import (
	"os"
	"testing"
)

// TestWorkingDirIsTheCheckoutTheProcessStandsIn pins what the graph syncer is
// pointed at: the TUI detects no checkout of its own, so whoever launched it
// is standing in the repository they mean — the same assumption
// `engram project graph sync` makes with no --repo-dir.
func TestWorkingDirIsTheCheckoutTheProcessStandsIn(t *testing.T) {
	want, err := os.Getwd()
	if err != nil {
		t.Skipf("this process has no working directory to compare against: %v", err)
	}

	if got := workingDir(); got != want {
		t.Fatalf("workingDir() = %q, want %q", got, want)
	}
}
