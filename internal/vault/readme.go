package vault

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Task states the vault's prose maps onto. They match the task states the
// store accepts, so an importer never has to translate twice.
const (
	StateDone       = "done"
	StatePending    = "pending"
	StateArchived   = "archived"
	StateUnverified = "unverified"
)

// ReadmeTask is one row of the "Mapa de tareas" table in the vault's root
// README: the hand-written index the user keeps of every task folder.
type ReadmeTask struct {
	// Title is the link text of the first cell.
	Title string
	// Path is the task folder relative to the vault root, as
	// "<project>/<task>": the link target with its "./" prefix and its
	// "/README.md" suffix removed.
	Path string
	// What is the second cell, a one-line description of the task.
	What string
	// StateRaw is the state cell verbatim. It is always preserved, including
	// for states this package does not recognise, so nothing downstream has to
	// guess what the user meant.
	StateRaw string
	// State is the mapped task state, or "" when StateRaw names none of the
	// four known states.
	State string
	// ClosedAt carries the date of a "Cerrado (YYYY-MM-DD)" cell.
	ClosedAt string
	// PendingNote carries the note after a "Con pendientes" cell, when the row
	// spells one out.
	PendingNote string
	// Count is the file count of the last cell, or 0 when it is not a number.
	Count int
}

var (
	taskMapHeading = regexp.MustCompile(`(?i)^#{1,6}\s*mapa de tareas\s*$`)
	taskLinkCell   = regexp.MustCompile(`^\[(.+?)\]\(([^)]+)\)$`)
	taskDirName    = regexp.MustCompile(`^([A-Z]+-[0-9]+)-(.+)$`)
	closedState    = regexp.MustCompile(`(?i)^cerrado(?:\s*\((\d{4}-\d{2}-\d{2})\))?$`)
	pendingState   = regexp.MustCompile(`(?i)^con pendientes\s*(?:[—–-]+\s*(.*))?$`)
	readmeH1       = regexp.MustCompile(`^#\s+(.+?)\s*$`)
	metadataLine   = regexp.MustCompile(`^\*\*([^*]+):\*\*\s*(.*)$`)
)

// ParseProjectReadme reads the "Mapa de tareas" table out of a vault README.
// Rows outside that table are ignored, so the instance and project tables the
// same file carries never turn into phantom tasks. A README without the table
// is an error rather than an empty result: the caller asked for the task map
// and there is none to report.
func ParseProjectReadme(path string) ([]ReadmeTask, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("vault: read %s: %w", path, err)
	}
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")

	heading := -1
	for i, line := range lines {
		if taskMapHeading.MatchString(strings.TrimSpace(line)) {
			heading = i
			break
		}
	}
	if heading < 0 {
		return nil, fmt.Errorf("vault: %s has no \"Mapa de tareas\" table", path)
	}

	var tasks []ReadmeTask
	started := false
	for _, line := range lines[heading+1:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" && !started {
			continue
		}
		if !strings.HasPrefix(trimmed, "|") {
			if started {
				break
			}
			if strings.HasPrefix(trimmed, "#") {
				break
			}
			continue
		}
		started = true
		cells := tableCells(trimmed)
		if len(cells) < 3 {
			continue
		}
		match := taskLinkCell.FindStringSubmatch(cells[0])
		if match == nil {
			// The header row and the "| --- |" separator land here, as does
			// any row whose first cell is not a link to a task folder.
			continue
		}
		task := ReadmeTask{
			Title:    strings.TrimSpace(match[1]),
			Path:     taskPathFromLink(match[2]),
			What:     cells[1],
			StateRaw: cells[2],
		}
		task.State, task.ClosedAt, task.PendingNote = parseState(cells[2])
		if len(cells) > 3 {
			if n, err := strconv.Atoi(strings.TrimSpace(cells[3])); err == nil {
				task.Count = n
			}
		}
		tasks = append(tasks, task)
	}
	return tasks, nil
}

