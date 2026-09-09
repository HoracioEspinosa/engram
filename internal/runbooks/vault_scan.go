package runbooks

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/HoracioEspinosa/engram/internal/store"
)

// runbooksDir is the vault folder the index is built from. Runbooks live
// under it, either at its root or one level down by category.
const runbooksDir = "Runbooks"

// templatesPrefix marks the folder whose notes are skeletons, not runbooks.
const templatesPrefix = runbooksDir + "/Templates/"

// maxNoteBytes caps how much of a note is read. Only the YAML header matters,
// and it always sits in the first few kilobytes; this keeps a stray multi-
// megabyte attachment from being slurped into memory.
const maxNoteBytes = 256 * 1024

// staleAgeDays is D-11's freshness threshold for a filesystem-sourced entry.
const staleAgeDays = 90

// ScanResult is the outcome of walking a vault checkout.
type ScanResult struct {
	// Scanned counts the markdown notes under Runbooks/ that were opened.
	Scanned int
	// Entries are the notes that passed the vault-side filters and are ready
	// for store.SyncRunbookIndex.
	Entries []store.RunbookIndexEntryInput
	// Skipped explains every note that was rejected here, with the same
	// reason vocabulary the store uses for the ones it rejects itself.
	Skipped []store.RunbookSkipped
}

// ErrVaultDirNotFound is returned when vaultDir has no Runbooks/ folder,
// which almost always means the caller pointed at the repository root
// instead of at the vault itself.
var ErrVaultDirNotFound = fmt.Errorf("runbooks: vault directory has no %s/ folder", runbooksDir)

// ScanVault walks vaultDir/Runbooks/**/*.md and builds the index entries.
//
// The filters applied here are the ones that need the note's own text:
// a `type` other than `runbook` (playbooks and MOCs are not indexed in v1),
// a template (by folder or by `template` tag), and an unknown service. The
// remaining rejections — malformed id, status outside the enum — are left to
// store.SyncRunbookIndex so each reason is produced in exactly one place.
//
// The clock is passed in so tests can pin age_days; use time.Now for
// production callers.
func ScanVault(vaultDir string, now time.Time) (ScanResult, error) {
	var result ScanResult

	if strings.TrimSpace(vaultDir) == "" {
		return result, fmt.Errorf("runbooks: vault_dir is required")
	}
	if !filepath.IsAbs(vaultDir) {
		return result, fmt.Errorf("runbooks: vault_dir must be an absolute path")
	}
	root := filepath.Join(vaultDir, runbooksDir)
	info, err := os.Stat(root)
	if os.IsNotExist(err) {
		return result, ErrVaultDirNotFound
	}
	if err != nil {
		return result, fmt.Errorf("runbooks: stat %s: %w", root, err)
	}
	if !info.IsDir() {
		return result, ErrVaultDirNotFound
	}

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(d.Name()), ".md") {
			return nil
		}

		rel, relErr := filepath.Rel(vaultDir, path)
		if relErr != nil {
			return relErr
		}
		vaultPath := filepath.ToSlash(rel)

		doc, readErr := readNoteHead(path)
		if readErr != nil {
			return readErr
		}
		result.Scanned++

		fm := parseFrontmatter(doc)
		entry, skipped := classify(fm, vaultPath, now)
		if skipped != nil {
			result.Skipped = append(result.Skipped, *skipped)
			return nil
		}
		result.Entries = append(result.Entries, *entry)
		return nil
	})
	if walkErr != nil {
		return ScanResult{}, fmt.Errorf("runbooks: walk %s: %w", root, walkErr)
	}
	return result, nil
}

// classify turns one parsed note into either an index entry or a skip.
func classify(fm frontmatter, vaultPath string, now time.Time) (*store.RunbookIndexEntryInput, *store.RunbookSkipped) {
	id := fm.str("id")

	if !strings.EqualFold(fm.str("type"), "runbook") {
		return nil, &store.RunbookSkipped{ID: id, VaultPath: vaultPath, Reason: "not_runbook"}
	}
	if strings.HasPrefix(vaultPath, templatesPrefix) || hasTag(fm.list("tags"), "template") {
		return nil, &store.RunbookSkipped{ID: id, VaultPath: vaultPath, Reason: "template"}
	}

	service, ok := CanonicalService(fm.str("service"))
	if !ok {
		return nil, &store.RunbookSkipped{ID: id, VaultPath: vaultPath, Reason: "unknown_service"}
	}

	title := fm.str("title")
	if title == "" {
		title = titleFromPath(vaultPath)
	}

	entry := store.RunbookIndexEntryInput{
		ID:              id,
		VaultPath:       vaultPath,
		Title:           title,
		Service:         service,
		Category:        strings.ToLower(fm.str("category")),
		Pattern:         strings.ToLower(fm.str("pattern")),
		Severity:        strings.ToUpper(fm.str("severity")),
		Status:          strings.ToLower(fm.str("status")),
		Symptoms:        fm.list("symptoms"),
		Tags:            fm.list("tags"),
		Owner:           fm.str("owner"),
		AutomationLevel: strings.ToLower(fm.str("automation_level")),
		LastUpdated:     fm.str("last_updated"),
		LastVerified:    fm.str("last_verified"),
	}

	if age, ok := ageDays(fm, now); ok {
		entry.AgeDays = &age
		stale := age > staleAgeDays
		entry.NeedsReview = &stale
	}
	return &entry, nil
}

// ageDays derives how old a note is from the freshest date its header
// carries. `last_verified` wins over `last_updated`, which wins over
// `last_occurrence`: the first two record maintenance, the third only records
// when the incident last happened.
func ageDays(fm frontmatter, now time.Time) (int, bool) {
	for _, key := range []string{"last_verified", "last_updated", "last_occurrence"} {
		raw := strings.TrimSpace(fm.str(key))
		if len(raw) < 10 {
			continue
		}
		t, err := time.Parse("2006-01-02", raw[:10])
		if err != nil {
			continue
		}
		days := int(now.UTC().Sub(t.UTC()).Hours() / 24)
		if days < 0 {
			days = 0
		}
		return days, true
	}
	return 0, false
}

func hasTag(tags []string, want string) bool {
	for _, t := range tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == want || strings.HasSuffix(t, "/"+want) {
			return true
		}
	}
	return false
}

// titleFromPath falls back to the note's filename when the header has no
// title, dropping the extension and any `RB-NNN ` prefix.
func titleFromPath(vaultPath string) string {
	base := strings.TrimSuffix(filepath.Base(vaultPath), filepath.Ext(vaultPath))
	if len(base) > 7 && strings.HasPrefix(base, "RB-") && base[6] == ' ' {
		base = base[7:]
	}
	return base
}

// readNoteHead reads at most maxNoteBytes of a note: the YAML header is all
// the index needs.
func readNoteHead(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("runbooks: open %s: %w", path, err)
	}
	defer f.Close()

	head, err := io.ReadAll(io.LimitReader(f, maxNoteBytes))
	if err != nil {
		return "", fmt.Errorf("runbooks: read %s: %w", path, err)
	}
	return string(head), nil
}
