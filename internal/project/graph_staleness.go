package project

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// Reasons a graph is or is not stale. The empty reason means fresh.
const (
	// StaleReasonNoGraph means the card records no graph_commit: there is no
	// graph to be fresh.
	StaleReasonNoGraph = "no_graph"
	// StaleReasonCodeChanged means code files the graph covers changed since
	// it was built.
	StaleReasonCodeChanged = "code_changed"
	// StaleReasonDocsOnly means the repository moved but nothing the graph
	// covers changed. The graph is still correct; only its provenance is
	// behind HEAD.
	StaleReasonDocsOnly = "docs_only"
	// StaleReasonGraphCommitUnreachable means the commit the graph was built
	// on is not in this repository any more (a rebase, a shallow clone, a
	// pruned branch), so nothing can be compared.
	StaleReasonGraphCommitUnreachable = "graph_commit_unreachable"
)

// graphDiffTimeout caps the diff. A stalled git process must not hang a TUI
// redraw or an MCP call, and a diff that takes this long is a symptom, not a
// result worth waiting for.
const graphDiffTimeout = 10 * time.Second

// GraphStaleness is the answer to "does this graph still describe this
// checkout?".
type GraphStaleness struct {
	Stale        bool   `json:"stale"`
	Reason       string `json:"reason"`
	ChangedFiles int    `json:"changed_files"`
	HeadCommit   string `json:"head_commit"`
	CheckedAt    string `json:"checked_at"`
}

// defaultCodeExtensions is the set of extensions a path must carry to count as
// code when the graph's manifest does not name its own.
var defaultCodeExtensions = []string{
	".go", ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".php", ".py", ".rb",
	".java", ".kt", ".cs", ".rs", ".swift", ".c", ".h", ".cc", ".cpp", ".hpp",
	".sql", ".templ", ".vue", ".svelte",
}

// CheckStaleness reports whether the graph built at graphCommit still
// describes repoDir, by asking which files changed between that commit and
// HEAD and counting only the ones the graph covers.
//
// Comparing commits alone answers a different question than the one users
// care about: a commit that only touches prose or ignore rules moves HEAD
// without touching a single node, and reporting that as stale trains everyone
// to ignore the indicator. So a non-empty diff with no code in it is reported
// as docs_only and is not stale; an unreachable graph commit and a card with
// no graph at all are stale, because neither can be shown to be current.
//
// graphPath locates the graph so the manifest beside it can name the
// extensions that count as code; an empty graphPath looks in
// <repoDir>/graphify-out. SyncGraph delegates its staleness verdict to this
// function once the project card can persist the reason, the file count and
// the check timestamp alongside the commit it already stores.
func CheckStaleness(repoDir, graphCommit, graphPath string, now time.Time) (GraphStaleness, error) {
	repoDir = strings.TrimSpace(repoDir)
	if repoDir == "" {
		return GraphStaleness{}, fmt.Errorf("engram-projects: staleness check needs a repository directory")
	}
	out := GraphStaleness{
		HeadCommit: gitHeadCommitTimeout(repoDir),
		CheckedAt:  now.UTC().Format("2006-01-02 15:04:05"),
	}

	graphCommit = strings.TrimSpace(graphCommit)
	if graphCommit == "" {
		out.Stale = true
		out.Reason = StaleReasonNoGraph
		return out, nil
	}
	if out.HeadCommit != "" && graphCommit == out.HeadCommit {
		return out, nil
	}

	changed, err := gitDiffNames(repoDir, graphCommit)
	if err != nil {
		out.Stale = true
		out.Reason = StaleReasonGraphCommitUnreachable
		return out, nil
	}
	if len(changed) == 0 {
		return out, nil
	}

	ignore, err := LoadIgnore(repoDir)
	if err != nil {
		return GraphStaleness{}, err
	}
	extensions := CodeExtensions(manifestPathFor(repoDir, graphPath))
	for _, rel := range changed {
		if ignore.Match(rel, false) {
			continue
		}
		if extensions[strings.ToLower(path.Ext(rel))] {
			out.ChangedFiles++
		}
	}
	out.Stale = out.ChangedFiles > 0
	if out.Stale {
		out.Reason = StaleReasonCodeChanged
	} else {
		out.Reason = StaleReasonDocsOnly
	}
	return out, nil
}

