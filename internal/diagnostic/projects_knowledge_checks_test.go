package diagnostic

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Gentleman-Programming/engram/internal/store"
)

func strp(v string) *string { return &v }

// seedRunbook writes one runbook_index row and returns the store it used.
func seedRunbook(t *testing.T, s *store.Store, id, project string, ageDays int) {
	t.Helper()
	entry := store.RunbookIndexEntryInput{
		ID: id, VaultPath: "Runbooks/Performance/" + id + " Preview.md", Title: "Preview",
		Service: project, Category: "performance", Status: "verified",
	}
	if ageDays > 0 {
		entry.AgeDays = &ageDays
	}
	if _, err := s.SyncRunbookIndex(store.RunbookIndexSyncParams{
		Source: "vault-fs", Entries: []store.RunbookIndexEntryInput{entry},
	}); err != nil {
		t.Fatalf("SyncRunbookIndex: %v", err)
	}
}

func TestRunbookIndexAgeCheck(t *testing.T) {
	t.Run("empty index is reported, not warned about", func(t *testing.T) {
		s := newDiagnosticTestStore(t)
		result, err := RunbookIndexAgeCheck{}.Run(context.Background(), Scope{Store: s, Project: "nextcloud"})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if result.Result != StatusOK || result.ReasonCode != CheckRunbookIndexAge+"_empty" {
			t.Fatalf("result = %+v, want an ok/_empty result", result)
		}
	})

	t.Run("a freshly synced index is ok", func(t *testing.T) {
		s := newDiagnosticTestStore(t)
		seedRunbook(t, s, "RB-003", "nextcloud", 113)
		result, err := RunbookIndexAgeCheck{}.Run(context.Background(), Scope{Store: s, Project: "nextcloud"})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if result.Result != StatusOK || result.ReasonCode != CheckRunbookIndexAge+"_ok" {
			t.Fatalf("result = %+v, want ok", result)
		}
		// The row's own stale flag is a different signal and has to survive
		// into the message: a fresh index full of stale documents is exactly
		// the state an operator must not read as "nothing to do".
		if !strings.Contains(result.Message, "flagged stale") {
			t.Fatalf("message = %q, want the stale row count", result.Message)
		}
	})

	t.Run("an index older than the threshold warns", func(t *testing.T) {
		s, dataDir := newDatedDiagnosticStore(t)
		seedRunbook(t, s, "RB-003", "nextcloud", 10)
		backdateRunbookSync(t, dataDir, "RB-003", runbookIndexMaxAgeDays+5)
		result, err := RunbookIndexAgeCheck{}.Run(context.Background(), Scope{Store: s, Project: "nextcloud"})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if result.Result != StatusWarning || result.ReasonCode != CheckRunbookIndexAge+"_stale_index" {
			t.Fatalf("result = %+v, want a warning about the index age", result)
		}
	})

	t.Run("a database without the projects schema is not a failure", func(t *testing.T) {
		s := newDiagnosticTestStore(t)
		if err := s.DropProjectsSchema(); err != nil {
			t.Fatalf("DropProjectsSchema: %v", err)
		}
		result, err := RunbookIndexAgeCheck{}.Run(context.Background(), Scope{Store: s})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if result.Result != StatusOK || result.ReasonCode != CheckRunbookIndexAge+"_schema_absent" {
			t.Fatalf("result = %+v, want ok/_schema_absent", result)
		}
	})
}

