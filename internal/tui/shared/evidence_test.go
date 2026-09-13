package shared

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEvidenceRootHonoursTheEnvOverride(t *testing.T) {
	t.Setenv(EvidenceDirEnv, "/tmp/custom-evidence")

	if got := EvidenceRoot(); got != "/tmp/custom-evidence" {
		t.Fatalf("EvidenceRoot() = %q, want the env override", got)
	}
}

func TestEvidenceRootDefaultsUnderHome(t *testing.T) {
	t.Setenv(EvidenceDirEnv, "")

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory available in this environment")
	}
	want := filepath.Join(home, EvidenceDirDefault)

	if got := EvidenceRoot(); got != want {
		t.Fatalf("EvidenceRoot() = %q, want %q", got, want)
	}
}
