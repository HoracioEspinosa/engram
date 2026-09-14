package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/vault"
)

// TestImportVaultPlansFiveTasksWithStates pins the whole read of a vault: the
// five task folders across two projects, the kind each folder name implies,
// and the state the root README's task map records for it — all as a plan,
// with nothing written.
func TestImportVaultPlansFiveTasksWithStates(t *testing.T) {
	s := newStore(t)
	root := newVault(t)

	plan, err := ImportVault(s, root, "", false, true, false, vault.Options{})
	if err != nil {
		t.Fatalf("ImportVault: %v", err)
	}
	if !plan.DryRun {
		t.Error("plan does not declare itself a dry run")
	}
	if len(plan.Tasks) != 5 {
		t.Fatalf("planned %d tasks, want 5: %+v", len(plan.Tasks), plan.Tasks)
	}
	if len(plan.Projects) != 2 {
		t.Fatalf("planned %v, want koi-garden and tsukimi-bridge", plan.Projects)
	}

	want := map[string]struct{ action, state, kind string }{
		"koi-garden/KOI-1042-hardening-autologin":     {ActionCreate, vault.StateDone, "bugfix"},
		"koi-garden/KOI-1099-lookup-timeout":          {ActionCreate, vault.StatePending, "incident"},
		"koi-garden/split-pond-filesharing":           {ActionCreate, vault.StateUnverified, "refactor"},
		"koi-garden/mantenimiento-del-estanque":       {ActionCreate, vault.StateArchived, "feature"},
		"tsukimi-bridge/TSU-204-migracion-thumbnails": {ActionCreate, vault.StatePending, "migration"},
	}
	for _, task := range plan.Tasks {
		expected, ok := want[task.VaultPath]
		if !ok {
			t.Errorf("unexpected task %s", task.VaultPath)
			continue
		}
		if task.Action != expected.action {
			t.Errorf("%s: action %s, want %s", task.VaultPath, task.Action, expected.action)
		}
		if task.State != expected.state {
			t.Errorf("%s: state %q, want %q", task.VaultPath, task.State, expected.state)
		}
		if task.Kind != expected.kind {
			t.Errorf("%s: kind %q, want %q", task.VaultPath, task.Kind, expected.kind)
		}
	}

	items, err := s.ListTasksPage("koi-garden", store.TaskListFilter{States: []string{"open"}})
	if err != nil {
		t.Fatalf("ListTasksPage: %v", err)
	}
	if items.Total != 0 {
		t.Fatalf("the plan wrote %d tasks", items.Total)
	}
}

// TestImportVaultApplyThenDryRunIsNoop is the idempotency contract: what an
// apply wrote, the next plan reports as nothing left to do. An importer that
// keeps finding work on an unchanged tree cannot be run on a schedule.
func TestImportVaultApplyThenDryRunIsNoop(t *testing.T) {
	s := newStore(t)
	root := newVault(t)

	applied, err := ImportVault(s, root, "", true, true, false, vault.Options{})
	if err != nil {
		t.Fatalf("apply ImportVault: %v", err)
	}
	if applied.Evidence.Added == 0 || applied.Benchmarks.Added == 0 {
		t.Fatalf("apply registered nothing: %+v", applied)
	}

	again, err := ImportVault(s, root, "", false, true, false, vault.Options{})
	if err != nil {
		t.Fatalf("second ImportVault: %v", err)
	}
	for _, task := range again.Tasks {
		if task.Action != ActionSkip {
			t.Errorf("%s: action %s on an unchanged tree, want skip", task.VaultPath, task.Action)
		}
	}
	if again.Evidence.Added != 0 {
		t.Errorf("second plan adds %d evidence rows, want 0", again.Evidence.Added)
	}
	if again.Benchmarks.Added != 0 {
		t.Errorf("second plan adds %d benchmarks, want 0", again.Benchmarks.Added)
	}
}

