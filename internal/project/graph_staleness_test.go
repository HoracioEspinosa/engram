package project

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeRepoFile creates a file inside a synthetic repository, with every
// missing parent directory.
func writeRepoFile(t *testing.T, dir, rel, body string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
}

// commitAll stages every file, including the ones the ignore files exclude, and
// commits them. It returns the resulting commit hash.
func commitAll(t *testing.T, dir, message string) string {
	t.Helper()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("add", "-A", "-f", ".")
	run("commit", "-q", "--allow-empty", "-m", message)
	return run("rev-parse", "HEAD")
}

func TestGraphNotStaleOnDocsOnlyCommit(t *testing.T) {
	dir := t.TempDir()
	initGit(t, dir)
	writeRepoFile(t, dir, "internal/store/store.go", "package store\n")
	writeRepoFile(t, dir, "README.md", "# engram\n")
	writeRepoFile(t, dir, ".gitignore", "*.db\n")
	graphCommit := commitAll(t, dir, "seed")

	// The commit that produced the false positive: ignore rules and prose,
	// with no code touched at all.
	writeRepoFile(t, dir, ".gitignore", "*.db\n*.exe\n")
	writeRepoFile(t, dir, ".graphifyignore", "graphify-out/\n")
	writeRepoFile(t, dir, "README.md", "# engram\n\nUna linea mas.\n")
	head := commitAll(t, dir, "chore(graphify): version the ignore rules")

	got, err := CheckStaleness(dir, graphCommit, "", time.Now())
	if err != nil {
		t.Fatalf("CheckStaleness: %v", err)
	}
	if got.Stale {
		t.Fatalf("a commit that touched no code must not be stale: %+v", got)
	}
	if got.Reason != StaleReasonDocsOnly {
		t.Fatalf("reason %q, want %q", got.Reason, StaleReasonDocsOnly)
	}
	if got.ChangedFiles != 0 {
		t.Fatalf("changed code files %d, want 0", got.ChangedFiles)
	}
	if got.HeadCommit != head {
		t.Fatalf("head %q, want %q", got.HeadCommit, head)
	}
	if got.CheckedAt == "" {
		t.Fatal("the check must stamp when it ran")
	}
}

func TestGraphStaleOnGoFileChange(t *testing.T) {
	dir := t.TempDir()
	initGit(t, dir)
	writeRepoFile(t, dir, "internal/store/store.go", "package store\n")
	writeRepoFile(t, dir, "README.md", "# engram\n")
	graphCommit := commitAll(t, dir, "seed")

	writeRepoFile(t, dir, "internal/store/store.go", "package store\n\nfunc New() {}\n")
	writeRepoFile(t, dir, "README.md", "# engram\n\notra linea\n")
	commitAll(t, dir, "feat(store): add New")

	got, err := CheckStaleness(dir, graphCommit, "", time.Now())
	if err != nil {
		t.Fatalf("CheckStaleness: %v", err)
	}
	if !got.Stale || got.Reason != StaleReasonCodeChanged {
		t.Fatalf("expected a stale graph with reason %q, got %+v", StaleReasonCodeChanged, got)
	}
	if got.ChangedFiles != 1 {
		t.Fatalf("changed code files %d, want 1 (the README is not code)", got.ChangedFiles)
	}
}

func TestGraphIgnoreMergesGitignoreLastMatchWins(t *testing.T) {
	dir := t.TempDir()
	initGit(t, dir)
	writeRepoFile(t, dir, ".gitignore", "tools/*.go\n")
	writeRepoFile(t, dir, ".graphifyignore", "cmd/engram/main.go\n!tools/keep.go\n")
	writeRepoFile(t, dir, "cmd/engram/main.go", "package main\n")
	writeRepoFile(t, dir, "tools/keep.go", "package tools\n")
	writeRepoFile(t, dir, "tools/drop.go", "package tools\n")
	writeRepoFile(t, dir, "internal/store/store.go", "package store\n")
	graphCommit := commitAll(t, dir, "seed")

	// Four .go files change. Only two of them count: main.go is excluded by
	// .graphifyignore, drop.go by .gitignore, and keep.go is pulled back in by
	// the negation that comes after both.
	writeRepoFile(t, dir, "cmd/engram/main.go", "package main\n\nfunc main() {}\n")
	writeRepoFile(t, dir, "tools/keep.go", "package tools\n\nfunc Keep() {}\n")
	writeRepoFile(t, dir, "tools/drop.go", "package tools\n\nfunc Drop() {}\n")
	writeRepoFile(t, dir, "internal/store/store.go", "package store\n\nfunc New() {}\n")
	commitAll(t, dir, "feat: touch everything")

	got, err := CheckStaleness(dir, graphCommit, "", time.Now())
	if err != nil {
		t.Fatalf("CheckStaleness: %v", err)
	}
	if !got.Stale || got.Reason != StaleReasonCodeChanged {
		t.Fatalf("expected %q, got %+v", StaleReasonCodeChanged, got)
	}
	if got.ChangedFiles != 2 {
		t.Fatalf("changed code files %d, want 2 (tools/keep.go and internal/store/store.go)", got.ChangedFiles)
	}
}

