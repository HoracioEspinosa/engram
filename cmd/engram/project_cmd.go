// engram-projects CLI (RFC rfc-engram-projects.md §7.1): `engram project
// <slug> …` groups the per-project operations — card, upsert, graph sync,
// tasks, evidence, runbooks and context — over the exact same store and
// internal/project calls the `projects` MCP profile uses, so the CLI, the
// MCP tools and the HTTP API cannot drift apart.
//
// The plural `engram projects list|consolidate|prune` command is untouched
// and keeps living in main.go.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	projectpkg "github.com/Gentleman-Programming/engram/internal/project"
	"github.com/Gentleman-Programming/engram/internal/store"
)

// detectProjectFull is injectable for testing; wraps project.DetectProjectFull.
// main.go's detectProject only returns the name, and the CLI envelope also
// reports the source and the path the name was resolved from.
var detectProjectFull = projectpkg.DetectProjectFull

// projTTYWriter opens the controlling terminal for `context --copy`, so
// the OSC 52 escape never lands in a redirected stdout. Injectable for tests.
var projTTYWriter = func() (io.WriteCloser, bool) {
	f, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return nil, false
	}
	return f, true
}

// ─── Enumerations (mirror internal/mcp/projects_tools.go) ────────────────────

var (
	projTaskKindEnum            = []string{"feature", "bugfix", "refactor", "incident", "migration", "spike"}
	projTaskStateEnum           = []string{"open", "analysis", "in_progress", "review", "verified", "done", "blocked", "cancelled"}
	projTaskListStateEnum       = append([]string{"active"}, projTaskStateEnum...)
	projJiraStatusCategoryEnum  = []string{"new", "indeterminate", "done"}
	projTaskLinkRoleEnum        = []string{"context", "decision", "root_cause", "evidence", "summary"}
	projEvidenceKindEnum        = []string{"png", "gif", "mp4", "json", "log", "txt"}
	projRunbookCategoryEnum     = []string{"auth", "database", "queue", "network", "performance", "data-integrity", "registration"}
	projRunbookPatternEnum      = []string{"missing-files", "auth-access", "file-save-failure", "sync-upload", "registration-subscription", "other"}
	projMatchModeEnum           = []string{"all", "any"}
	projContextPackSectionEnum  = []string{"header", "card", "pointers", "pinned", "observations", "evidence", "runbooks", "refs", "footer"}
	projContextPackFormatEnum   = []string{"markdown", "json"}
	projReservedProjectSlugSet  = map[string]bool{"migrate": true, "current": true}
	projSubcommands             = map[string]bool{"card": true, "upsert": true, "graph": true, "tasks": true, "evidence": true, "runbooks": true, "context": true, "promote": true}
	projDefaultEvidenceDirEnv   = "CD_EVIDENCE_DIR"
	projDefaultEvidenceRelative = filepath.Join(".clarodrive", "evidence")
)

func projEnumContains(values []string, v string) bool {
	for _, x := range values {
		if x == v {
			return true
		}
	}
	return false
}

