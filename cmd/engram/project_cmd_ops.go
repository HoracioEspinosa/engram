// Subcommand implementations for `engram project <slug> …` (RFC
// rfc-engram-projects.md §7.1). Dispatch, project resolution and the shared
// rendering helpers live in project_cmd.go.
package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	projectpkg "github.com/Gentleman-Programming/engram/internal/project"
	"github.com/Gentleman-Programming/engram/internal/runbooks"
	"github.com/Gentleman-Programming/engram/internal/store"
)

func projOpenStore(cfg store.Config) (*store.Store, bool) {
	s, err := storeNew(cfg)
	if err != nil {
		fatal(err)
		return nil, false
	}
	return s, true
}

// ─── card ────────────────────────────────────────────────────────────────────

func cmdProjectCard(cfg store.Config, slug string, args []string) {
	f := projNewFlags("engram project card")
	jsonOut := f.fs.Bool("json", false, "print the JSON envelope")
	withSummary := f.fs.Bool("graph-summary", false, "include the graph_summary blob")
	if !f.parse(args) {
		return
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

	card, err := s.GetProjectCard(sc.Slug)
	if errors.Is(err, store.ErrNoProjectCard) {
		projFail(*jsonOut, "no_card", fmt.Sprintf("no project card for %s", sc.Slug),
			map[string]any{"hint": fmt.Sprintf("run: engram project %s upsert", sc.Slug)})
		return
	}
	if err != nil {
		fatal(err)
		return
	}
	if !*withSummary {
		card.GraphSummary = nil
	}
	counts, err := s.ProjectCardCounts(sc.Slug)
	if err != nil {
		fatal(err)
		return
	}
	sync, err := s.ProjectSyncSummary(sc.Slug)
	if err != nil {
		fatal(err)
		return
	}

	result := map[string]any{"card": projCardForJSON(card), "counts": counts, "sync": sync}
	projPrintResult(*jsonOut, sc, result, func() {
		projRenderCard(card, &counts, &sync)
	})
}

// projCardForJSON re-renders a card so graph_summary is emitted as the nested
// object it semantically is, instead of the JSON string the column stores.
// The MCP tools return the raw string; the CLI unwraps it so `jq
// '.result.card.graph_summary.god_nodes'` works (RFC §7.1, §10.1).
func projCardForJSON(card store.ProjectCard) map[string]any {
	raw, err := json.Marshal(card)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	if summary, ok := out["graph_summary"].(string); ok {
		var nested any
		if err := json.Unmarshal([]byte(summary), &nested); err == nil {
			out["graph_summary"] = nested
		}
	}
	return out
}

func projRenderCard(card store.ProjectCard, counts *store.ProjectCardCounts, sync *store.ProjectSyncSummary) {
	w := os.Stdout
	fmt.Fprintf(w, "project:   %s (%s)\n", card.Slug, projDash(card.DisplayName))
	fmt.Fprintf(w, "repo:      %s (branch %s)\n", projDash(projStrVal(card.RepoURL)), projDash(card.DefaultBranch))
	jira := projDash(card.JiraProject)
	if c := projStrVal(card.JiraComponent); c != "" {
		jira += " / " + c
	}
	fmt.Fprintf(w, "jira:      %s\n", jira)
	fmt.Fprintf(w, "knowledge: %s\n", projDash(projStrVal(card.KnowledgeHubPath)))
	graph := projDash(card.GraphPath)
	if commit := projStrVal(card.GraphCommit); commit != "" {
		graph += " @ " + projShort(commit, 8)
	}
	if builtAt := projStrVal(card.GraphBuiltAt); builtAt != "" {
		graph += " (built " + builtAt + ")"
	}
	fmt.Fprintf(w, "graph:     %s\n", graph)
	fmt.Fprintf(w, "owner:     %s\n", projDash(projStrVal(card.Owner)))
	fmt.Fprintf(w, "updated:   %s\n", card.UpdatedAt)
	if counts != nil {
		fmt.Fprintf(w, "counts:    %d obs · %d pinned · %d/%d tasks active · %d evidence (%d unattached) · %d runbooks (%d stale)\n",
			counts.Observations, counts.Pinned, counts.TasksActive, counts.TasksTotal,
			counts.Evidence, counts.EvidenceUnattached, counts.Runbooks, counts.RunbooksStale)
	}
	if sync != nil {
		fmt.Fprintf(w, "sync:      enrolled %s · lifecycle %s · last acked seq %d\n",
			projYesNo(sync.Enrolled), projDash(sync.Lifecycle), sync.LastAckedSeq)
	}
	if raw := projStrVal(card.GraphSummary); raw != "" {
		projRenderGraphSummary(w, raw)
	}
}

func projRenderGraphSummary(w io.Writer, raw string) {
	var summary projectpkg.GraphSummary
	if err := json.Unmarshal([]byte(raw), &summary); err != nil {
		fmt.Fprintf(w, "summary:   (unreadable graph_summary blob: %v)\n", err)
		return
	}
	fmt.Fprintf(w, "summary:   %s nodes · %s edges · %s communities · %d%% EXTRACTED · %d%% INFERRED\n",
		projThousands(summary.NodeCount), projThousands(summary.EdgeCount),
		projThousands(summary.CommunityCount), summary.ExtractedPct, summary.InferredPct)
	if len(summary.GodNodes) > 0 {
		labels := make([]string, 0, 3)
		for i, gn := range summary.GodNodes {
			if i == 3 {
				break
			}
			labels = append(labels, fmt.Sprintf("%s (%d)", gn.Label, gn.Edges))
		}
		suffix := ""
		if len(summary.GodNodes) > 3 {
			suffix = " …"
		}
		fmt.Fprintf(w, "god nodes: %s%s\n", strings.Join(labels, ", "), suffix)
	}
}

// ─── upsert ──────────────────────────────────────────────────────────────────

func cmdProjectUpsert(cfg store.Config, slug string, args []string) {
	f := projNewFlags("engram project upsert")
	jsonOut := f.fs.Bool("json", false, "print the JSON envelope")
	displayName := f.fs.String("display-name", "", "human-readable project name")
	repoURL := f.fs.String("repo-url", "", "clone URL of the repository")
	defaultBranch := f.fs.String("default-branch", "", "trunk branch name")
	jiraProject := f.fs.String("jira-project", "", "Jira project key")
	jiraComponent := f.fs.String("jira-component", "", "Jira component")
	knowledgeHub := f.fs.String("knowledge-hub", "", "vault-relative path of the service hub")
	owner := f.fs.String("owner", "", "owning team or person")
	graphPath := f.fs.String("graph-path", "", "repo-relative path of graph.json")
	if !f.parse(args) {
		return
	}

	s, ok := projOpenStore(cfg)
	if !ok {
		return
	}
	defer s.Close()

	sc := projScopeForCreate(s, slug, *jsonOut)
	if sc.Slug == "" {
		return
	}

	card, created, err := s.UpsertProjectCard(store.UpsertProjectCardParams{
		Slug:             sc.Slug,
		DisplayName:      f.ptr("display-name", displayName),
		RepoURL:          f.ptr("repo-url", repoURL),
		DefaultBranch:    f.ptr("default-branch", defaultBranch),
		JiraProject:      f.ptr("jira-project", jiraProject),
		JiraComponent:    f.ptr("jira-component", jiraComponent),
		KnowledgeHubPath: f.ptr("knowledge-hub", knowledgeHub),
		Owner:            f.ptr("owner", owner),
		GraphPath:        f.ptr("graph-path", graphPath),
	})
	if err != nil {
		fatal(err)
		return
	}

	result := map[string]any{"card": projCardForJSON(card), "created": created}
	projPrintResult(*jsonOut, sc, result, func() {
		verb := "updated"
		if created {
			verb = "created"
		}
		fmt.Printf("%s project card %s\n", verb, card.Slug)
		projRenderCard(card, nil, nil)
	})
}

// ─── graph sync ──────────────────────────────────────────────────────────────

func cmdProjectGraph(cfg store.Config, slug string, args []string) {
	if len(args) == 0 || args[0] != "sync" {
		fmt.Fprintln(os.Stderr, "usage: engram project [<slug>] graph sync [--repo-dir <dir>] [--graph-path <rel>] [--json]")
		exitFunc(1)
		return
	}

	f := projNewFlags("engram project graph sync")
	jsonOut := f.fs.Bool("json", false, "print the JSON envelope")
	repoDir := f.fs.String("repo-dir", "", "repository root holding graphify-out/ (default: cwd)")
	graphPath := f.fs.String("graph-path", "", "repo-relative path of graph.json (default: the card's)")
	if !f.parse(args[1:]) {
		return
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

	card, err := s.GetProjectCard(sc.Slug)
	if errors.Is(err, store.ErrNoProjectCard) {
		projFail(*jsonOut, "no_card", fmt.Sprintf("no project card for %s", sc.Slug),
			map[string]any{"hint": fmt.Sprintf("run: engram project %s upsert", sc.Slug)})
		return
	}
	if err != nil {
		fatal(err)
		return
	}

	dir := strings.TrimSpace(*repoDir)
	if dir == "" {
		dir = sc.Path
	}
	if dir == "" {
		cwd, cwdErr := os.Getwd()
		if cwdErr != nil {
			fatal(cwdErr)
			return
		}
		dir = cwd
	}
	if abs, absErr := filepath.Abs(dir); absErr == nil {
		dir = abs
	}

	path := strings.TrimSpace(*graphPath)
	if path == "" {
		path = card.GraphPath
	}

	graph, err := projectpkg.SyncGraph(s, sc.Slug, dir, path)
	switch {
	case errors.Is(err, store.ErrGraphNotFound):
		projFail(*jsonOut, "graph_not_found", "graph.json not found",
			map[string]any{"repo_dir": dir, "graph_path": path, "hint": "run: graphify update ."})
		return
	case errors.Is(err, store.ErrGraphMissingCommit):
		projFail(*jsonOut, "graph_missing_commit",
			"graph.json has no built_at_commit; no graph fact was persisted", nil)
		return
	case err != nil:
		fatal(err)
		return
	}

	result := map[string]any{"graph": graph, "graph_path": path, "repo_dir": dir}
	projPrintResult(*jsonOut, sc, result, func() {
		fmt.Printf("graph:   %s\n", path)
		head := "HEAD unknown"
		switch {
		case graph.HeadCommit == "":
		case graph.Stale:
			head = fmt.Sprintf("HEAD is %s — graph is stale", projShort(graph.HeadCommit, 8))
		default:
			head = "HEAD matches"
		}
		fmt.Printf("commit:  %s (%s)\n", graph.GraphCommit, head)
		fmt.Printf("summary: %s nodes · %s edges · %s communities\n",
			projThousands(graph.NodeCount), projThousands(graph.EdgeCount), projThousands(graph.CommunityCount))
		fmt.Printf("stamped project_cards.%s (graph_commit, graph_built_at, graph_summary)\n", sc.Slug)
	})
}

// ─── tasks ───────────────────────────────────────────────────────────────────

func cmdProjectTasks(cfg store.Config, slug string, args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: engram project [<slug>] tasks <list|upsert|link> [flags]")
		exitFunc(1)
		return
	}
	switch args[0] {
	case "list":
		cmdProjectTasksList(cfg, slug, args[1:])
	case "upsert":
		cmdProjectTasksUpsert(cfg, slug, args[1:])
	case "link":
		cmdProjectTasksLink(cfg, slug, args[1:])
	default:
		fmt.Fprintf(os.Stderr, "engram: unknown tasks subcommand %q\n", args[0])
		fmt.Fprintln(os.Stderr, "usage: engram project [<slug>] tasks <list|upsert|link> [flags]")
		exitFunc(1)
	}
}

func cmdProjectTasksList(cfg store.Config, slug string, args []string) {
	f := projNewFlags("engram project tasks list")
	jsonOut := f.fs.Bool("json", false, "print the JSON envelope")
	state := f.fs.String("state", "active", "state filter; active = every state except done and cancelled")
	kind := f.fs.String("kind", "", "kind filter")
	jiraKey := f.fs.String("jira", "", "exact Jira key filter")
	query := f.fs.String("q", "", "FTS5 query over title, jira_key, sdd_change and branch")
	limit := f.fs.Int("limit", 20, "maximum rows (1-100)")
	offset := f.fs.Int("offset", 0, "rows to skip")
	staleAfter := f.fs.String("stale-after", "24h", "Jira-mirror freshness window, e.g. 24h")
	if !f.parse(args) {
		return
	}

	if !projEnumContains(projTaskListStateEnum, *state) {
		projFail(*jsonOut, "invalid_enum", fmt.Sprintf("state %q is invalid", *state),
			map[string]any{"hint": "one of " + strings.Join(projTaskListStateEnum, ", ")})
		return
	}
	if *kind != "" && !projEnumContains(projTaskKindEnum, *kind) {
		projFail(*jsonOut, "invalid_enum", fmt.Sprintf("kind %q is invalid", *kind),
			map[string]any{"hint": "one of " + strings.Join(projTaskKindEnum, ", ")})
		return
	}
	staleHours, err := projParseStaleAfter(*staleAfter)
	if err != nil {
		projFail(*jsonOut, "invalid_enum", err.Error(), nil)
		return
	}
	if *offset < 0 {
		projFail(*jsonOut, "invalid_enum", "--offset cannot be negative", nil)
		return
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

	filter := store.TaskListFilter{
		State:           *state,
		Kind:            *kind,
		JiraKey:         *jiraKey,
		Query:           *query,
		Limit:           projClampInt(*limit, 1, 100, 20),
		Offset:          *offset,
		StaleAfterHours: staleHours,
	}
	items, total, err := s.ListTasks(sc.Slug, filter)
	if err != nil {
		fatal(err)
		return
	}

	result := map[string]any{
		"items": items, "total": total, "limit": filter.Limit, "offset": filter.Offset,
	}
	projPrintResult(*jsonOut, sc, result, func() {
		if len(items) == 0 {
			fmt.Printf("No tasks in %s for this filter.\n", sc.Slug)
			return
		}
		t := &projTable{headers: []string{"ID", "KEY", "KIND", "STATE", "JIRA STATUS", "SYNCED", "STALE", "OBS", "EVD", "TITLE"}}
		for _, item := range items {
			t.add(
				strconv.FormatInt(item.ID, 10),
				projTaskKey(item.Task),
				item.Kind,
				item.State,
				projDash(projStrVal(item.JiraStatus)),
				projDash(projStrVal(item.StateSyncedAt)),
				projYesNo(item.StateStale),
				strconv.Itoa(item.Observations),
				strconv.Itoa(item.Evidence),
				item.Title,
			)
		}
		t.render(os.Stdout)
		fmt.Printf("\n%d of %d task(s) · offset %d\n", len(items), total, filter.Offset)
	})
}

// projTaskKey is the human identifier of a task: its Jira key, else its SDD
// change, else its sync id.
func projTaskKey(t store.Task) string {
	if k := projStrVal(t.JiraKey); k != "" {
		return k
	}
	if c := projStrVal(t.SDDChange); c != "" {
		return "change:" + c
	}
	return t.SyncID
}

func cmdProjectTasksUpsert(cfg store.Config, slug string, args []string) {
	f := projNewFlags("engram project tasks upsert")
	jsonOut := f.fs.Bool("json", false, "print the JSON envelope")
	syncID := f.fs.String("sync-id", "", "existing task sync id (task-<16 hex>)")
	jiraKey := f.fs.String("jira", "", "Jira key, e.g. CDBS-10336")
	sddChange := f.fs.String("sdd-change", "", "SDD change slug")
	title := f.fs.String("title", "", "task title (required when creating)")
	kind := f.fs.String("kind", "", "task kind (required when creating)")
	state := f.fs.String("state", "", "engram-side state")
	jiraStatus := f.fs.String("jira-status", "", "literal Jira status name")
	jiraStatusCategory := f.fs.String("jira-status-category", "", "new|indeterminate|done")
	branch := f.fs.String("branch", "", "working branch")
	prURL := f.fs.String("pr", "", "pull request URL")
	knowledgeRef := f.fs.String("knowledge-ref", "", "vault-relative path documenting the task")
	assignee := f.fs.String("assignee", "", "assignee")
	if !f.parse(args) {
		return
	}

	if !f.given("sync-id") && !f.given("jira") && !f.given("sdd-change") {
		projFail(*jsonOut, "missing_field", "one of --sync-id, --jira or --sdd-change is required", nil)
		return
	}
	if f.given("kind") && !projEnumContains(projTaskKindEnum, *kind) {
		projFail(*jsonOut, "invalid_enum", fmt.Sprintf("kind %q is invalid", *kind),
			map[string]any{"hint": "one of " + strings.Join(projTaskKindEnum, ", ")})
		return
	}
	if f.given("state") && !projEnumContains(projTaskStateEnum, *state) {
		projFail(*jsonOut, "invalid_enum", fmt.Sprintf("state %q is invalid", *state),
			map[string]any{"hint": "one of " + strings.Join(projTaskStateEnum, ", ")})
		return
	}
	if f.given("jira-status-category") && !projEnumContains(projJiraStatusCategoryEnum, *jiraStatusCategory) {
		projFail(*jsonOut, "invalid_enum", fmt.Sprintf("jira-status-category %q is invalid", *jiraStatusCategory),
			map[string]any{"hint": "one of " + strings.Join(projJiraStatusCategoryEnum, ", ")})
		return
	}

	s, ok := projOpenStore(cfg)
	if !ok {
		return
	}
	defer s.Close()

	sc := projScopeForCreate(s, slug, *jsonOut)
	if sc.Slug == "" {
		return
	}

	res, err := s.UpsertTask(store.UpsertTaskParams{
		Project:            sc.Slug,
		SyncID:             f.ptr("sync-id", syncID),
		JiraKey:            f.ptr("jira", jiraKey),
		SDDChange:          f.ptr("sdd-change", sddChange),
		Title:              f.ptr("title", title),
		Kind:               f.ptr("kind", kind),
		State:              f.ptr("state", state),
		JiraStatus:         f.ptr("jira-status", jiraStatus),
		JiraStatusCategory: f.ptr("jira-status-category", jiraStatusCategory),
		Branch:             f.ptr("branch", branch),
		PRUrl:              f.ptr("pr", prURL),
		KnowledgeRef:       f.ptr("knowledge-ref", knowledgeRef),
		Assignee:           f.ptr("assignee", assignee),
	})
	if err != nil {
		if projKnowledgeRefFail(*jsonOut, err) {
			return
		}
		var missing *store.MissingFieldError
		if errors.As(err, &missing) {
			projFail(*jsonOut, "missing_field", missing.Error(), map[string]any{"field": missing.Field})
			return
		}
		var conflict *store.TaskKeyConflictError
		if errors.As(err, &conflict) {
			projFail(*jsonOut, "task_key_conflict", conflict.Error(),
				map[string]any{"existing_project": conflict.ExistingProject})
			return
		}
		fatal(err)
		return
	}

	result := map[string]any{"task": res.Task, "created": res.Created, "card_created": res.CardCreated}
	projPrintResult(*jsonOut, sc, result, func() {
		verb := "updated"
		if res.Created {
			verb = "created"
		}
		suffix := ""
		if res.CardCreated {
			suffix = " (project card created)"
		}
		fmt.Printf("%s task #%d %s %s · %s/%s%s\n", verb, res.Task.ID, res.Task.SyncID,
			projTaskKey(res.Task), res.Task.Kind, res.Task.State, suffix)
		fmt.Printf("title:   %s\n", res.Task.Title)
		if b := projStrVal(res.Task.Branch); b != "" {
			fmt.Printf("branch:  %s\n", b)
		}
		if p := projStrVal(res.Task.PRUrl); p != "" {
			fmt.Printf("pr:      %s\n", p)
		}
	})
}

func cmdProjectTasksLink(cfg store.Config, slug string, args []string) {
	positional, rest := projSplitPositional(args, 1)
	f := projNewFlags("engram project tasks link")
	jsonOut := f.fs.Bool("json", false, "print the JSON envelope")
	observation := f.fs.String("observation", "", "observation id or obs-<hex> sync id")
	role := f.fs.String("role", "", "context|decision|root_cause|evidence|summary")
	knowledgeRef := f.fs.String("knowledge-ref", "", "vault-relative path documenting the fact")
	graphRef := f.fs.String("graph-ref", "", "symbol or community label taken from graphify")
	graphCommit := f.fs.String("graph-commit", "", "40-hex commit the graph_ref was read at")
	runbookID := f.fs.String("runbook", "", "runbook id, e.g. RB-003")
	jiraRef := f.fs.String("jira-ref", "", "Jira key referenced by the observation")
	if !f.parse(rest) {
		return
	}

	if len(positional) == 0 {
		projFail(*jsonOut, "missing_field", "usage: engram project [<slug>] tasks link <task> --observation <id|obs-hex>", nil)
		return
	}
	taskRef := positional[0]
	if strings.TrimSpace(*observation) == "" {
		projFail(*jsonOut, "missing_field", "--observation is required", nil)
		return
	}
	if f.given("role") && !projEnumContains(projTaskLinkRoleEnum, *role) {
		projFail(*jsonOut, "invalid_enum", fmt.Sprintf("role %q is invalid", *role),
			map[string]any{"hint": "one of " + strings.Join(projTaskLinkRoleEnum, ", ")})
		return
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

	task, err := s.ResolveTaskRef(sc.Slug, taskRef)
	if errors.Is(err, store.ErrUnknownTask) {
		projFail(*jsonOut, "unknown_task", fmt.Sprintf("task %q not found in project %s", taskRef, sc.Slug), nil)
		return
	}
	if err != nil {
		fatal(err)
		return
	}

	obsID, ok := projResolveObservationID(s, *observation, *jsonOut)
	if !ok {
		return
	}

	res, err := s.LinkTaskObservation(store.LinkTaskObservationParams{
		Task:          task,
		ObservationID: obsID,
		Role:          *role,
		KnowledgeRef:  f.ptr("knowledge-ref", knowledgeRef),
		GraphRef:      f.ptr("graph-ref", graphRef),
		GraphCommit:   f.ptr("graph-commit", graphCommit),
		RunbookID:     f.ptr("runbook", runbookID),
		JiraRef:       f.ptr("jira-ref", jiraRef),
	})
	switch {
	case errors.Is(err, store.ErrUnknownObservation):
		projFail(*jsonOut, "unknown_observation", fmt.Sprintf("observation %q not found", *observation), nil)
		return
	case errors.Is(err, store.ErrCrossProjectLink):
		projFail(*jsonOut, "cross_project_link", "observation and task belong to different projects", nil)
		return
	case errors.Is(err, store.ErrGraphCommitRequired):
		projFail(*jsonOut, "graph_commit_required", "--graph-ref requires --graph-commit", nil)
		return
	case err != nil:
		if projKnowledgeRefFail(*jsonOut, err) {
			return
		}
		fatal(err)
		return
	}

	result := map[string]any{
		"linked": res.Linked, "task_sync_id": res.TaskSyncID, "observation_sync_id": res.ObservationSyncID,
		"role": res.Role, "refs_added": res.RefsAdded, "refs": res.Refs,
	}
	projPrintResult(*jsonOut, sc, result, func() {
		verb := "linked"
		if !res.Linked {
			verb = "already linked"
		}
		fmt.Printf("%s observation %s to task %s as %s · %d ref(s) added\n",
			verb, res.ObservationSyncID, res.TaskSyncID, res.Role, res.RefsAdded)
		for _, ref := range res.Refs {
			line := fmt.Sprintf("  %-13s %s", ref.RefKind, ref.Ref)
			if ref.GraphCommit != nil {
				line += " @ " + projShort(*ref.GraphCommit, 8)
			}
			fmt.Println(line)
		}
	})
}

// projResolveObservationID accepts either a numeric observation id or an
// obs-<hex> sync id, matching mem_task_link's two input shapes.
func projResolveObservationID(s *store.Store, raw string, jsonOut bool) (int64, bool) {
	raw = strings.TrimSpace(raw)
	if id, err := strconv.ParseInt(raw, 10, 64); err == nil {
		if id < 1 {
			projFail(jsonOut, "invalid_enum", "--observation must be a positive id", nil)
			return 0, false
		}
		return id, true
	}
	obs, err := s.GetObservationBySyncID(raw)
	if err != nil || obs == nil {
		projFail(jsonOut, "unknown_observation", fmt.Sprintf("observation %q not found", raw), nil)
		return 0, false
	}
	return obs.ID, true
}

// ─── evidence ────────────────────────────────────────────────────────────────

func cmdProjectEvidence(cfg store.Config, slug string, args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: engram project [<slug>] evidence <add|list> [flags]")
		exitFunc(1)
		return
	}
	switch args[0] {
	case "add":
		cmdProjectEvidenceAdd(cfg, slug, args[1:])
	case "list":
		cmdProjectEvidenceList(cfg, slug, args[1:])
	default:
		fmt.Fprintf(os.Stderr, "engram: unknown evidence subcommand %q\n", args[0])
		fmt.Fprintln(os.Stderr, "usage: engram project [<slug>] evidence <add|list> [flags]")
		exitFunc(1)
	}
}

// projEvidenceDir is the root captured evidence is stored relative to (D-06).
func projEvidenceDir() string {
	if v := strings.TrimSpace(os.Getenv(projDefaultEvidenceDirEnv)); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return projDefaultEvidenceRelative
	}
	return filepath.Join(home, projDefaultEvidenceRelative)
}

func cmdProjectEvidenceAdd(cfg store.Config, slug string, args []string) {
	positional, rest := projSplitPositional(args, 1)
	f := projNewFlags("engram project evidence add")
	jsonOut := f.fs.Bool("json", false, "print the JSON envelope")
	file := f.fs.String("file", "", "captured file; derives --path, --sha256 and --size-bytes")
	path := f.fs.String("path", "", "path relative to the evidence directory")
	sha := f.fs.String("sha256", "", "64 lowercase hex chars")
	kind := f.fs.String("kind", "", "png|gif|mp4|json|log|txt")
	proves := f.fs.String("proves", "", "what the capture proves")
	configStamp := f.fs.String("config-stamp", "", "configuration value stamped inside the capture")
	capturedAt := f.fs.String("captured-at", "", "ISO-8601 capture timestamp (default: now)")
	sizeBytes := f.fs.Int64("size-bytes", 0, "file size in bytes")
	manifest := f.fs.String("manifest", "", "relative path of the ticket manifest.json")
	attachedJira := f.fs.Bool("attached-jira", false, "the file is already attached to the Jira issue")
	confluenceURL := f.fs.String("confluence-url", "", "Confluence URL holding the file")
	if !f.parse(rest) {
		return
	}

	if len(positional) == 0 {
		projFail(*jsonOut, "missing_field", "usage: engram project [<slug>] evidence add <task> (--file <path> | --path <rel> --sha256 <hex>) --kind <k> --proves <text>", nil)
		return
	}
	taskRef := positional[0]

	relPath := strings.TrimSpace(*path)
	digest := strings.TrimSpace(*sha)
	size := *sizeBytes
	hasSize := f.given("size-bytes")

	if f.given("file") {
		derivedPath, derivedSHA, derivedSize, err := projDeriveEvidenceFromFile(*file)
		if err != nil {
			projFail(*jsonOut, "invalid_file", err.Error(),
				map[string]any{"evidence_dir": projEvidenceDir()})
			return
		}
		if relPath == "" {
			relPath = derivedPath
		}
		if digest == "" {
			digest = derivedSHA
		}
		if !hasSize {
			size = derivedSize
			hasSize = true
		}
	}

	if relPath == "" || digest == "" || strings.TrimSpace(*kind) == "" || strings.TrimSpace(*proves) == "" {
		projFail(*jsonOut, "missing_field", "task, --path (or --file), --sha256, --kind and --proves are required", nil)
		return
	}
	if !projIsSHA256(digest) {
		projFail(*jsonOut, "invalid_sha256", fmt.Sprintf("sha256 %q is not 64 lowercase hex chars", digest), nil)
		return
	}
	if !projEnumContains(projEvidenceKindEnum, *kind) {
		projFail(*jsonOut, "invalid_enum", fmt.Sprintf("kind %q is invalid", *kind),
			map[string]any{"hint": "one of " + strings.Join(projEvidenceKindEnum, ", ")})
		return
	}
	if strings.HasPrefix(relPath, "/") || strings.HasPrefix(relPath, "~") {
		projFail(*jsonOut, "absolute_path_rejected",
			fmt.Sprintf("path %q must be relative to the evidence directory", relPath),
			map[string]any{"evidence_dir": projEvidenceDir()})
		return
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

	task, err := s.ResolveTaskRef(sc.Slug, taskRef)
	if errors.Is(err, store.ErrUnknownTask) {
		projFail(*jsonOut, "unknown_task", fmt.Sprintf("task %q not found in project %s", taskRef, sc.Slug), nil)
		return
	}
	if err != nil {
		fatal(err)
		return
	}

	params := store.AddEvidenceParams{
		Task:                  task,
		Path:                  relPath,
		SHA256:                digest,
		Kind:                  *kind,
		Proves:                *proves,
		ConfigStamp:           f.ptr("config-stamp", configStamp),
		CapturedAt:            f.ptr("captured-at", capturedAt),
		ManifestPath:          f.ptr("manifest", manifest),
		AttachedJira:          *attachedJira,
		AttachedConfluenceURL: f.ptr("confluence-url", confluenceURL),
	}
	if hasSize && size > 0 {
		params.SizeBytes = &size
	}

	evidence, duplicate, limits, err := s.AddEvidence(params)
	if err != nil {
		fatal(err)
		return
	}

	result := map[string]any{"evidence": evidence, "duplicate": duplicate, "limits": limits}
	projPrintResult(*jsonOut, sc, result, func() {
		parts := []string{
			fmt.Sprintf("evidence #%d %s", evidence.ID, evidence.SyncID),
			"sha256 " + projShort(evidence.SHA256, 12),
		}
		if evidence.SizeBytes != nil {
			parts = append(parts, projHumanBytes(*evidence.SizeBytes))
		}
		if limits.OK {
			parts = append(parts, "limits ok")
		} else {
			parts = append(parts, "limits exceeded: "+strings.Join(limits.Violations, ", "))
		}
		if evidence.AttachedJira {
			parts = append(parts, "attached to Jira")
		}
		if duplicate {
			parts = append(parts, "already registered")
		}
		fmt.Println(strings.Join(parts, " · "))
		fmt.Printf("path:    %s\n", evidence.Path)
		fmt.Printf("proves:  %s\n", evidence.Proves)
	})
}

// projDeriveEvidenceFromFile hashes a captured file and derives its
// evidence-relative path and size, so the caller never has to run shasum by
// hand (RFC §7.1, `--file`).
func projDeriveEvidenceFromFile(file string) (relPath, digest string, size int64, err error) {
	abs, err := filepath.Abs(strings.TrimSpace(file))
	if err != nil {
		return "", "", 0, fmt.Errorf("cannot resolve %q: %w", file, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", "", 0, fmt.Errorf("cannot read %q: %w", file, err)
	}
	if info.IsDir() {
		return "", "", 0, fmt.Errorf("%q is a directory", file)
	}

	root := projEvidenceDir()
	if absRoot, rootErr := filepath.Abs(root); rootErr == nil {
		root = absRoot
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", 0, fmt.Errorf("%q is outside the evidence directory %s", file, root)
	}

	f, err := os.Open(abs)
	if err != nil {
		return "", "", 0, fmt.Errorf("cannot open %q: %w", file, err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", "", 0, fmt.Errorf("cannot hash %q: %w", file, err)
	}
	return filepath.ToSlash(rel), fmt.Sprintf("%x", h.Sum(nil)), info.Size(), nil
}

func projIsSHA256(v string) bool {
	if len(v) != 64 {
		return false
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		if !(c >= '0' && c <= '9') && !(c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func cmdProjectEvidenceList(cfg store.Config, slug string, args []string) {
	positional, rest := projSplitPositional(args, 1)
	f := projNewFlags("engram project evidence list")
	jsonOut := f.fs.Bool("json", false, "print the JSON envelope")
	attachedJira := f.fs.Bool("attached-jira", false, "filter by Jira attachment state")
	kind := f.fs.String("kind", "", "png|gif|mp4|json|log|txt")
	limit := f.fs.Int("limit", 50, "maximum rows (1-200)")
	offset := f.fs.Int("offset", 0, "rows to skip")
	if !f.parse(rest) {
		return
	}

	if f.given("kind") && !projEnumContains(projEvidenceKindEnum, *kind) {
		projFail(*jsonOut, "invalid_enum", fmt.Sprintf("kind %q is invalid", *kind),
			map[string]any{"hint": "one of " + strings.Join(projEvidenceKindEnum, ", ")})
		return
	}
	if *offset < 0 {
		projFail(*jsonOut, "invalid_enum", "--offset cannot be negative", nil)
		return
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

	filter := store.EvidenceListFilter{
		AttachedJira: f.boolPtr("attached-jira", attachedJira),
		Kind:         *kind,
		Limit:        projClampInt(*limit, 1, 200, 50),
		Offset:       *offset,
	}
	if len(positional) > 0 {
		task, err := s.ResolveTaskRef(sc.Slug, positional[0])
		if errors.Is(err, store.ErrUnknownTask) {
			projFail(*jsonOut, "unknown_task", fmt.Sprintf("task %q not found in project %s", positional[0], sc.Slug), nil)
			return
		}
		if err != nil {
			fatal(err)
			return
		}
		filter.TaskSyncID = task.SyncID
	}

	items, total, totalBytes, err := s.ListEvidence(sc.Slug, filter)
	if err != nil {
		fatal(err)
		return
	}

	result := map[string]any{
		"items": items, "total": total, "total_bytes": totalBytes,
		"limit": filter.Limit, "offset": filter.Offset,
	}
	projPrintResult(*jsonOut, sc, result, func() {
		if len(items) == 0 {
			fmt.Printf("No evidence in %s for this filter.\n", sc.Slug)
			return
		}
		t := &projTable{headers: []string{"ID", "TASK", "KIND", "SIZE", "JIRA", "CAPTURED", "PATH", "PROVES"}}
		for _, item := range items {
			size := "-"
			if item.SizeBytes != nil {
				size = projHumanBytes(*item.SizeBytes)
			}
			taskKey := projDash(projStrVal(item.JiraKey))
			if taskKey == "-" {
				taskKey = item.TaskSyncID
			}
			t.add(
				strconv.FormatInt(item.ID, 10),
				taskKey,
				item.Kind,
				size,
				projYesNo(item.AttachedJira),
				item.CapturedAt,
				item.Path,
				item.Proves,
			)
		}
		t.render(os.Stdout)
		fmt.Printf("\n%d of %d file(s) · %s total\n", len(items), total, projHumanBytes(totalBytes))
	})
}

// ─── runbooks ────────────────────────────────────────────────────────────────

func cmdProjectRunbooks(cfg store.Config, slug string, args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: engram project [<slug>] runbooks <sync|find> [flags]")
		exitFunc(1)
		return
	}
	switch args[0] {
	case "sync":
		cmdProjectRunbooksSync(cfg, slug, args[1:])
	case "find":
		cmdProjectRunbooksFind(cfg, slug, args[1:])
	default:
		fmt.Fprintf(os.Stderr, "engram: unknown runbooks subcommand %q\n", args[0])
		fmt.Fprintln(os.Stderr, "usage: engram project [<slug>] runbooks <sync|find> [flags]")
		exitFunc(1)
	}
}

func cmdProjectRunbooksSync(cfg store.Config, slug string, args []string) {
	f := projNewFlags("engram project runbooks sync")
	jsonOut := f.fs.Bool("json", false, "print the JSON envelope")
	vaultDir := f.fs.String("vault-dir", "", "vault checkout holding Runbooks/**/*.md")
	entriesFile := f.fs.String("entries-file", "", "JSON file with the entries array")
	pruneMissing := f.fs.Bool("prune-missing", false, "delete index rows absent from the entries")
	if !f.parse(args) {
		return
	}

	if f.given("vault-dir") == f.given("entries-file") {
		projFail(*jsonOut, "missing_field", "exactly one of --vault-dir or --entries-file is required", nil)
		return
	}

	var entries []store.RunbookIndexEntryInput
	var skipped []store.RunbookSkipped
	scanned := 0
	source := "knowledge-mcp"

	if f.given("vault-dir") {
		source = "vault-fs"
		dir := strings.TrimSpace(*vaultDir)
		if abs, err := filepath.Abs(dir); err == nil {
			dir = abs
		}
		scan, err := runbooks.ScanVault(dir, time.Now())
		if errors.Is(err, runbooks.ErrVaultDirNotFound) {
			projFail(*jsonOut, "vault_dir_not_found", err.Error(),
				map[string]any{"vault_dir": dir, "hint": "point --vault-dir at the vault root, the folder that contains Runbooks/"})
			return
		}
		if err != nil {
			projFail(*jsonOut, "vault_scan_failed", err.Error(), map[string]any{"vault_dir": dir})
			return
		}
		entries, skipped, scanned = scan.Entries, scan.Skipped, scan.Scanned
	} else {
		parsed, err := projReadRunbookEntriesFile(*entriesFile)
		if err != nil {
			projFail(*jsonOut, "entries_rejected", err.Error(), map[string]any{"entries_file": *entriesFile})
			return
		}
		entries = parsed
		scanned = len(parsed)
	}

	if len(entries) == 0 {
		projFail(*jsonOut, "entries_rejected", "no indexable runbook entries were found",
			map[string]any{"scanned": scanned, "skipped": skipped})
		return
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

	// store.SyncRunbookIndex keys every row by the entry's own `service`, so
	// the project-scoped form has to drop the other services here: without
	// this filter `engram project <slug> runbooks sync` would index the whole
	// vault and `--prune-missing`, which IS scoped to the slug, would then
	// disagree with what was just written.
	entries, skipped = projFilterRunbookEntriesByProject(entries, skipped, sc.Slug)
	if len(entries) == 0 {
		projFail(*jsonOut, "entries_rejected",
			fmt.Sprintf("no runbook entries belong to project %s", sc.Slug),
			map[string]any{"scanned": scanned, "skipped": skipped})
		return
	}

	res, err := runbooks.SyncIndex(s, store.RunbookIndexSyncParams{
		Project:      sc.Slug,
		Source:       source,
		PruneMissing: *pruneMissing,
		Entries:      entries,
	})
	if err != nil {
		fatal(err)
		return
	}
	res.Skipped = append(skipped, res.Skipped...)

	result := map[string]any{"sync": res, "scanned": scanned, "source": source}
	projPrintResult(*jsonOut, sc, result, func() {
		parts := []string{
			fmt.Sprintf("scanned %d runbook document(s)", scanned),
			fmt.Sprintf("upserted %d", res.Upserted),
			fmt.Sprintf("unchanged %d", res.Unchanged),
		}
		if res.Pruned > 0 {
			parts = append(parts, fmt.Sprintf("pruned %d", res.Pruned))
		}
		if len(res.Skipped) > 0 {
			parts = append(parts, fmt.Sprintf("skipped %d (%s)", len(res.Skipped), projSkipSummary(res.Skipped)))
		}
		parts = append(parts,
			fmt.Sprintf("stale %d", res.StaleCount),
			fmt.Sprintf("exec recomputed %d", res.ExecRecomputed))
		fmt.Println(strings.Join(parts, " · "))
	})
}

// projFilterRunbookEntriesByProject keeps only the entries whose canonical
// service is the project being synced, reporting the rest as `other_project`
// skips so the caller can see what a wider vault held.
func projFilterRunbookEntriesByProject(entries []store.RunbookIndexEntryInput, skipped []store.RunbookSkipped, slug string) ([]store.RunbookIndexEntryInput, []store.RunbookSkipped) {
	kept := make([]store.RunbookIndexEntryInput, 0, len(entries))
	for _, e := range entries {
		service, _ := store.NormalizeProject(e.Service)
		if service != slug {
			skipped = append(skipped, store.RunbookSkipped{ID: e.ID, VaultPath: e.VaultPath, Reason: "other_project"})
			continue
		}
		kept = append(kept, e)
	}
	return kept, skipped
}

// projSkipSummary collapses the skip reasons into "template x5, missing_service x1".
func projSkipSummary(skipped []store.RunbookSkipped) string {
	counts := map[string]int{}
	for _, sk := range skipped {
		reason := sk.Reason
		if reason == "" {
			reason = "unknown"
		}
		counts[reason]++
	}
	reasons := make([]string, 0, len(counts))
	for reason := range counts {
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)
	parts := make([]string, 0, len(reasons))
	for _, reason := range reasons {
		parts = append(parts, fmt.Sprintf("%s x%d", reason, counts[reason]))
	}
	return strings.Join(parts, ", ")
}

// projRunbookEntryFile is the on-disk shape of --entries-file: either a bare
// array or an object with an `entries` key, so a saved mem_runbook_index_sync
// payload can be replayed verbatim.
type projRunbookEntryFile struct {
	Entries []projRunbookEntryJSON `json:"entries"`
}

type projRunbookEntryJSON struct {
	ID              string   `json:"id"`
	VaultPath       string   `json:"vault_path"`
	Title           string   `json:"title"`
	Service         string   `json:"service"`
	Category        string   `json:"category"`
	Pattern         string   `json:"pattern"`
	Severity        string   `json:"severity"`
	Status          string   `json:"status"`
	Symptoms        []string `json:"symptoms"`
	Tags            []string `json:"tags"`
	Owner           string   `json:"owner"`
	AutomationLevel string   `json:"automation_level"`
	LastUpdated     string   `json:"last_updated"`
	LastVerified    string   `json:"last_verified"`
	NeedsReview     *bool    `json:"needs_review"`
	AgeDays         *int     `json:"age_days"`
}

func projReadRunbookEntriesFile(path string) ([]store.RunbookIndexEntryInput, error) {
	raw, err := os.ReadFile(strings.TrimSpace(path))
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}

	var wrapper projRunbookEntryFile
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		var bare []projRunbookEntryJSON
		if arrErr := json.Unmarshal(raw, &bare); arrErr != nil {
			return nil, fmt.Errorf("%s is not a JSON array of entries nor an object with an \"entries\" key", path)
		}
		wrapper.Entries = bare
	}
	if len(wrapper.Entries) == 0 {
		var bare []projRunbookEntryJSON
		if arrErr := json.Unmarshal(raw, &bare); arrErr == nil {
			wrapper.Entries = bare
		}
	}
	if len(wrapper.Entries) == 0 {
		return nil, fmt.Errorf("%s contains no entries", path)
	}

	out := make([]store.RunbookIndexEntryInput, 0, len(wrapper.Entries))
	for i, e := range wrapper.Entries {
		if e.ID == "" || e.VaultPath == "" || e.Title == "" || e.Service == "" || e.Category == "" || e.Status == "" {
			return nil, fmt.Errorf("entry %d in %s is missing one of id, vault_path, title, service, category, status", i, path)
		}
		out = append(out, store.RunbookIndexEntryInput{
			ID: e.ID, VaultPath: e.VaultPath, Title: e.Title, Service: e.Service,
			Category: e.Category, Pattern: e.Pattern, Severity: e.Severity, Status: e.Status,
			Symptoms: e.Symptoms, Tags: e.Tags, Owner: e.Owner,
			AutomationLevel: e.AutomationLevel, LastUpdated: e.LastUpdated,
			LastVerified: e.LastVerified, NeedsReview: e.NeedsReview, AgeDays: e.AgeDays,
		})
	}
	return out, nil
}

func cmdProjectRunbooksFind(cfg store.Config, slug string, args []string) {
	positional, rest := projSplitPositional(args, 1)
	f := projNewFlags("engram project runbooks find")
	jsonOut := f.fs.Bool("json", false, "print the JSON envelope")
	category := f.fs.String("category", "", "runbook category filter")
	pattern := f.fs.String("pattern", "", "runbook pattern filter")
	includeStale := f.fs.Bool("include-stale", true, "include stale runbooks (use --include-stale=false to exclude)")
	matchMode := f.fs.String("match-mode", "any", "all|any")
	limit := f.fs.Int("limit", 5, "maximum rows (1-20)")
	if !f.parse(rest) {
		return
	}

	if len(positional) == 0 {
		projFail(*jsonOut, "missing_field", "usage: engram project [<slug>] runbooks find <symptom> [flags]", nil)
		return
	}
	query := positional[0]
	if len(strings.TrimSpace(query)) < 3 {
		projFail(*jsonOut, "missing_field", "the search query must be at least 3 characters", nil)
		return
	}
	if f.given("category") && !projEnumContains(projRunbookCategoryEnum, *category) {
		projFail(*jsonOut, "invalid_enum", fmt.Sprintf("category %q is invalid", *category),
			map[string]any{"hint": "one of " + strings.Join(projRunbookCategoryEnum, ", ")})
		return
	}
	if f.given("pattern") && !projEnumContains(projRunbookPatternEnum, *pattern) {
		projFail(*jsonOut, "invalid_enum", fmt.Sprintf("pattern %q is invalid", *pattern),
			map[string]any{"hint": "one of " + strings.Join(projRunbookPatternEnum, ", ")})
		return
	}
	if !projEnumContains(projMatchModeEnum, *matchMode) {
		projFail(*jsonOut, "invalid_enum", fmt.Sprintf("match-mode %q is invalid", *matchMode),
			map[string]any{"hint": "one of " + strings.Join(projMatchModeEnum, ", ")})
		return
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

	items, total, err := s.FindRunbooks(store.RunbookFindParams{
		Query:        query,
		Project:      sc.Slug,
		Category:     *category,
		Pattern:      *pattern,
		IncludeStale: *includeStale,
		MatchMode:    *matchMode,
		Limit:        projClampInt(*limit, 1, 20, 5),
	})
	if err != nil {
		fatal(err)
		return
	}

	result := map[string]any{"items": items, "total": total}
	projPrintResult(*jsonOut, sc, result, func() {
		if len(items) == 0 {
			fmt.Printf("No runbooks in %s match %q.\n", sc.Slug, query)
			return
		}
		projRenderRunbookHits(os.Stdout, items)
		fmt.Printf("\n%d of %d runbook(s)\n", len(items), total)
	})
}

func projRenderRunbookHits(w io.Writer, items []store.RunbookFindItem) {
	idW, titleW, taxonomyW, statusW := 0, 0, 0, 0
	taxonomy := make([]string, len(items))
	for i, item := range items {
		tax := item.Category
		if p := projStrVal(item.Pattern); p != "" {
			tax += "/" + p
		}
		taxonomy[i] = tax
		if projWidth(item.ID) > idW {
			idW = projWidth(item.ID)
		}
		if projWidth(item.Title) > titleW {
			titleW = projWidth(item.Title)
		}
		if projWidth(tax) > taxonomyW {
			taxonomyW = projWidth(tax)
		}
		if projWidth(item.Status) > statusW {
			statusW = projWidth(item.Status)
		}
	}
	for i, item := range items {
		freshness := "fresh"
		if item.Stale {
			freshness = "STALE"
			if item.AgeDays != nil {
				freshness = fmt.Sprintf("STALE(%dd)", *item.AgeDays)
			}
		}
		fmt.Fprintf(w, "%s  %s  %s  %s  %s  exec %d  rank %.2f\n",
			projPad(item.ID, idW), projPad(item.Title, titleW), projPad(taxonomy[i], taxonomyW),
			projPad(item.Status, statusW), projPad(freshness, 10), item.ExecCount, item.Rank)
		fmt.Fprintf(w, "%s%s\n", strings.Repeat(" ", idW+2), item.VaultPath)
	}
}

// ─── context ─────────────────────────────────────────────────────────────────

func cmdProjectContext(cfg store.Config, slug string, args []string) {
	positional, rest := projSplitPositional(args, 1)
	f := projNewFlags("engram project context")
	jsonOut := f.fs.Bool("json", false, "shorthand for --format json")
	format := f.fs.String("format", "markdown", "markdown|json")
	maxChars := f.fs.Int("max-chars", 12000, "character budget (2000-40000)")
	observationsLimit := f.fs.Int("observations-limit", 8, "linked observations to include (1-30)")
	observationChars := f.fs.Int("observation-chars", 600, "characters per observation (200-4000)")
	sections := f.fs.String("sections", "", "comma-separated subset of the canonical sections")
	includeRunbooks := f.fs.Bool("include-runbooks", true, "include candidate runbooks (use --include-runbooks=false to omit)")
	repoDir := f.fs.String("repo-dir", "", "repository root; enables the graph HEAD staleness check")
	copyToClipboard := f.fs.Bool("copy", false, "also copy the pack to the system clipboard via OSC 52")
	if !f.parse(rest) {
		return
	}

	resolvedFormat := *format
	if *jsonOut {
		resolvedFormat = "json"
	}
	if !projEnumContains(projContextPackFormatEnum, resolvedFormat) {
		projFail(resolvedFormat == "json", "invalid_enum", fmt.Sprintf("format %q is invalid", resolvedFormat),
			map[string]any{"hint": "one of " + strings.Join(projContextPackFormatEnum, ", ")})
		return
	}
	envelope := resolvedFormat == "json"

	if len(positional) == 0 {
		projFail(envelope, "missing_field", "usage: engram project [<slug>] context <task> [flags]", nil)
		return
	}
	taskRef := positional[0]

	selected := projSplitList(*sections)
	for _, sec := range selected {
		if !projEnumContains(projContextPackSectionEnum, sec) {
			projFail(envelope, "invalid_enum", fmt.Sprintf("section %q is invalid", sec),
				map[string]any{"hint": "one of " + strings.Join(projContextPackSectionEnum, ", ")})
			return
		}
	}

	s, ok := projOpenStore(cfg)
	if !ok {
		return
	}
	defer s.Close()

	sc := projScopeForRead(s, slug, envelope)
	if sc.Slug == "" {
		return
	}

	opts := projectpkg.DefaultContextPackOptions()
	opts.MaxChars = projClampInt(*maxChars, 2000, 40000, opts.MaxChars)
	opts.ObservationsLimit = projClampInt(*observationsLimit, 1, 30, opts.ObservationsLimit)
	opts.ObservationChars = projClampInt(*observationChars, 200, 4000, opts.ObservationChars)
	opts.Sections = selected
	opts.IncludeRunbooks = *includeRunbooks
	opts.Format = resolvedFormat
	opts.RepoDir = strings.TrimSpace(*repoDir)

	pack, rendered, err := projectpkg.BuildContextPack(s, sc.Slug, taskRef, opts)
	if errors.Is(err, store.ErrUnknownTask) {
		projFail(envelope, "unknown_task", fmt.Sprintf("task %q not found in project %s", taskRef, sc.Slug), nil)
		return
	}
	if err != nil {
		fatal(err)
		return
	}

	payload := rendered
	if envelope {
		if out, marshalErr := jsonMarshalIndent(pack, "", "  "); marshalErr == nil {
			payload = string(out)
		}
	}

	if *copyToClipboard {
		projCopyOSC52(payload)
	}

	projPrintResult(envelope, sc, pack, func() {
		fmt.Print(rendered)
		if !strings.HasSuffix(rendered, "\n") {
			fmt.Println()
		}
	})

	if *copyToClipboard {
		truncated := ""
		if pack.Truncated {
			truncated = " · truncated"
		}
		fmt.Fprintf(os.Stderr, "context pack: %s chars%s · copied to clipboard\n",
			projThousands(pack.Chars), truncated)
	}
}

// projCopyOSC52 asks the terminal to put content on the system clipboard.
// The escape goes to the controlling terminal so a redirected stdout keeps
// only the pack itself; stderr is the fallback when there is no tty.
func projCopyOSC52(content string) {
	sequence := fmt.Sprintf("\x1b]52;c;%s\x07", base64.StdEncoding.EncodeToString([]byte(content)))
	if tty, ok := projTTYWriter(); ok {
		defer tty.Close()
		fmt.Fprint(tty, sequence)
		return
	}
	fmt.Fprint(os.Stderr, sequence)
}
