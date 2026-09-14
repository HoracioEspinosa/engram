// Package workspace orchestrates the knowledge vault against the store.
//
// internal/vault is pure filesystem code and internal/store is pure
// persistence; neither knows the other exists, which is what keeps each of
// them testable on its own. Something still has to hold a scan of the vault
// against the rows it should produce, decide what is new and what merely
// moved, and refuse to write when the caller only asked for a plan. That
// arbitration is this package: it imports both, and nothing imports it except
// the surfaces — the CLI, the MCP tools and the TUI — so the three cannot
// drift apart on what an import means.
//
// Every error a surface has to report is a value carrying the envelope code
// it is published under, so an operation's failure is named once here instead
// of being re-derived by each caller.
package workspace

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/vault"
)

// VaultRootEnv names the environment variable that answers "where is the
// vault?" when neither the caller nor the project card does.
const VaultRootEnv = "ENGRAM_VAULT_ROOT"

// CodedError is an error that names the envelope code it is reported under.
// The code is part of the contract — a script branches on it — so it is
// declared next to the message rather than reconstructed by string matching
// at each surface.
type CodedError struct {
	code string
	text string
}

// Error implements error.
func (e *CodedError) Error() string { return e.text }

// Code returns the envelope code for this failure.
func (e *CodedError) Code() string { return e.code }

// The failures a workspace operation reports. Each is wrapped with the detail
// of the specific occurrence (`fmt.Errorf("%w: %s", ErrUnknownTask, ref)`),
// which leaves the code reachable through errors.As.
var (
	// ErrUnknownTask reports a task reference nothing answers to.
	ErrUnknownTask = &CodedError{"unknown_task", "unknown task"}
	// ErrAmbiguousTask reports a reference that resolves in more than one
	// project. Task references are resolved across the whole store, so a slug
	// two projects both use has to be disambiguated by the caller rather than
	// silently decided here.
	ErrAmbiguousTask = &CodedError{"ambiguous_task", "task reference matches more than one project"}
	// ErrUnknownProject reports a slug with no project card.
	ErrUnknownProject = &CodedError{"unknown_project", "unknown project"}
	// ErrEvidenceRootUnresolved reports that nothing said where the task's
	// vault folder is.
	ErrEvidenceRootUnresolved = &CodedError{"evidence_root_unresolved", "evidence root unresolved"}
	// ErrPathEscapesVault reports a resolved path that leaves the vault root.
	ErrPathEscapesVault = &CodedError{"path_escapes_vault", "path escapes the vault root"}
	// ErrRestrictedPath reports a path whose real location sits under a
	// restricted root.
	ErrRestrictedPath = &CodedError{"restricted_path_rejected", "restricted path rejected"}
	// ErrRunNotFound reports a benchmark run file that is not there.
	ErrRunNotFound = &CodedError{"run_not_found", "benchmark run not found"}
	// ErrNotEngramBenchmarkV1 reports a run in the harness's own format with
	// no pointer map to read it with.
	ErrNotEngramBenchmarkV1 = &CodedError{"not_engram_benchmark_v1", "run is not engram.benchmark.v1 and no pointer map was given"}
	// ErrPointerUnresolved reports a pointer map that no longer matches the
	// run it maps.
	ErrPointerUnresolved = &CodedError{"pointer_unresolved", "json pointer unresolved"}
	// ErrVaultRootUnresolved reports that nothing said where the vault is.
	ErrVaultRootUnresolved = &CodedError{"vault_root_unresolved", "vault root unresolved"}
	// ErrVaultReadmeUnparsed reports a vault README with no task map in it.
	ErrVaultReadmeUnparsed = &CodedError{"vault_readme_unparsed", "vault README carries no task map"}
)

// coder is the interface CodeOf looks for.
type coder interface{ Code() string }

// CodeOf returns the envelope code err was declared under, or "" when the
// error is not one this package names. A surface uses it to fill the `code`
// field without a switch of its own.
func CodeOf(err error) string {
	var c coder
	if errors.As(err, &c) {
		return c.Code()
	}
	return ""
}