func projClampInt(v, min, max, def int) int {
	if v == 0 {
		return def
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// ─── Error and envelope helpers ──────────────────────────────────────────────

// projFail reports a CLI error and exits non-zero. With --json it prints the
// same {"error","code",…} envelope the MCP tools use, so a script can branch
// on `code` instead of parsing prose.
func projFail(jsonOut bool, code, message string, fields map[string]any) {
	if jsonOut {
		envelope := map[string]any{"error": message, "code": code}
		for k, v := range fields {
			envelope[k] = v
		}
		if out, err := jsonMarshalIndent(envelope, "", "  "); err == nil {
			fmt.Fprintln(os.Stdout, string(out))
		}
	} else {
		fmt.Fprintf(os.Stderr, "engram: %s\n", message)
		if hint, ok := fields["hint"].(string); ok && hint != "" {
			fmt.Fprintf(os.Stderr, "  hint: %s\n", hint)
		}
		if available, ok := fields["available_projects"].([]string); ok && len(available) > 0 {
			fmt.Fprintf(os.Stderr, "  known projects: %s\n", strings.Join(available, ", "))
		}
	}
	exitFunc(1)
}

// projScope is the resolved project for one invocation.
type projScope struct {
	Slug   string
	Source string
	Path   string
}

// projPrintResult writes either the {"project","project_source","project_path",
// "result"} JSON envelope or the human rendering produced by render.
func projPrintResult(jsonOut bool, sc projScope, result any, render func()) {
	if !jsonOut {
		render()
		return
	}
	envelope := map[string]any{
		"project":        sc.Slug,
		"project_source": sc.Source,
		"project_path":   sc.Path,
		"result":         result,
	}
	out, err := jsonMarshalIndent(envelope, "", "  ")
	if err != nil {
		fatal(err)
		return
	}
	fmt.Fprintln(os.Stdout, string(out))
}

// ─── Project resolution ──────────────────────────────────────────────────────

// projResolveScope applies the precedence documented in RFC §7.1: an explicit
// slug wins, then ENGRAM_PROJECT, then the cwd detection every other engram
// command uses.
func projResolveScope(explicit string) (projScope, error) {
	if strings.TrimSpace(explicit) != "" {
		slug, _ := store.NormalizeProject(explicit)
		return projScope{Slug: slug, Source: projectpkg.SourceExplicitOverride}, nil
	}
	if env := strings.TrimSpace(os.Getenv("ENGRAM_PROJECT")); env != "" {
		slug, _ := store.NormalizeProject(env)
		return projScope{Slug: slug, Source: projectpkg.SourceExplicitOverride}, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return projScope{}, fmt.Errorf("cannot determine project: %w", err)
	}
	det := detectProjectFull(cwd)
	if det.Error != nil {
		return projScope{}, det.Error
	}
	slug, _ := store.NormalizeProject(det.Project)
	if slug == "" {
		return projScope{}, errors.New("cannot determine project from the current directory")
	}
	return projScope{Slug: slug, Source: det.Source, Path: det.Path}, nil
}

// projIsSlugValid mirrors the tools' slug rule so the CLI cannot write a card
// the MCP surface would have refused.
func projIsSlugValid(slug string) bool {
	if slug == "" || len(slug) > 64 || projReservedProjectSlugSet[slug] {
		return false
	}
	for i := 0; i < len(slug); i++ {
		c := slug[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case c == '-' && i > 0:
		default:
			return false
		}
	}
	return true
}

// projScopeForRead resolves the project for a read-only subcommand and
// refuses one the store has never heard of.
func projScopeForRead(s *store.Store, explicit string, jsonOut bool) projScope {
	sc, err := projResolveScope(explicit)
	if err != nil {
		projFail(jsonOut, "ambiguous_project", fmt.Sprintf("cannot determine project: %s", err), nil)
		return projScope{}
	}
	if projBackedProject(s, sc.Slug) {
		return sc
	}
	stats, _ := s.Stats()
	available := []string(nil)
	if stats != nil {
		available = stats.Projects
	}
	projFail(jsonOut, "unknown_project", fmt.Sprintf("project %q not found in store", sc.Slug),
		map[string]any{"available_projects": available})
	return projScope{}
}

// projScopeForCreate resolves the project for a subcommand that may create a
// brand-new project_cards or tasks row. It applies RFC §5.0/§5.2's guard: an
// explicit slug is accepted when the store already backs it, or when it
// matches what ENGRAM_PROJECT/cwd resolve to; otherwise the write is refused
// loudly rather than bucketing data under a mistyped project.
func projScopeForCreate(s *store.Store, explicit string, jsonOut bool) projScope {
	sc, err := projResolveScope(explicit)
	if err != nil {
		projFail(jsonOut, "ambiguous_project", fmt.Sprintf("cannot determine project: %s", err), nil)
		return projScope{}
	}
	if !projIsSlugValid(sc.Slug) {
		projFail(jsonOut, "invalid_slug", fmt.Sprintf("invalid project slug %q", sc.Slug), nil)
		return projScope{}
	}
	if strings.TrimSpace(explicit) == "" || projBackedProject(s, sc.Slug) {
		return sc
	}
	if detected, derr := projResolveScope(""); derr == nil && detected.Slug == sc.Slug {
		return detected
	}
	stats, _ := s.Stats()
	available := []string(nil)
	if stats != nil {
		available = stats.Projects
	}
	projFail(jsonOut, "unknown_project",
		fmt.Sprintf("project %q is not backed by an existing card or observations, and does not match the project detected from cwd/ENGRAM_PROJECT", sc.Slug),
		map[string]any{
			"available_projects": available,
			"hint":               "run the command from the project's repository, or set ENGRAM_PROJECT",
		})
	return projScope{}
}

func projBackedProject(s *store.Store, slug string) bool {
	if slug == "" {
		return false
	}
	if backed, err := s.ProjectExists(slug); err == nil && backed {
		return true
	}
	if card, err := s.ProjectCardExists(slug); err == nil && card {
		return true
	}
	return false
}

// ─── Flag plumbing ───────────────────────────────────────────────────────────

// projFlags wraps a flag.FlagSet with the "was it actually given?" bookkeeping
// the upsert commands need: a nil pointer means "leave the column untouched",
// which an empty string cannot express.
type projFlags struct {
	fs  *flag.FlagSet
	set map[string]bool
}

func projNewFlags(name string) *projFlags {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	return &projFlags{fs: fs, set: map[string]bool{}}
}

// parse consumes args and records which flags were explicitly provided.
func (p *projFlags) parse(args []string) bool {
	if err := p.fs.Parse(args); err != nil {
		exitFunc(1)
		return false
	}
	p.fs.Visit(func(f *flag.Flag) { p.set[f.Name] = true })
	if p.fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "engram: unexpected argument %q\n", p.fs.Arg(0))
		exitFunc(1)
		return false
	}
	return true
}

