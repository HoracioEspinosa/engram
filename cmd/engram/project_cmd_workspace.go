// Workspace subcommands of `engram project`: the project tree, the aliases
// that redirect a name onto it, the vault scan and import, the benchmarks a
// change was argued with, and the search that reads across all of them.
//
// Every one of them calls internal/workspace, which is the same code the
// workspace MCP tools call, so the shell and the agent cannot disagree on what
// an import did.
package main

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/vault"
	"github.com/HoracioEspinosa/engram/internal/workspace"
)

// ─── Enumerations ────────────────────────────────────────────────────────────

var (
	projProjectKindEnum      = []string{"umbrella", "repo", "instance", "service", "dataset", "knowledge"}
	projAliasSourceEnum      = []string{"git_remote", "dir", "env", "manual", "normalizer"}
	projBenchmarkUnitEnum    = []string{"ms", "s", "count", "bytes", "kib", "mib", "pct", "ops", "rps", "usd", "score"}
	projBenchmarkDirections  = []string{"lower", "higher"}
	projWorkspaceKindEnum    = []string{"observation", "task", "evidence", "runbook", "card", "benchmark"}
	projProjectKindGlyphs    = map[string]string{"umbrella": "▣", "repo": "▪", "instance": "▫", "service": "◆", "dataset": "▤", "knowledge": "▦"}
	projDefaultProjectGlyph  = "·"
	projTreeIndentUnit       = "  "
	projBenchmarkListDefault = 50
)

// ─── Shared helpers ──────────────────────────────────────────────────────────

// projLooseScope resolves the project for a command whose subject is the whole
// forest rather than one project. It never fails: a tree spanning every project
// has no single one to refuse over, and an envelope reporting an empty project
// is more honest than one refusing to answer.
func projLooseScope(explicit string) projScope {
	sc, err := projResolveScope(explicit)
	if err != nil {
		return projScope{}
	}
	return sc
}

// projNamedScope is the envelope scope for a command the caller scoped by
// name. A blank name reports an empty scope rather than the project the
// current directory happens to sit in, which would describe the caller instead
// of the answer.
func projNamedScope(name string) projScope {
	if strings.TrimSpace(name) == "" {
		return projScope{}
	}
	return projLooseScope(name)
}

// projStringList collects a flag given more than once, which is how a card
// takes several tags or several aliases in one call.
type projStringList []string

func (l *projStringList) String() string { return strings.Join(*l, ",") }

func (l *projStringList) Set(v string) error {
	for _, item := range projSplitList(v) {
		*l = append(*l, item)
	}
	return nil
}

// projFailWorkspace reports a workspace failure under the code the operation
// itself declared, so the CLI never re-derives one from an error message.
func projFailWorkspace(jsonOut bool, err error, fields map[string]any) {
	code := workspace.CodeOf(err)
	if code == "" {
		fatal(err)
		return
	}
	projFail(jsonOut, code, err.Error(), fields)
}

// ─── tree ────────────────────────────────────────────────────────────────────

func cmdProjectTree(cfg store.Config, slug string, args []string) {
	if len(args) > 0 {
		switch args[0] {
		case "suggest":
			cmdProjectTreeSuggest(cfg, slug, args[1:])
			return
		case "apply":
			cmdProjectTreeApply(cfg, slug, args[1:])
			return
		case "doctor":
			cmdProjectTreeDoctor(cfg, slug, args[1:])
			return
		}
	}
	cmdProjectTreeList(cfg, slug, args)
}

func cmdProjectTreeList(cfg store.Config, slug string, args []string) {
	positional, rest := projSplitPositional(args, 1)
	f := projNewFlags("engram project tree")
	jsonOut := f.fs.Bool("json", false, "print the JSON envelope")
	counts := f.fs.Bool("counts", false, "include per-project counters")
	if !f.parse(rest) {
		return
	}

	root := slug
	if len(positional) > 0 {
		root = positional[0]
	}

	s, ok := projOpenStore(cfg)
	if !ok {
		return
	}
	defer s.Close()

	nodes, err := s.ProjectTree(root, *counts)
	if errors.Is(err, store.ErrNoProjectCard) {
		projFail(*jsonOut, "unknown_project", fmt.Sprintf("project %q has no card", root), nil)
		return
	}
	if err != nil {
		fatal(err)
		return
	}

	// A whole-forest walk has no single project to report, and naming the one
	// the current directory happens to sit in would describe the caller rather
	// than the answer.
	sc := projScope{Slug: root}
	result := map[string]any{"root": root, "nodes": nodes}
	projPrintResult(*jsonOut, sc, result, func() {
		if len(nodes) == 0 {
			fmt.Println("no project cards")
			return
		}
		base := nodes[0].Depth
		for _, node := range nodes {
			indent := strings.Repeat(projTreeIndentUnit, node.Depth-base)
			line := fmt.Sprintf("%s%s %s", indent, projProjectGlyph(node.Kind), node.Slug)
			if node.DisplayName != "" && node.DisplayName != node.Slug {
				line += "  (" + node.DisplayName + ")"
			}
			if node.Counts != nil {
				line += fmt.Sprintf("  %d obs · %d/%d tasks · %d evidence",
					node.Counts.Observations, node.Counts.TasksActive, node.Counts.TasksTotal, node.Counts.Evidence)
			}
			fmt.Println(line)
		}
	})
}

