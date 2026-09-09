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
	seen := map[string]bool{}
	for _, pointer := range pointers {
		if pointer.Path == "" {
			continue
		}
		checked++
		key := pointer.Path
		if !seen[key] {
			seen[key] = vaultDocumentExists(vaultDir, pointer.Path)
		}
		if seen[key] {
			continue
		}
		dangling++
		if len(findings) >= maxDanglingFindings {
			continue
		}
		findings = append(findings, Finding{
			CheckID:              c.Code(),
			Severity:             SeverityWarning,
			ReasonCode:           c.Code(),
			Message:              fmt.Sprintf("%s %q points at %q, which is not in the vault checkout.", pointer.Kind, pointer.Owner, pointer.Path),
			Why:                  "A pointer that no longer resolves silently downgrades the context pack: the agent is told the fact is documented and finds nothing there, so it re-derives it from code.",
			Evidence:             mustJSON(pointer),
			SafeNextStep:         "Pull the vault checkout; if the document really was renamed or removed, restamp the pointer with mem_task_link (or `engram project <slug> tasks link --knowledge-ref`).",
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

// vaultDocumentExists resolves one vault-relative pointer inside the
// checkout. A pointer that escapes the checkout is treated as missing rather
// than followed: the shape rule already rejects those on write, and an older
// row must not make the check read outside the vault.
func vaultDocumentExists(vaultDir, path string) bool {
	if strings.HasPrefix(path, "/") || strings.HasPrefix(path, "~") {
		return false
	}
	root := filepath.Clean(vaultDir)
	target := filepath.Clean(filepath.Join(root, filepath.FromSlash(path)))
	if target != root && !strings.HasPrefix(target, root+string(filepath.Separator)) {
		return false
	}
	info, err := os.Stat(target)
	return err == nil && !info.IsDir()
}

// compile-time proof that the store satisfies what this check reads.
var _ interface {
	KnowledgeVaultPointers(string) ([]store.VaultPointer, error)
} = (*store.Store)(nil)
