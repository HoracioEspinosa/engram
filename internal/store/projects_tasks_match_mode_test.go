package store

import (
	"fmt"
	"testing"
)

// TestListTasksMatchModeWidensRecall pins the two ways a task query's tokens
// can combine: every token by default, any of them when the caller asks for
// the broader search.
func TestListTasksMatchModeWidensRecall(t *testing.T) {
	s := newProjectsSchemaTestStore(t)
	for i, title := range []string{"drain the pond", "feed the trout"} {
		key := fmt.Sprintf("PROJ-%d", i+1)
		kind := "incident"
		titleCopy := title
		if _, err := s.UpsertTask(UpsertTaskParams{Project: "riverside", JiraKey: &key, Title: &titleCopy, Kind: &kind}); err != nil {
			t.Fatalf("UpsertTask %q: %v", title, err)
		}
	}

	all, _, err := s.ListTasks("riverside", TaskListFilter{Query: "pond trout"})
	if err != nil {
		t.Fatalf("ListTasks all: %v", err)
	}
	if len(all) != 0 {
		t.Fatalf("the default match mode needs every token, got %d tasks", len(all))
	}

	any, _, err := s.ListTasks("riverside", TaskListFilter{Query: "pond trout", MatchMode: "any"})
	if err != nil {
		t.Fatalf("ListTasks any: %v", err)
	}
	if len(any) != 2 {
		t.Fatalf("match_mode=any must find both tasks, got %d", len(any))
	}
}
