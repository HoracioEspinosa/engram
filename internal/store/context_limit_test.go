package store

import (
	"fmt"
	"strings"
	"testing"
)

// TestFormatContextLimitedCapsRecentObservations pins mem_context's limit: the
// caller can ask for fewer recent observations than the configured maximum,
// and can never ask for more.
func TestFormatContextLimitedCapsRecentObservations(t *testing.T) {
	s := newTestStore(t)
	if err := s.CreateSession("s1", "koi-garden", ""); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	for i := 0; i < 8; i++ {
		if _, err := s.AddObservation(AddObservationParams{
			SessionID: "s1", Type: "manual", Title: fmt.Sprintf("note %d", i),
			Content: fmt.Sprintf("body %d", i), Project: "koi-garden",
		}); err != nil {
			t.Fatalf("AddObservation %d: %v", i, err)
		}
	}

	capped, err := s.FormatContextLimited("koi-garden", "project", 3)
	if err != nil {
		t.Fatalf("FormatContextLimited: %v", err)
	}
	if got := strings.Count(capped, "- [manual]"); got != 3 {
		t.Fatalf("expected 3 recent observations, got %d", got)
	}

	full, err := s.FormatContext("koi-garden", "project")
	if err != nil {
		t.Fatalf("FormatContext: %v", err)
	}
	beyond, err := s.FormatContextLimited("koi-garden", "project", 10_000)
	if err != nil {
		t.Fatalf("FormatContextLimited beyond the cap: %v", err)
	}
	if beyond != full {
		t.Fatal("a limit above the configured maximum must render the same context as no limit")
	}
}
