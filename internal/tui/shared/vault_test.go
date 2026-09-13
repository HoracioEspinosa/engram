package shared

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVaultRootHonoursTheEnvOverride(t *testing.T) {
	t.Setenv(VaultRootEnv, "/tmp/custom-vault")

	if got := VaultRoot(); got != "/tmp/custom-vault" {
		t.Fatalf("VaultRoot() = %q, want the env override", got)
	}
}

func TestVaultRootDefaultsUnderHome(t *testing.T) {
	t.Setenv(VaultRootEnv, "")

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory available in this environment")
	}
	want := filepath.Join(home, VaultRootDefault)

	if got := VaultRoot(); got != want {
		t.Fatalf("VaultRoot() = %q, want %q", got, want)
	}
}
