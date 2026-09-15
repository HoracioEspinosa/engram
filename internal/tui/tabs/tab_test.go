package tabs

import "testing"

func TestIDsAreDistinctAndOrdered(t *testing.T) {
	// The order is the bar's order, slot by slot.
	ids := []ID{Home, Memory, Tasks, Evidence, Benchmarks, Runbooks, Graph, Settings}

	seen := map[ID]bool{}
	for _, id := range ids {
		if seen[id] {
			t.Fatalf("tab id %d is duplicated", id)
		}
		seen[id] = true
	}

	if Home != 0 {
		t.Fatalf("Home = %d, want 0: it is the tab the workspace opens on", Home)
	}
	for i := 1; i < len(ids); i++ {
		if ids[i] <= ids[i-1] {
			t.Fatalf("tab bar order broken at %d: %v is not after %v", i, ids[i], ids[i-1])
		}
	}
}

func TestIDString(t *testing.T) {
	cases := map[ID]string{
		Home:       "home",
		Memory:     "memory",
		Tasks:      "tasks",
		Evidence:   "evidence",
		Benchmarks: "benchmarks",
		Runbooks:   "runbooks",
		Graph:      "graph",
		Settings:   "settings",
		ID(99):     "unknown",
	}
	for id, want := range cases {
		if got := id.String(); got != want {
			t.Errorf("ID(%d).String() = %q, want %q", id, got, want)
		}
	}
}

func TestNavigateEmitsNavigateMsg(t *testing.T) {
	cmd := Navigate(Settings)
	if cmd == nil {
		t.Fatal("Navigate should return a non-nil command")
	}

	msg, ok := cmd().(NavigateMsg)
	if !ok {
		t.Fatalf("command returned %T, want NavigateMsg", cmd())
	}
	if msg.Target != Settings {
		t.Fatalf("target = %v, want %v", msg.Target, Settings)
	}
}

// TestNavigateToTaskEvidenceEmitsAnEvidenceTargetWithTheTaskID pins the
// dependency the task detail screen's "e" key declares: the Evidence list
// filters by task_id when reached from a task's detail, which is what TaskID
// carries — ObservationID only serves the Memory deep link.
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

// TestNavigateToTaskEmitsATasksTargetWithTheTaskID pins the evidence detail
// screen's "Enter" on an evidence file, which opens that file's task inside
// Tasks — the mirror image of NavigateToTaskEvidence.
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

// TestNavigateToMemorySearchEmitsAMemoryTargetWithTheQuery pins the "t" key
// on the Runbooks index and on the runbook Markdown view: it needs a field
// none of the above cover, since ObservationID and TaskID both name an id,
// not a search string.
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

// TestNavigateToObservationEmitsAMemoryTargetWithTheObservationID pins the
// task detail screen's "Enter" on a task's linked observation. app/update.go's
// NavigateMsg branch for it is covered through app's tests; this covers the
// constructor on its own.
func TestNavigateToObservationEmitsAMemoryTargetWithTheObservationID(t *testing.T) {
	cmd := NavigateToObservation(101)
	if cmd == nil {
		t.Fatal("NavigateToObservation should return a non-nil command")
	}

	msg, ok := cmd().(NavigateMsg)
	if !ok {
		t.Fatalf("command returned %T, want NavigateMsg", cmd())
	}
	if msg.Target != Memory {
		t.Fatalf("target = %v, want %v", msg.Target, Memory)
	}
	if msg.ObservationID != 101 {
		t.Fatalf("ObservationID = %d, want 101", msg.ObservationID)
	}
}

// TestGoingHomeIsAPlainNavigation pins that "go home" needs no message of its
// own any more: Home is a tab, so the tabs that offer a way back name it the
// same way every other cross-tab jump does.
func TestGoingHomeIsAPlainNavigation(t *testing.T) {
	cmd := Navigate(Home)
	if cmd == nil {
		t.Fatal("Navigate should return a non-nil command")
	}
	msg, ok := cmd().(NavigateMsg)
	if !ok {
		t.Fatalf("command returned %T, want NavigateMsg", cmd())
	}
	if msg.Target != Home {
		t.Fatalf("Target = %v, want Home", msg.Target)
	}
}
