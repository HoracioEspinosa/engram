package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	projectpkg "github.com/Gentleman-Programming/engram/internal/project"
	"github.com/Gentleman-Programming/engram/internal/store"
	_ "modernc.org/sqlite"
)

// ─── Harness ─────────────────────────────────────────────────────────────────

// stubExit replaces exitFunc and reports whether a non-zero exit was requested.
func stubExit(t *testing.T) *bool {
	t.Helper()
	exited := false
	old := exitFunc
	exitFunc = func(code int) {
		if code != 0 {
			exited = true
		}
	}
	t.Cleanup(func() { exitFunc = old })
	return &exited
}

// stubDetection pins cwd project detection so the tests never depend on where
// `go test` happens to run from.
func stubDetection(t *testing.T, name, path string) {
	t.Helper()
	old := detectProjectFull
	detectProjectFull = func(string) projectpkg.DetectionResult {
		return projectpkg.DetectionResult{Project: name, Source: projectpkg.SourceGitRoot, Path: path}
	}
	t.Cleanup(func() { detectProjectFull = old })
}

func openTestStore(t *testing.T, cfg store.Config) *store.Store {
	t.Helper()
	s, err := store.New(cfg)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func seedCard(t *testing.T, cfg store.Config, slug string) {
	t.Helper()
	s := openTestStore(t, cfg)
	if _, _, err := s.UpsertProjectCard(store.UpsertProjectCardParams{Slug: slug}); err != nil {
		t.Fatalf("UpsertProjectCard(%s): %v", slug, err)
	}
}

func seedTask(t *testing.T, cfg store.Config, slug, jiraKey, title, kind string) store.Task {
	t.Helper()
	s := openTestStore(t, cfg)
	res, err := s.UpsertTask(store.UpsertTaskParams{
		Project: slug, JiraKey: &jiraKey, Title: &title, Kind: &kind,
	})
	if err != nil {
		t.Fatalf("UpsertTask(%s): %v", jiraKey, err)
	}
	return res.Task
}

// runProject invokes `engram project …` with args and captures both streams.
func runProject(t *testing.T, cfg store.Config, args ...string) (stdout, stderr string) {
	t.Helper()
	withArgs(t, append([]string{"engram", "project"}, args...)...)
	return captureOutput(t, func() { cmdProject(cfg) })
}

// decodeEnvelope parses the CLI's {"project",…,"result"} success envelope.
func decodeEnvelope(t *testing.T, out string) map[string]any {
	t.Helper()
	var envelope map[string]any
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, out)
	}
	return envelope
}

// decodeErrorCode returns the `code` of the CLI's JSON error envelope.
func decodeErrorCode(t *testing.T, out string) string {
	t.Helper()
	envelope := decodeEnvelope(t, out)
	code, _ := envelope["code"].(string)
	if code == "" {
		t.Fatalf("expected an error envelope with a code, got:\n%s", out)
	}
	return code
}

func envelopeResult(t *testing.T, out string) map[string]any {
	t.Helper()
	envelope := decodeEnvelope(t, out)
	result, ok := envelope["result"].(map[string]any)
	if !ok {
		t.Fatalf("envelope has no object result:\n%s", out)
	}
	return result
}

// ─── Pure helpers ────────────────────────────────────────────────────────────

func TestProjParseStaleAfter(t *testing.T) {
	tests := []struct {
		in      string
		want    int
		wantErr bool
	}{
		{in: "", want: 24},
		{in: "24h", want: 24},
		{in: "48h", want: 48},
		{in: "90m", want: 2},   // rounded up: a partial hour still counts
		{in: "12", want: 12},   // bare number of hours
		{in: "1h30m", want: 2}, // rounded up
		{in: "0", wantErr: true},
		{in: "-5", wantErr: true},
		{in: "30s", wantErr: true}, // rounds to 1h? no: 0.008h ceils to 1... guarded below
		{in: "yesterday", wantErr: true},
	}
	for _, tc := range tests {
		got, err := projParseStaleAfter(tc.in)
		if tc.wantErr {
			// 30s ceils to 1 hour, which is a legitimate (if odd) window.
			if tc.in == "30s" {
				if err != nil || got != 1 {
					t.Fatalf("projParseStaleAfter(%q) = %d, %v; want 1, nil", tc.in, got, err)
				}
				continue
			}
			if err == nil {
				t.Fatalf("projParseStaleAfter(%q) = %d, want an error", tc.in, got)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Fatalf("projParseStaleAfter(%q) = %d, %v; want %d, nil", tc.in, got, err, tc.want)
		}
	}
}

func TestProjIsSlugValid(t *testing.T) {
	valid := []string{"nextcloud", "cd-knowledge-mcp", "a", "app2", "x9-y"}
	for _, slug := range valid {
		if !projIsSlugValid(slug) {
			t.Fatalf("projIsSlugValid(%q) = false, want true", slug)
		}
	}
	// Reserved slugs and shapes the MCP tools reject must stay rejected here.
	invalid := []string{"", "migrate", "current", "-lead", "Upper", "with space", "under_score", strings.Repeat("a", 65)}
	for _, slug := range invalid {
		if projIsSlugValid(slug) {
			t.Fatalf("projIsSlugValid(%q) = true, want false", slug)
		}
	}
}

func TestProjIsSHA256(t *testing.T) {
	good := strings.Repeat("ab12", 16) // 64 lowercase hex chars
	if !projIsSHA256(good) {
		t.Fatalf("projIsSHA256(%q) = false, want true", good)
	}
	bad := []string{"", strings.Repeat("a", 63), strings.Repeat("A", 64), strings.Repeat("g", 64)}
	for _, v := range bad {
		if projIsSHA256(v) {
			t.Fatalf("projIsSHA256(%q) = true, want false", v)
		}
	}
}

func TestProjSplitPositional(t *testing.T) {
	pos, rest := projSplitPositional([]string{"CDBS-1", "--kind", "png"}, 1)
	if len(pos) != 1 || pos[0] != "CDBS-1" || len(rest) != 2 {
		t.Fatalf("pos=%#v rest=%#v", pos, rest)
	}
	pos, rest = projSplitPositional([]string{"--kind", "png"}, 1)
	if len(pos) != 0 || len(rest) != 2 {
		t.Fatalf("a leading flag must not be taken as positional: pos=%#v rest=%#v", pos, rest)
	}
	pos, rest = projSplitPositional(nil, 1)
	if len(pos) != 0 || len(rest) != 0 {
		t.Fatalf("empty args: pos=%#v rest=%#v", pos, rest)
	}
}

func TestProjTableAlignsColumns(t *testing.T) {
	tbl := &projTable{headers: []string{"ID", "TITLE"}}
	tbl.add("1", "short")
	tbl.add("1000", "a much longer title")
	var buf bytes.Buffer
	tbl.render(&buf)
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("want header + 2 rows, got %#v", lines)
	}
	col := strings.Index(lines[0], "TITLE")
	for _, line := range lines[1:] {
		if !strings.HasPrefix(line[col:], strings.TrimSpace(line[col:])) {
			t.Fatalf("column not aligned at %d: %q", col, line)
		}
	}
	if strings.Index(lines[1], "short") != col || strings.Index(lines[2], "a much") != col {
		t.Fatalf("second column starts at different offsets:\n%s", buf.String())
	}
}

