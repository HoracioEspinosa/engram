package shared

import "testing"

func TestVaultRootHonoursTheEnvOverride(t *testing.T) {
	t.Setenv(VaultRootEnv, "/tmp/custom-vault")

	root, ok := VaultRoot()
	if !ok {
		t.Fatal("VaultRoot() ok = false, want true when the env var is set")
	}
	if root != "/tmp/custom-vault" {
		t.Fatalf("VaultRoot() root = %q, want the env override", root)
	}
}

// TestVaultRootReportsUnconfiguredWhenUnset pins the no-default rule: the
// variable has nothing to fall back to, so an unset ENGRAM_VAULT_ROOT must be
// reported as "not configured" (ok=false) rather than resolved to a guessed
// path that may or may not exist on this machine.
func TestVaultRootReportsUnconfiguredWhenUnset(t *testing.T) {
	t.Setenv(VaultRootEnv, "")

	root, ok := VaultRoot()
	if ok {
		t.Fatalf("VaultRoot() ok = true, root = %q, want ok=false when the env var is unset", root)
	}
	if root != "" {
		t.Fatalf("VaultRoot() root = %q, want empty when unconfigured", root)
	}
}

// TestVaultRootReportsUnconfiguredWhenBlank pins the same "not configured"
// outcome for a variable set to whitespace only — indistinguishable from
// unset from a reader's point of view, and from os.Getenv's.
func TestVaultRootReportsUnconfiguredWhenBlank(t *testing.T) {
	t.Setenv(VaultRootEnv, "   ")

	if _, ok := VaultRoot(); ok {
		t.Fatal("VaultRoot() ok = true, want false for a blank value")
	}
}
