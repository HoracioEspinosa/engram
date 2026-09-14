package store

import "testing"

func TestTaskKeyPrefersJiraThenSlug(t *testing.T) {
	ptr := func(s string) *string { return &s }

	cases := []struct {
		name string
		task Task
		want string
	}{
		{
			name: "jira key wins over everything else",
			task: Task{SyncID: "task-0000000000000001", JiraKey: ptr("ACME-101"), Slug: ptr("recovery"), SDDChange: ptr("koi-workspace")},
			want: "ACME-101",
		},
		{
			name: "slug wins when there is no jira key",
			task: Task{SyncID: "task-0000000000000002", Slug: ptr("recovery"), SDDChange: ptr("koi-workspace")},
			want: "recovery",
		},
		{
			name: "sdd change answers when there is neither jira key nor slug",
			task: Task{SyncID: "task-0000000000000003", SDDChange: ptr("koi-workspace")},
			want: "koi-workspace",
		},
		{
			name: "sync id is the last resort",
			task: Task{SyncID: "task-0000000000000004"},
			want: "task-0000000000000004",
		},
		{
			name: "blank fields are skipped, not displayed",
			task: Task{SyncID: "task-0000000000000005", JiraKey: ptr("  "), Slug: ptr(""), SDDChange: ptr("\t")},
			want: "task-0000000000000005",
		},
		{
			name: "a padded key is displayed trimmed",
			task: Task{SyncID: "task-0000000000000006", JiraKey: ptr(" ACME-102 ")},
			want: "ACME-102",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.task.Key(); got != tc.want {
				t.Fatalf("Key() = %q, want %q", got, tc.want)
			}
		})
	}
}