// manifestPathFor locates the graph's manifest next to graph.json, defaulting
// to the conventional graphify-out directory of the repository.
func manifestPathFor(repoDir, graphPath string) string {
	if strings.TrimSpace(graphPath) == "" {
		return filepath.Join(repoDir, "graphify-out", "manifest.json")
	}
	if !filepath.IsAbs(graphPath) {
		graphPath = filepath.Join(repoDir, graphPath)
	}
	return filepath.Join(filepath.Dir(graphPath), "manifest.json")
}

// gitDiffNames lists the paths that changed between graphCommit and HEAD.
func gitDiffNames(repoDir, graphCommit string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), graphDiffTimeout)
	defer cancel()
	cmd := newProjectCommandContext(ctx, "git", "-C", repoDir, "diff", "--name-only", graphCommit+"..HEAD")
	stdout, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("engram-projects: git diff %s..HEAD: %w", graphCommit, err)
	}
	var out []string
	for _, line := range strings.Split(string(stdout), "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out, nil
}

// CodeExtensions returns the extensions that make a changed path count as
// code. When the graph's manifest names its own list it wins; otherwise the
// default set applies.
//
// graphify writes manifest.json as a stat index — a map of extractor to the
// file paths it stamped — so in practice no extension list is there and the
// default is what runs. The extensions are deliberately not derived from those
// file paths: a manifest written before a language entered the repository
// would silently stop counting that language as code, which is the failure
// mode this whole check exists to avoid.
func CodeExtensions(manifestPath string) map[string]bool {
	if listed := manifestExtensions(manifestPath); len(listed) > 0 {
		return listed
	}
	out := make(map[string]bool, len(defaultCodeExtensions))
	for _, ext := range defaultCodeExtensions {
		out[ext] = true
	}
	return out
}

// manifestExtensions reads an explicit extension list out of a manifest, at
// "extensions" or "code_extensions", either at the top level or under "code".
// Anything else yields nothing and leaves the default set in place.
func manifestExtensions(manifestPath string) map[string]bool {
	if strings.TrimSpace(manifestPath) == "" {
		return nil
	}
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil
	}
	var manifest map[string]json.RawMessage
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil
	}
	scopes := []map[string]json.RawMessage{manifest}
	if nested, ok := manifest["code"]; ok {
		var inner map[string]json.RawMessage
		if err := json.Unmarshal(nested, &inner); err == nil {
			scopes = append(scopes, inner)
		}
	}
	for _, scope := range scopes {
		for _, key := range []string{"extensions", "code_extensions"} {
			value, ok := scope[key]
			if !ok {
				continue
			}
			var listed []string
			if err := json.Unmarshal(value, &listed); err != nil {
				continue
			}
			out := make(map[string]bool, len(listed))
			for _, ext := range listed {
				if normalized := normalizeExtension(ext); normalized != "" {
					out[normalized] = true
				}
			}
			if len(out) > 0 {
				return out
			}
		}
	}
	return nil
}

// normalizeExtension lowercases an extension and gives it the leading dot the
// lookup expects.
func normalizeExtension(ext string) string {
	trimmed := strings.ToLower(strings.TrimSpace(ext))
	if trimmed == "" || trimmed == "." {
		return ""
	}
	if !strings.HasPrefix(trimmed, ".") {
		trimmed = "." + trimmed
	}
	return trimmed
}

// ─── ignore rules ────────────────────────────────────────────────────────────

// Ignore is the merged exclusion list of a repository: .gitignore first, then
// .graphifyignore, with the last matching rule deciding. That order is what
// .graphifyignore itself declares — it merges with .gitignore rather than
// replacing it — and it is what lets a negation there pull a path back in that
// .gitignore excluded.
type Ignore struct {
	rules []ignoreRule
}

// ignoreRule is one parsed pattern.
type ignoreRule struct {
	// segments is the pattern split on "/", each entry a glob matched against
	// one path segment, except "**" which matches any run of segments.
	segments []string
	// negate marks a "!" rule, which re-includes what an earlier rule excluded.
	negate bool
	// dirOnly marks a pattern written with a trailing "/": it matches
	// directories only.
	dirOnly bool
	// anchored marks a pattern that must match from the repository root,
	// either because it was written with a leading "/" or because it contains
	// a "/" of its own.
	anchored bool
}