// Skip is one entry an operation refused to act on, and why.
type Skip struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// ─── Task resolution ─────────────────────────────────────────────────────────

var (
	taskSyncIDRef = regexp.MustCompile(`^task-[0-9a-f]{16}$`)
	jiraKeyRef    = regexp.MustCompile(`^[A-Z][A-Z0-9]+-[0-9]+$`)
	localIDRef    = regexp.MustCompile(`^#([0-9]+)$`)
	changeRef     = regexp.MustCompile(`^change:([a-z0-9][a-z0-9-]*)$`)
)

// taskColumns is the projection ResolveTask scans. It is deliberately the
// subset this package uses rather than the store's whole task projection: a
// column added to the table must not silently change what a scan here expects.
const taskColumns = `id, sync_id, project, jira_key, sdd_change, title, kind, state,
	slug, summary, pending_note, vault_path`

// ResolveTask resolves a task reference across the whole store, in the five
// forms the workspace surfaces accept: a Jira key, a task sync id, "#<id>",
// "change:<sdd-change>", and a task slug.
//
// The store's own ResolveTaskRef is scoped to one project and knows the first
// four forms. The workspace operations are given a reference without a
// project — the task is what names the project, not the other way round — so
// the lookup happens here, over store.DB(), and a reference that answers in
// two projects is refused rather than decided by row order.
func ResolveTask(s *store.Store, ref string) (store.Task, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return store.Task{}, fmt.Errorf("%w: empty reference", ErrUnknownTask)
	}

	var (
		column string
		value  any
	)
	switch {
	case taskSyncIDRef.MatchString(ref):
		column, value = "sync_id", ref
	case jiraKeyRef.MatchString(ref):
		column, value = "jira_key", ref
	case localIDRef.MatchString(ref):
		id, err := strconv.ParseInt(localIDRef.FindStringSubmatch(ref)[1], 10, 64)
		if err != nil {
			return store.Task{}, fmt.Errorf("%w: %s", ErrUnknownTask, ref)
		}
		column, value = "id", id
	case changeRef.MatchString(ref):
		column, value = "sdd_change", changeRef.FindStringSubmatch(ref)[1]
	default:
		column, value = "slug", ref
	}

	rows, err := s.DB().Query(
		`SELECT `+taskColumns+` FROM tasks WHERE `+column+` = ? AND deleted_at IS NULL ORDER BY id`, value)
	if err != nil {
		return store.Task{}, fmt.Errorf("engram-workspace: resolve task %s: %w", ref, err)
	}
	defer rows.Close()

	var found []store.Task
	for rows.Next() {
		var t store.Task
		if err := rows.Scan(&t.ID, &t.SyncID, &t.Project, &t.JiraKey, &t.SDDChange, &t.Title,
			&t.Kind, &t.State, &t.Slug, &t.Summary, &t.PendingNote, &t.VaultPath); err != nil {
			return store.Task{}, fmt.Errorf("engram-workspace: scan task %s: %w", ref, err)
		}
		found = append(found, t)
	}
	if err := rows.Err(); err != nil {
		return store.Task{}, fmt.Errorf("engram-workspace: resolve task %s: %w", ref, err)
	}

	switch len(found) {
	case 0:
		return store.Task{}, fmt.Errorf("%w: %s", ErrUnknownTask, ref)
	case 1:
		return found[0], nil
	}
	projects := make([]string, 0, len(found))
	for _, t := range found {
		projects = append(projects, t.Project)
	}
	return store.Task{}, fmt.Errorf("%w: %s resolves in %s", ErrAmbiguousTask, ref, strings.Join(projects, ", "))
}

