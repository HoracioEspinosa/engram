package diagnostic

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/HoracioEspinosa/engram/internal/store"
)

// CheckKnowledgeRefDangling is the doctor check for vault pointers whose
// document is gone (RFC §9.2, §10.5 step 1).
const CheckKnowledgeRefDangling = "knowledge_ref_dangling"

// KnowledgeRefVaultDirEnv names the checkout the pointers are resolved
// against. The weekly LaunchAgent already pulls that checkout before the
// bridge runs, which is what makes the check meaningful there.
const KnowledgeRefVaultDirEnv = "ENGRAM_VAULT_ROOT"

// maxDanglingFindings caps how many individual pointers are reported. The
// count in the evidence is always exact; the findings list is for a human
// reading a terminal.
const maxDanglingFindings = 20

// KnowledgeRefDanglingCheck resolves every stored vault pointer against a
// checkout and reports the ones whose file no longer exists.
//
// engram deliberately does not verify a knowledge_ref when it is written: the
// MCP write path never reads the vault, so a reference is accepted on shape
// alone (§9.2). This check is the other half of that trade — the verification
// happens later, offline, where a missing checkout is an inconvenience rather
// than a failed tool call.
type KnowledgeRefDanglingCheck struct {
	// VaultDir overrides the checkout location. Empty reads
	// ENGRAM_VAULT_ROOT, which is how the LaunchAgent and a developer shell
	// both configure it.
	VaultDir string
}

func (KnowledgeRefDanglingCheck) Code() string { return CheckKnowledgeRefDangling }

func (c KnowledgeRefDanglingCheck) Run(ctx context.Context, scope Scope) (CheckResult, error) {
	_ = ctx
	pointers, err := scope.Store.KnowledgeVaultPointers(scope.Project)
	if err != nil {
		return CheckResult{}, err
	}

	vaultDir := strings.TrimSpace(c.VaultDir)
	if vaultDir == "" {
		vaultDir = strings.TrimSpace(os.Getenv(KnowledgeRefVaultDirEnv))
	}
	if vaultDir == "" {
		return CheckResult{
			CheckID:    c.Code(),
			Result:     StatusOK,
			Severity:   SeverityInfo,
			ReasonCode: c.Code() + "_vault_not_configured",
			Message: fmt.Sprintf("%d vault pointer(s) stored; none checked because %s is not set.",
				len(pointers), KnowledgeRefVaultDirEnv),
			Why:          "Resolving a knowledge_ref needs a checkout of the vault, and engram never guesses one: pointing at the wrong tree would report every reference as dangling.",
			Evidence:     mustJSON(map[string]any{"pointers": len(pointers), "vault_dir": ""}),
			SafeNextStep: "Export " + KnowledgeRefVaultDirEnv + "=<checkout>/vault/clarodrive and run this check again.",
		}, nil
	}

	info, statErr := os.Stat(vaultDir)
	if statErr != nil || !info.IsDir() {
		return CheckResult{
			CheckID:      c.Code(),
			Result:       StatusWarning,
			Severity:     SeverityWarning,
			ReasonCode:   c.Code() + "_vault_unreadable",
			Message:      fmt.Sprintf("The configured vault checkout %q is not a readable directory.", vaultDir),
			Why:          "With no checkout to resolve against, every stored pointer is unverifiable, and a check that cannot run must say so instead of reporting a clean result.",
			Evidence:     mustJSON(map[string]any{"pointers": len(pointers), "vault_dir": vaultDir}),
			SafeNextStep: "Point " + KnowledgeRefVaultDirEnv + " at the vault root — the folder that contains Services/ and Runbooks/.",
		}, nil
	}

	findings := make([]Finding, 0)
	dangling := 0
	checked := 0
	seen := map[string]vaultPointerStatus{}
	for _, pointer := range pointers {
		if pointer.Path == "" {
			continue
		}
		checked++
		key := pointer.Path
		status, ok := seen[key]
		if !ok {
			status = resolveVaultPointer(vaultDir, pointer.Path)
			seen[key] = status
		}
		if status == vaultPointerOK {
			continue
		}
		dangling++
		if len(findings) >= maxDanglingFindings {
			continue
		}

		reasonCode := c.Code()
		message := fmt.Sprintf("%s %q points at %q, which is not in the vault checkout.", pointer.Kind, pointer.Owner, pointer.Path)
		why := "A pointer that no longer resolves silently downgrades the context pack: the agent is told the fact is documented and finds nothing there, so it re-derives it from code."
		safeNextStep := "Pull the vault checkout; if the document really was renamed or removed, restamp the pointer with mem_task_link (or `engram project <slug> tasks link --knowledge-ref`)."
		if status == vaultPointerIsDirectory {
			reasonCode = c.Code() + "_points_to_directory"
			message = fmt.Sprintf("%s %q points at %q, which is a directory; the contract expects a document.", pointer.Kind, pointer.Owner, pointer.Path)
			why = "A pointer must name the document itself: a containing folder reads as \"present\" to a casual look, but the context pack still has nothing to attach and a browsing agent gets a directory listing instead of the fact it was promised."
			safeNextStep = "Point the pointer at the specific document inside that folder (for example its README or hub note), or restamp it with mem_task_link (or `engram project <slug> tasks link --knowledge-ref`)."
		}

		findings = append(findings, Finding{
			CheckID:              c.Code(),
			Severity:             SeverityWarning,
			ReasonCode:           reasonCode,
			Message:              message,
			Why:                  why,
			Evidence:             mustJSON(pointer),
			SafeNextStep:         safeNextStep,
			RequiresConfirmation: true,
		})
	}

	if dangling == 0 {
		return CheckResult{
			CheckID:      c.Code(),
			Result:       StatusOK,
			Severity:     SeverityInfo,
			ReasonCode:   c.Code() + "_ok",
			Message:      fmt.Sprintf("All %d vault pointer(s) resolve in %q.", checked, vaultDir),
			Why:          "Every knowledge_ref, hub path and runbook path stored here names a document that exists in the checkout.",
			Evidence:     mustJSON(map[string]any{"checked": checked, "dangling": 0, "vault_dir": vaultDir}),
			SafeNextStep: "No action required.",
		}, nil
	}

	result := resultFromFindings(c.Code(), map[string]any{"checked": checked, "dangling": dangling}, findings)
	result.Message = fmt.Sprintf("%d of %d vault pointer(s) no longer resolve in %q.", dangling, checked, vaultDir)
	result.Evidence = mustJSON(map[string]any{
		"checked": checked, "dangling": dangling, "reported": len(findings), "vault_dir": vaultDir,
	})
	return result, nil
}

