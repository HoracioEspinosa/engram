package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/vault"
)

// Actions an import takes on a task.
const (
	// ActionCreate registers a task the store had never heard of.
	ActionCreate = "create"
	// ActionUpdate changes something about a task that already exists.
	ActionUpdate = "update"
	// ActionSkip means the vault and the store already agree.
	ActionSkip = "skip"
)

// archTaskDir is the one underscore-prefixed folder an import can be asked to
// read. It holds architecture notes rather than a ticket, so it is imported as
// a spike or not at all.
const archTaskDir = "_arquitectura"

// TaskAction is one task's line of the import plan.
type TaskAction struct {
	Action  string `json:"action"`
	Project string `json:"project"`
	Slug    string `json:"slug"`
	JiraKey string `json:"jira_key,omitempty"`
	// State is the state the vault says the task is in, empty when the vault
	// names none this package recognises. A state is never guessed: an
	// unrecognised one leaves whatever the store already records untouched.
	State string `json:"state,omitempty"`
	// ClosedAt is the date a "Cerrado (...)" cell carries, verbatim. It is
	// reported so a plan can be read against the table it came from.
	ClosedAt  string `json:"closed_at,omitempty"`
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	VaultPath string `json:"vault_path"`
}

// CountReport is how many rows an import wrote, refreshed and left alone.
type CountReport struct {
	Added   int `json:"added"`
	Updated int `json:"updated"`
	Skipped int `json:"skipped"`
}

// ImportPlan is what an import did, or would do.
type ImportPlan struct {
	Root       string       `json:"root"`
	DryRun     bool         `json:"dry_run"`
	Projects   []string     `json:"projects"`
	Tasks      []TaskAction `json:"tasks"`
	Evidence   CountReport  `json:"evidence"`
	Benchmarks CountReport  `json:"benchmarks"`
	Warnings   []string     `json:"warnings"`
}

// ImportVault reads a whole knowledge tree — <root>/<project>/<task>/ — into
// projects, tasks, evidence and benchmarks.
//
// Everything the vault records about a task that a person may since have
// edited in the store is written once, at creation: the title, the summary,
// the folder and the kind the folder name suggests. Re-running the import
// therefore never undoes a correction. The exception is state, which the vault
// is the authority on and which the caller can still refuse with applyStates.
//
// Folders prefixed with "_" or "." are not tasks; they are quarantine,
// out-of-domain and architecture material. Only _arquitectura can be asked
// for, and it comes in as a spike.
func ImportVault(s *store.Store, root, project string, apply, applyStates, includeArch bool, o vault.Options) (ImportPlan, error) {
	resolved, err := VaultRoot(s, root, project)
	if err != nil {
		return ImportPlan{}, err
	}
	restricted, err := vault.IsRestricted(resolved, restrictedRoots(o))
	if err != nil {
		return ImportPlan{}, err
	}
	if restricted {
		return ImportPlan{}, fmt.Errorf("%w: %s", ErrRestrictedPath, resolved)
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return ImportPlan{}, fmt.Errorf("%w: %s is not a directory", ErrVaultRootUnresolved, resolved)
	}

	plan := ImportPlan{
		Root:     resolved,
		DryRun:   !apply,
		Projects: []string{},
		Tasks:    []TaskAction{},
		Warnings: []string{},
	}

	states, err := readmeStates(resolved, applyStates)
	if err != nil {
		return ImportPlan{}, err
	}
	if applyStates && len(states) == 0 {
		plan.Warnings = append(plan.Warnings, "the vault README maps no task to a state")
	}

	for _, slug := range projectDirs(resolved, project, &plan) {
		scanned, err := vault.ScanProject(resolved, slug, o)
		if err != nil {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("%s: %v", slug, err))
			continue
		}
		plan.Warnings = append(plan.Warnings, scanned.Warnings...)

		tasks := taskDirsOf(resolved, slug, includeArch)
		if len(tasks) == 0 {
			continue
		}
		plan.Projects = append(plan.Projects, slug)
		for _, dirName := range tasks {
			if err := importTask(s, resolved, slug, dirName, states, apply, applyStates, o, &plan); err != nil {
				return ImportPlan{}, err
			}
		}
	}
	return plan, nil
}

// readmeStates reads the vault README's task map into a
// "<project>/<task>" -> state index. It is only required when the caller asked
// for states: a vault whose README carries no map is still importable by its
// folders, and refusing to read it at all would be an opinion about prose.
func readmeStates(root string, required bool) (map[string]vault.ReadmeTask, error) {
	rows, err := vault.ParseProjectReadme(filepath.Join(root, "README.md"))
	if err != nil {
		if required {
			return nil, fmt.Errorf("%w: %s", ErrVaultReadmeUnparsed, err)
		}
		return nil, nil
	}
	index := make(map[string]vault.ReadmeTask, len(rows))
	for _, row := range rows {
		index[row.Path] = row
	}
	return index, nil
}

