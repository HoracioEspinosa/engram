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
