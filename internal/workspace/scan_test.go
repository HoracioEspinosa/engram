package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/vault"
)

// TestScanEvidenceDryRunWritesNothing is the whole contract of a dry run: the
// same walk, the same plan, and not one row. A "preview" that writes is worse
// than no preview, because it teaches people to approve without reading.
func TestScanEvidenceDryRunWritesNothing(t *testing.T) {
	s := newStore(t)
	root := newVault(t)
	t.Setenv(VaultRootEnv, root)
	task := seedTask(t, s, "koi-garden", "KOI-1042", "hardening-autologin",
		"koi-garden/KOI-1042-hardening-autologin")

	report, err := ScanEvidence(s, "KOI-1042", "", true, vault.Options{})
	if err != nil {
		t.Fatalf("ScanEvidence: %v", err)
	}
	if !report.DryRun {
		t.Error("report does not declare itself a dry run")
	}
	if report.Added != 3 {
		t.Errorf("planned %d additions, want 3 (analysis, patch, evidence)", report.Added)
	}
	if report.TotalBytes == 0 {
		t.Error("a dry run still reads the files, so it knows their size")
	}

	_, total, _, err := s.ListEvidence("koi-garden", store.EvidenceListFilter{TaskSyncID: task.SyncID})
	if err != nil {
		t.Fatalf("ListEvidence: %v", err)
	}
	if total != 0 {
		t.Fatalf("dry run wrote %d evidence rows", total)
	}
}

// TestScanEvidenceIsIdempotent pins that a second scan of an unchanged tree
// reports the same files as already known rather than registering them twice.
func TestScanEvidenceIsIdempotent(t *testing.T) {
	s := newStore(t)
	root := newVault(t)
	t.Setenv(VaultRootEnv, root)
	task := seedTask(t, s, "koi-garden", "KOI-1042", "hardening-autologin",
		"koi-garden/KOI-1042-hardening-autologin")

	first, err := ScanEvidence(s, "KOI-1042", "", false, vault.Options{})
	if err != nil {
		t.Fatalf("first ScanEvidence: %v", err)
	}
	if first.Added != 3 || first.Updated != 0 {
		t.Fatalf("first scan added %d / updated %d, want 3 / 0", first.Added, first.Updated)
	}

	second, err := ScanEvidence(s, "KOI-1042", "", false, vault.Options{})
	if err != nil {
		t.Fatalf("second ScanEvidence: %v", err)
	}
	if second.Added != 0 || second.Updated != 3 {
		t.Fatalf("second scan added %d / updated %d, want 0 / 3", second.Added, second.Updated)
	}

	_, total, _, err := s.ListEvidence("koi-garden", store.EvidenceListFilter{TaskSyncID: task.SyncID})
	if err != nil {
		t.Fatalf("ListEvidence: %v", err)
	}
	if total != 3 {
		t.Fatalf("store holds %d evidence rows after two scans, want 3", total)
	}
}

// TestScanEvidenceRejectsRestrictedSymlink pins that a symlink planted inside
// the vault cannot smuggle a restricted tree into the store: the entry is
// reported, never opened, and never registered.
func TestScanEvidenceRejectsRestrictedSymlink(t *testing.T) {
	s := newStore(t)
	root := newVault(t)
	t.Setenv(VaultRootEnv, root)
	task := seedTask(t, s, "koi-garden", "KOI-1042", "hardening-autologin",
		"koi-garden/KOI-1042-hardening-autologin")

	secrets := t.TempDir()
	writeFile(t, filepath.Join(secrets, "token.txt"), "super-secret")
	link := filepath.Join(root, "koi-garden", "KOI-1042-hardening-autologin", "evidences", "restringido")
	if err := os.Symlink(secrets, link); err != nil {
		t.Skipf("symlinks are not available here: %v", err)
	}

	report, err := ScanEvidence(s, "KOI-1042", "", false, vault.Options{Restricted: []string{secrets}})
	if err != nil {
		t.Fatalf("ScanEvidence: %v", err)
	}

	found := false
	for _, skip := range report.Skipped {
		if skip.Reason == vault.SkipRestricted {
			found = true
		}
	}
	if !found {
		t.Fatalf("no restricted entry reported; skipped = %+v", report.Skipped)
	}

	items, _, _, err := s.ListEvidence("koi-garden", store.EvidenceListFilter{TaskSyncID: task.SyncID})
	if err != nil {
		t.Fatalf("ListEvidence: %v", err)
	}
	for _, item := range items {
		if filepath.Base(item.Path) == "token.txt" {
			t.Fatalf("a restricted file was registered: %s", item.Path)
		}
	}
}

