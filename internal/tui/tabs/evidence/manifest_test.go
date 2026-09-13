package evidence

import (
	"os"
	"path/filepath"
	"testing"
)

func writeManifest(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, manifestFileName), []byte(content), 0o644); err != nil {
		t.Fatalf("write manifest.json: %v", err)
	}
}

func TestReadManifestEntryReturnsNotExistsWhenTheFileIsMissing(t *testing.T) {
	dir := t.TempDir()

	entry, exists, err := readManifestEntry(filepath.Join(dir, "CDBS-10555-01.png"))
	if err != nil {
		t.Fatalf("readManifestEntry: %v", err)
	}
	if exists {
		t.Fatal("exists should be false when manifest.json is not on disk")
	}
	if entry != nil {
		t.Fatalf("entry = %+v, want nil", entry)
	}
}

func TestReadManifestEntryMatchesByBasename(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, `{
		"files": [
			{
				"file": "CDBS-10555-04-strict-rejects-logout.png",
				"sha256": "b1f0e6c2",
				"proves": "the same token redirects 303 to /logout after the version bump",
				"captured_at": "2026-08-23T21:39:14-05:00",
				"config_stamp": "amx.tokenversion.mode=strict",
				"positive_control": "CDBS-10555-03-positive-control-root.png",
				"negative_control": "self",
				"pii_masked": true
			},
			{
				"file": "CDBS-10555-03-positive-control-root.png",
				"proves": "same token, before the bump"
			}
		]
	}`)

	evidencePath := filepath.Join(dir, "CDBS-10555-04-strict-rejects-logout.png")
	entry, exists, err := readManifestEntry(evidencePath)
	if err != nil {
		t.Fatalf("readManifestEntry: %v", err)
	}
	if !exists {
		t.Fatal("exists should be true when manifest.json is present")
	}
	if entry == nil {
		t.Fatal("entry should be found for this file")
	}
	if entry.PositiveControl != "CDBS-10555-03-positive-control-root.png" || entry.NegativeControl != "self" {
		t.Fatalf("entry = %+v, want the matching row's controls", entry)
	}
	if !entry.PIIMasked {
		t.Fatal("pii_masked should round-trip as true")
	}
}

func TestReadManifestEntryExistsButNoMatchingRow(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, `{"files": [{"file": "other-file.png", "proves": "unrelated"}]}`)

	entry, exists, err := readManifestEntry(filepath.Join(dir, "CDBS-10555-04-strict-rejects-logout.png"))
	if err != nil {
		t.Fatalf("readManifestEntry: %v", err)
	}
	if !exists {
		t.Fatal("exists should be true: the manifest.json file is there")
	}
	if entry != nil {
		t.Fatalf("entry = %+v, want nil: no row names this file", entry)
	}
}

func TestReadManifestEntryReportsMalformedJSON(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, `{not valid json`)

	_, exists, err := readManifestEntry(filepath.Join(dir, "a.png"))
	if err == nil {
		t.Fatal("malformed manifest.json should return an error")
	}
	if !exists {
		t.Fatal("exists should still be true: the file is there, just unparsable")
	}
}