// projProjectGlyph renders a project kind in one column, so an indented tree
// stays aligned whatever kinds it holds.
func projProjectGlyph(kind string) string {
	if glyph, ok := projProjectKindGlyphs[kind]; ok {
		return glyph
	}
	return projDefaultProjectGlyph
}

func cmdProjectTreeSuggest(cfg store.Config, slug string, args []string) {
	f := projNewFlags("engram project tree suggest")
	jsonOut := f.fs.Bool("json", false, "print the JSON envelope")
	if !f.parse(args) {
		return
	}

	s, ok := projOpenStore(cfg)
	if !ok {
		return
	}
	defer s.Close()

	suggestions, err := workspace.SuggestTree(s)
	if err != nil {
		fatal(err)
		return
	}
	if suggestions == nil {
		suggestions = []store.ProjectTreeSuggestion{}
	}

	// The separator pairs ride along with the families: both are answers to
	// "what is wrong with how these projects are named", and only one of them
	// the tree can fix.
	pairs, err := workspace.SuggestSeparatorPairs(s)
	if err != nil {
		fatal(err)
		return
	}
	if pairs == nil {
		pairs = []store.ProjectSeparatorPair{}
	}

	result := map[string]any{"suggestions": suggestions, "separator_pairs": pairs}
	projPrintResult(*jsonOut, projLooseScope(slug), result, func() {
		if len(suggestions) == 0 && len(pairs) == 0 {
			fmt.Println("no families to group")
			return
		}
		if len(suggestions) > 0 {
			table := &projTable{headers: []string{"PARENT", "EXISTS", "CHILDREN", "REASON"}}
			for _, s := range suggestions {
				table.add(s.Parent, projYesNo(s.ParentExists), strings.Join(s.Children, ", "), s.Reason)
			}
			table.render(os.Stdout)
			fmt.Println()
		}
		if len(pairs) > 0 {
			fmt.Println("one project spelled more than one way (a tree cannot fix this; see: engram projects merge):")
			for _, pair := range pairs {
				fmt.Printf("  %s: %s\n", pair.Folded, strings.Join(pair.Names, ", "))
			}
			fmt.Println()
		}
		if len(suggestions) > 0 {
			fmt.Println("nothing was changed; run: engram project tree apply --from-suggest")
		}
	})
}

func cmdProjectTreeApply(cfg store.Config, slug string, args []string) {
	f := projNewFlags("engram project tree apply")
	jsonOut := f.fs.Bool("json", false, "print the JSON envelope")
	fromSuggest := f.fs.Bool("from-suggest", false, "apply what `tree suggest` proposes")
	yes := f.fs.Bool("yes", false, "do not ask for confirmation")
	if !f.parse(args) {
		return
	}
	if !*fromSuggest {
		projFail(*jsonOut, "missing_field",
			"engram project tree apply needs --from-suggest: there is no other source of moves", nil)
		return
	}

	s, ok := projOpenStore(cfg)
	if !ok {
		return
	}
	defer s.Close()

	suggestions, err := workspace.SuggestTree(s)
	if err != nil {
		fatal(err)
		return
	}
	if len(suggestions) == 0 {
		projPrintResult(*jsonOut, projLooseScope(slug),
			map[string]any{"applied": []any{}, "conflicts": []any{}, "parents_created": []any{}},
			func() { fmt.Println("no families to group") })
		return
	}
	if !*yes && !*jsonOut {
		fmt.Println("about to reparent:")
		for _, suggestion := range suggestions {
			fmt.Printf("  %s <- %s\n", suggestion.Parent, strings.Join(suggestion.Children, ", "))
		}
		if !projConfirm("apply these moves?") {
			fmt.Println("nothing was changed")
			return
		}
	}

	report, err := workspace.ApplySuggestedTree(s, suggestions)
	if err != nil {
		fatal(err)
		return
	}

	projPrintResult(*jsonOut, projLooseScope(slug), report, func() {
		for _, move := range report.Applied {
			fmt.Printf("moved %s under %s\n", move.Child, move.Parent)
		}
		for _, parent := range report.ParentsCreated {
			fmt.Printf("created umbrella %s\n", parent)
		}
		for _, conflict := range report.Conflicts {
			fmt.Printf("refused %s -> %s: %s (%s)\n",
				conflict.Child, conflict.Parent, conflict.Reason, conflict.Code)
		}
		fmt.Printf("%d moved · %d refused\n", len(report.Applied), len(report.Conflicts))
	})
}

