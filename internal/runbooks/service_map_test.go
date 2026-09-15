package runbooks

import (
	"os"
	"path/filepath"
	"testing"
)

// writeServicesFile drops a services.json at <dir>/Runbooks/services.json,
// the same path NewServiceMap looks at when given VaultDir: dir.
func writeServicesFile(t *testing.T, dir, content string) {
	t.Helper()
	full := filepath.Join(dir, runbooksDir, servicesFileName)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write services.json: %v", err)
	}
}

func TestServiceMapReadsServicesFile(t *testing.T) {
	root := t.TempDir()
	writeServicesFile(t, root, `{"services":[{"slug":"koi-garden","aliases":["koi","garden"]}]}`)

	sm := NewServiceMap(ServiceMapOptions{VaultDir: root})

	for _, raw := range []string{"koi-garden", "KOI", " garden "} {
		slug, ok := sm.Canonical(raw)
		if !ok || slug != "koi-garden" {
			t.Fatalf("Canonical(%q) = (%q, %v), want (koi-garden, true)", raw, slug, ok)
		}
	}
	// The file does not open the door to anything unlisted: a value outside
	// it and outside the compatibility default is still rejected.
	if slug, ok := sm.Canonical("not-a-service"); ok {
		t.Fatalf("Canonical(not-a-service) = (%q, %v), want rejected", slug, ok)
	}
	// The compatibility default still backs a services.json vault: the file
	// only adds slugs, it does not withdraw the fifteen legacy ones.
	if slug, ok := sm.Canonical("nextcloud"); !ok || slug != "nextcloud" {
		t.Fatalf("Canonical(nextcloud) = (%q, %v), want (nextcloud, true)", slug, ok)
	}
}

func TestServiceMapFallsBackToDefaults(t *testing.T) {
	root := t.TempDir() // no Runbooks/services.json at all

	sm := NewServiceMap(ServiceMapOptions{VaultDir: root})

	if slug, ok := sm.Canonical("  Nextcloud "); !ok || slug != "nextcloud" {
		t.Fatalf("Canonical(nextcloud) = (%q, %v), want (nextcloud, true)", slug, ok)
	}
	if slug, ok := sm.Canonical("not-a-service"); ok {
		t.Fatalf("Canonical(not-a-service) = (%q, %v), want rejected with no file and no resolver", slug, ok)
	}
}

func TestServiceMapIgnoresMalformedFile(t *testing.T) {
	root := t.TempDir()
	writeServicesFile(t, root, `{not valid json`)

	sm := NewServiceMap(ServiceMapOptions{VaultDir: root})

	// A malformed file behaves like no file: the compatibility default still
	// answers, nothing panics or errors out of NewServiceMap.
	if slug, ok := sm.Canonical("nextcloud"); !ok || slug != "nextcloud" {
		t.Fatalf("Canonical(nextcloud) = (%q, %v), want (nextcloud, true)", slug, ok)
	}
}

func TestServiceMapUsesResolver(t *testing.T) {
	var asked []string
	resolver := func(slug string) bool {
		asked = append(asked, slug)
		return slug == "koi-garden-pond-02"
	}
	sm := NewServiceMap(ServiceMapOptions{VaultDir: t.TempDir(), Resolver: resolver})

	slug, ok := sm.Canonical("koi-garden-pond-02")
	if !ok || slug != "koi-garden-pond-02" {
		t.Fatalf("Canonical via resolver = (%q, %v), want (koi-garden-pond-02, true)", slug, ok)
	}
	if len(asked) != 1 || asked[0] != "koi-garden-pond-02" {
		t.Fatalf("resolver was asked %#v, want exactly one call with the normalized slug", asked)
	}

	// A value the resolver rejects, and the default does not know, is still
	// rejected rather than accepted outright.
	if slug, ok := sm.Canonical("not-a-service"); ok {
		t.Fatalf("Canonical(not-a-service) = (%q, %v), want rejected", slug, ok)
	}
	// The compatibility default still runs after a resolver that says no.
	if slug, ok := sm.Canonical("nextcloud"); !ok || slug != "nextcloud" {
		t.Fatalf("Canonical(nextcloud) with a resolver present = (%q, %v), want (nextcloud, true)", slug, ok)
	}
}

func TestServiceMapExplicitFileOverridesEnvAndVaultDir(t *testing.T) {
	// Guards the precedence NewServiceMap documents: an explicit
	// ServicesFile wins even when ENGRAM_RUNBOOK_SERVICES and VaultDir both
	// also resolve to a file.
	root := t.TempDir()
	writeServicesFile(t, root, `{"services":[{"slug":"from-vault-dir"}]}`)

	envFile := filepath.Join(t.TempDir(), "env-services.json")
	if err := os.WriteFile(envFile, []byte(`{"services":[{"slug":"from-env"}]}`), 0o644); err != nil {
		t.Fatalf("write env services file: %v", err)
	}
	t.Setenv(servicesFileEnv, envFile)

	explicit := filepath.Join(t.TempDir(), "explicit-services.json")
	if err := os.WriteFile(explicit, []byte(`{"services":[{"slug":"from-explicit-file"}]}`), 0o644); err != nil {
		t.Fatalf("write explicit file: %v", err)
	}

	sm := NewServiceMap(ServiceMapOptions{VaultDir: root, ServicesFile: explicit})
	if _, ok := sm.Canonical("from-vault-dir"); ok {
		t.Fatal("the vault-relative file must not be consulted when ServicesFile is explicit")
	}
	if _, ok := sm.Canonical("from-env"); ok {
		t.Fatal("ENGRAM_RUNBOOK_SERVICES must not be consulted when ServicesFile is explicit")
	}
	if slug, ok := sm.Canonical("from-explicit-file"); !ok || slug != "from-explicit-file" {
		t.Fatalf("Canonical(from-explicit-file) = (%q, %v), want true", slug, ok)
	}
}

func TestCanonicalServiceHonoursEnvOverride(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), "env-services.json")
	if err := os.WriteFile(envFile, []byte(`{"services":[{"slug":"from-env"}]}`), 0o644); err != nil {
		t.Fatalf("write env services file: %v", err)
	}
	t.Setenv(servicesFileEnv, envFile)

	if slug, ok := CanonicalService("from-env"); !ok || slug != "from-env" {
		t.Fatalf("CanonicalService(from-env) = (%q, %v), want (from-env, true)", slug, ok)
	}
	// The compatibility default keeps answering alongside the env override.
	if slug, ok := CanonicalService("nextcloud"); !ok || slug != "nextcloud" {
		t.Fatalf("CanonicalService(nextcloud) = (%q, %v), want (nextcloud, true)", slug, ok)
	}
}