func TestProjTableAlignsMultiByteCells(t *testing.T) {
	// Accented titles are the everyday case here; measuring a column in bytes
	// instead of characters pushes every following column out of line.
	tbl := &projTable{headers: []string{"KEY", "TITLE", "OBS"}}
	tbl.add("CDBS-1", "Previews devuelven 503", "7")
	tbl.add("CDBS-2", "Sesión caída tras el fix", "3")
	var buf bytes.Buffer
	tbl.render(&buf)
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("want header + 2 rows, got %#v", lines)
	}
	col := func(line string) int {
		return len([]rune(line)) - len([]rune(strings.TrimLeft(line[strings.LastIndex(line, " ")+1:], " ")))
	}
	if col(lines[1]) != col(lines[2]) {
		t.Fatalf("the last column is misaligned across rows:\n%s", buf.String())
	}
	if strings.Contains(lines[2], "  7") {
		t.Fatalf("unexpected row content:\n%s", buf.String())
	}
}

func TestProjShortIsRuneSafe(t *testing.T) {
	if got := projShort("Sesión caída", 6); got != "Sesión…" {
		t.Fatalf("projShort truncated mid-rune: %q", got)
	}
	if got := projShort("short", 20); got != "short" {
		t.Fatalf("projShort(%q) = %q", "short", got)
	}
	if got := projShort("", 5); got != "-" {
		t.Fatalf("empty must render as a dash, got %q", got)
	}
}

func TestProjHumanBytes(t *testing.T) {
	cases := map[int64]string{0: "-", 512: "512 B", 2048: "2.0 KiB", 5 * 1024 * 1024: "5.0 MiB"}
	for in, want := range cases {
		if got := projHumanBytes(in); got != want {
			t.Fatalf("projHumanBytes(%d) = %q, want %q", in, got, want)
		}
	}
}

// ─── Dispatch and project resolution ─────────────────────────────────────────

func TestCmdProjectTreatsFirstArgAsSubcommandWhenItIsOne(t *testing.T) {
	cfg := testConfig(t)
	seedCard(t, cfg, "nextcloud")
	t.Setenv("ENGRAM_PROJECT", "nextcloud")
	stubDetection(t, "somewhere-else", "/tmp/elsewhere")
	exited := stubExit(t)

	stdout, stderr := runProject(t, cfg, "card", "--json")
	if *exited {
		t.Fatalf("unexpected failure: %s", stderr)
	}
	envelope := decodeEnvelope(t, stdout)
	if envelope["project"] != "nextcloud" {
		t.Fatalf("project = %v, want nextcloud (ENGRAM_PROJECT must win over cwd)", envelope["project"])
	}
}

func TestCmdProjectTreatsFirstArgAsSlugWhenItIsNotASubcommand(t *testing.T) {
	cfg := testConfig(t)
	seedCard(t, cfg, "middleware")
	t.Setenv("ENGRAM_PROJECT", "nextcloud")
	exited := stubExit(t)

	stdout, stderr := runProject(t, cfg, "middleware", "card", "--json")
	if *exited {
		t.Fatalf("unexpected failure: %s", stderr)
	}
	envelope := decodeEnvelope(t, stdout)
	if envelope["project"] != "middleware" {
		t.Fatalf("project = %v, want middleware (an explicit slug must win over ENGRAM_PROJECT)", envelope["project"])
	}
}