// findTaskBySlug returns the task a project keeps under slug, or
// sql.ErrNoRows. It is the lookup key an import uses for the task folders that
// carry no Jira ticket.
func findTaskBySlug(s *store.Store, project, slug string) (store.Task, error) {
	var t store.Task
	err := s.DB().QueryRow(
		`SELECT `+taskColumns+` FROM tasks WHERE project = ? AND slug = ? AND deleted_at IS NULL ORDER BY id LIMIT 1`,
		project, slug).
		Scan(&t.ID, &t.SyncID, &t.Project, &t.JiraKey, &t.SDDChange, &t.Title,
			&t.Kind, &t.State, &t.Slug, &t.Summary, &t.PendingNote, &t.VaultPath)
	if err != nil {
		return store.Task{}, err
	}
	return t, nil
}

// findTaskByJiraKey returns the task holding a Jira key anywhere in the store.
// Keys are unique across projects, so the project is not part of the lookup.
func findTaskByJiraKey(s *store.Store, key string) (store.Task, error) {
	var t store.Task
	err := s.DB().QueryRow(
		`SELECT `+taskColumns+` FROM tasks WHERE jira_key = ? AND deleted_at IS NULL LIMIT 1`, key).
		Scan(&t.ID, &t.SyncID, &t.Project, &t.JiraKey, &t.SDDChange, &t.Title,
			&t.Kind, &t.State, &t.Slug, &t.Summary, &t.PendingNote, &t.VaultPath)
	if err != nil {
		return store.Task{}, err
	}
	return t, nil
}

// ─── Vault location ──────────────────────────────────────────────────────────

// VaultRoot resolves the vault root for a project: the explicit argument
// first, then what the project card's knowledge hub points at, then the
// environment.
//
// A hub path naming a file is read as the document inside the tree rather than
// the tree itself, so its directory is the candidate. Which of the two the
// candidate turns out to be — the vault root or one project folder inside it —
// is settled by TaskDir, which looks for the task under both.
func VaultRoot(s *store.Store, explicit, slug string) (string, error) {
	hub := ""
	if strings.TrimSpace(explicit) == "" && strings.TrimSpace(slug) != "" {
		card, err := s.ResolveProjectCard(slug)
		if err == nil && card.KnowledgeHubPath != nil {
			hub = hubDir(*card.KnowledgeHubPath)
		}
	}
	root, err := vault.Root(explicit, hub, os.Getenv(VaultRootEnv))
	if err != nil {
		return "", fmt.Errorf("%w: pass a root, set %s, or record a knowledge hub on the card",
			ErrVaultRootUnresolved, VaultRootEnv)
	}
	return root, nil
}

// hubDir turns a knowledge hub path into a directory candidate: itself when it
// is a directory, its parent when it is a file.
func hubDir(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}
	if info, err := os.Stat(trimmed); err == nil && info.IsDir() {
		return trimmed
	}
	return filepath.Dir(trimmed)
}

// restrictedRoots is the list a check runs against: the caller's, or the
// package default when the caller named none. vault.Options applies the same
// fallback internally, but the checks this package makes on its own — a task
// folder, a run file — have to apply it themselves.
func restrictedRoots(o vault.Options) []string {
	if len(o.Restricted) > 0 {
		return o.Restricted
	}
	return vault.DefaultRestrictedRoots()
}