// projConfirm asks a yes/no question on the terminal. A non-interactive
// invocation answers no, because a prompt nobody sees is not consent.
func projConfirm(question string) bool {
	fmt.Printf("%s [y/N] ", question)
	var answer string
	if _, err := fmt.Fscanln(os.Stdin, &answer); err != nil {
		return false
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes"
}

func cmdProjectTreeDoctor(cfg store.Config, slug string, args []string) {
	f := projNewFlags("engram project tree doctor")
	jsonOut := f.fs.Bool("json", false, "print the JSON envelope")
	fix := f.fs.Bool("fix", false, "detach every broken card to the top of the tree")
	if !f.parse(args) {
		return
	}

	s, ok := projOpenStore(cfg)
	if !ok {
		return
	}
	defer s.Close()

	faults, err := workspace.TreeDoctor(s, *fix)
	if err != nil {
		fatal(err)
		return
	}

	result := map[string]any{"faults": faults, "fixed": *fix}
	projPrintResult(*jsonOut, projLooseScope(slug), result, func() {
		if len(faults) == 0 {
			fmt.Println("the project tree is consistent")
			return
		}
		table := &projTable{headers: []string{"PROJECT", "FAULT", "FIXED", "DETAIL"}}
		for _, fault := range faults {
			table.add(fault.Slug, fault.Fault, projYesNo(fault.Fixed), fault.Detail)
		}
		table.render(os.Stdout)
	})
}

// ─── set-parent ──────────────────────────────────────────────────────────────

func cmdProjectSetParent(cfg store.Config, slug string, args []string) {
	positional, rest := projSplitPositional(args, 1)
	f := projNewFlags("engram project set-parent")
	jsonOut := f.fs.Bool("json", false, "print the JSON envelope")
	to := f.fs.String("to", "", "slug of the parent project")
	toRoot := f.fs.Bool("root", false, "move the project to the top of the tree")
	if !f.parse(rest) {
		return
	}

	child := slug
	if len(positional) > 0 {
		child = positional[0]
	}
	if strings.TrimSpace(child) == "" {
		projFail(*jsonOut, "missing_field", "usage: engram project set-parent <slug> --to <parent>|--root", nil)
		return
	}
	if (*to == "") == !*toRoot {
		projFail(*jsonOut, "missing_field", "pass exactly one of --to <parent> and --root", nil)
		return
	}

	s, ok := projOpenStore(cfg)
	if !ok {
		return
	}
	defer s.Close()

	var parent *string
	if !*toRoot {
		target := strings.TrimSpace(*to)
		parent = &target
	}

	if err := s.SetProjectParent(child, parent); err != nil {
		switch {
		case errors.Is(err, store.ErrProjectCycle):
			projFail(*jsonOut, "project_cycle", err.Error(), nil)
		case errors.Is(err, store.ErrProjectDepthExceeded):
			projFail(*jsonOut, "project_depth_exceeded", err.Error(), nil)
		case errors.Is(err, store.ErrNoProjectCard):
			projFail(*jsonOut, "unknown_project", err.Error(), nil)
		default:
			fatal(err)
		}
		return
	}

	card, err := s.GetProjectCard(child)
	if err != nil {
		fatal(err)
		return
	}
	projPrintResult(*jsonOut, projScope{Slug: child}, map[string]any{"card": card}, func() {
		if card.ParentSlug == nil {
			fmt.Printf("%s is now at the top of the tree\n", card.Slug)
			return
		}
		fmt.Printf("%s is now under %s (depth %d)\n", card.Slug, *card.ParentSlug, card.Depth)
	})
}

// ─── alias ───────────────────────────────────────────────────────────────────

func cmdProjectAlias(cfg store.Config, slug string, args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: engram project alias <list|add|rm> [flags]")
		exitFunc(1)
		return
	}
	switch args[0] {
	case "list":
		cmdProjectAliasList(cfg, slug, args[1:])
	case "add":
		cmdProjectAliasAdd(cfg, slug, args[1:])
	case "rm":
		cmdProjectAliasRemove(cfg, slug, args[1:])
	default:
		fmt.Fprintf(os.Stderr, "engram: unknown alias subcommand %q\n", args[0])
		fmt.Fprintln(os.Stderr, "usage: engram project alias <list|add|rm> [flags]")
		exitFunc(1)
	}
}