// projectDirs lists the project folders to walk, narrowed to one when the
// caller named it.
func projectDirs(root, only string, plan *ImportPlan) []string {
	entries, err := os.ReadDir(root)
	if err != nil {
		plan.Warnings = append(plan.Warnings, fmt.Sprintf("read %s: %v", root, err))
		return nil
	}
	var slugs []string
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".") {
			continue
		}
		if strings.TrimSpace(only) != "" && !strings.EqualFold(name, only) {
			continue
		}
		slugs = append(slugs, name)
	}
	sort.Strings(slugs)
	return slugs
}

// taskDirsOf lists a project's task folders. A task folder is one holding a
// README: that is what separates the tasks from the Runbooks/ tree and the
// other shapes a vault root keeps beside them.
func taskDirsOf(root, project string, includeArch bool) []string {
	entries, err := os.ReadDir(filepath.Join(root, project))
	if err != nil {
		return nil
	}
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() {
			continue
		}
		if strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".") {
			if !(includeArch && name == archTaskDir) {
				continue
			}
		}
		if _, err := os.Stat(filepath.Join(root, project, name, "README.md")); err != nil {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// importTask plans, and optionally applies, one task folder.
func importTask(s *store.Store, root, project, dirName string, states map[string]vault.ReadmeTask,
	apply, applyStates bool, o vault.Options, plan *ImportPlan) error {
	scanned, err := vault.ScanTask(root, project, dirName, o)
	if err != nil {
		plan.Warnings = append(plan.Warnings, fmt.Sprintf("%s/%s: %v", project, dirName, err))
		return nil
	}

	vaultPath := project + "/" + dirName
	state, closedAt, pendingNote := stateFor(states, vaultPath, scanned)
	action := TaskAction{
		Project:   project,
		Slug:      scanned.Slug,
		JiraKey:   scanned.JiraKey,
		State:     state,
		ClosedAt:  closedAt,
		Kind:      kindOf(dirName, scanned.Title),
		Title:     scanned.Title,
		VaultPath: vaultPath,
	}

	existing, found, err := existingTask(s, project, scanned.JiraKey, scanned.Slug)
	if err != nil {
		return err
	}
	switch {
	case !found:
		action.Action = ActionCreate
	case applyStates && state != "" && existing.State != state:
		action.Action = ActionUpdate
	default:
		action.Action = ActionSkip
		action.Kind = existing.Kind
	}
	plan.Tasks = append(plan.Tasks, action)

	task := existing
	if apply && action.Action != ActionSkip {
		task, err = writeTask(s, action, existing, found, applyStates, pendingNote)
		if err != nil {
			return err
		}
	}

	// A dry run reports the evidence and the benchmarks through the same
	// walk an apply takes, with the task row it would have written standing
	// in as an empty key. Counting everything readable as new instead — the
	// shortcut a task with no row used to take — skipped the deduplication
	// the apply then performed, so the plan a person reviews overstated the
	// new rows by every file the run itself would fold together.
	taskDir := filepath.Join(root, project, dirName)
	report := ScanReport{Skipped: []Skip{}, BenchmarkCandidates: []string{}}
	if err := recordFiles(s, task, root, taskDir, scanned.Files, !apply, &report); err != nil {
		return err
	}
	plan.Evidence.Added += report.Added
	plan.Evidence.Updated += report.Updated
	plan.Evidence.Skipped += len(report.Skipped)
	for _, skip := range report.Skipped {
		plan.Warnings = append(plan.Warnings,
			fmt.Sprintf("%s/%s/%s: %s", project, dirName, skip.Path, skip.Reason))
	}

	return importRuns(s, task, root, taskDir, scanned.BenchmarkRuns, apply, plan)
}

// writeTask creates or updates the row a planned action describes. The
// descriptive columns are written only on creation; a later import carries the
// state and nothing else.
func writeTask(s *store.Store, action TaskAction, existing store.Task, found, applyStates bool, pendingNote string) (store.Task, error) {
	params := store.UpsertTaskParams{Project: action.Project}
	if action.JiraKey != "" {
		params.JiraKey = &action.JiraKey
	}
	if !found {
		params.Slug = &action.Slug
		params.Title = &action.Title
		params.Kind = &action.Kind
		params.VaultPath = &action.VaultPath
		if action.Title == "" {
			params.Title = &action.Slug
		}
	} else {
		params.SyncID = &existing.SyncID
	}
	if applyStates && action.State != "" {
		params.State = &action.State
		if action.State == vault.StatePending && pendingNote != "" {
			params.PendingNote = &pendingNote
		}
	}

	result, err := s.UpsertTask(params)
	if err != nil {
		return store.Task{}, fmt.Errorf("engram-workspace: upsert task %s: %w", action.VaultPath, err)
	}
	return result.Task, nil
}

// importRuns records the measurements a task's engram.benchmark.v1 runs carry.
// A foreign run is left alone: it is already registered as evidence, and
// reading numbers out of it needs a pointer map somebody wrote on purpose.
func importRuns(s *store.Store, task store.Task, root, taskDir string, runs []vault.RunFile, apply bool, plan *ImportPlan) error {
	// planned mirrors AddBenchmark's idempotency key for the measurements
	// this run would write but the store has not seen yet, for the same
	// reason recordFiles keeps one: without it a dry run counts a repeated
	// measurement as new where the apply counts it as a duplicate.
	planned := make(map[string]bool)
	for _, run := range runs {
		if run.Format != vault.FormatV1 || run.Run == nil {
			plan.Benchmarks.Skipped++
			continue
		}
		runPath := runPathFor(root, filepath.Join(taskDir, filepath.FromSlash(run.RelPath)))
		name := strings.TrimSpace(run.Run.Name)
		if name == "" {
			name = strings.TrimSuffix(filepath.Base(run.RelPath), filepath.Ext(run.RelPath))
		}
		for _, m := range run.Run.Metrics {
			if !apply {
				key := name + "\x00" + m.Metric + "\x00" + run.Run.CapturedAt
				known := planned[key]
				if !known && task.SyncID != "" {
					var err error
					if known, err = benchmarkExists(s, task.SyncID, name, m.Metric, run.Run.CapturedAt); err != nil {
						return err
					}
				}
				planned[key] = true
				if known {
					plan.Benchmarks.Updated++
					continue
				}
				plan.Benchmarks.Added++
				continue
			}
			result, err := s.AddBenchmark(store.AddBenchmarkParams{
				Task:        task,
				Name:        name,
				Metric:      m.Metric,
				Unit:        m.Unit,
				Direction:   m.Direction,
				Value:       m.Value,
				Baseline:    run.Run.Baseline,
				RunPath:     &runPath,
				ConfigStamp: optional(run.Run.ConfigStamp),
				CapturedAt:  run.Run.CapturedAt,
				Notes:       optional(run.Run.Notes),
				Source:      benchmarkSourceJSON,
			})
			if err != nil {
				return fmt.Errorf("engram-workspace: record %s: %w", m.Metric, err)
			}
			if result.Created {
				plan.Benchmarks.Added++
				continue
			}
			plan.Benchmarks.Updated++
		}
	}
	return nil
}

// existingTask finds the row a vault folder already corresponds to, by Jira key
// when the folder names one and by (project, slug) otherwise.
func existingTask(s *store.Store, project, jiraKey, slug string) (store.Task, bool, error) {
	// A Jira key is unique across projects, so it is looked up without one;
	// a slug only means something inside the project that keeps it.
	if jiraKey != "" {
		t, found, err := findTaskIn(s, "", jiraKey)
		if err != nil {
			return store.Task{}, false, fmt.Errorf("engram-workspace: look up %s: %w", jiraKey, err)
		}
		return t, found, nil
	}
	t, found, err := findTaskIn(s, project, slug)
	if err != nil {
		return store.Task{}, false, fmt.Errorf("engram-workspace: look up %s/%s: %w", project, slug, err)
	}
	return t, found, nil
}

// stateFor picks the state a task is in: the vault README's task map first,
// because that table is where the user keeps the overview, and the task's own
// README as the fallback. The map is keyed by "<project>/<task>", the folder
// each row's link points at, which is the one thing a row and a folder walk
// can be matched on.
func stateFor(states map[string]vault.ReadmeTask, vaultPath string, scanned vault.TaskDir) (state, closedAt, pendingNote string) {
	if row, ok := states[vaultPath]; ok && row.State != "" {
		return row.State, row.ClosedAt, row.PendingNote
	}
	return scanned.State, "", scanned.PendingNote
}

// kindKeywords maps what a task folder is called onto the kind it most likely
// is. It runs once, when a task is first imported: a guess is a starting point
// for a person to correct, never something that overwrites the correction.
var kindKeywords = []struct {
	kind  string
	words []string
}{
	{"incident", []string{"incidente", "incident", "outage", "caida", "caída", "timeout"}},
	{"migration", []string{"migracion", "migración", "migration", "migrate", "upgrade"}},
	{"bugfix", []string{"fix", "bug", "error", "hardening", "hotfix"}},
	{"refactor", []string{"refactor", "split", "division", "división", "extraer", "cleanup"}},
	{"spike", []string{"spike", "poc", "evaluar", "investigacion", "investigación", "analisis", "análisis", "arquitectura"}},
}

// kindOf derives a task's kind from its folder name and title, falling back to
// "feature" when neither suggests otherwise.
//
// The summary is deliberately not read. A paragraph describing the work names
// every noun around it — "endurecer el autologin tras el incidente" is a fix,
// not an incident — and a heuristic fed that much prose matches whichever
// keyword happens to appear first.
func kindOf(dirName, title string) string {
	if dirName == archTaskDir {
		return "spike"
	}
	haystack := strings.ToLower(dirName + " " + title)
	for _, entry := range kindKeywords {
		for _, word := range entry.words {
			if strings.Contains(haystack, word) {
				return entry.kind
			}
		}
	}
	return "feature"
}

// benchmarkExists is the read half of AddBenchmark's idempotency key, which a
// dry run needs in order to report a re-import as a no-op rather than as work.
func benchmarkExists(s *store.Store, taskSyncID, name, metric, capturedAt string) (bool, error) {
	_, found, err := s.FindBenchmark(taskSyncID, name, metric, capturedAt)
	return found, err
}