// TaskDir resolves the folder root keeps a task's knowledge in, trying, in
// order: the vault path the task itself records, the ticket-prefixed folder
// under the project, the bare slug under the project, and the same two
// directly under root — the last pair for a root that already is the project
// folder, which is what a knowledge hub path usually points at.
//
// The first candidate that exists wins. None of them existing is
// ErrEvidenceRootUnresolved rather than an empty scan: a task whose folder
// cannot be found has not been proven to hold nothing.
func TaskDir(root string, t store.Task, o vault.Options) (string, error) {
	slug := ""
	if t.Slug != nil {
		slug = strings.TrimSpace(*t.Slug)
	}
	jira := ""
	if t.JiraKey != nil {
		jira = strings.TrimSpace(*t.JiraKey)
	}

	var candidates []string
	if t.VaultPath != nil && strings.TrimSpace(*t.VaultPath) != "" {
		candidates = append(candidates, filepath.FromSlash(strings.TrimSpace(*t.VaultPath)))
	}
	for _, name := range taskFolderNames(jira, slug) {
		candidates = append(candidates,
			filepath.Join(t.Project, name),
			name,
		)
	}

	for _, rel := range candidates {
		full := filepath.Join(root, rel)
		inside, err := withinRoot(root, full)
		if err != nil {
			return "", err
		}
		if !inside {
			return "", fmt.Errorf("%w: %s", ErrPathEscapesVault, rel)
		}
		if info, err := os.Stat(full); err == nil && info.IsDir() {
			restricted, rerr := vault.IsRestricted(full, restrictedRoots(o))
			if rerr != nil {
				return "", rerr
			}
			if restricted {
				return "", fmt.Errorf("%w: %s", ErrRestrictedPath, full)
			}
			return full, nil
		}
	}
	// Last resort: a folder whose name merely leads with the ticket. Most task
	// folders are named "<TICKET>-<what it was about>", and the store only
	// records the ticket, so without this a task nobody has imported yet would
	// never find its own knowledge.
	if jira != "" {
		for _, parent := range []string{filepath.Join(root, t.Project), root} {
			if match := dirStartingWith(parent, jira+"-"); match != "" {
				restricted, err := vault.IsRestricted(match, restrictedRoots(o))
				if err != nil {
					return "", err
				}
				if restricted {
					return "", fmt.Errorf("%w: %s", ErrRestrictedPath, match)
				}
				return match, nil
			}
		}
	}
	return "", fmt.Errorf("%w: no folder for task %s under %s", ErrEvidenceRootUnresolved, taskLabel(t), root)
}

// dirStartingWith returns the first subdirectory of parent whose name begins
// with prefix, in name order so two runs agree, or "" when there is none.
func dirStartingWith(parent, prefix string) string {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), prefix) {
			return filepath.Join(parent, entry.Name())
		}
	}
	return ""
}

// taskFolderNames lists the folder names a task may be filed under, most
// specific first.
func taskFolderNames(jira, slug string) []string {
	var names []string
	if jira != "" && slug != "" {
		names = append(names, jira+"-"+slug)
	}
	if slug != "" {
		names = append(names, slug)
	}
	if jira != "" {
		names = append(names, jira)
	}
	return names
}

// taskLabel names a task the way a message should: its ticket when it has one,
// its slug otherwise, and its sync id when it has neither.
func taskLabel(t store.Task) string {
	if t.JiraKey != nil && strings.TrimSpace(*t.JiraKey) != "" {
		return *t.JiraKey
	}
	if t.Slug != nil && strings.TrimSpace(*t.Slug) != "" {
		return *t.Slug
	}
	return t.SyncID
}

// withinRoot reports whether path stays inside root once every symlink on both
// sides is resolved, which is what makes a "../.." in a recorded vault path
// fail here instead of reading somebody's home directory.
func withinRoot(root, path string) (bool, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false, fmt.Errorf("engram-workspace: resolve root %s: %w", root, err)
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false, fmt.Errorf("engram-workspace: resolve path %s: %w", path, err)
	}
	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil {
		return false, nil
	}
	return rel == "." || !strings.HasPrefix(rel, ".."), nil
}

// vaultRelative renders path as a slash-separated path relative to root, which
// is the spelling an evidence row records so the same file registered from two
// machines is one row.
func vaultRelative(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

// runPathFor renders a benchmark run's location the way the column accepts it:
// relative to the vault root, with no ".." in it. A run kept outside the vault
// has no such spelling, so it is recorded by name — the row points at a
// measurement, and the file it came from is provenance, not an address.
func runPathFor(root, path string) string {
	if strings.TrimSpace(root) != "" {
		if inside, err := withinRoot(root, path); err == nil && inside {
			if rel := vaultRelative(root, path); rel != "." && !strings.HasPrefix(rel, "..") {
				return rel
			}
		}
	}
	return filepath.Base(path)
}

// isNoRows reports the "nothing matched" case of a single-row lookup.
func isNoRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }
