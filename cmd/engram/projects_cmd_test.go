package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/store"
	_ "modernc.org/sqlite"
)

// runProjects invokes `engram projects …` with args and captures both streams.
func runProjects(t *testing.T, cfg store.Config, args ...string) (stdout, stderr string) {
	t.Helper()
	withArgs(t, append([]string{"engram", "projects"}, args...)...)
	return captureOutput(t, func() { cmdProjects(cfg) })
}

// seedProjectMemory writes one session and one memory under project, so the
// store counts the name as a real project rather than a typo.
func seedProjectMemory(t *testing.T, cfg store.Config, project, sessionID, title string) {
	t.Helper()
	s := openTestStore(t, cfg)
	if err := s.CreateSession(sessionID, project, "/work/"+sessionID); err != nil {
		t.Fatalf("CreateSession(%s): %v", project, err)
	}
	if _, err := s.AddObservation(store.AddObservationParams{
		SessionID: sessionID,
		Type:      "decision",
		Title:     title,
		Content:   title,
		Project:   project,
		Scope:     "project",
	}); err != nil {
		t.Fatalf("AddObservation(%s): %v", project, err)
	}
}

// TestProjectsMergeMovesRowsIntoTheTarget pins the command the alias refusal
// and the CLI reference both point at: it exists, it moves rows, and the
// source name stops answering afterwards.
func TestProjectsMergeMovesRowsIntoTheTarget(t *testing.T) {
	cfg := testConfig(t)
	stubExit(t)
	seedProjectMemory(t, cfg, "koi_garden", "s1", "the old name")
	seedProjectMemory(t, cfg, "koi-garden", "s2", "the new name")

	stdout, _ := runProjects(t, cfg, "merge", "koi_garden", "koi-garden")
	if !strings.Contains(stdout, "observations") {
		t.Fatalf("merge reported no table summary:\n%s", stdout)
	}

	s := openTestStore(t, cfg)
	projects, err := s.ListProjectsWithStats()
	if err != nil {
		t.Fatalf("ListProjectsWithStats: %v", err)
	}
	counts := map[string]int{}
	for _, p := range projects {
		counts[p.Name] = p.ObservationCount
	}
	if counts["koi_garden"] != 0 {
		t.Errorf("koi_garden still holds %d observation(s) after the merge", counts["koi_garden"])
	}
	if counts["koi-garden"] != 2 {
		t.Errorf("koi-garden holds %d observation(s), want 2", counts["koi-garden"])
	}
}

// TestProjectsMergeJSONCarriesTheMergeResult pins the --json contract: the
// envelope every other subcommand prints, with the full MergeResult as its
// result, so a rollout script branches on numbers instead of parsing prose.
func TestProjectsMergeJSONCarriesTheMergeResult(t *testing.T) {
	cfg := testConfig(t)
	stubExit(t)
	seedProjectMemory(t, cfg, "koi_garden", "s1", "the old name")
	seedProjectMemory(t, cfg, "koi-garden", "s2", "the new name")

	stdout, _ := runProjects(t, cfg, "merge", "koi_garden", "koi-garden", "--json")
	envelope := decodeEnvelope(t, stdout)
	if envelope["project"] != "koi-garden" {
		t.Fatalf("envelope project = %v, want koi-garden", envelope["project"])
	}
	result, ok := envelope["result"].(map[string]any)
	if !ok {
		t.Fatalf("envelope has no object result:\n%s", stdout)
	}
	if result["canonical"] != "koi-garden" {
		t.Errorf("canonical = %v, want koi-garden", result["canonical"])
	}
	moved, ok := result["table_rows_moved"].(map[string]any)
	if !ok {
		t.Fatalf("result carries no table_rows_moved:\n%s", stdout)
	}
	if moved["observations"] != float64(1) {
		t.Errorf("observations moved = %v, want 1", moved["observations"])
	}
	var merged []string
	raw, _ := json.Marshal(result["sources_merged"])
	_ = json.Unmarshal(raw, &merged)
	if len(merged) != 1 || merged[0] != "koi_garden" {
		t.Errorf("sources_merged = %v, want [koi_garden]", merged)
	}
}