func TestCmdProjectUnknownSubcommandFails(t *testing.T) {
	cfg := testConfig(t)
	exited := stubExit(t)
	_, stderr := runProject(t, cfg, "nextcloud", "frobnicate")
	if !*exited {
		t.Fatal("an unknown subcommand must exit non-zero")
	}
	if !strings.Contains(stderr, "unknown project subcommand") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestCmdProjectNoArgsPrintsUsage(t *testing.T) {
	cfg := testConfig(t)
	exited := stubExit(t)
	stdout, _ := runProject(t, cfg)
	if !*exited {
		t.Fatal("no subcommand must exit non-zero")
	}
	if !strings.Contains(stdout, "usage: engram project") {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestCmdProjectHelpExitsZero(t *testing.T) {
	cfg := testConfig(t)
	exited := stubExit(t)
	stdout, _ := runProject(t, cfg, "help")
	if *exited {
		t.Fatal("`project help` must exit zero")
	}
	for _, want := range []string{"tasks list", "evidence add", "runbooks sync", "context <task>"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("usage is missing %q:\n%s", want, stdout)
		}
	}
}

func TestCmdProjectUnknownProjectIsRefused(t *testing.T) {
	cfg := testConfig(t)
	seedCard(t, cfg, "nextcloud")
	t.Setenv("ENGRAM_PROJECT", "")
	stubDetection(t, "nextcloud", "/tmp/nextcloud")
	exited := stubExit(t)

	stdout, _ := runProject(t, cfg, "ghost", "card", "--json")
	if !*exited {
		t.Fatal("an unknown project must exit non-zero")
	}
	if code := decodeErrorCode(t, stdout); code != "unknown_project" {
		t.Fatalf("code = %q, want unknown_project", code)
	}
}

// ─── card ────────────────────────────────────────────────────────────────────

func TestCmdProjectCardWithoutCardReportsNoCard(t *testing.T) {
	cfg := testConfig(t)
	mustSeedObservation(t, cfg, "s-card", "nextcloud", "discovery", "t", "c", "project")
	t.Setenv("ENGRAM_PROJECT", "nextcloud")
	exited := stubExit(t)

	stdout, _ := runProject(t, cfg, "card", "--json")
	if !*exited {
		t.Fatal("a project without a card must exit non-zero")
	}
	if code := decodeErrorCode(t, stdout); code != "no_card" {
		t.Fatalf("code = %q, want no_card", code)
	}
}

func TestCmdProjectCardRendersCountsAndSync(t *testing.T) {
	cfg := testConfig(t)
	seedCard(t, cfg, "nextcloud")
	seedTask(t, cfg, "nextcloud", "CDBS-10336", "Previews return 503", "incident")
	t.Setenv("ENGRAM_PROJECT", "nextcloud")
	exited := stubExit(t)

	stdout, stderr := runProject(t, cfg, "card")
	if *exited {
		t.Fatalf("unexpected failure: %s", stderr)
	}
	for _, want := range []string{"project:   nextcloud", "counts:", "1/1 tasks active", "sync:"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("card output is missing %q:\n%s", want, stdout)
		}
	}
}

func TestCmdProjectCardHidesGraphSummaryUnlessAsked(t *testing.T) {
	cfg := testConfig(t)
	seedCard(t, cfg, "nextcloud")
	s := openTestStore(t, cfg)
	summary := `{"node_count":3,"edge_count":2,"community_count":1,"extracted_pct":100,"inferred_pct":0,` +
		`"god_nodes":[{"label":"OCP\\IConfig","edges":7,"file":"lib/public/IConfig.php"}]}`
	commit := strings.Repeat("a1b2c3d4", 5) // synthetic 40-hex commit
	if err := s.StampProjectGraph("nextcloud", commit, "2026-09-01 10:00:00", &summary); err != nil {
		t.Fatalf("StampProjectGraph: %v", err)
	}
	t.Setenv("ENGRAM_PROJECT", "nextcloud")
	stubExit(t)

	plain, _ := runProject(t, cfg, "card")
	if strings.Contains(plain, "god nodes:") {
		t.Fatalf("graph_summary leaked without --graph-summary:\n%s", plain)
	}

	withSummary, _ := runProject(t, cfg, "card", "--graph-summary")
	if !strings.Contains(withSummary, "god nodes:") || !strings.Contains(withSummary, "3 nodes") {
		t.Fatalf("--graph-summary did not render the summary:\n%s", withSummary)
	}
}

func TestCmdProjectCardJSONUnwrapsGraphSummary(t *testing.T) {
	cfg := testConfig(t)
	seedCard(t, cfg, "nextcloud")
	s := openTestStore(t, cfg)
	summary := `{"node_count":3,"god_nodes":[{"label":"OCP\\IConfig","edges":7,"file":"x.php"}]}`
	commit := strings.Repeat("a1b2c3d4", 5)
	if err := s.StampProjectGraph("nextcloud", commit, "2026-09-01 10:00:00", &summary); err != nil {
		t.Fatalf("StampProjectGraph: %v", err)
	}
	t.Setenv("ENGRAM_PROJECT", "nextcloud")
	stubExit(t)

	stdout, _ := runProject(t, cfg, "card", "--graph-summary", "--json")
	result := envelopeResult(t, stdout)
	card, _ := result["card"].(map[string]any)
	if card == nil {
		t.Fatalf("no card in result:\n%s", stdout)
	}
	nested, ok := card["graph_summary"].(map[string]any)
	if !ok {
		t.Fatalf("graph_summary must be a JSON object for jq, got %T:\n%s", card["graph_summary"], stdout)
	}
	if nested["node_count"].(float64) != 3 {
		t.Fatalf("node_count = %v", nested["node_count"])
	}
}

// ─── upsert ──────────────────────────────────────────────────────────────────

func TestCmdProjectUpsertCreatesThenLeavesOmittedFieldsAlone(t *testing.T) {
	cfg := testConfig(t)
	t.Setenv("ENGRAM_PROJECT", "")
	stubDetection(t, "nextcloud", "/tmp/nextcloud")
	exited := stubExit(t)

	stdout, stderr := runProject(t, cfg, "upsert", "--display-name", "Nextcloud server", "--owner", "plat", "--json")
	if *exited {
		t.Fatalf("unexpected failure: %s", stderr)
	}
	result := envelopeResult(t, stdout)
	if created, _ := result["created"].(bool); !created {
		t.Fatalf("first upsert must report created:\n%s", stdout)
	}

	// Second upsert touches only the owner: display_name must survive.
	stdout, stderr = runProject(t, cfg, "upsert", "--owner", "dev-nextcloud", "--json")
	if *exited {
		t.Fatalf("unexpected failure: %s", stderr)
	}
	result = envelopeResult(t, stdout)
	card, _ := result["card"].(map[string]any)
	if card["display_name"] != "Nextcloud server" {
		t.Fatalf("display_name was clobbered by an upsert that did not set it: %v", card["display_name"])
	}
	if card["owner"] != "dev-nextcloud" {
		t.Fatalf("owner = %v, want dev-nextcloud", card["owner"])
	}
}

func TestCmdProjectUpsertRefusesUnbackedExplicitSlug(t *testing.T) {
	cfg := testConfig(t)
	t.Setenv("ENGRAM_PROJECT", "")
	stubDetection(t, "nextcloud", "/tmp/nextcloud")
	exited := stubExit(t)

	stdout, _ := runProject(t, cfg, "typoed-slug", "upsert", "--display-name", "Oops", "--json")
	if !*exited {
		t.Fatal("an unbacked explicit slug must be refused")
	}
	if code := decodeErrorCode(t, stdout); code != "unknown_project" {
		t.Fatalf("code = %q, want unknown_project", code)
	}
	// The canary: nothing may have been written under the mistyped slug.
	s := openTestStore(t, cfg)
	if exists, _ := s.ProjectCardExists("typoed-slug"); exists {
		t.Fatal("a card was created for a refused slug")
	}
}

// ─── tasks ───────────────────────────────────────────────────────────────────

func TestCmdProjectTasksUpsertValidation(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantCode string
	}{
		{name: "no key", args: []string{"tasks", "upsert", "--title", "T", "--kind", "bugfix", "--json"}, wantCode: "missing_field"},
		{name: "bad kind", args: []string{"tasks", "upsert", "--jira", "CDBS-1", "--title", "T", "--kind", "chore", "--json"}, wantCode: "invalid_enum"},
		{name: "bad state", args: []string{"tasks", "upsert", "--jira", "CDBS-1", "--title", "T", "--kind", "bugfix", "--state", "wip", "--json"}, wantCode: "invalid_enum"},
		{name: "bad category", args: []string{"tasks", "upsert", "--jira", "CDBS-1", "--title", "T", "--kind", "bugfix", "--jira-status-category", "pending", "--json"}, wantCode: "invalid_enum"},
		{name: "no title on create", args: []string{"tasks", "upsert", "--jira", "CDBS-1", "--kind", "bugfix", "--json"}, wantCode: "missing_field"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testConfig(t)
			seedCard(t, cfg, "nextcloud")
			t.Setenv("ENGRAM_PROJECT", "nextcloud")
			exited := stubExit(t)

			stdout, _ := runProject(t, cfg, tc.args...)
			if !*exited {
				t.Fatalf("expected a failure, stdout=%s", stdout)
			}
			if code := decodeErrorCode(t, stdout); code != tc.wantCode {
				t.Fatalf("code = %q, want %q", code, tc.wantCode)
			}
		})
	}
}

func TestCmdProjectTasksUpsertCreatesAndUpdates(t *testing.T) {
	cfg := testConfig(t)
	seedCard(t, cfg, "nextcloud")
	t.Setenv("ENGRAM_PROJECT", "nextcloud")
	exited := stubExit(t)

	stdout, stderr := runProject(t, cfg, "tasks", "upsert",
		"--jira", "CDBS-10336", "--title", "Previews return 503", "--kind", "incident", "--json")
	if *exited {
		t.Fatalf("unexpected failure: %s", stderr)
	}
	result := envelopeResult(t, stdout)
	if created, _ := result["created"].(bool); !created {
		t.Fatalf("first upsert must create:\n%s", stdout)
	}

	stdout, stderr = runProject(t, cfg, "tasks", "upsert", "--jira", "CDBS-10336", "--state", "review", "--json")
	if *exited {
		t.Fatalf("unexpected failure: %s", stderr)
	}
	result = envelopeResult(t, stdout)
	if created, _ := result["created"].(bool); created {
		t.Fatalf("second upsert must update, not create:\n%s", stdout)
	}
	task, _ := result["task"].(map[string]any)
	if task["state"] != "review" || task["title"] != "Previews return 503" {
		t.Fatalf("task = %#v", task)
	}
}

func TestCmdProjectTasksListFiltersAndRejectsBadState(t *testing.T) {
	cfg := testConfig(t)
	seedCard(t, cfg, "nextcloud")
	seedTask(t, cfg, "nextcloud", "CDBS-1", "Open incident", "incident")
	done := seedTask(t, cfg, "nextcloud", "CDBS-2", "Closed bug", "bugfix")
	s := openTestStore(t, cfg)
	doneState := "done"
	if _, err := s.UpsertTask(store.UpsertTaskParams{Project: "nextcloud", SyncID: &done.SyncID, State: &doneState}); err != nil {
		t.Fatalf("close task: %v", err)
	}
	t.Setenv("ENGRAM_PROJECT", "nextcloud")
	exited := stubExit(t)

	stdout, stderr := runProject(t, cfg, "tasks", "list", "--json")
	if *exited {
		t.Fatalf("unexpected failure: %s", stderr)
	}
	result := envelopeResult(t, stdout)
	items, _ := result["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("default --state active must hide done tasks, got %d items", len(items))
	}

	stdout, _ = runProject(t, cfg, "tasks", "list", "--state", "done", "--json")
	result = envelopeResult(t, stdout)
	items, _ = result["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("--state done must return the closed task, got %d items", len(items))
	}

	stdout, _ = runProject(t, cfg, "tasks", "list", "--state", "wip", "--json")
	if !*exited {
		t.Fatal("an invalid state must exit non-zero")
	}
	if code := decodeErrorCode(t, stdout); code != "invalid_enum" {
		t.Fatalf("code = %q, want invalid_enum", code)
	}
}

func TestCmdProjectTasksListHumanTableHasHeaders(t *testing.T) {
	cfg := testConfig(t)
	seedCard(t, cfg, "nextcloud")
	seedTask(t, cfg, "nextcloud", "CDBS-10336", "Previews return 503", "incident")
	t.Setenv("ENGRAM_PROJECT", "nextcloud")
	exited := stubExit(t)

	stdout, stderr := runProject(t, cfg, "tasks", "list")
	if *exited {
		t.Fatalf("unexpected failure: %s", stderr)
	}
	for _, want := range []string{"ID", "KEY", "KIND", "STATE", "STALE", "OBS", "EVD", "TITLE", "CDBS-10336"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("table is missing %q:\n%s", want, stdout)
		}
	}
}

func TestCmdProjectTasksLink(t *testing.T) {
	cfg := testConfig(t)
	seedCard(t, cfg, "nextcloud")
	seedTask(t, cfg, "nextcloud", "CDBS-10336", "Previews return 503", "incident")
	obsID := mustSeedObservation(t, cfg, "s-link", "nextcloud", "discovery", "Root cause", "GCS object store", "project")
	t.Setenv("ENGRAM_PROJECT", "nextcloud")
	exited := stubExit(t)

	stdout, stderr := runProject(t, cfg, "tasks", "link", "CDBS-10336",
		"--observation", itoa(obsID), "--role", "root_cause", "--json")
	if *exited {
		t.Fatalf("unexpected failure: %s", stderr)
	}
	result := envelopeResult(t, stdout)
	if linked, _ := result["linked"].(bool); !linked {
		t.Fatalf("link did not report linked:\n%s", stdout)
	}
	if result["role"] != "root_cause" {
		t.Fatalf("role = %v", result["role"])
	}
}

