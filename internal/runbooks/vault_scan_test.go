package runbooks

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fixedNow pins the clock so age_days assertions are deterministic.
var fixedNow = time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)

func writeVault(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return root
}

const runbookNote = `---
type: runbook
id: RB-003
title: "Preview endpoint slow, returning 503, or serving incorrect sizes"
service: nextcloud
severity: P2
category: performance
status: verified
jira_project: CDBS
related_tickets: []
symptoms:
  - "/core/preview returns HTTP 503"
  - "First fetch to a new image takes 8-17 seconds"
affected_services:
  - nextcloud
  - imaginary
root_cause: "Multiple causes: see the Root Causes table."
tags:
  - type/runbook
  - service/nextcloud
last_updated: 2026-05-15
---

# RB-003: Preview endpoint slow
`

func TestScanVault_IndexesRealRunbookFrontmatter(t *testing.T) {
	root := writeVault(t, map[string]string{
		"Runbooks/Performance/RB-003 Preview Endpoint Slow Or Failing.md": runbookNote,
	})

	result, err := ScanVault(root, fixedNow)
	if err != nil {
		t.Fatalf("ScanVault: %v", err)
	}
	if result.Scanned != 1 || len(result.Entries) != 1 {
		t.Fatalf("scanned=%d entries=%d, want 1/1", result.Scanned, len(result.Entries))
	}

	e := result.Entries[0]
	if e.ID != "RB-003" {
		t.Fatalf("id = %q", e.ID)
	}
	if e.VaultPath != "Runbooks/Performance/RB-003 Preview Endpoint Slow Or Failing.md" {
		t.Fatalf("vault_path = %q", e.VaultPath)
	}
	if e.Title != "Preview endpoint slow, returning 503, or serving incorrect sizes" {
		t.Fatalf("title lost its quoted colon: %q", e.Title)
	}
	if e.Service != "nextcloud" || e.Category != "performance" || e.Status != "verified" || e.Severity != "P2" {
		t.Fatalf("unexpected mapped fields: %+v", e)
	}
	if len(e.Symptoms) != 2 || e.Symptoms[0] != "/core/preview returns HTTP 503" {
		t.Fatalf("symptoms = %#v", e.Symptoms)
	}
	if len(e.Tags) != 2 {
		t.Fatalf("tags = %#v", e.Tags)
	}
	// 2026-05-15 -> 2026-09-05 is 113 days, past the 90-day threshold.
	if e.AgeDays == nil || *e.AgeDays != 113 {
		t.Fatalf("age_days = %v, want 113", e.AgeDays)
	}
	if e.NeedsReview == nil || !*e.NeedsReview {
		t.Fatalf("a 113-day-old note must be stale, got %v", e.NeedsReview)
	}
}

func TestScanVault_FreshNoteIsNotStale(t *testing.T) {
	root := writeVault(t, map[string]string{
		"Runbooks/RB-007 Fresh.md": `---
type: runbook
id: RB-007
title: Fresh
service: middleware
category: auth
status: verified
last_updated: 2026-08-30
---
`,
	})
	result, err := ScanVault(root, fixedNow)
	if err != nil {
		t.Fatalf("ScanVault: %v", err)
	}
	e := result.Entries[0]
	if e.AgeDays == nil || *e.AgeDays != 6 {
		t.Fatalf("age_days = %v, want 6", e.AgeDays)
	}
	if e.NeedsReview == nil || *e.NeedsReview {
		t.Fatalf("a 6-day-old note must not be stale, got %v", e.NeedsReview)
	}
}

func TestScanVault_LastVerifiedWinsOverLastUpdated(t *testing.T) {
	root := writeVault(t, map[string]string{
		"Runbooks/RB-008 Verified.md": `---
type: runbook
id: RB-008
title: Verified recently
service: portal
category: network
status: verified
last_updated: 2020-01-01
last_verified: 2026-09-01
---
`,
	})
	result, err := ScanVault(root, fixedNow)
	if err != nil {
		t.Fatalf("ScanVault: %v", err)
	}
	if e := result.Entries[0]; e.AgeDays == nil || *e.AgeDays != 4 {
		t.Fatalf("age_days = %v, want 4 (from last_verified)", e.AgeDays)
	}
}