func cmdProjectAliasList(cfg store.Config, slug string, args []string) {
	positional, rest := projSplitPositional(args, 1)
	f := projNewFlags("engram project alias list")
	jsonOut := f.fs.Bool("json", false, "print the JSON envelope")
	if !f.parse(rest) {
		return
	}

	target := slug
	if len(positional) > 0 {
		target = positional[0]
	}

	s, ok := projOpenStore(cfg)
	if !ok {
		return
	}
	defer s.Close()

	aliases, err := s.ListProjectAliases(target)
	if err != nil {
		fatal(err)
		return
	}
	if aliases == nil {
		aliases = []store.ProjectAlias{}
	}

	projPrintResult(*jsonOut, projScope{Slug: target}, map[string]any{"aliases": aliases}, func() {
		if len(aliases) == 0 {
			fmt.Println("no aliases")
			return
		}
		table := &projTable{headers: []string{"ALIAS", "PROJECT", "SOURCE", "UPDATED"}}
		for _, alias := range aliases {
			table.add(alias.Alias, alias.Slug, alias.Source, alias.UpdatedAt)
		}
		table.render(os.Stdout)
	})
}

func cmdProjectAliasAdd(cfg store.Config, slug string, args []string) {
	positional, rest := projSplitPositional(args, 1)
	f := projNewFlags("engram project alias add")
	jsonOut := f.fs.Bool("json", false, "print the JSON envelope")
	to := f.fs.String("to", "", "slug the alias redirects to")
	source := f.fs.String("source", "manual", strings.Join(projAliasSourceEnum, "|"))
	if !f.parse(rest) {
		return
	}
	if len(positional) == 0 {
		projFail(*jsonOut, "missing_field", "usage: engram project alias add <alias> --to <slug>", nil)
		return
	}
	alias := positional[0]

	target := strings.TrimSpace(*to)
	if target == "" {
		target = slug
	}
	if target == "" {
		projFail(*jsonOut, "missing_field", "--to names the project the alias redirects to", nil)
		return
	}
	if !projEnumContains(projAliasSourceEnum, *source) {
		projFail(*jsonOut, "invalid_enum", fmt.Sprintf("source %q is invalid", *source),
			map[string]any{"hint": "one of " + strings.Join(projAliasSourceEnum, ", ")})
		return
	}

	s, ok := projOpenStore(cfg)
	if !ok {
		return
	}
	defer s.Close()

	err := s.UpsertProjectAlias(alias, target, *source)
	var owns *store.AliasOwnsRowsError
	switch {
	case errors.As(err, &owns):
		projFail(*jsonOut, "alias_owns_rows", err.Error(), map[string]any{
			"alias":        owns.Alias,
			"slug":         owns.Slug,
			"observations": owns.Rows,
			"hint":         fmt.Sprintf("run: engram projects merge %s %s", owns.Alias, owns.Slug),
		})
		return
	case errors.Is(err, store.ErrNoProjectCard):
		projFail(*jsonOut, "unknown_project", fmt.Sprintf("project %q is not backed by a card or observations", target), nil)
		return
	case err != nil:
		fatal(err)
		return
	}

	resolution, err := s.ResolveProjectSlug(alias)
	if err != nil {
		fatal(err)
		return
	}
	projPrintResult(*jsonOut, projScope{Slug: target},
		map[string]any{"alias": alias, "slug": target, "source": *source, "resolved": resolution},
		func() { fmt.Printf("%s now resolves to %s (via %s)\n", alias, resolution.Slug, resolution.Via) })
}

func cmdProjectAliasRemove(cfg store.Config, slug string, args []string) {
	positional, rest := projSplitPositional(args, 1)
	f := projNewFlags("engram project alias rm")
	jsonOut := f.fs.Bool("json", false, "print the JSON envelope")
	if !f.parse(rest) {
		return
	}
	if len(positional) == 0 {
		projFail(*jsonOut, "missing_field", "usage: engram project alias rm <alias>", nil)
		return
	}
	alias := positional[0]

	s, ok := projOpenStore(cfg)
	if !ok {
		return
	}
	defer s.Close()

	if err := s.DeleteProjectAlias(alias); err != nil {
		fatal(err)
		return
	}
	projPrintResult(*jsonOut, projScope{Slug: slug}, map[string]any{"alias": alias, "deleted": true},
		func() { fmt.Printf("alias %s retired\n", alias) })
}

// ─── graph check ─────────────────────────────────────────────────────────────

