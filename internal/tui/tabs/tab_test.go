package tabs

import "testing"

func TestIDsAreDistinctAndOrdered(t *testing.T) {
	ids := []ID{Memory, Tasks, Evidence, Runbooks, Cloud}

	seen := map[ID]bool{}
	for _, id := range ids {
		if seen[id] {
			t.Fatalf("tab id %d is duplicated", id)
		}
		seen[id] = true
	}

	if Memory != 0 {
		t.Fatalf("Memory = %d, want 0: it is the tab the workspace opens on", Memory)
	}
	for i := 1; i < len(ids); i++ {
		if ids[i] <= ids[i-1] {
			t.Fatalf("tab bar order broken at %d: %v is not after %v", i, ids[i], ids[i-1])
		}
	}
}

func TestIDString(t *testing.T) {
	cases := map[ID]string{
		Memory:   "memory",
		Tasks:    "tasks",
		Evidence: "evidence",
		Runbooks: "runbooks",
		Cloud:    "cloud",
		ID(99):   "unknown",
	}
	for id, want := range cases {
		if got := id.String(); got != want {
			t.Errorf("ID(%d).String() = %q, want %q", id, got, want)
		}
	}
}

func TestNavigateEmitsNavigateMsg(t *testing.T) {
	cmd := Navigate(Cloud)
	if cmd == nil {
		t.Fatal("Navigate should return a non-nil command")
	}

	msg, ok := cmd().(NavigateMsg)
	if !ok {
		t.Fatalf("command returned %T, want NavigateMsg", cmd())
	}
	if msg.Target != Cloud {
		t.Fatalf("target = %v, want %v", msg.Target, Cloud)
	}
}

// TestNavigateToTaskEvidenceEmitsAnEvidenceTargetWithTheTaskID pins the
// dependency T-10.03's S4 "e" key left declared: rfc-tui.md §3.1 S6 filters
// by task_id when reached from a task's detail, which needs a field
// NavigateMsg did not have (only ObservationID, for the Memory deep link).
func TestNavigateToTaskEvidenceEmitsAnEvidenceTargetWithTheTaskID(t *testing.T) {
	cmd := NavigateToTaskEvidence(42)
	if cmd == nil {
		t.Fatal("NavigateToTaskEvidence should return a non-nil command")
	}

	msg, ok := cmd().(NavigateMsg)
	if !ok {
		t.Fatalf("command returned %T, want NavigateMsg", cmd())
	}
	if msg.Target != Evidence {
		t.Fatalf("target = %v, want %v", msg.Target, Evidence)
	}
	if msg.TaskID != 42 {
		t.Fatalf("TaskID = %d, want 42", msg.TaskID)
	}
}

// TestNavigateToTaskEmitsATasksTargetWithTheTaskID pins rfc-tui.md §3.1 S7's
// "Enter" on an evidence file, which opens that file's task inside Tasks —
// the mirror image of NavigateToTaskEvidence.
func TestNavigateToTaskEmitsATasksTargetWithTheTaskID(t *testing.T) {
	cmd := NavigateToTask(7)
	if cmd == nil {
		t.Fatal("NavigateToTask should return a non-nil command")
	}

	msg, ok := cmd().(NavigateMsg)
	if !ok {
		t.Fatalf("command returned %T, want NavigateMsg", cmd())
	}
	if msg.Target != Tasks {
		t.Fatalf("target = %v, want %v", msg.Target, Tasks)
	}
	if msg.TaskID != 7 {
		t.Fatalf("TaskID = %d, want 7", msg.TaskID)
	}
}

// TestNavigateToMemorySearchEmitsAMemoryTargetWithTheQuery pins rfc-tui.md
// §3.1 S8/S9's "t" key: it needs a field none of the above cover, since
// ObservationID and TaskID both name an id, not a search string.
func TestNavigateToMemorySearchEmitsAMemoryTargetWithTheQuery(t *testing.T) {
	cmd := NavigateToMemorySearch("runbook/RB-003")
	if cmd == nil {
		t.Fatal("NavigateToMemorySearch should return a non-nil command")
	}

	msg, ok := cmd().(NavigateMsg)
	if !ok {
		t.Fatalf("command returned %T, want NavigateMsg", cmd())
	}
	if msg.Target != Memory {
		t.Fatalf("target = %v, want %v", msg.Target, Memory)
	}
	if msg.Query != "runbook/RB-003" {
		t.Fatalf("Query = %q, want %q", msg.Query, "runbook/RB-003")
	}
}