func TestScanVault_SkipReasons(t *testing.T) {
	root := writeVault(t, map[string]string{
		"Runbooks/Templates/Auth Issue Template.md": `---
type: runbook
category: auth
status: open
tags: [type/runbook, template]
created: {{date}}
---
`,
		"Runbooks/Inline Tagged Template.md": `---
type: runbook
id: RB-100
title: Tagged template outside the folder
service: middleware
category: auth
status: draft
tags:
  - template
---
`,
		"Runbooks/Telmex Account Diagnostic Playbook.md": `---
type: playbook
title: Telmex diagnostic
service: middleware
---
`,
		"Runbooks/Runbooks.md": `# Map of content, no frontmatter at all
`,
		"Runbooks/RB-009 Unknown Service.md": `---
type: runbook
id: RB-009
title: Outside the canonical list
service: some-service-that-does-not-exist
category: network
status: verified
---
`,
	})

	result, err := ScanVault(root, fixedNow)
	if err != nil {
		t.Fatalf("ScanVault: %v", err)
	}
	if len(result.Entries) != 0 {
		t.Fatalf("expected nothing indexable, got %#v", result.Entries)
	}
	if result.Scanned != 5 {
		t.Fatalf("scanned = %d, want 5", result.Scanned)
	}

	counts := map[string]int{}
	for _, s := range result.Skipped {
		counts[s.Reason]++
	}
	if counts["template"] != 2 {
		t.Fatalf("template skips = %d, want 2 (folder + inline tag): %#v", counts["template"], result.Skipped)
	}
	if counts["not_runbook"] != 2 {
		t.Fatalf("not_runbook skips = %d, want 2 (playbook + no frontmatter)", counts["not_runbook"])
	}
	if counts["unknown_service"] != 1 {
		t.Fatalf("unknown_service skips = %d, want 1", counts["unknown_service"])
	}
}

// TestScanVault_MalformedStatusAndIDReachTheStore checks the split of
// responsibilities: reasons that need the store's enums are not decided here.
func TestScanVault_MalformedStatusAndIDReachTheStore(t *testing.T) {
	root := writeVault(t, map[string]string{
		"Runbooks/Broken.md": `---
type: runbook
id: NOT-AN-ID
title: Broken but still a runbook
service: middleware
category: auth
status: open
---
`,
	})
	result, err := ScanVault(root, fixedNow)
	if err != nil {
		t.Fatalf("ScanVault: %v", err)
	}
	if len(result.Entries) != 1 || len(result.Skipped) != 0 {
		t.Fatalf("expected the entry to be forwarded, got entries=%#v skipped=%#v",
			result.Entries, result.Skipped)
	}
	if result.Entries[0].ID != "NOT-AN-ID" || result.Entries[0].Status != "open" {
		t.Fatalf("values were rewritten instead of forwarded: %+v", result.Entries[0])
	}
}

func TestScanVault_IgnoresNonMarkdownAndNestedDirs(t *testing.T) {
	root := writeVault(t, map[string]string{
		"Runbooks/attachments/diagram.png": "not markdown",
		"Runbooks/Deep/Nested/RB-011 Deep.md": `---
type: runbook
id: RB-011
title: Deeply nested
service: mailing
category: queue
status: draft
---
`,
		"Services/Nextcloud/NC33 Upgrade Runbook.md": `---
type: runbook
id: RB-012
title: Outside the Runbooks folder
service: nextcloud
category: database
status: verified
---
`,
	})

	result, err := ScanVault(root, fixedNow)
	if err != nil {
		t.Fatalf("ScanVault: %v", err)
	}
	if result.Scanned != 1 {
		t.Fatalf("scanned = %d, want 1: only Runbooks/**/*.md counts", result.Scanned)
	}
	if len(result.Entries) != 1 || result.Entries[0].ID != "RB-011" {
		t.Fatalf("unexpected entries: %#v", result.Entries)
	}
}