func cmdProjectGraphCheck(cfg store.Config, slug string, args []string) {
	positional, rest := projSplitPositional(args, 1)
	f := projNewFlags("engram project graph check")
	jsonOut := f.fs.Bool("json", false, "print the JSON envelope")
	repoDir := f.fs.String("repo-dir", "", "repository root to compare against (default: cwd)")
	if !f.parse(rest) {
		return
	}
	if len(positional) > 0 && strings.TrimSpace(slug) == "" {
		slug = positional[0]
	}

	s, ok := projOpenStore(cfg)
	if !ok {
		return
	}
	defer s.Close()

	sc := projScopeForRead(s, slug, *jsonOut)
	if sc.Slug == "" {
		return
	}

	dir := strings.TrimSpace(*repoDir)
	if dir == "" {
		dir = sc.Path
	}
	if dir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			fatal(err)
			return
		}
		dir = cwd
	}

	staleness, err := workspace.GraphCheckIn(s, sc.Slug, dir)
	if err != nil {
		projFailWorkspace(*jsonOut, err, map[string]any{"repo_dir": dir})
		return
	}

	result := map[string]any{"graph": staleness, "repo_dir": dir}
	projPrintResult(*jsonOut, sc, result, func() {
		state := "fresh"
		if staleness.Stale {
			state = "stale"
		}
		fmt.Printf("graph:   %s (%s)\n", state, projDash(staleness.Reason))
		fmt.Printf("changed: %d file(s) the graph covers\n", staleness.ChangedFiles)
		fmt.Printf("head:    %s\n", projDash(projShort(staleness.HeadCommit, 8)))
		fmt.Printf("checked: %s\n", staleness.CheckedAt)
	})
}

// ─── evidence scan ───────────────────────────────────────────────────────────

func cmdProjectEvidenceScan(cfg store.Config, slug string, args []string) {
	positional, rest := projSplitPositional(args, 1)
	f := projNewFlags("engram project evidence scan")
	jsonOut := f.fs.Bool("json", false, "print the JSON envelope")
	category := f.fs.String("category", "", "one vault category; default: all eleven")
	apply := f.fs.Bool("apply", false, "register what the scan finds (default: plan only)")
	maxBytes := f.fs.Int64("max-bytes", 0, "size beyond which a file is recorded but not hashed")
	if !f.parse(rest) {
		return
	}
	if len(positional) == 0 {
		projFail(*jsonOut, "missing_field", "usage: engram project evidence scan <task> [--category X] [--apply]", nil)
		return
	}
	taskRef := positional[0]

	s, ok := projOpenStore(cfg)
	if !ok {
		return
	}
	defer s.Close()

	report, err := workspace.ScanEvidence(s, taskRef, *category, !*apply, vault.Options{MaxBytes: *maxBytes})
	if err != nil {
		projFailWorkspace(*jsonOut, err, map[string]any{"task": taskRef})
		return
	}

	sc := projScope{Slug: report.Project}
	projPrintResult(*jsonOut, sc, report, func() {
		mode := "planned"
		if *apply {
			mode = "registered"
		}
		fmt.Printf("%s: %d new · %d already known · %d skipped · %s\n",
			mode, report.Added, report.Updated, len(report.Skipped), projHumanBytes(report.TotalBytes))
		fmt.Printf("folder:  %s\n", report.TaskDir)
		if len(report.BenchmarkCandidates) > 0 {
			fmt.Printf("runs:    %s\n", strings.Join(report.BenchmarkCandidates, ", "))
			fmt.Println("         import them with: engram project bench import <task> <run.json>")
		}
		if len(report.Skipped) > 0 {
			table := &projTable{headers: []string{"SKIPPED", "REASON"}}
			for _, skip := range report.Skipped {
				table.add(skip.Path, skip.Reason)
			}
			table.render(os.Stdout)
		}
		if !*apply {
			fmt.Println("nothing was written; re-run with --apply")
		}
	})
}

// ─── bench ───────────────────────────────────────────────────────────────────

func cmdProjectBench(cfg store.Config, slug string, args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: engram project bench <add|list|import> [flags]")
		exitFunc(1)
		return
	}
	switch args[0] {
	case "add":
		cmdProjectBenchAdd(cfg, slug, args[1:])
	case "list":
		cmdProjectBenchList(cfg, slug, args[1:])
	case "import":
		cmdProjectBenchImport(cfg, slug, args[1:])
	default:
		fmt.Fprintf(os.Stderr, "engram: unknown bench subcommand %q\n", args[0])
		fmt.Fprintln(os.Stderr, "usage: engram project bench <add|list|import> [flags]")
		exitFunc(1)
	}
}