func TestKnowledgeRefDanglingCheck(t *testing.T) {
	seedPointers := func(t *testing.T) *store.Store {
		t.Helper()
		s := newDiagnosticTestStore(t)
		if _, err := s.UpsertTask(store.UpsertTaskParams{
			Project: "nextcloud", JiraKey: strp("CDBS-1"), Title: strp("t"), Kind: strp("incident"),
			KnowledgeRef: strp("Services/Nextcloud/Architecture.md#Object Store"),
		}); err != nil {
			t.Fatalf("UpsertTask: %v", err)
		}
		seedRunbook(t, s, "RB-003", "nextcloud", 113)
		return s
	}

	t.Run("without a configured vault nothing is claimed", func(t *testing.T) {
		s := seedPointers(t)
		t.Setenv(KnowledgeRefVaultDirEnv, "")
		result, err := KnowledgeRefDanglingCheck{}.Run(context.Background(), Scope{Store: s})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if result.Result != StatusOK || result.ReasonCode != CheckKnowledgeRefDangling+"_vault_not_configured" {
			t.Fatalf("result = %+v, want ok/_vault_not_configured", result)
		}
	})

	t.Run("a vault that does not exist is a warning, not a clean bill", func(t *testing.T) {
		s := seedPointers(t)
		result, err := KnowledgeRefDanglingCheck{VaultDir: filepath.Join(t.TempDir(), "ghost")}.
			Run(context.Background(), Scope{Store: s})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if result.Result != StatusWarning || result.ReasonCode != CheckKnowledgeRefDangling+"_vault_unreadable" {
			t.Fatalf("result = %+v, want a warning about the checkout", result)
		}
	})

	t.Run("resolvable pointers pass", func(t *testing.T) {
		s := seedPointers(t)
		vault := t.TempDir()
		writeVaultDoc(t, vault, "Services/Nextcloud/Architecture.md")
		writeVaultDoc(t, vault, "Runbooks/Performance/RB-003 Preview.md")
		result, err := KnowledgeRefDanglingCheck{VaultDir: vault}.Run(context.Background(), Scope{Store: s})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if result.Result != StatusOK || result.ReasonCode != CheckKnowledgeRefDangling+"_ok" {
			t.Fatalf("result = %+v, want ok", result)
		}
	})

	t.Run("a removed document is reported per pointer", func(t *testing.T) {
		s := seedPointers(t)
		vault := t.TempDir()
		// The runbook is there; the curated document the task cites is not.
		writeVaultDoc(t, vault, "Runbooks/Performance/RB-003 Preview.md")
		result, err := KnowledgeRefDanglingCheck{VaultDir: vault}.Run(context.Background(), Scope{Store: s})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if result.Result != StatusWarning {
			t.Fatalf("result = %+v, want a warning", result)
		}
		if len(result.Findings) != 1 || result.Findings[0].ReasonCode != CheckKnowledgeRefDangling {
			t.Fatalf("findings = %+v, want exactly the dangling task pointer", result.Findings)
		}
		if !strings.Contains(result.Findings[0].Message, "Services/Nextcloud/Architecture.md") {
			t.Fatalf("finding does not name the missing document: %q", result.Findings[0].Message)
		}
	})

	t.Run("a directory is not a document", func(t *testing.T) {
		s := seedPointers(t)
		vault := t.TempDir()
		if err := os.MkdirAll(filepath.Join(vault, "Services/Nextcloud/Architecture.md"), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		writeVaultDoc(t, vault, "Runbooks/Performance/RB-003 Preview.md")
		result, err := KnowledgeRefDanglingCheck{VaultDir: vault}.Run(context.Background(), Scope{Store: s})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if result.Result != StatusWarning {
			t.Fatalf("a directory standing in for a note was accepted: %+v", result)
		}
	})
}

func TestVaultDocumentExists_RejectsEscapes(t *testing.T) {
	vault := t.TempDir()
	outside := filepath.Join(filepath.Dir(vault), "outside.md")
	if err := os.WriteFile(outside, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if vaultDocumentExists(vault, "../"+filepath.Base(outside)) {
		t.Fatal("a pointer escaping the checkout was resolved")
	}
	if vaultDocumentExists(vault, outside) {
		t.Fatal("an absolute pointer was resolved")
	}
	writeVaultDoc(t, vault, "Services/Doc.md")
	if !vaultDocumentExists(vault, "Services/Doc.md") {
		t.Fatal("a document inside the checkout was reported missing")
	}
}

func writeVaultDoc(t *testing.T, vault, rel string) {
	t.Helper()
	path := filepath.Join(vault, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte("---\ntype: doc\n---\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// backdateRunbookSync ages a runbook_index row's synced_at. The check reads
// that column and nothing else, so this is the only way to reach the stale
// branch without waiting two weeks.
func backdateRunbookSync(t *testing.T, dataDir, id string, days int) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(dataDir, "engram.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(
		`UPDATE runbook_index SET synced_at = datetime('now', ?) WHERE id = ?`,
		fmt.Sprintf("-%d days", days), id,
	); err != nil {
		t.Fatalf("backdate synced_at: %v", err)
	}
}

// newDatedDiagnosticStore is newDiagnosticTestStore with the data directory
// handed back, so a test can reach the database file itself.
func newDatedDiagnosticStore(t *testing.T) (*store.Store, string) {
	t.Helper()
	cfg, err := store.DefaultConfig()
	if err != nil {
		t.Fatalf("DefaultConfig: %v", err)
	}
	cfg.DataDir = t.TempDir()
	s, err := store.New(cfg)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, cfg.DataDir
}