func TestGraphCommitUnreachable(t *testing.T) {
	dir := t.TempDir()
	initGit(t, dir)
	writeRepoFile(t, dir, "internal/store/store.go", "package store\n")
	commitAll(t, dir, "seed")

	got, err := CheckStaleness(dir, "0123456789abcdef0123456789abcdef01234567", "", time.Now())
	if err != nil {
		t.Fatalf("CheckStaleness: %v", err)
	}
	if got.Reason != StaleReasonGraphCommitUnreachable {
		t.Fatalf("reason %q, want %q", got.Reason, StaleReasonGraphCommitUnreachable)
	}
	if !got.Stale {
		t.Fatal("a graph built on a commit this repository no longer has cannot be called fresh")
	}
}

func TestGraphFreshWhenHeadMatches(t *testing.T) {
	dir := t.TempDir()
	initGit(t, dir)
	writeRepoFile(t, dir, "internal/store/store.go", "package store\n")
	head := commitAll(t, dir, "seed")

	got, err := CheckStaleness(dir, head, "", time.Now())
	if err != nil {
		t.Fatalf("CheckStaleness: %v", err)
	}
	if got.Stale || got.Reason != "" {
		t.Fatalf("a graph built on HEAD is fresh, got %+v", got)
	}
	if got.HeadCommit != head {
		t.Fatalf("head %q, want %q", got.HeadCommit, head)
	}
}

func TestGraphNoGraphCommit(t *testing.T) {
	dir := t.TempDir()
	initGit(t, dir)
	writeRepoFile(t, dir, "internal/store/store.go", "package store\n")
	commitAll(t, dir, "seed")

	got, err := CheckStaleness(dir, "", "", time.Now())
	if err != nil {
		t.Fatalf("CheckStaleness: %v", err)
	}
	if !got.Stale || got.Reason != StaleReasonNoGraph {
		t.Fatalf("expected %q, got %+v", StaleReasonNoGraph, got)
	}
}

func TestGraphFreshOnAnEmptyDiff(t *testing.T) {
	dir := t.TempDir()
	initGit(t, dir)
	writeRepoFile(t, dir, "internal/store/store.go", "package store\n")
	graphCommit := commitAll(t, dir, "seed")
	commitAll(t, dir, "chore: an empty commit")

	got, err := CheckStaleness(dir, graphCommit, "", time.Now())
	if err != nil {
		t.Fatalf("CheckStaleness: %v", err)
	}
	if got.Stale || got.Reason != "" {
		t.Fatalf("a commit that changed no file at all leaves the graph fresh, got %+v", got)
	}
}

func TestCheckStalenessRequiresARepository(t *testing.T) {
	if _, err := CheckStaleness("  ", "abc", "", time.Now()); err == nil {
		t.Fatal("expected an error without a repository directory")
	}
}

func TestCheckStalenessHonoursTheManifestBesideTheGraph(t *testing.T) {
	dir := t.TempDir()
	initGit(t, dir)
	writeRepoFile(t, dir, "app/worker.rb", "puts 1\n")
	graphCommit := commitAll(t, dir, "seed")
	writeRepoFile(t, dir, "app/worker.rb", "puts 2\n")
	commitAll(t, dir, "feat: touch ruby")

	// The conventional location, with a manifest that leaves ruby out.
	writeRepoFile(t, dir, "graphify-out/manifest.json", `{"extensions":["go"]}`)
	got, err := CheckStaleness(dir, graphCommit, "", time.Now())
	if err != nil {
		t.Fatalf("CheckStaleness: %v", err)
	}
	if got.Stale || got.Reason != StaleReasonDocsOnly {
		t.Fatalf("a manifest that does not cover ruby makes the change docs_only, got %+v", got)
	}

	// A graph somewhere else: the manifest beside it is the one that counts.
	writeRepoFile(t, dir, "otro/manifest.json", `{"extensions":["rb"]}`)
	got, err = CheckStaleness(dir, graphCommit, "otro/graph.json", time.Now())
	if err != nil {
		t.Fatalf("CheckStaleness: %v", err)
	}
	if !got.Stale || got.ChangedFiles != 1 {
		t.Fatalf("the manifest beside the graph must be the one read, got %+v", got)
	}

	// The same graph named by an absolute path resolves to the same manifest.
	got, err = CheckStaleness(dir, graphCommit, filepath.Join(dir, "otro", "graph.json"), time.Now())
	if err != nil {
		t.Fatalf("CheckStaleness: %v", err)
	}
	if !got.Stale || got.ChangedFiles != 1 {
		t.Fatalf("an absolute graph path must resolve the same manifest, got %+v", got)
	}
}