func (p *projFlags) given(name string) bool { return p.set[name] }

// ptr returns a pointer to v only when the flag was explicitly given.
func (p *projFlags) ptr(name string, v *string) *string {
	if !p.set[name] {
		return nil
	}
	return v
}

func (p *projFlags) boolPtr(name string, v *bool) *bool {
	if !p.set[name] {
		return nil
	}
	return v
}

// projSplitPositional peels up to max leading non-flag tokens off args.
func projSplitPositional(args []string, max int) (positional, rest []string) {
	i := 0
	for i < len(args) && i < max && !strings.HasPrefix(args[i], "-") {
		i++
	}
	return args[:i], args[i:]
}

// ─── Rendering helpers ───────────────────────────────────────────────────────

// projWidth is the printed width of s in characters. Column padding counts
// runes, not bytes: an accented task title is one column per character, and
// measuring it in bytes would push every following column out of line.
func projWidth(s string) int { return utf8.RuneCountInString(s) }

// projPad left-aligns s in a field of width columns.
func projPad(s string, width int) string {
	if n := projWidth(s); n < width {
		return s + strings.Repeat(" ", width-n)
	}
	return s
}

// projTable renders aligned columns the way `engram projects list` does.
type projTable struct {
	headers []string
	rows    [][]string
}

func (t *projTable) add(cells ...string) { t.rows = append(t.rows, cells) }

func (t *projTable) render(w io.Writer) {
	if len(t.rows) == 0 {
		return
	}
	widths := make([]int, len(t.headers))
	for i, h := range t.headers {
		widths[i] = projWidth(h)
	}
	for _, row := range t.rows {
		for i, cell := range row {
			if i < len(widths) && projWidth(cell) > widths[i] {
				widths[i] = projWidth(cell)
			}
		}
	}
	writeRow := func(cells []string) {
		var b strings.Builder
		for i, cell := range cells {
			if i == len(cells)-1 {
				b.WriteString(cell)
				break
			}
			b.WriteString(projPad(cell, widths[i]+2))
		}
		fmt.Fprintln(w, strings.TrimRight(b.String(), " "))
	}
	writeRow(t.headers)
	for _, row := range t.rows {
		writeRow(row)
	}
}

func projStrVal(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func projDash(v string) string {
	if strings.TrimSpace(v) == "" {
		return "-"
	}
	return v
}

func projYesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func projShort(v string, n int) string {
	if v == "" {
		return "-"
	}
	runes := []rune(v)
	if len(runes) <= n {
		return v
	}
	return string(runes[:n]) + "…"
}

func projHumanBytes(n int64) string {
	switch {
	case n <= 0:
		return "-"
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KiB", float64(n)/1024)
	case n < 1024*1024*1024:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1024*1024))
	default:
		return fmt.Sprintf("%.1f GiB", float64(n)/(1024*1024*1024))
	}
}

// projThousands formats n with thin separators for the human summary lines.
func projThousands(n int) string {
	s := strconv.Itoa(n)
	if len(s) <= 3 {
		return s
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}

// projParseStaleAfter accepts "24h", "90m" or a bare number of hours and
// returns whole hours, which is what TaskListFilter stores.
func projParseStaleAfter(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 24, nil
	}
	if n, err := strconv.Atoi(raw); err == nil {
		if n < 1 {
			return 0, fmt.Errorf("--stale-after must be at least 1 hour")
		}
		return n, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("--stale-after %q is not a duration (e.g. 24h) or a number of hours", raw)
	}
	hours := int(math.Ceil(d.Hours()))
	if hours < 1 {
		return 0, fmt.Errorf("--stale-after must be at least 1 hour")
	}
	return hours, nil
}