// tableCells splits a markdown table row into its trimmed cells, dropping the
// empty fields the leading and trailing pipes produce.
func tableCells(row string) []string {
	parts := strings.Split(row, "|")
	if len(parts) > 0 && strings.TrimSpace(parts[0]) == "" {
		parts = parts[1:]
	}
	if len(parts) > 0 && strings.TrimSpace(parts[len(parts)-1]) == "" {
		parts = parts[:len(parts)-1]
	}
	cells := make([]string, 0, len(parts))
	for _, p := range parts {
		cells = append(cells, strings.TrimSpace(p))
	}
	return cells
}

// taskPathFromLink turns "./koi-garden/KOI-1042-x/README.md" into
// "koi-garden/KOI-1042-x".
func taskPathFromLink(link string) string {
	path := strings.TrimSpace(link)
	path = strings.TrimPrefix(path, "./")
	path = strings.TrimSuffix(path, "/")
	if idx := strings.LastIndex(path, "/"); idx >= 0 && strings.EqualFold(path[idx+1:], "README.md") {
		path = path[:idx]
	}
	return strings.Trim(path, "/")
}

// parseState maps the four states the vault spells out. Anything else returns
// an empty state, leaving the caller to keep the raw cell and change nothing:
// a state nobody recognises is not a reason to overwrite one that is already
// recorded.
func parseState(raw string) (state, closedAt, pendingNote string) {
	trimmed := strings.TrimSpace(raw)
	if match := closedState.FindStringSubmatch(trimmed); match != nil {
		return StateDone, match[1], ""
	}
	if match := pendingState.FindStringSubmatch(trimmed); match != nil {
		return StatePending, "", strings.TrimSpace(match[1])
	}
	switch strings.ToLower(trimmed) {
	case "histórico", "historico":
		return StateArchived, "", ""
	case "sin confirmar":
		return StateUnverified, "", ""
	}
	return "", "", ""
}

// ParseTaskDirName splits a task folder name into its Jira key and its slug.
// A folder without a leading ticket keeps its whole name as the slug, which is
// how the satellite and catch-all folders in the vault are named.
func ParseTaskDirName(name string) (jiraKey, slug string) {
	trimmed := strings.TrimSpace(name)
	if match := taskDirName.FindStringSubmatch(trimmed); match != nil {
		return match[1], match[2]
	}
	return "", trimmed
}

// readmeHeader is what a README contributes to the folder that holds it.
type readmeHeader struct {
	Title       string
	Summary     string
	State       string
	StateRaw    string
	ClosedAt    string
	PendingNote string
}

// parseReadmeHeader reads a folder README: its first H1, its first real
// paragraph, and the "**Estado:**" metadata line when it carries one.
func parseReadmeHeader(path string) (readmeHeader, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return readmeHeader{}, err
	}
	var header readmeHeader
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")

	var paragraph []string
	sawTitle := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if match := readmeH1.FindStringSubmatch(trimmed); match != nil && !sawTitle {
			header.Title = strings.TrimSpace(match[1])
			sawTitle = true
			continue
		}
		if match := metadataLine.FindStringSubmatch(trimmed); match != nil {
			if strings.EqualFold(strings.TrimSpace(match[1]), "estado") {
				header.StateRaw = strings.TrimSpace(match[2])
				header.State, header.ClosedAt, header.PendingNote = parseState(header.StateRaw)
			}
			continue
		}
		if trimmed == "" {
			if len(paragraph) > 0 {
				break
			}
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			if len(paragraph) > 0 {
				break
			}
			continue
		}
		paragraph = append(paragraph, trimmed)
	}
	header.Summary = strings.Join(paragraph, " ")
	return header, nil
}

// readmeTitle returns the first H1 of the README in dir, or "" when there is
// no README or it carries no heading.
func readmeTitle(dir string) string {
	header, err := parseReadmeHeader(filepath.Join(dir, "README.md"))
	if err != nil {
		return ""
	}
	return header.Title
}