func TestIgnoreMatcherPatterns(t *testing.T) {
	dir := t.TempDir()
	writeRepoFile(t, dir, ".gitignore", strings.Join([]string{
		"# a comment, and a blank line follow",
		"",
		"*.log",
		"build/",
	}, "\n")+"\n")
	writeRepoFile(t, dir, ".graphifyignore", strings.Join([]string{
		"*.min.js",
		"**/testdata/",
		"/engram",
		"tmp?/",
		"docs/*.md",
		"!docs/KEEP.md",
	}, "\n")+"\n")

	ignore, err := LoadIgnore(dir)
	if err != nil {
		t.Fatalf("LoadIgnore: %v", err)
	}

	cases := []struct {
		name    string
		relPath string
		isDir   bool
		want    bool
	}{
		{name: "gitignore glob", relPath: "app.log", want: true},
		{name: "gitignore glob nested", relPath: "internal/run.log", want: true},
		{name: "gitignore directory rule", relPath: "build/out.js", want: true},
		{name: "gitignore directory itself", relPath: "build", isDir: true, want: true},
		{name: "directory rule ignores a same-named file", relPath: "build", want: false},
		{name: "graphifyignore glob", relPath: "static/app.min.js", want: true},
		{name: "glob does not overreach", relPath: "static/app.js", want: false},
		{name: "double star at any depth", relPath: "internal/a/testdata/x.json", want: true},
		{name: "double star at the root", relPath: "testdata/x.json", want: true},
		{name: "anchored at the root", relPath: "engram", want: true},
		{name: "anchored does not match deeper", relPath: "cmd/engram", want: false},
		{name: "question mark matches one character", relPath: "tmp1/x.txt", want: true},
		{name: "question mark matches any character", relPath: "tmpX/x.txt", want: true},
		{name: "question mark needs a character", relPath: "tmp/x.txt", want: false},
		{name: "path glob", relPath: "docs/guide.md", want: true},
		{name: "negation wins as the last match", relPath: "docs/KEEP.md", want: false},
		{name: "segment glob does not cross a slash", relPath: "docs/sub/guide.md", want: false},
		{name: "unmatched path", relPath: "internal/store/store.go", want: false},
		{name: "empty path", relPath: "", want: false},
		{name: "leading dot slash is normalised", relPath: "./app.log", want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ignore.Match(tc.relPath, tc.isDir); got != tc.want {
				t.Fatalf("Match(%q, isDir=%v) = %v, want %v", tc.relPath, tc.isDir, got, tc.want)
			}
		})
	}
}

func TestLoadIgnoreToleratesMissingFiles(t *testing.T) {
	dir := t.TempDir()
	ignore, err := LoadIgnore(dir)
	if err != nil {
		t.Fatalf("LoadIgnore: %v", err)
	}
	if ignore.Match("internal/store/store.go", false) {
		t.Fatal("a repository without ignore files ignores nothing")
	}
}

func TestCodeExtensionsFallsBackToTheDefaultSet(t *testing.T) {
	defaults := CodeExtensions("")
	for _, ext := range []string{".go", ".ts", ".tsx", ".php", ".py", ".templ", ".vue", ".svelte"} {
		if !defaults[ext] {
			t.Fatalf("the default set must contain %s", ext)
		}
	}
	if defaults[".md"] {
		t.Fatal("markdown is not code")
	}

	// graphify's own manifest is a stat index of file paths, not an extension
	// list, so it leaves the default set in place.
	dir := t.TempDir()
	statIndex := filepath.Join(dir, "manifest.json")
	writeRepoFile(t, dir, "manifest.json", `{"code":{"internal/store/store.go":"abc"}}`)
	if got := CodeExtensions(statIndex); !got[".go"] || !got[".rs"] {
		t.Fatalf("a manifest without an extension list must not narrow the default set: %v", got)
	}

	listed := filepath.Join(dir, "listed.json")
	writeRepoFile(t, dir, "listed.json", `{"extensions":["go","  .TS  "]}`)
	got := CodeExtensions(listed)
	if !got[".go"] || !got[".ts"] {
		t.Fatalf("an explicit list must be honoured, normalised and lowercased: %v", got)
	}
	if got[".rs"] {
		t.Fatalf("an explicit list replaces the default set: %v", got)
	}
}