func TestCmdProjectTasksLinkGraphRefRequiresCommit(t *testing.T) {
	cfg := testConfig(t)
	seedCard(t, cfg, "nextcloud")
	seedTask(t, cfg, "nextcloud", "CDBS-10336", "Previews return 503", "incident")
	obsID := mustSeedObservation(t, cfg, "s-link2", "nextcloud", "discovery", "t", "c", "project")
	t.Setenv("ENGRAM_PROJECT", "nextcloud")
	exited := stubExit(t)

	stdout, _ := runProject(t, cfg, "tasks", "link", "CDBS-10336",
		"--observation", itoa(obsID), "--graph-ref", "OCP\\IConfig", "--json")
	if !*exited {
		t.Fatal("--graph-ref without --graph-commit must be refused")
	}
	if code := decodeErrorCode(t, stdout); code != "graph_commit_required" {
		t.Fatalf("code = %q, want graph_commit_required", code)
	}
}

func TestCmdProjectTasksLinkUnknownTargets(t *testing.T) {
	cfg := testConfig(t)
	seedCard(t, cfg, "nextcloud")
	seedTask(t, cfg, "nextcloud", "CDBS-10336", "T", "incident")
	t.Setenv("ENGRAM_PROJECT", "nextcloud")
	exited := stubExit(t)

	stdout, _ := runProject(t, cfg, "tasks", "link", "CDBS-99999", "--observation", "1", "--json")
	if !*exited || decodeErrorCode(t, stdout) != "unknown_task" {
		t.Fatalf("want unknown_task, got:\n%s", stdout)
	}

	stdout, _ = runProject(t, cfg, "tasks", "link", "CDBS-10336", "--observation", "obs-deadbeefdeadbeef", "--json")
	if code := decodeErrorCode(t, stdout); code != "unknown_observation" {
		t.Fatalf("code = %q, want unknown_observation", code)
	}
}

// ─── evidence ────────────────────────────────────────────────────────────────

