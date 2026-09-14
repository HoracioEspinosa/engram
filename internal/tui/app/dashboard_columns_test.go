package app

import (
	"strings"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	"github.com/charmbracelet/x/ansi"
)

// loadedDashboard returns a dashboard already showing tasks, so the view can
// be rendered without walking the load command.
func loadedDashboard(t *testing.T, tasks []store.TaskListItem) Model {
	t.Helper()
	m := New(nil, nil, nil, nil, nil, "", theme.New(theme.CatppuccinMocha()), "")
	m.screen, m.tree.open = screenDashboard, false
	m.project = "nextcloud"
	m.dashboard = newDashboardModel(testDashboardReader(), "nextcloud")
	m.dashboard.loaded = true
	m.dashboard.card = store.ProjectCard{Slug: "nextcloud", DefaultBranch: "master"}
	m.dashboard.tasks = tasks
	return m
}

// TestDashboardRowsNeverShowRawSyncIDForKeyedTasks pins the reason Task.Key()
// exists: the sync id names the row, not the work. A task the vault filed
// under a readable slug used to be listed by its sync id because the dashboard
// only ever looked at jira_key.
func TestDashboardRowsNeverShowRawSyncIDForKeyedTasks(t *testing.T) {
	cases := []struct {
		name    string
		task    store.Task
		want    string
		refused string
	}{
		{
			name:    "a jira key names the task",
			task:    store.Task{SyncID: "task-aa1122", JiraKey: strp("CDBS-10336"), Kind: "bugfix", State: "review", Title: "Previews 503"},
			want:    "CDBS-10336",
			refused: "task-aa1122",
		},
		{
			name:    "a slug names a task with no jira key",
			task:    store.Task{SyncID: "task-bb3344", Slug: strp("previews"), Kind: "bugfix", State: "review", Title: "Previews 503"},
			want:    "previews",
			refused: "task-bb3344",
		},
		{
			name:    "an sdd change names a task with neither",
			task:    store.Task{SyncID: "task-cc5566", SDDChange: strp("koi-ws"), Kind: "feature", State: "open", Title: "Koi workspace"},
			want:    "koi-ws",
			refused: "task-cc5566",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := loadedDashboard(t, []store.TaskListItem{{Task: tc.task}})
			view := m.viewDashboard()

			if !strings.Contains(view, tc.want) {
				t.Fatalf("dashboard does not name the task %q:\n%s", tc.want, view)
			}
			if strings.Contains(view, tc.refused) {
				t.Fatalf("dashboard still shows the raw sync id %q:\n%s", tc.refused, view)
			}
		})
	}
}

// TestDashboardTaskRowsKeepTheirColumnsAligned covers what a rune count gets
// wrong: a wide key or a wide state pushed every column to its right out of
// line, and an overlong key ran over the column entirely.
func TestDashboardTaskRowsKeepTheirColumnsAligned(t *testing.T) {
	const title = "unmistakable"
	tasks := []store.TaskListItem{
		{Task: store.Task{SyncID: "task-1", JiraKey: strp("CDBS-1"), Kind: "bugfix", State: "review", Title: title}},
		{Task: store.Task{SyncID: "task-2", Slug: strp("決定を記録する"), Kind: "bugfix", State: "review", Title: title}},
		{Task: store.Task{SyncID: "task-3", Slug: strp("a-very-long-task-slug-indeed"), Kind: "bugfix", State: "review", Title: title}},
		{Task: store.Task{SyncID: "task-4", Slug: strp("café"), Kind: "bugfix", State: "review", Title: title}},
	}
	m := loadedDashboard(t, tasks)

	rows := strings.Split(strings.TrimRight(m.viewDashboardTasks(), "\n"), "\n")
	if len(rows) != len(tasks) {
		t.Fatalf("rendered %d rows, want %d", len(rows), len(tasks))
	}

	columns := make([]int, 0, len(rows))
	for i, row := range rows {
		plain := ansi.Strip(row)
		at := strings.Index(plain, title)
		if at < 0 {
			t.Fatalf("row %d: title not found in %q", i, plain)
		}
		columns = append(columns, ansi.StringWidth(plain[:at]))
	}

	for i := 1; i < len(columns); i++ {
		if columns[i] != columns[0] {
			t.Fatalf("title column starts at %d cells on row %d and %d cells on row 0:\n%s",
				columns[i], i, columns[0], strings.Join(rows, "\n"))
		}
	}
}
