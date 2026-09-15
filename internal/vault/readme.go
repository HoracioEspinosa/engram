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
	// Project is the project folder the row's link points into, falling back
	// to the project the subheading above the table names.
	Project string
	// Dir is the task folder name the row's link points at.
	Dir string
	// JiraKey is the ticket Dir leads with, empty for the folders that carry
	// none. It is read from the folder, never from the link text: the text is
	// prose a person edits, the folder is what an importer walks.
	JiraKey string
	// Title is the link text with the "<TICKET> — " prefix removed, since the
	// ticket is already carried by JiraKey.
	Title string
	// Path is the task folder relative to the vault root, as
	// "<project>/<task>". It is Project and Dir joined, and it is the key an
	// importer correlates its own folder walk against.
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
	// ClosedAt carries what a "Cerrado (...)" cell puts between its
	// parentheses, verbatim: the table is prose, and a date it spells as
	// "2-jul-2026" is preserved rather than dropped for not being ISO.
	ClosedAt string
	// PendingNote carries the note after a "Con pendientes" cell, when the row
	// spells one out.
	PendingNote string
	// Count is the file count of the last cell, or 0 when it is not a number.
	Count int
}

var (
	taskMapHeading    = regexp.MustCompile(`(?i)^(#{1,6})\s*mapa de tareas\s*$`)
	headingLine       = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	projectSubheading = regexp.MustCompile("^`?\\s*([^`/\\s]+)/?\\s*`?")
	taskLinkCell      = regexp.MustCompile(`^\[(.+?)\]\(([^)]+)\)$`)
	taskTicketPrefix  = regexp.MustCompile(`^[A-Z]+-[0-9]+\s*[—–-]\s*`)
	taskDirName       = regexp.MustCompile(`^([A-Z]+-[0-9]+)-(.+)$`)
	closedState       = regexp.MustCompile(`(?i)^cerrado\b\s*(?:\(([^)]*)\))?`)
	pendingState      = regexp.MustCompile(`(?i)^con pendientes\b\s*(.*)$`)
	archivedState     = regexp.MustCompile(`(?i)^hist[óo]rico\b`)
	unverifiedState   = regexp.MustCompile(`(?i)^sin confirmar\b`)
	readmeH1          = regexp.MustCompile(`^#\s+(.+?)\s*$`)
	metadataLine      = regexp.MustCompile(`^\*\*([^*]+):\*\*\s*(.*)$`)
)

// ParseProjectReadme reads the "Mapa de tareas" section out of a vault README.
//
// The section is not one table. A vault that holds more than one project
// splits it into a subheading and a table per project, with prose in between,
// and it ends where the next heading at the map's own level begins. Reading it
// that way is what keeps the lookup tables further down the file — which link
// to the very same folders — from turning into phantom tasks.
//
// A README without the section is an error rather than an empty result: the
// caller asked for the task map and there is none to report.
func ParseProjectReadme(path string) ([]ReadmeTask, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("vault: read %s: %w", path, err)
	}
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")

	heading := -1
	level := 0
	for i, line := range lines {
		if match := taskMapHeading.FindStringSubmatch(strings.TrimSpace(line)); match != nil {
			heading = i
			level = len(match[1])
			break
		}
	}
	if heading < 0 {
		return nil, fmt.Errorf("vault: %s has no \"Mapa de tareas\" table", path)
	}

	var tasks []ReadmeTask
	project := ""
	for _, line := range lines[heading+1:] {
		trimmed := strings.TrimSpace(line)
		if match := headingLine.FindStringSubmatch(trimmed); match != nil {
			if len(match[1]) <= level {
				break
			}
			project = projectFromSubheading(match[2])
			continue
		}
		if !strings.HasPrefix(trimmed, "|") {
			continue
		}
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
		slug, dir := taskFolderFromLink(match[2])
		if dir == "" {
			continue
		}
		if slug == "" {
			slug = project
		}
		jiraKey, _ := ParseTaskDirName(dir)
		task := ReadmeTask{
			Project:  slug,
			Dir:      dir,
			JiraKey:  jiraKey,
			Title:    taskTitle(match[1]),
			Path:     strings.Trim(slug+"/"+dir, "/"),
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

// projectFromSubheading reads the project a "### `nextcloud/` — ..." heading
// introduces. A subheading naming no folder leaves the project unset, which
// only matters for rows whose own link does not name one either.
func projectFromSubheading(text string) string {
	match := projectSubheading.FindStringSubmatch(strings.TrimSpace(text))
	if match == nil {
		return ""
	}
	return strings.Trim(match[1], "/")
}

// taskFolderFromLink splits "./koi-garden/KOI-1042-x/README.md" into its
// project and its task folder. A link that names only the task folder returns
// an empty project, leaving the caller to take it from the subheading above.
func taskFolderFromLink(link string) (project, dir string) {
	path := taskPathFromLink(link)
	if path == "" {
		return "", ""
	}
	segments := strings.Split(path, "/")
	if len(segments) == 1 {
		return "", segments[0]
	}
	return segments[len(segments)-2], segments[len(segments)-1]
}

// taskTitle drops the ticket a link text leads with. The ticket already lives
// in the folder name, and repeating it in the title only makes every listing
// spell it twice.
func taskTitle(text string) string {
	return strings.TrimSpace(taskTicketPrefix.ReplaceAllString(strings.TrimSpace(text), ""))
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

// parseState maps the four states the vault spells out. The state is read from
// the start of the cell and whatever follows is a qualifier, because that is
// how the table is written: "Con pendientes — 2 de 6 temas", "Histórico — la
// app ya no existe", "Sin confirmar cuál corre hoy". A cell that opens with
// none of the four returns an empty state, leaving the caller to keep the raw
// cell and change nothing: a state nobody recognises is not a reason to
// overwrite one that is already recorded.
func parseState(raw string) (state, closedAt, pendingNote string) {
	trimmed := strings.TrimSpace(raw)
	if match := closedState.FindStringSubmatch(trimmed); match != nil {
		return StateDone, strings.TrimSpace(match[1]), ""
	}
	if match := pendingState.FindStringSubmatch(trimmed); match != nil {
		return StatePending, "", trimStateNote(match[1])
	}
	if archivedState.MatchString(trimmed) {
		return StateArchived, "", ""
	}
	if unverifiedState.MatchString(trimmed) {
		return StateUnverified, "", ""
	}
	return "", "", ""
}

// trimStateNote strips the dash a state cell separates its note with, so the
// note reads as the sentence the user wrote rather than as a table fragment.
func trimStateNote(note string) string {
	return strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(note), "—–-"))
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
