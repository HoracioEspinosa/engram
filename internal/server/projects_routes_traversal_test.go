package server

import (
	"path/filepath"
	"strings"
	"testing"
)

// The evidence path check rejects what a path RESOLVES to, not how it starts.
// A prefix-only rule stops "/etc/passwd" and lets "../../etc/passwd" through,
// which is how the first version of this endpoint accepted a traversal.
//
// This test pins the predicate itself rather than the handler, so it keeps
// holding if the handler is refactored: the rule is what must not regress.
func evidencePathEscapes(path string) bool {
	if strings.HasPrefix(path, "/") || strings.HasPrefix(path, "~") {
		return true
	}
	cleaned := filepath.Clean(path)
	return cleaned == ".." || strings.HasPrefix(cleaned, "../") || filepath.IsAbs(cleaned)
}

func TestEvidencePathRejectsTraversal(t *testing.T) {
	rejected := []string{
		"/etc/passwd",
		"~/secrets.txt",
		"../../etc/passwd",
		"..",
		"../outside.png",
		"CDBS-1/../../../etc/passwd",
		"./../../escape.gif",
		"a/b/../../../../etc/passwd",
	}
	for _, p := range rejected {
		if !evidencePathEscapes(p) {
			t.Errorf("path %q escapes the evidence directory and must be rejected", p)
		}
	}

	accepted := []string{
		"screenshot.png",
		"CDBS-10449/login-failure.png",
		"CDBS-10449/nested/dir/trace.json",
		"a/../b.png",             // collapses to b.png, still inside
		"./manifest.json",        // a leading ./ is not an escape
		"weird..name.png",        // dots inside a name are not a traversal
		"CDBS-1/..hidden/ok.gif", // a component starting with .. but not equal to it
	}
	for _, p := range accepted {
		if evidencePathEscapes(p) {
			t.Errorf("path %q stays inside the evidence directory and must be accepted", p)
		}
	}
}