// TestScanEvidenceReportsBenchmarkCandidates pins that a run file is offered
// to the import command instead of being read as measurements by a scan, and
// that the map beside it is not mistaken for a run.
func TestScanEvidenceReportsBenchmarkCandidates(t *testing.T) {
	s := newStore(t)
	root := newVault(t)
	t.Setenv(VaultRootEnv, root)
	seedTask(t, s, "koi-garden", "KOI-1099", "lookup-timeout", "koi-garden/KOI-1099-lookup-timeout")

	report, err := ScanEvidence(s, "KOI-1099", "benchmarks", true, vault.Options{})
	if err != nil {
		t.Fatalf("ScanEvidence: %v", err)
	}
	if len(report.BenchmarkCandidates) != 2 {
		t.Fatalf("reported %v as candidates, want the two runs", report.BenchmarkCandidates)
	}
	for _, candidate := range report.BenchmarkCandidates {
		if filepath.Base(candidate) == benchmarkMapName {
			t.Fatalf("%s is the map, not a run", candidate)
		}
	}
}

// TestScanEvidenceUnknownTaskCarriesItsCode pins that a failure a surface has
// to publish arrives with the code it is published under, so no caller
// re-derives it from the message.
func TestScanEvidenceUnknownTaskCarriesItsCode(t *testing.T) {
	s := newStore(t)
	root := newVault(t)
	t.Setenv(VaultRootEnv, root)

	_, err := ScanEvidence(s, "KOI-9999", "", true, vault.Options{})
	if !errors.Is(err, ErrUnknownTask) {
		t.Fatalf("got %v, want ErrUnknownTask", err)
	}
	if code := CodeOf(err); code != "unknown_task" {
		t.Fatalf("CodeOf = %q, want unknown_task", code)
	}
}

// TestScanEvidenceRejectsUnknownCategory pins that the category set stays
// closed: an unknown folder name is a mistake worth reporting, not a twelfth
// bucket to invent.
func TestScanEvidenceRejectsUnknownCategory(t *testing.T) {
	s := newStore(t)
	root := newVault(t)
	t.Setenv(VaultRootEnv, root)
	seedTask(t, s, "koi-garden", "KOI-1042", "hardening-autologin",
		"koi-garden/KOI-1042-hardening-autologin")

	if _, err := ScanEvidence(s, "KOI-1042", "screenshots", true, vault.Options{}); err == nil {
		t.Fatal("expected an unknown category to be refused")
	}
}

// TestTaskDirFallsBackToTheProjectFolder pins the resolution order: a task
// with no recorded vault path is still found under <project>/<ticket>-<slug>,
// which is how every folder the user has not imported yet is named.
func TestTaskDirFallsBackToTheProjectFolder(t *testing.T) {
	s := newStore(t)
	root := newVault(t)
	task := seedTask(t, s, "koi-garden", "KOI-1042", "hardening-autologin", "")
	task.VaultPath = nil

	dir, err := TaskDir(root, task, vault.Options{})
	if err != nil {
		t.Fatalf("TaskDir: %v", err)
	}
	want := filepath.Join(root, "koi-garden", "KOI-1042-hardening-autologin")
	if dir != want {
		t.Fatalf("TaskDir = %s, want %s", dir, want)
	}
}