func TestCmdProjectEvidenceAddDerivesFromFile(t *testing.T) {
	cfg := testConfig(t)
	seedCard(t, cfg, "nextcloud")
	seedTask(t, cfg, "nextcloud", "CDBS-10336", "Previews return 503", "incident")

	evidenceDir := t.TempDir()
	t.Setenv("CD_EVIDENCE_DIR", evidenceDir)
	capture := filepath.Join(evidenceDir, "nextcloud", "CDBS-10336", "01-preview-200.png")
	if err := os.MkdirAll(filepath.Dir(capture), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(capture, []byte("synthetic png bytes"), 0o644); err != nil {
		t.Fatalf("write capture: %v", err)
	}
	t.Setenv("ENGRAM_PROJECT", "nextcloud")
	exited := stubExit(t)

	stdout, stderr := runProject(t, cfg, "evidence", "add", "CDBS-10336",
		"--file", capture, "--kind", "png", "--proves", "preview returns 200 after the fix", "--json")
	if *exited {
		t.Fatalf("unexpected failure: %s", stderr)
	}
	result := envelopeResult(t, stdout)
	evidence, _ := result["evidence"].(map[string]any)
	if evidence["path"] != "nextcloud/CDBS-10336/01-preview-200.png" {
		t.Fatalf("path = %v, want the evidence-dir-relative path", evidence["path"])
	}
	if sha, _ := evidence["sha256"].(string); !projIsSHA256(sha) {
		t.Fatalf("sha256 = %v, want 64 lowercase hex chars", evidence["sha256"])
	}
	if size, _ := evidence["size_bytes"].(float64); int64(size) != int64(len("synthetic png bytes")) {
		t.Fatalf("size_bytes = %v", evidence["size_bytes"])
	}
}

func TestCmdProjectEvidenceAddRejectsFileOutsideEvidenceDir(t *testing.T) {
	cfg := testConfig(t)
	seedCard(t, cfg, "nextcloud")
	seedTask(t, cfg, "nextcloud", "CDBS-10336", "T", "incident")
	t.Setenv("CD_EVIDENCE_DIR", t.TempDir())

	outside := filepath.Join(t.TempDir(), "elsewhere.png")
	if err := os.WriteFile(outside, []byte("synthetic"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Setenv("ENGRAM_PROJECT", "nextcloud")
	exited := stubExit(t)

	stdout, _ := runProject(t, cfg, "evidence", "add", "CDBS-10336",
		"--file", outside, "--kind", "png", "--proves", "x", "--json")
	if !*exited {
		t.Fatal("a file outside the evidence directory must be refused")
	}
	if code := decodeErrorCode(t, stdout); code != "invalid_file" {
		t.Fatalf("code = %q, want invalid_file", code)
	}
	// The canary: nothing may have been registered.
	s := openTestStore(t, cfg)
	_, total, _, err := s.ListEvidence("nextcloud", store.EvidenceListFilter{Limit: 10})
	if err != nil {
		t.Fatalf("ListEvidence: %v", err)
	}
	if total != 0 {
		t.Fatalf("a refused capture was registered anyway (total=%d)", total)
	}
}

func TestCmdProjectEvidenceAddValidation(t *testing.T) {
	good := strings.Repeat("ab12", 16)
	tests := []struct {
		name     string
		args     []string
		wantCode string
	}{
		{name: "no task", args: []string{"evidence", "add", "--path", "a.png", "--sha256", good, "--kind", "png", "--proves", "x", "--json"}, wantCode: "missing_field"},
		{name: "no proves", args: []string{"evidence", "add", "CDBS-1", "--path", "a.png", "--sha256", good, "--kind", "png", "--json"}, wantCode: "missing_field"},
		{name: "bad sha", args: []string{"evidence", "add", "CDBS-1", "--path", "a.png", "--sha256", "notahash", "--kind", "png", "--proves", "x", "--json"}, wantCode: "invalid_sha256"},
		{name: "bad kind", args: []string{"evidence", "add", "CDBS-1", "--path", "a.png", "--sha256", good, "--kind", "pdf", "--proves", "x", "--json"}, wantCode: "invalid_enum"},
		{name: "absolute path", args: []string{"evidence", "add", "CDBS-1", "--path", "/etc/passwd", "--sha256", good, "--kind", "png", "--proves", "x", "--json"}, wantCode: "absolute_path_rejected"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testConfig(t)
			seedCard(t, cfg, "nextcloud")
			seedTask(t, cfg, "nextcloud", "CDBS-1", "T", "incident")
			t.Setenv("ENGRAM_PROJECT", "nextcloud")
			exited := stubExit(t)

			stdout, _ := runProject(t, cfg, tc.args...)
			if !*exited {
				t.Fatalf("expected a failure, stdout=%s", stdout)
			}
			if code := decodeErrorCode(t, stdout); code != tc.wantCode {
				t.Fatalf("code = %q, want %q", code, tc.wantCode)
			}
		})
	}
}

func TestCmdProjectEvidenceListFiltersByKind(t *testing.T) {
	cfg := testConfig(t)
	seedCard(t, cfg, "nextcloud")
	task := seedTask(t, cfg, "nextcloud", "CDBS-10336", "T", "incident")
	s := openTestStore(t, cfg)
	for i, kind := range []string{"png", "log"} {
		sha := strings.Repeat("0123456789abcdef", 4)[:63] + itoa(int64(i))
		if _, _, _, err := s.AddEvidence(store.AddEvidenceParams{
			Task: task, Path: "nextcloud/CDBS-10336/f" + itoa(int64(i)), SHA256: sha,
			Kind: kind, Proves: "synthetic",
		}); err != nil {
			t.Fatalf("AddEvidence: %v", err)
		}
	}
	t.Setenv("ENGRAM_PROJECT", "nextcloud")
	exited := stubExit(t)

	stdout, stderr := runProject(t, cfg, "evidence", "list", "--kind", "png", "--json")
	if *exited {
		t.Fatalf("unexpected failure: %s", stderr)
	}
	result := envelopeResult(t, stdout)
	items, _ := result["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("--kind png must return 1 item, got %d", len(items))
	}

	stdout, _ = runProject(t, cfg, "evidence", "list", "--json")
	result = envelopeResult(t, stdout)
	items, _ = result["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("an unfiltered list must return both items, got %d", len(items))
	}
}

// ─── runbooks ────────────────────────────────────────────────────────────────

const testRunbookNote = `---
type: runbook
id: RB-003
title: "Preview endpoint slow, returning 503"
service: nextcloud
severity: P2
category: performance
status: verified
symptoms:
  - "/core/preview returns HTTP 503"
tags:
  - type/runbook
last_updated: 2026-05-15
---

# RB-003
`

const testForeignRunbookNote = `---
type: runbook
id: RB-004
title: "Auth middleware rejects a valid token"
service: middleware
severity: P1
category: auth
status: verified
symptoms:
  - "401 on a valid session"
tags:
  - type/runbook
last_updated: 2026-08-01
---

# RB-004
`

func writeTestVault(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return root
}

func TestCmdProjectRunbooksSyncRequiresExactlyOneSource(t *testing.T) {
	cfg := testConfig(t)
	seedCard(t, cfg, "nextcloud")
	t.Setenv("ENGRAM_PROJECT", "nextcloud")

	for _, args := range [][]string{
		{"runbooks", "sync", "--json"},
		{"runbooks", "sync", "--vault-dir", t.TempDir(), "--entries-file", "x.json", "--json"},
	} {
		exited := stubExit(t)
		stdout, _ := runProject(t, cfg, args...)
		if !*exited {
			t.Fatalf("args %v must be refused", args)
		}
		if code := decodeErrorCode(t, stdout); code != "missing_field" {
			t.Fatalf("code = %q, want missing_field", code)
		}
	}
}

func TestCmdProjectRunbooksSyncFromVaultIndexesOnlyItsProject(t *testing.T) {
	cfg := testConfig(t)
	seedCard(t, cfg, "nextcloud")
	vault := writeTestVault(t, map[string]string{
		"Runbooks/Performance/RB-003 Preview.md": testRunbookNote,
		"Runbooks/Auth/RB-004 Token.md":          testForeignRunbookNote,
	})
	t.Setenv("ENGRAM_PROJECT", "nextcloud")
	exited := stubExit(t)

	stdout, stderr := runProject(t, cfg, "runbooks", "sync", "--vault-dir", vault, "--json")
	if *exited {
		t.Fatalf("unexpected failure: %s", stderr)
	}
	result := envelopeResult(t, stdout)
	if result["source"] != "vault-fs" {
		t.Fatalf("source = %v, want vault-fs", result["source"])
	}
	sync, _ := result["sync"].(map[string]any)
	if upserted, _ := sync["upserted"].(float64); upserted != 1 {
		t.Fatalf("upserted = %v, want 1 (middleware's runbook must not be indexed here)", sync["upserted"])
	}
	skipped, _ := sync["skipped"].([]any)
	foundOther := false
	for _, sk := range skipped {
		row, _ := sk.(map[string]any)
		if row["reason"] == "other_project" && row["id"] == "RB-004" {
			foundOther = true
		}
	}
	if !foundOther {
		t.Fatalf("RB-004 must be reported as other_project, skipped=%#v", skipped)
	}

	// The canary: the store must hold only the nextcloud row.
	s := openTestStore(t, cfg)
	rows, total, err := s.ListRunbookIndex("middleware", store.RunbookListFilter{Limit: 10})
	if err != nil {
		t.Fatalf("ListRunbookIndex: %v", err)
	}
	if total != 0 {
		t.Fatalf("middleware runbooks were indexed by a nextcloud-scoped sync: %#v", rows)
	}
}

func TestCmdProjectRunbooksSyncFromEntriesFile(t *testing.T) {
	cfg := testConfig(t)
	seedCard(t, cfg, "nextcloud")
	file := filepath.Join(t.TempDir(), "entries.json")
	payload := `{"entries":[{"id":"RB-003","vault_path":"Runbooks/Performance/RB-003 Preview.md",` +
		`"title":"Preview endpoint slow","service":"nextcloud","category":"performance","status":"verified",` +
		`"symptoms":["/core/preview returns HTTP 503"],"needs_review":true}]}`
	if err := os.WriteFile(file, []byte(payload), 0o644); err != nil {
		t.Fatalf("write entries: %v", err)
	}
	t.Setenv("ENGRAM_PROJECT", "nextcloud")
	exited := stubExit(t)

	stdout, stderr := runProject(t, cfg, "runbooks", "sync", "--entries-file", file, "--json")
	if *exited {
		t.Fatalf("unexpected failure: %s", stderr)
	}
	result := envelopeResult(t, stdout)
	if result["source"] != "knowledge-mcp" {
		t.Fatalf("source = %v, want knowledge-mcp", result["source"])
	}
	sync, _ := result["sync"].(map[string]any)
	if upserted, _ := sync["upserted"].(float64); upserted != 1 {
		t.Fatalf("upserted = %v, want 1", sync["upserted"])
	}
	if stale, _ := sync["stale_count"].(float64); stale != 1 {
		t.Fatalf("stale_count = %v, want 1 (needs_review drives staleness for knowledge-mcp)", sync["stale_count"])
	}
}

func TestCmdProjectRunbooksSyncRejectsMalformedEntriesFile(t *testing.T) {
	cfg := testConfig(t)
	seedCard(t, cfg, "nextcloud")
	file := filepath.Join(t.TempDir(), "entries.json")
	if err := os.WriteFile(file, []byte(`{"entries":[{"id":"RB-003"}]}`), 0o644); err != nil {
		t.Fatalf("write entries: %v", err)
	}
	t.Setenv("ENGRAM_PROJECT", "nextcloud")
	exited := stubExit(t)

	stdout, _ := runProject(t, cfg, "runbooks", "sync", "--entries-file", file, "--json")
	if !*exited {
		t.Fatal("an entry missing required fields must be refused")
	}
	if code := decodeErrorCode(t, stdout); code != "entries_rejected" {
		t.Fatalf("code = %q, want entries_rejected", code)
	}
}

func TestCmdProjectRunbooksSyncRejectsVaultWithoutRunbooksFolder(t *testing.T) {
	cfg := testConfig(t)
	seedCard(t, cfg, "nextcloud")
	t.Setenv("ENGRAM_PROJECT", "nextcloud")
	exited := stubExit(t)

	stdout, _ := runProject(t, cfg, "runbooks", "sync", "--vault-dir", t.TempDir(), "--json")
	if !*exited {
		t.Fatal("a vault without Runbooks/ must be refused")
	}
	if code := decodeErrorCode(t, stdout); code != "vault_dir_not_found" {
		t.Fatalf("code = %q, want vault_dir_not_found", code)
	}
}

func TestCmdProjectRunbooksFind(t *testing.T) {
	cfg := testConfig(t)
	seedCard(t, cfg, "nextcloud")
	vault := writeTestVault(t, map[string]string{"Runbooks/Performance/RB-003 Preview.md": testRunbookNote})
	t.Setenv("ENGRAM_PROJECT", "nextcloud")
	exited := stubExit(t)

	if _, stderr := runProject(t, cfg, "runbooks", "sync", "--vault-dir", vault, "--json"); *exited {
		t.Fatalf("seed sync failed: %s", stderr)
	}

	stdout, stderr := runProject(t, cfg, "runbooks", "find", "preview 503", "--json")
	if *exited {
		t.Fatalf("unexpected failure: %s", stderr)
	}
	result := envelopeResult(t, stdout)
	items, _ := result["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("want 1 hit, got %d:\n%s", len(items), stdout)
	}
	hit, _ := items[0].(map[string]any)
	if hit["id"] != "RB-003" {
		t.Fatalf("id = %v, want RB-003", hit["id"])
	}

	// Human rendering carries the vault path on its own line.
	human, _ := runProject(t, cfg, "runbooks", "find", "preview 503")
	if !strings.Contains(human, "RB-003") || !strings.Contains(human, "Runbooks/Performance/RB-003 Preview.md") {
		t.Fatalf("human output is missing the id or the vault path:\n%s", human)
	}
}

func TestCmdProjectRunbooksFindValidation(t *testing.T) {
	cfg := testConfig(t)
	seedCard(t, cfg, "nextcloud")
	t.Setenv("ENGRAM_PROJECT", "nextcloud")

	for _, tc := range []struct {
		args     []string
		wantCode string
	}{
		{args: []string{"runbooks", "find", "--json"}, wantCode: "missing_field"},
		{args: []string{"runbooks", "find", "ab", "--json"}, wantCode: "missing_field"},
		{args: []string{"runbooks", "find", "preview", "--category", "cooking", "--json"}, wantCode: "invalid_enum"},
		{args: []string{"runbooks", "find", "preview", "--match-mode", "some", "--json"}, wantCode: "invalid_enum"},
	} {
		exited := stubExit(t)
		stdout, _ := runProject(t, cfg, tc.args...)
		if !*exited {
			t.Fatalf("args %v must be refused", tc.args)
		}
		if code := decodeErrorCode(t, stdout); code != tc.wantCode {
			t.Fatalf("args %v: code = %q, want %q", tc.args, code, tc.wantCode)
		}
	}
}

// ─── context ─────────────────────────────────────────────────────────────────

func TestCmdProjectContextRendersMarkdown(t *testing.T) {
	cfg := testConfig(t)
	seedCard(t, cfg, "nextcloud")
	seedTask(t, cfg, "nextcloud", "CDBS-10336", "Previews return 503", "incident")
	t.Setenv("ENGRAM_PROJECT", "nextcloud")
	exited := stubExit(t)

	stdout, stderr := runProject(t, cfg, "context", "CDBS-10336")
	if *exited {
		t.Fatalf("unexpected failure: %s", stderr)
	}
	if !strings.Contains(stdout, "CDBS-10336") {
		t.Fatalf("markdown pack is missing the ticket key:\n%s", stdout)
	}
}

func TestCmdProjectContextJSONEnvelope(t *testing.T) {
	cfg := testConfig(t)
	seedCard(t, cfg, "nextcloud")
	seedTask(t, cfg, "nextcloud", "CDBS-10336", "Previews return 503", "incident")
	t.Setenv("ENGRAM_PROJECT", "nextcloud")
	exited := stubExit(t)

	stdout, stderr := runProject(t, cfg, "context", "CDBS-10336", "--format", "json")
	if *exited {
		t.Fatalf("unexpected failure: %s", stderr)
	}
	result := envelopeResult(t, stdout)
	if _, ok := result["chars"]; !ok {
		t.Fatalf("json pack is missing chars:\n%s", stdout)
	}
}

func TestCmdProjectContextUnknownTask(t *testing.T) {
	cfg := testConfig(t)
	seedCard(t, cfg, "nextcloud")
	t.Setenv("ENGRAM_PROJECT", "nextcloud")
	exited := stubExit(t)

	stdout, _ := runProject(t, cfg, "context", "CDBS-99999", "--json")
	if !*exited {
		t.Fatal("an unknown task must exit non-zero")
	}
	if code := decodeErrorCode(t, stdout); code != "unknown_task" {
		t.Fatalf("code = %q, want unknown_task", code)
	}
}

func TestCmdProjectContextInvalidSection(t *testing.T) {
	cfg := testConfig(t)
	seedCard(t, cfg, "nextcloud")
	seedTask(t, cfg, "nextcloud", "CDBS-10336", "T", "incident")
	t.Setenv("ENGRAM_PROJECT", "nextcloud")
	exited := stubExit(t)

	stdout, _ := runProject(t, cfg, "context", "CDBS-10336", "--sections", "header,bogus", "--json")
	if !*exited {
		t.Fatal("an invalid section must exit non-zero")
	}
	if code := decodeErrorCode(t, stdout); code != "invalid_enum" {
		t.Fatalf("code = %q, want invalid_enum", code)
	}
}

// nopWriteCloser adapts a buffer so the OSC 52 test can inspect what would
// have been written to the terminal.
type nopWriteCloser struct{ buf *bytes.Buffer }

func (n nopWriteCloser) Write(p []byte) (int, error) { return n.buf.Write(p) }
func (n nopWriteCloser) Close() error                { return nil }

func TestCmdProjectContextCopyWritesOSC52ToTheTerminalOnly(t *testing.T) {
	cfg := testConfig(t)
	seedCard(t, cfg, "nextcloud")
	seedTask(t, cfg, "nextcloud", "CDBS-10336", "Previews return 503", "incident")
	t.Setenv("ENGRAM_PROJECT", "nextcloud")
	exited := stubExit(t)

	var tty bytes.Buffer
	old := projTTYWriter
	projTTYWriter = func() (io.WriteCloser, bool) { return nopWriteCloser{buf: &tty}, true }
	t.Cleanup(func() { projTTYWriter = old })

	stdout, stderr := runProject(t, cfg, "context", "CDBS-10336", "--copy")
	if *exited {
		t.Fatalf("unexpected failure: %s", stderr)
	}
	if !strings.HasPrefix(tty.String(), "\x1b]52;c;") {
		t.Fatalf("no OSC 52 sequence reached the terminal: %q", tty.String())
	}
	// The canary: the escape must never contaminate the piped pack.
	if strings.Contains(stdout, "\x1b]52") {
		t.Fatalf("the OSC 52 escape leaked into stdout:\n%q", stdout)
	}
	if !strings.Contains(stdout, "CDBS-10336") {
		t.Fatalf("--copy must still print the pack:\n%s", stdout)
	}
	if !strings.Contains(stderr, "copied to clipboard") {
		t.Fatalf("stderr is missing the confirmation: %q", stderr)
	}
}

// ─── graph sync ──────────────────────────────────────────────────────────────

func TestCmdProjectGraphSyncStampsTheCard(t *testing.T) {
	cfg := testConfig(t)
	seedCard(t, cfg, "nextcloud")

	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "graphify-out"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	commit := strings.Repeat("a1b2c3d4", 5) // synthetic 40-hex commit
	graph := `{"built_at_commit":"` + commit + `","directed":true,"nodes":[` +
		`{"id":"n1","label":"OCP\\IConfig","file_type":"code","source_file":"lib/IConfig.php","community":0},` +
		`{"id":"n2","label":"Wrapper","file_type":"code","source_file":"lib/Wrapper.php","community":0}],` +
		`"links":[{"source":"n1","target":"n2","relation":"calls","confidence":"EXTRACTED","weight":1}]}`
	if err := os.WriteFile(filepath.Join(repo, "graphify-out", "graph.json"), []byte(graph), 0o644); err != nil {
		t.Fatalf("write graph.json: %v", err)
	}
	t.Setenv("ENGRAM_PROJECT", "nextcloud")
	exited := stubExit(t)

	stdout, stderr := runProject(t, cfg, "graph", "sync", "--repo-dir", repo, "--json")
	if *exited {
		t.Fatalf("unexpected failure: %s\n%s", stderr, stdout)
	}
	result := envelopeResult(t, stdout)
	graphResult, _ := result["graph"].(map[string]any)
	if graphResult["graph_commit"] != commit {
		t.Fatalf("graph_commit = %v, want %s", graphResult["graph_commit"], commit)
	}
	if n, _ := graphResult["node_count"].(float64); int(n) != 2 {
		t.Fatalf("node_count = %v, want 2", graphResult["node_count"])
	}

	s := openTestStore(t, cfg)
	card, err := s.GetProjectCard("nextcloud")
	if err != nil {
		t.Fatalf("GetProjectCard: %v", err)
	}
	if card.GraphCommit == nil || *card.GraphCommit != commit {
		t.Fatalf("the card was not stamped: %v", card.GraphCommit)
	}
	if card.GraphSummary == nil {
		t.Fatal("graph_summary was not persisted alongside graph_commit")
	}
}

func TestCmdProjectGraphSyncMissingGraphLeavesCardUntouched(t *testing.T) {
	cfg := testConfig(t)
	seedCard(t, cfg, "nextcloud")
	t.Setenv("ENGRAM_PROJECT", "nextcloud")
	exited := stubExit(t)

	stdout, _ := runProject(t, cfg, "graph", "sync", "--repo-dir", t.TempDir(), "--json")
	if !*exited {
		t.Fatal("a missing graph.json must exit non-zero")
	}
	if code := decodeErrorCode(t, stdout); code != "graph_not_found" {
		t.Fatalf("code = %q, want graph_not_found", code)
	}
	s := openTestStore(t, cfg)
	card, err := s.GetProjectCard("nextcloud")
	if err != nil {
		t.Fatalf("GetProjectCard: %v", err)
	}
	if card.GraphCommit != nil {
		t.Fatalf("a failed sync stamped the card anyway: %v", *card.GraphCommit)
	}
}

func TestCmdProjectGraphRequiresSyncSubcommand(t *testing.T) {
	cfg := testConfig(t)
	seedCard(t, cfg, "nextcloud")
	t.Setenv("ENGRAM_PROJECT", "nextcloud")
	exited := stubExit(t)

	_, stderr := runProject(t, cfg, "graph", "rebuild")
	if !*exited {
		t.Fatal("`graph rebuild` must exit non-zero")
	}
	if !strings.Contains(stderr, "graph sync") {
		t.Fatalf("stderr = %q", stderr)
	}
}

// itoa keeps the test table readable without importing strconv everywhere.
func itoa(v int64) string {
	return jsonNumber(v)
}

func jsonNumber(v int64) string {
	b, _ := json.Marshal(v)
	return string(b)
}