// TestProjectsMergeRefusesWhatWouldMoveNothing pins the exit codes a rollout
// script depends on: a missing operand, a source that does not exist and a
// source that is already the target are errors, not empty successes.
func TestProjectsMergeRefusesWhatWouldMoveNothing(t *testing.T) {
	cases := []struct {
		name string
		args []string
		code string
	}{
		{"missing operand", []string{"merge", "koi-garden"}, "missing_field"},
		{"unknown source", []string{"merge", "no-such-project", "koi-garden"}, "unknown_project"},
		{"source is the target", []string{"merge", "koi-garden", "koi-garden"}, "merge_into_self"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testConfig(t)
			exited := stubExit(t)
			seedProjectMemory(t, cfg, "koi-garden", "s1", "the only project")

			stdout, _ := runProjects(t, cfg, append(tc.args, "--json")...)
			if code := decodeErrorCode(t, stdout); code != tc.code {
				t.Fatalf("code = %q, want %q", code, tc.code)
			}
			if !*exited {
				t.Error("the command exited 0 on an error")
			}
		})
	}
}

// TestProjectsListJSONHonoursTheFlag pins that --json is read rather than
// ignored, and that a card's identity travels beside the counts.
func TestProjectsListJSONHonoursTheFlag(t *testing.T) {
	cfg := testConfig(t)
	stubExit(t)
	seedProjectMemory(t, cfg, "koi-garden", "s1", "a memory")
	s := openTestStore(t, cfg)
	display := "Koi Garden"
	kind := "repo"
	if _, _, err := s.UpsertProjectCard(store.UpsertProjectCardParams{
		Slug: "koi-garden", DisplayName: &display, Kind: &kind,
	}); err != nil {
		t.Fatalf("UpsertProjectCard: %v", err)
	}

	stdout, _ := runProjects(t, cfg, "list", "--json")
	var rows []map[string]any
	if err := json.Unmarshal([]byte(stdout), &rows); err != nil {
		t.Fatalf("stdout is not a JSON array: %v\n%s", err, stdout)
	}
	if len(rows) != 1 {
		t.Fatalf("listed %d project(s), want 1:\n%s", len(rows), stdout)
	}
	row := rows[0]
	if row["name"] != "koi-garden" || row["slug"] != "koi-garden" {
		t.Errorf("name/slug = %v/%v, want koi-garden", row["name"], row["slug"])
	}
	if row["display_name"] != display {
		t.Errorf("display_name = %v, want %q", row["display_name"], display)
	}
	if row["kind"] != kind {
		t.Errorf("kind = %v, want %q", row["kind"], kind)
	}
	counts, ok := row["counts"].(map[string]any)
	if !ok {
		t.Fatalf("row carries no counts:\n%s", stdout)
	}
	if counts["observations"] != float64(1) {
		t.Errorf("observations = %v, want 1", counts["observations"])
	}
	if counts["sessions"] != float64(1) {
		t.Errorf("sessions = %v, want 1", counts["sessions"])
	}
}

// TestProjectsListJSONOmitsCardFieldsWithoutACard pins that a project with no
// card still lists, with its counts and without inventing an identity for it.
func TestProjectsListJSONOmitsCardFieldsWithoutACard(t *testing.T) {
	cfg := testConfig(t)
	stubExit(t)
	seedProjectMemory(t, cfg, "koi-garden", "s1", "a memory")

	stdout, _ := runProjects(t, cfg, "list", "--json")
	var rows []map[string]any
	if err := json.Unmarshal([]byte(stdout), &rows); err != nil {
		t.Fatalf("stdout is not a JSON array: %v\n%s", err, stdout)
	}
	if len(rows) != 1 {
		t.Fatalf("listed %d project(s), want 1:\n%s", len(rows), stdout)
	}
	if _, ok := rows[0]["slug"]; ok {
		t.Errorf("a project without a card reported a slug:\n%s", stdout)
	}
	if _, ok := rows[0]["counts"]; !ok {
		t.Errorf("a project without a card lost its counts:\n%s", stdout)
	}
}

// TestProjectsListJSONOnAnEmptyStoreIsAnEmptyArray pins that a script never
// has to branch on prose: no projects is `[]`, not "No projects found."
func TestProjectsListJSONOnAnEmptyStoreIsAnEmptyArray(t *testing.T) {
	cfg := testConfig(t)
	stubExit(t)

	stdout, _ := runProjects(t, cfg, "list", "--json")
	if strings.TrimSpace(stdout) != "[]" {
		t.Fatalf("stdout = %q, want []", strings.TrimSpace(stdout))
	}
}
