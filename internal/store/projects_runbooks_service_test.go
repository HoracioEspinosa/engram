package store

import (
	"testing"
	"time"
)

// An entry naming a service outside the canonical list must be skipped, not
// silently turned into a project. Before the resolver existed, the store ran
// the value through NormalizeProject, which lowercases and never rejects, so
// a typo in the vault created a project card of its own and the runbook was
// indexed under a service nobody would ever query.
func TestSyncRunbookIndex_UnknownServiceIsSkippedNotCreated(t *testing.T) {
	s := newProjectsSchemaTestStore(t)

	resolve := func(raw string) (string, bool) {
		if raw == "nextcloud" {
			return "nextcloud", true
		}
		return "", false
	}

	result, err := s.SyncRunbookIndex(RunbookIndexSyncParams{
		Source:         "knowledge-mcp",
		ResolveService: resolve,
		Entries: []RunbookIndexEntryInput{
			{ID: "RB-003", VaultPath: "Runbooks/Performance/RB-003 Preview.md", Title: "Preview", Service: "nextcloud", Category: "performance", Status: "verified"},
			{ID: "RB-900", VaultPath: "Runbooks/RB-900 Ghost.md", Title: "Ghost", Service: "nextcluod", Category: "auth", Status: "verified"},
			{ID: "RB-901", VaultPath: "Runbooks/RB-901 Blank.md", Title: "Blank", Service: "", Category: "auth", Status: "verified"},
		},
	})
	if err != nil {
		t.Fatalf("SyncRunbookIndex: %v", err)
	}
	if result.Upserted != 1 {
		t.Fatalf("upserted = %d, want only the entry with a canonical service", result.Upserted)
	}
	reasons := map[string]string{}
	for _, sk := range result.Skipped {
		reasons[sk.ID] = sk.Reason
	}
	if reasons["RB-900"] != "unknown_service" {
		t.Fatalf("RB-900 reason = %q, want unknown_service (skips: %+v)", reasons["RB-900"], result.Skipped)
	}
	if reasons["RB-901"] != "missing_service" {
		t.Fatalf("RB-901 reason = %q, want missing_service (skips: %+v)", reasons["RB-901"], result.Skipped)
	}

	var cards int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM project_cards WHERE slug = 'nextcluod'`).Scan(&cards); err != nil {
		t.Fatalf("count cards: %v", err)
	}
	if cards != 0 {
		t.Fatal("an unknown service created a project card")
	}
}

// The store keeps its documented skip order: template, malformed id and
// status outside the enum are decided before the service is even looked at,
// so a template naming a bogus service is still reported as a template.
func TestSyncRunbookIndex_ServiceIsCheckedLast(t *testing.T) {
	s := newProjectsSchemaTestStore(t)
	rejectAll := func(string) (string, bool) { return "", false }

	result, err := s.SyncRunbookIndex(RunbookIndexSyncParams{
		Source:         "knowledge-mcp",
		ResolveService: rejectAll,
		Entries: []RunbookIndexEntryInput{
			{ID: "RB-000", VaultPath: "Runbooks/Templates/Auth.md", Title: "tpl", Service: "ghost", Status: "draft"},
			{ID: "nope", VaultPath: "Runbooks/Nope.md", Title: "n", Service: "ghost", Status: "draft"},
			{ID: "RB-002", VaultPath: "Runbooks/RB-002.md", Title: "s", Service: "ghost", Status: "open"},
		},
	})
	if err != nil {
		t.Fatalf("SyncRunbookIndex: %v", err)
	}
	want := []string{"template", "invalid_id", "invalid_status"}
	if len(result.Skipped) != len(want) {
		t.Fatalf("skipped = %+v, want %v", result.Skipped, want)
	}
	for i, reason := range want {
		if result.Skipped[i].Reason != reason {
			t.Fatalf("skipped[%d].reason = %q, want %q", i, result.Skipped[i].Reason, reason)
		}
	}
}

// The "knowledge-mcp" source must never read the absence of needs_review as
// a freshness claim. A non-nil needs_review is the source's own assertion
// and is honored verbatim, true or false; when it is nil, no assertion was
// made at all, and the row's staleness falls back to the age derived from
// last_updated against D-11's threshold — the same rule the "vault-fs"
// source applies to its own age_days. This is what let four runbooks with no
// needs_review field survive as "fresh" long past that threshold.
func TestSyncRunbookIndex_KnowledgeMCPNeedsReviewSemantics(t *testing.T) {
	now := time.Now().UTC()
	old := now.AddDate(0, 0, -(RunbookStaleAgeDays + 1)).Format("2006-01-02")
	fresh := now.AddDate(0, 0, -(RunbookStaleAgeDays - 1)).Format("2006-01-02")
	trueVal, falseVal := true, false

	cases := []struct {
		name        string
		lastUpdated string
		needsReview *bool
		wantStale   bool
	}{
		{"nil needs_review, old last_updated falls back to stale", old, nil, true},
		{"nil needs_review, fresh last_updated stays fresh", fresh, nil, false},
		{"nil needs_review, no last_updated at all stays fresh (no age to derive)", "", nil, false},
		{"explicit false is honored even past the age threshold", old, &falseVal, false},
		{"explicit true is honored even under the age threshold", fresh, &trueVal, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newProjectsSchemaTestStore(t)
			result, err := s.SyncRunbookIndex(RunbookIndexSyncParams{
				Source: "knowledge-mcp",
				Entries: []RunbookIndexEntryInput{{
					ID: "RB-100", VaultPath: "Runbooks/RB-100.md", Title: "x", Service: "nextcloud",
					Category: "auth", Status: "verified", LastUpdated: tc.lastUpdated, NeedsReview: tc.needsReview,
				}},
			})
			if err != nil {
				t.Fatalf("SyncRunbookIndex: %v", err)
			}
			gotStale := result.StaleCount == 1
			if gotStale != tc.wantStale {
				t.Fatalf("stale = %v, want %v (result: %+v)", gotStale, tc.wantStale, result)
			}
		})
	}
}

// Without a resolver the store keeps the lenient fallback, so a direct caller
// that predates the map is not broken by it.
func TestSyncRunbookIndex_NoResolverKeepsLenientFallback(t *testing.T) {
	s := newProjectsSchemaTestStore(t)
	result, err := s.SyncRunbookIndex(RunbookIndexSyncParams{
		Source: "knowledge-mcp",
		Entries: []RunbookIndexEntryInput{
			{ID: "RB-500", VaultPath: "Runbooks/RB-500.md", Title: "x", Service: "Whatever", Category: "auth", Status: "draft"},
		},
	})
	if err != nil {
		t.Fatalf("SyncRunbookIndex: %v", err)
	}
	if result.Upserted != 1 || len(result.Skipped) != 0 {
		t.Fatalf("result = %+v, want the pre-resolver behaviour", result)
	}
}