func cmdProjectBenchAdd(cfg store.Config, slug string, args []string) {
	positional, rest := projSplitPositional(args, 1)
	f := projNewFlags("engram project bench add")
	jsonOut := f.fs.Bool("json", false, "print the JSON envelope")
	name := f.fs.String("name", "", "name of the run the measurement came from")
	metric := f.fs.String("metric", "", "what was measured")
	unit := f.fs.String("unit", "", strings.Join(projBenchmarkUnitEnum, "|"))
	value := f.fs.Float64("value", 0, "the measurement")
	baseline := f.fs.Bool("baseline", false, "make this the baseline of its metric")
	direction := f.fs.String("direction", "", "lower|higher; default: what the unit implies")
	runPath := f.fs.String("run-path", "", "vault-relative path of the run file")
	configStamp := f.fs.String("config-stamp", "", "configuration the run was taken under")
	capturedAt := f.fs.String("captured-at", "", "ISO-8601 capture timestamp (default: now)")
	notes := f.fs.String("notes", "", "what the measurement says")
	if !f.parse(rest) {
		return
	}
	if len(positional) == 0 {
		projFail(*jsonOut, "missing_field",
			"usage: engram project bench add <task> --name N --metric M --unit U --value V", nil)
		return
	}
	if strings.TrimSpace(*name) == "" || strings.TrimSpace(*metric) == "" || strings.TrimSpace(*unit) == "" || !f.given("value") {
		projFail(*jsonOut, "missing_field", "--name, --metric, --unit and --value are required", nil)
		return
	}
	if !projEnumContains(projBenchmarkUnitEnum, *unit) {
		projFail(*jsonOut, "invalid_enum", fmt.Sprintf("unit %q is invalid", *unit),
			map[string]any{"hint": "one of " + strings.Join(projBenchmarkUnitEnum, ", ")})
		return
	}
	if f.given("direction") && !projEnumContains(projBenchmarkDirections, *direction) {
		projFail(*jsonOut, "invalid_enum", fmt.Sprintf("direction %q is invalid", *direction),
			map[string]any{"hint": "one of " + strings.Join(projBenchmarkDirections, ", ")})
		return
	}

	s, ok := projOpenStore(cfg)
	if !ok {
		return
	}
	defer s.Close()

	task, err := workspace.ResolveTask(s, positional[0])
	if err != nil {
		projFailWorkspace(*jsonOut, err, map[string]any{"task": positional[0]})
		return
	}

	result, err := s.AddBenchmark(store.AddBenchmarkParams{
		Task:        task,
		Name:        *name,
		Metric:      *metric,
		Unit:        *unit,
		Direction:   *direction,
		Value:       *value,
		Baseline:    *baseline,
		RunPath:     f.ptr("run-path", runPath),
		ConfigStamp: f.ptr("config-stamp", configStamp),
		CapturedAt:  strings.TrimSpace(*capturedAt),
		Notes:       f.ptr("notes", notes),
		Source:      "manual",
	})
	if errors.Is(err, store.ErrInvalidBenchmarkUnit) {
		projFail(*jsonOut, "invalid_enum", err.Error(), nil)
		return
	}
	if err != nil {
		fatal(err)
		return
	}

	envelope := map[string]any{
		"benchmark":        result.Benchmark,
		"created":          result.Created,
		"demoted_baseline": result.DemotedBaseline,
	}
	projPrintResult(*jsonOut, projScope{Slug: task.Project}, envelope, func() {
		b := result.Benchmark
		line := fmt.Sprintf("%s %s = %s %s", b.Name, b.Metric, projFormatValue(b.Value), b.Unit)
		if b.Baseline {
			line += " (baseline)"
		}
		if !result.Created {
			line += " · already recorded"
		}
		fmt.Println(line)
		if result.DemotedBaseline != nil {
			fmt.Printf("previous baseline %s demoted\n", *result.DemotedBaseline)
		}
	})
}

func cmdProjectBenchList(cfg store.Config, slug string, args []string) {
	positional, rest := projSplitPositional(args, 1)
	f := projNewFlags("engram project bench list")
	jsonOut := f.fs.Bool("json", false, "print the JSON envelope")
	metric := f.fs.String("metric", "", "list one metric only")
	includeChildren := f.fs.Bool("include-children", false, "widen to child tasks or the project subtree")
	limit := f.fs.Int("limit", projBenchmarkListDefault, "maximum rows (1-200)")
	offset := f.fs.Int("offset", 0, "rows to skip")
	if !f.parse(rest) {
		return
	}

	s, ok := projOpenStore(cfg)
	if !ok {
		return
	}
	defer s.Close()

	filter := store.BenchmarkListFilter{
		Metric:          strings.TrimSpace(*metric),
		IncludeChildren: *includeChildren,
		Limit:           projClampInt(*limit, 1, 200, projBenchmarkListDefault),
		Offset:          *offset,
	}
	sc := projScope{}
	if len(positional) > 0 {
		task, err := workspace.ResolveTask(s, positional[0])
		if err != nil {
			projFailWorkspace(*jsonOut, err, map[string]any{"task": positional[0]})
			return
		}
		filter.Task = task.SyncID
		sc.Slug = task.Project
	} else {
		sc = projScopeForRead(s, slug, *jsonOut)
		if sc.Slug == "" {
			return
		}
		filter.Project = sc.Slug
	}

	page, err := s.ListBenchmarks(filter)
	if err != nil {
		fatal(err)
		return
	}

	projPrintResult(*jsonOut, sc, page, func() {
		if len(page.Items) == 0 {
			fmt.Println("no measurements")
			return
		}
		table := &projTable{headers: []string{"METRIC", "VALUE", "UNIT", "BASELINE", "Δ", "RUN", "CAPTURED"}}
		for _, item := range page.Items {
			table.add(item.Metric, projFormatValue(item.Value), item.Unit,
				projBaselineCell(item), projDeltaCell(item), item.Name, item.CapturedAt)
		}
		table.render(os.Stdout)
		fmt.Printf("\n%d of %d\n", len(page.Items), page.Total)
	})
}