// vaultPointerStatus classifies what a vault-relative pointer resolves to
// inside the checkout: a usable document, a directory standing in for one,
// or nothing at all (including a pointer that escapes the checkout, which is
// treated as missing rather than followed).
type vaultPointerStatus int

const (
	vaultPointerMissing vaultPointerStatus = iota
	vaultPointerIsDirectory
	vaultPointerOK
)

// resolveVaultPointer resolves one vault-relative pointer inside the
// checkout. A pointer that escapes the checkout is treated as missing rather
// than followed: the shape rule already rejects those on write, and an older
// row must not make the check read outside the vault.
func resolveVaultPointer(vaultDir, path string) vaultPointerStatus {
	if strings.HasPrefix(path, "/") || strings.HasPrefix(path, "~") {
		return vaultPointerMissing
	}
	root := filepath.Clean(vaultDir)
	target := filepath.Clean(filepath.Join(root, filepath.FromSlash(path)))
	if target != root && !strings.HasPrefix(target, root+string(filepath.Separator)) {
		return vaultPointerMissing
	}
	info, err := os.Stat(target)
	if err != nil {
		return vaultPointerMissing
	}
	if info.IsDir() {
		return vaultPointerIsDirectory
	}
	return vaultPointerOK
}

// vaultDocumentExists reports whether path resolves to a document (not a
// directory) inside vaultDir. It is the boolean shape resolveVaultPointer
// used to expose before a directory needed its own outcome.
func vaultDocumentExists(vaultDir, path string) bool {
	return resolveVaultPointer(vaultDir, path) == vaultPointerOK
}

// compile-time proof that the store satisfies what this check reads.
var _ interface {
	KnowledgeVaultPointers(string) ([]store.VaultPointer, error)
} = (*store.Store)(nil)