// TestTaskDirFindsAFolderLedByTheTicket pins the fallback that makes the scan
// usable before anything has been imported: the store records only the ticket,
// and the folder is named "<TICKET>-<what it was about>".
func TestTaskDirFindsAFolderLedByTheTicket(t *testing.T) {
	s := newStore(t)
	root := newVault(t)
	task := seedTask(t, s, "koi-garden", "KOI-1099", "", "")
	task.Slug = nil
	task.VaultPath = nil

	dir, err := TaskDir(root, task, vault.Options{})
	if err != nil {
		t.Fatalf("TaskDir: %v", err)
	}
	want := filepath.Join(root, "koi-garden", "KOI-1099-lookup-timeout")
	if dir != want {
		t.Fatalf("TaskDir = %s, want %s", dir, want)
	}
}

// TestTaskDirRefusesAPathOutsideTheVault pins that a vault path climbing out
// of the root is refused rather than followed. The tasks table's own CHECK
// already rejects such a path on the way in; this is the second line, for a
// row that reached the table before the constraint existed or through a sync
// from a peer that did not have it.
func TestTaskDirRefusesAPathOutsideTheVault(t *testing.T) {
	s := newStore(t)
	root := newVault(t)
	task := seedTask(t, s, "koi-garden", "KOI-1042", "hardening-autologin", "")
	escape := "../../etc"
	task.VaultPath = &escape

	_, err := TaskDir(root, task, vault.Options{})
	if !errors.Is(err, ErrPathEscapesVault) {
		t.Fatalf("got %v, want ErrPathEscapesVault", err)
	}
	if code := CodeOf(err); code != "path_escapes_vault" {
		t.Fatalf("CodeOf = %q, want path_escapes_vault", code)
	}
}

// TestVaultRootUnresolvedWhenNothingSaysWhere pins that a missing root is
// named as such instead of defaulting to somewhere plausible.
func TestVaultRootUnresolvedWhenNothingSaysWhere(t *testing.T) {
	s := newStore(t)
	t.Setenv(VaultRootEnv, "")

	_, err := VaultRoot(s, "", "koi-garden")
	if !errors.Is(err, ErrVaultRootUnresolved) {
		t.Fatalf("got %v, want ErrVaultRootUnresolved", err)
	}
	if code := CodeOf(err); code != "vault_root_unresolved" {
		t.Fatalf("CodeOf = %q, want vault_root_unresolved", code)
	}
}

// TestResolveTaskAcceptsEveryReferenceForm pins the five spellings a surface
// accepts for a task, including the slug the store's own resolver does not
// know.
func TestResolveTaskAcceptsEveryReferenceForm(t *testing.T) {
	s := newStore(t)
	task := seedTask(t, s, "koi-garden", "KOI-1042", "hardening-autologin",
		"koi-garden/KOI-1042-hardening-autologin")

	for _, ref := range []string{"KOI-1042", task.SyncID, "#" + itoa(task.ID), "hardening-autologin"} {
		resolved, err := ResolveTask(s, ref)
		if err != nil {
			t.Fatalf("ResolveTask(%q): %v", ref, err)
		}
		if resolved.ID != task.ID {
			t.Errorf("ResolveTask(%q) = task %d, want %d", ref, resolved.ID, task.ID)
		}
	}
}

// TestResolveTaskRefusesAnAmbiguousSlug pins that a slug two projects both use
// is refused rather than decided by row order.
func TestResolveTaskRefusesAnAmbiguousSlug(t *testing.T) {
	s := newStore(t)
	seedTask(t, s, "koi-garden", "", "mantenimiento", "koi-garden/mantenimiento")
	seedTask(t, s, "tsukimi-bridge", "", "mantenimiento", "tsukimi-bridge/mantenimiento")

	_, err := ResolveTask(s, "mantenimiento")
	if !errors.Is(err, ErrAmbiguousTask) {
		t.Fatalf("got %v, want ErrAmbiguousTask", err)
	}
	if code := CodeOf(err); code != "ambiguous_task" {
		t.Fatalf("CodeOf = %q, want ambiguous_task", code)
	}
}

// itoa avoids dragging strconv into the assertions for one number.
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