// projFormatValue renders a measurement without the trailing zeros a float
// would otherwise carry into a column.
func projFormatValue(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// projBaselineCell shows the baseline a measurement is compared against, and
// marks the row that is the baseline.
func projBaselineCell(item store.BenchmarkDelta) string {
	if item.Baseline {
		return "* self"
	}
	if item.BaselineValue == nil {
		return "-"
	}
	return projFormatValue(*item.BaselineValue)
}

// projDeltaCell renders the change against the baseline with the sign that
// means "better" for the metric's own direction, so a number that improved
// reads as an improvement whichever way its unit points.
func projDeltaCell(item store.BenchmarkDelta) string {
	// The baseline is what everything else is compared against, so its own
	// delta is zero by construction. Printing that as a change would put a
	// number in the column that means nothing.
	if item.DeltaPct == nil || item.Baseline {
		return "-"
	}
	delta := *item.DeltaPct
	mark := ""
	switch {
	case delta == 0:
		mark = " ="
	case (delta < 0) == (item.Direction == store.BenchmarkDirectionLower):
		mark = " better"
	default:
		mark = " worse"
	}
	return fmt.Sprintf("%+.2f%%%s", delta, mark)
}

func cmdProjectBenchImport(cfg store.Config, slug string, args []string) {
	positional, rest := projSplitPositional(args, 2)
	f := projNewFlags("engram project bench import")
	jsonOut := f.fs.Bool("json", false, "print the JSON envelope")
	mapFile := f.fs.String("map", "", "JSON Pointer map for a run in the harness's own format")
	baseline := f.fs.Bool("baseline", false, "make these the baselines of their metrics")
	apply := f.fs.Bool("apply", false, "record the measurements (default: plan only)")
	if !f.parse(rest) {
		return
	}
	if len(positional) < 2 {
		projFail(*jsonOut, "missing_field",
			"usage: engram project bench import <task> <run.json> [--map map.json] [--apply]", nil)
		return
	}

	var mapping []vault.PointerMap
	if strings.TrimSpace(*mapFile) != "" {
		loaded, err := vault.LoadPointerMap(*mapFile)
		if err != nil {
			projFail(*jsonOut, "invalid_file", err.Error(), nil)
			return
		}
		mapping = loaded
	}

	s, ok := projOpenStore(cfg)
	if !ok {
		return
	}
	defer s.Close()

	report, err := workspace.ImportBenchmarks(s, positional[0], positional[1], mapping, *baseline, !*apply)
	if err != nil {
		projFailWorkspace(*jsonOut, err, map[string]any{"task": positional[0], "run_path": positional[1]})
		return
	}

	projPrintResult(*jsonOut, projScope{Slug: report.Project}, report, func() {
		mode := "planned"
		if *apply {
			mode = "recorded"
		}
		fmt.Printf("%s: %d measurement(s) · %d already known · format %s\n",
			mode, report.Imported, report.Duplicates, report.Format)
		fmt.Printf("run:     %s (%s)\n", report.Name, report.RunPath)
		if len(report.Metrics) > 0 {
			fmt.Printf("metrics: %s\n", strings.Join(report.Metrics, ", "))
		}
		if !*apply {
			fmt.Println("nothing was written; re-run with --apply")
		}
	})
}

// ─── import-vault ────────────────────────────────────────────────────────────

func cmdProjectImportVault(cfg store.Config, slug string, args []string) {
	positional, rest := projSplitPositional(args, 1)
	f := projNewFlags("engram project import-vault")
	jsonOut := f.fs.Bool("json", false, "print the JSON envelope")
	project := f.fs.String("project", "", "import one project folder only")
	apply := f.fs.Bool("apply", false, "write the plan (default: plan only)")
	noStates := f.fs.Bool("no-states", false, "leave task states as the store records them")
	includeArch := f.fs.Bool("include-arch", false, "import _arquitectura as a spike")
	maxBytes := f.fs.Int64("max-bytes", 0, "size beyond which a file is recorded but not hashed")
	if !f.parse(rest) {
		return
	}

	root := ""
	if len(positional) > 0 {
		root = positional[0]
	}
	target := strings.TrimSpace(*project)
	if target == "" {
		target = slug
	}

	s, ok := projOpenStore(cfg)
	if !ok {
		return
	}
	defer s.Close()

	plan, err := workspace.ImportVault(s, root, target, *apply, !*noStates, *includeArch,
		vault.Options{MaxBytes: *maxBytes})
	if err != nil {
		projFailWorkspace(*jsonOut, err, map[string]any{"root": root})
		return
	}

	projPrintResult(*jsonOut, projNamedScope(target), plan, func() {
		table := &projTable{headers: []string{"ACTION", "PROJECT", "TASK", "KIND", "STATE"}}
		for _, task := range plan.Tasks {
			key := task.Slug
			if task.JiraKey != "" {
				key = task.JiraKey + " " + task.Slug
			}
			table.add(task.Action, task.Project, key, task.Kind, projDash(task.State))
		}
		table.render(os.Stdout)
		fmt.Printf("\nroot:       %s\n", plan.Root)
		fmt.Printf("projects:   %s\n", projDash(strings.Join(plan.Projects, ", ")))
		fmt.Printf("evidence:   %d new · %d known · %d skipped\n",
			plan.Evidence.Added, plan.Evidence.Updated, plan.Evidence.Skipped)
		fmt.Printf("benchmarks: %d new · %d known · %d skipped\n",
			plan.Benchmarks.Added, plan.Benchmarks.Updated, plan.Benchmarks.Skipped)
		for _, warning := range plan.Warnings {
			fmt.Printf("warning:    %s\n", warning)
		}
		if plan.DryRun {
			fmt.Println("nothing was written; re-run with --apply")
		}
	})
}

// ─── search ──────────────────────────────────────────────────────────────────

func cmdProjectSearch(cfg store.Config, slug string, args []string) {
	positional, rest := projSplitPositional(args, 1)
	f := projNewFlags("engram project search")
	jsonOut := f.fs.Bool("json", false, "print the JSON envelope")
	project := f.fs.String("project", "", "scope the search to one project")
	subtree := f.fs.Bool("subtree", false, "widen --project to the project and everything under it")
	var kinds projStringList
	f.fs.Var(&kinds, "kind", "narrow to one kind; repeatable")
	perKind := f.fs.Int("per-kind", 0, "hits per kind (1-25, default 5)")
	if !f.parse(rest) {
		return
	}
	if len(positional) == 0 {
		projFail(*jsonOut, "missing_field", "usage: engram project search <query> [--project X] [--kind K]", nil)
		return
	}
	for _, kind := range kinds {
		if !projEnumContains(projWorkspaceKindEnum, kind) {
			projFail(*jsonOut, "invalid_enum", fmt.Sprintf("kind %q is invalid", kind),
				map[string]any{"hint": "one of " + strings.Join(projWorkspaceKindEnum, ", ")})
			return
		}
	}

	target := strings.TrimSpace(*project)
	if target == "" {
		target = slug
	}

	s, ok := projOpenStore(cfg)
	if !ok {
		return
	}
	defer s.Close()

	results, err := s.SearchWorkspace(store.SearchWorkspaceParams{
		Query:   positional[0],
		Project: target,
		Subtree: *subtree,
		Kinds:   kinds,
		PerKind: *perKind,
	})
	switch {
	case errors.Is(err, store.ErrWorkspaceQueryTooShort):
		projFail(*jsonOut, "query_too_short", err.Error(), nil)
		return
	case errors.Is(err, store.ErrUnknownWorkspaceKind):
		projFail(*jsonOut, "invalid_enum", err.Error(),
			map[string]any{"hint": "one of " + strings.Join(projWorkspaceKindEnum, ", ")})
		return
	case err != nil:
		fatal(err)
		return
	}

	projPrintResult(*jsonOut, projNamedScope(target), results, func() {
		if len(results.Hits) == 0 {
			fmt.Println("no matches")
			return
		}
		table := &projTable{headers: []string{"KIND", "REF", "PROJECT", "TITLE", "UPDATED"}}
		for _, hit := range results.Hits {
			table.add(hit.Kind, hit.Ref, hit.Project, projShort(hit.Title, 48), hit.UpdatedAt)
		}
		table.render(os.Stdout)
		fmt.Println()
		for _, kind := range projSortedKinds(results.Totals) {
			fmt.Printf("%s: %d\n", kind, results.Totals[kind])
		}
	})
}

// projSortedKinds orders the totals so two runs of the same search print the
// same summary.
func projSortedKinds(totals map[string]int) []string {
	kinds := make([]string, 0, len(totals))
	for kind := range totals {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	return kinds
}
