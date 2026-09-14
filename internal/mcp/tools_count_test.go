package mcp

import (
	"os"
	"regexp"
	"strconv"
	"testing"
)

// docToolCountPattern finds the total registered tool count a doc claims,
// anchored to a fixed phrase so an unrelated number elsewhere in the file
// (a profile's own tool count, a line number, a version) can never match.
type docToolCountPattern struct {
	path    string
	pattern *regexp.Regexp
}

// TestDocsToolCountMatchesRegistry guards against the docs drifting from the
// MCP tool registry again: it reads the total tool count out of README.md,
// DOCS.md and docs/ARCHITECTURE.md and asserts each one matches the number of
// tools NewServer actually registers with no --tools filter (the "all"
// profile). The registry count is read from the running code, not hardcoded,
// so a future tool addition or removal fails this test until the docs catch
// up — it is not enough to edit the three doc files without also touching
// the registry, or vice versa.
func TestDocsToolCountMatchesRegistry(t *testing.T) {
	s := newMCPTestStore(t)
	srv := NewServer(s)
	registryCount := len(srv.ListTools())
	if registryCount == 0 {
		t.Fatal("NewServer registered zero tools; the registry is broken, not just the docs")
	}

	docs := []docToolCountPattern{
		{
			path:    "../../README.md",
			pattern: regexp.MustCompile(`Engram registers (\d+) tools across four composable profiles`),
		},
		{
			path:    "../../DOCS.md",
			pattern: regexp.MustCompile("Omitting `--tools` registers everything \\((\\d+) tools today\\)"),
		},
		{
			path:    "../../docs/ARCHITECTURE.md",
			pattern: regexp.MustCompile(`MCP stdio server \((\d+) tools`),
		},
	}

	for _, doc := range docs {
		content, err := os.ReadFile(doc.path)
		if err != nil {
			t.Fatalf("read %s: %v", doc.path, err)
		}

		match := doc.pattern.FindSubmatch(content)
		if match == nil {
			t.Fatalf("%s: no total tool count found matching %s — did the anchor sentence move or get reworded?", doc.path, doc.pattern.String())
		}

		documented := string(match[1])
		if documented != strconv.Itoa(registryCount) {
			t.Errorf("%s documents %s registered tools, but NewServer registers %d — update the doc or the registry stayed inconsistent", doc.path, documented, registryCount)
		}
	}
}