// LoadIgnore reads .gitignore and then .graphifyignore from repoDir. Missing
// files are not an error: a repository is free to carry neither.
func LoadIgnore(repoDir string) (*Ignore, error) {
	out := &Ignore{}
	for _, name := range []string{".gitignore", ".graphifyignore"} {
		raw, err := os.ReadFile(filepath.Join(repoDir, name))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("engram-projects: read %s: %w", name, err)
		}
		for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
			if rule, ok := parseIgnoreLine(line); ok {
				out.rules = append(out.rules, rule)
			}
		}
	}
	return out, nil
}

// parseIgnoreLine turns one line into a rule, reporting false for blanks and
// comments.
func parseIgnoreLine(line string) (ignoreRule, bool) {
	pattern := strings.TrimRight(line, " \t")
	pattern = strings.TrimLeft(pattern, " \t")
	if pattern == "" || strings.HasPrefix(pattern, "#") {
		return ignoreRule{}, false
	}
	var rule ignoreRule
	if strings.HasPrefix(pattern, "!") {
		rule.negate = true
		pattern = pattern[1:]
	} else if strings.HasPrefix(pattern, `\`) {
		// An escaped leading "#" or "!" is a literal character.
		pattern = pattern[1:]
	}
	if strings.HasSuffix(pattern, "/") {
		rule.dirOnly = true
		pattern = strings.TrimSuffix(pattern, "/")
	}
	if strings.HasPrefix(pattern, "/") {
		rule.anchored = true
		pattern = strings.TrimPrefix(pattern, "/")
	}
	if pattern == "" {
		return ignoreRule{}, false
	}
	if strings.Contains(pattern, "/") {
		rule.anchored = true
	}
	rule.segments = strings.Split(pattern, "/")
	return rule, true
}

// Match reports whether relPath is excluded. relPath is repository-relative
// and slash-separated.
//
// Every ancestor directory is tested first, and an excluded ancestor settles
// the answer: git cannot re-include a file whose parent directory is
// excluded, and a rule such as "graphify-out/" has to exclude everything
// underneath even though the diff only ever names files.
func (ig *Ignore) Match(relPath string, isDir bool) bool {
	normalized := strings.Trim(strings.TrimPrefix(filepath.ToSlash(relPath), "./"), "/")
	if normalized == "" || ig == nil || len(ig.rules) == 0 {
		return false
	}
	segments := strings.Split(normalized, "/")
	for i := range segments {
		last := i == len(segments)-1
		excluded := ig.matchSegments(segments[:i+1], !last || isDir)
		if last {
			return excluded
		}
		if excluded {
			return true
		}
	}
	return false
}

// matchSegments applies every rule to one path, in order, letting the last
// match decide.
func (ig *Ignore) matchSegments(pathSegments []string, isDir bool) bool {
	excluded := false
	for _, rule := range ig.rules {
		if rule.dirOnly && !isDir {
			continue
		}
		if rule.matches(pathSegments) {
			excluded = !rule.negate
		}
	}
	return excluded
}

// matches reports whether the rule covers this path. An anchored rule matches
// from the root; an unanchored one may start at any segment, which is how a
// bare "node_modules" or "*.min.js" matches at every depth.
func (r ignoreRule) matches(pathSegments []string) bool {
	if r.anchored {
		return matchSegmentRun(r.segments, pathSegments)
	}
	for i := range pathSegments {
		if matchSegmentRun(r.segments, pathSegments[i:]) {
			return true
		}
	}
	return false
}

// matchSegmentRun matches a pattern's segments against a path's segments,
// where "**" consumes any number of segments, including none.
func matchSegmentRun(pattern, pathSegments []string) bool {
	if len(pattern) == 0 {
		return len(pathSegments) == 0
	}
	if pattern[0] == "**" {
		for i := 0; i <= len(pathSegments); i++ {
			if matchSegmentRun(pattern[1:], pathSegments[i:]) {
				return true
			}
		}
		return false
	}
	if len(pathSegments) == 0 {
		return false
	}
	if !matchSegment(pattern[0], pathSegments[0]) {
		return false
	}
	return matchSegmentRun(pattern[1:], pathSegments[1:])
}

// matchSegment globs one path segment. path.Match gives "*", "?" and
// character classes without letting any of them cross a separator, which is
// exactly the per-segment semantics gitignore asks for. A malformed pattern
// falls back to a literal comparison rather than silently matching nothing.
func matchSegment(pattern, name string) bool {
	ok, err := path.Match(pattern, name)
	if err != nil {
		return pattern == name
	}
	return ok
}