// TestImportVaultAppliesStatesOnlyWhenAsked pins that state is the one column
// a re-import may change, and only with the caller's consent.
func TestImportVaultAppliesStatesOnlyWhenAsked(t *testing.T) {
	s := newStore(t)
	root := newVault(t)

	if _, err := ImportVault(s, root, "koi-garden", true, false, false, vault.Options{}); err != nil {
		t.Fatalf("apply without states: %v", err)
	}
	task, err := ResolveTask(s, "KOI-1042")
	if err != nil {
		t.Fatalf("ResolveTask: %v", err)
	}
	if task.State == vault.StateDone {
		t.Fatal("states were applied although the caller refused them")
	}

	plan, err := ImportVault(s, root, "koi-garden", true, true, false, vault.Options{})
	if err != nil {
		t.Fatalf("apply with states: %v", err)
	}
	updates := 0
	for _, action := range plan.Tasks {
		if action.Action == ActionUpdate {
			updates++
		}
	}
	if updates == 0 {
		t.Fatal("applying states changed nothing")
	}
	task, err = ResolveTask(s, "KOI-1042")
	if err != nil {
		t.Fatalf("ResolveTask: %v", err)
	}
	if task.State != vault.StateDone {
		t.Fatalf("KOI-1042 is %q, want %q", task.State, vault.StateDone)
	}
}

// TestImportVaultSkipsUnderscoreFoldersUnlessAsked pins that quarantine and
// architecture folders stay out of a task list, and that _arquitectura comes
// in as a spike when the caller asks for it.
func TestImportVaultSkipsUnderscoreFoldersUnlessAsked(t *testing.T) {
	s := newStore(t)
	root := newVault(t)
	writeFile(t, filepath.Join(root, "koi-garden", archTaskDir, "README.md"),
		"# Arquitectura del estanque\n\nNotas de diseño.\n")

	plan, err := ImportVault(s, root, "koi-garden", false, false, false, vault.Options{})
	if err != nil {
		t.Fatalf("ImportVault: %v", err)
	}
	for _, task := range plan.Tasks {
		if task.Slug == archTaskDir || task.VaultPath == "koi-garden/_cuarentena" {
			t.Fatalf("%s was imported without --include-arch", task.VaultPath)
		}
	}

	withArch, err := ImportVault(s, root, "koi-garden", false, false, true, vault.Options{})
	if err != nil {
		t.Fatalf("ImportVault --include-arch: %v", err)
	}
	found := false
	for _, task := range withArch.Tasks {
		if task.VaultPath == "koi-garden/"+archTaskDir {
			found = true
			if task.Kind != "spike" {
				t.Errorf("%s: kind %q, want spike", task.VaultPath, task.Kind)
			}
		}
		if task.VaultPath == "koi-garden/_cuarentena" {
			t.Error("_cuarentena is never a task")
		}
	}
	if !found {
		t.Fatal("--include-arch did not import the architecture folder")
	}
}

// TestImportVaultIgnoresTheRunbookTree pins that the runbook tree beside the
// projects is not read as a project: its folders hold no README, so they hold
// no tasks.
func TestImportVaultIgnoresTheRunbookTree(t *testing.T) {
	s := newStore(t)
	root := newVault(t)

	plan, err := ImportVault(s, root, "", false, false, false, vault.Options{})
	if err != nil {
		t.Fatalf("ImportVault: %v", err)
	}
	for _, project := range plan.Projects {
		if project == "Runbooks" {
			t.Fatal("the runbook tree was imported as a project")
		}
	}
}

// TestImportVaultRefusesAMissingTaskMapWhenStatesAreAsked pins that a caller
// who asked for states and cannot have them is told, instead of quietly
// importing a vault with every state left blank.
func TestImportVaultRefusesAMissingTaskMapWhenStatesAreAsked(t *testing.T) {
	s := newStore(t)
	root := newVault(t)
	if err := os.Remove(filepath.Join(root, "README.md")); err != nil {
		t.Fatalf("remove README: %v", err)
	}

	_, err := ImportVault(s, root, "", false, true, false, vault.Options{})
	if !errors.Is(err, ErrVaultReadmeUnparsed) {
		t.Fatalf("got %v, want ErrVaultReadmeUnparsed", err)
	}
	if code := CodeOf(err); code != "vault_readme_unparsed" {
		t.Fatalf("CodeOf = %q, want vault_readme_unparsed", code)
	}

	if _, err := ImportVault(s, root, "", false, false, false, vault.Options{}); err != nil {
		t.Fatalf("a vault with no task map is still importable by its folders: %v", err)
	}
}

// TestImportVaultUnresolvedRootCarriesItsCode pins the failure a surface shows
// when nothing says where the vault is.
func TestImportVaultUnresolvedRootCarriesItsCode(t *testing.T) {
	s := newStore(t)
	t.Setenv(VaultRootEnv, "")

	_, err := ImportVault(s, "", "", false, false, false, vault.Options{})
	if !errors.Is(err, ErrVaultRootUnresolved) {
		t.Fatalf("got %v, want ErrVaultRootUnresolved", err)
	}
}