// projSplitList splits a comma-separated flag value, dropping empty items.
func projSplitList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// ─── Entry point ─────────────────────────────────────────────────────────────

func cmdProject(cfg store.Config) {
	args := os.Args[2:]
	if len(args) == 0 {
		printProjectUsage()
		exitFunc(1)
		return
	}
	switch args[0] {
	case "help", "--help", "-h":
		printProjectUsage()
		return
	}

	slug := ""
	if !projSubcommands[args[0]] {
		slug = args[0]
		args = args[1:]
	}
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "engram: missing subcommand for `engram project %s`\n", slug)
		printProjectUsage()
		exitFunc(1)
		return
	}

	sub, rest := args[0], args[1:]
	switch sub {
	case "card":
		cmdProjectCard(cfg, slug, rest)
	case "upsert":
		cmdProjectUpsert(cfg, slug, rest)
	case "graph":
		cmdProjectGraph(cfg, slug, rest)
	case "tasks":
		cmdProjectTasks(cfg, slug, rest)
	case "evidence":
		cmdProjectEvidence(cfg, slug, rest)
	case "runbooks":
		cmdProjectRunbooks(cfg, slug, rest)
	case "context":
		cmdProjectContext(cfg, slug, rest)
	case "promote":
		cmdProjectPromote(cfg, slug, rest)
	case "help", "--help", "-h":
		printProjectUsage()
	default:
		fmt.Fprintf(os.Stderr, "engram: unknown project subcommand %q\n", sub)
		printProjectUsage()
		exitFunc(1)
	}
}

func printProjectUsage() {
	fmt.Fprint(os.Stdout, `usage: engram project [<slug>] <subcommand> [flags]

When <slug> is omitted it is resolved by precedence: ENGRAM_PROJECT, then the
project detected from the current directory. Every subcommand accepts --json.

Subcommands:
  card                        Dashboard card: pointers, counters and sync status
                                [--graph-summary]
  upsert                      Create or update the project card
                                [--display-name] [--repo-url] [--default-branch]
                                [--jira-project] [--jira-component] [--knowledge-hub]
                                [--owner] [--graph-path]
  graph sync                  Stamp graph_commit/graph_built_at/graph_summary
                                [--repo-dir <dir>] [--graph-path <rel>]
  tasks list                  List tasks with Jira-mirror freshness
                                [--state] [--kind] [--jira] [--q] [--limit]
                                [--offset] [--stale-after 24h]
  tasks upsert                Create or update a task
                                [--jira KEY] [--sdd-change] [--sync-id] [--title]
                                [--kind] [--state] [--jira-status]
                                [--jira-status-category] [--branch] [--pr]
                                [--knowledge-ref] [--assignee]
  tasks link <task>           Link an observation to a task and record refs
                                --observation <id|obs-hex> [--role] [--knowledge-ref]
                                [--graph-ref] [--graph-commit] [--runbook RB-003]
                                [--jira-ref KEY]
  evidence add <task>         Register a captured evidence file
                                (--path <rel> --sha256 <hex> | --file <path>)
                                --kind --proves [--config-stamp] [--captured-at]
                                [--size-bytes] [--manifest] [--attached-jira]
                                [--confluence-url]
  evidence list [<task>]      List evidence for the project or one task
                                [--attached-jira] [--kind] [--limit] [--offset]
  runbooks sync               Rebuild the runbook index
                                (--vault-dir <dir> | --entries-file <file.json>)
                                [--prune-missing]
  runbooks find <symptom>     Rank candidate runbooks by symptoms
                                [--category] [--pattern] [--include-stale=false]
                                [--match-mode all|any] [--limit]
  context <task>              Compose the task's context pack
                                [--max-chars] [--format markdown|json] [--sections]
                                [--observations-limit] [--observation-chars]
                                [--include-runbooks=false] [--repo-dir] [--copy]
  promote list                Pinned observations eligible to become a vault document
                                [--types decision,discovery] [--limit]
  promote stamp <obs>         Record the merged vault document on the observation
                                --knowledge-ref <path> [--allow-unpinned] [--allow-any-type]

Examples:
  engram project nextcloud card --graph-summary
  engram project nextcloud graph sync --repo-dir .
  engram project nextcloud tasks list --state active --stale-after 24h
  engram project nextcloud context CDBS-10336 --max-chars 6000 --copy
  engram project nextcloud promote list --json
  engram project nextcloud promote stamp obs-1a2b3c --knowledge-ref "Services/Nextcloud/Previews.md"
`)
}