func TestScanVault_MissingRunbooksFolder(t *testing.T) {
	if _, err := ScanVault(t.TempDir(), fixedNow); err != ErrVaultDirNotFound {
		t.Fatalf("err = %v, want ErrVaultDirNotFound", err)
	}
}

func TestScanVault_RejectsRelativeAndEmptyPaths(t *testing.T) {
	if _, err := ScanVault("relative/path", fixedNow); err == nil {
		t.Fatal("expected a relative vault_dir to be rejected")
	}
	if _, err := ScanVault("   ", fixedNow); err == nil {
		t.Fatal("expected an empty vault_dir to be rejected")
	}
}

func TestScanVault_TitleFallsBackToFilename(t *testing.T) {
	root := writeVault(t, map[string]string{
		"Runbooks/RB-013 Preview Slow.md": `---
type: runbook
id: RB-013
service: imaginary
category: performance
status: draft
---
`,
	})
	result, err := ScanVault(root, fixedNow)
	if err != nil {
		t.Fatalf("ScanVault: %v", err)
	}
	if got := result.Entries[0].Title; got != "Preview Slow" {
		t.Fatalf("title = %q, want %q", got, "Preview Slow")
	}
}

func TestCanonicalService(t *testing.T) {
	for _, raw := range []string{"nextcloud", "  Middleware ", "ENTERPRISE-SERVER", "knowledge-mcp"} {
		if _, ok := CanonicalService(raw); !ok {
			t.Fatalf("CanonicalService(%q) = false, want true", raw)
		}
	}
	for _, raw := range []string{"", "not-a-service", "next cloud"} {
		if slug, ok := CanonicalService(raw); ok {
			t.Fatalf("CanonicalService(%q) = %q, want rejected", raw, slug)
		}
	}
	if got := CanonicalServices(); len(got) != 15 || got[0] > got[1] {
		t.Fatalf("CanonicalServices() = %v, want 15 sorted values", got)
	}
}

func TestParseFrontmatter_Subset(t *testing.T) {
	fm := parseFrontmatter(`---
scalar: plain
quoted: "with: colon"
single: 'apostrophes'
flow: [a, "b, still b", c]
block:
  - one
  - "two"
nested:
  key: value
empty_flow: []
---
body
`)
	if fm.str("scalar") != "plain" {
		t.Fatalf("scalar = %q", fm.str("scalar"))
	}
	if fm.str("quoted") != "with: colon" {
		t.Fatalf("quoted = %q", fm.str("quoted"))
	}
	if fm.str("single") != "apostrophes" {
		t.Fatalf("single = %q", fm.str("single"))
	}
	flow := fm.list("flow")
	if len(flow) != 3 || flow[1] != "b, still b" {
		t.Fatalf("flow = %#v", flow)
	}
	block := fm.list("block")
	if len(block) != 2 || block[1] != "two" {
		t.Fatalf("block = %#v", block)
	}
	// A nested mapping must not leak its inner key into the top level.
	if fm.str("key") != "" {
		t.Fatalf("nested key leaked: %q", fm.str("key"))
	}
	if len(fm.list("empty_flow")) != 0 {
		t.Fatalf("empty_flow = %#v", fm.list("empty_flow"))
	}
}

func TestParseFrontmatter_NoHeaderYieldsNothing(t *testing.T) {
	fm := parseFrontmatter("# Just a heading\n\ntype: runbook\n")
	if fm.str("type") != "" {
		t.Fatalf("body text was parsed as frontmatter: %q", fm.str("type"))
	}
}
