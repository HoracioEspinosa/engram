package shared

import (
	"os"
	"path/filepath"
	"strings"
)

// VaultRootEnv and VaultRootDefault resolve the local clone of the knowledge
// vault that runbook_index.vault_path (and project_cards.knowledge_hub_path)
// are relative to (rfc-tui.md §9.1: "Ruta del vault"). Every developer's
// clone of cd-knowledge-mcp can live at a different path, which is exactly
// why the RFC documents an escape hatch instead of a single hardcoded
// location.
const (
	VaultRootEnv     = "ENGRAM_VAULT_ROOT"
	VaultRootDefault = "Projects/ClaroDrive/clarodrive-knowledge-mcp/vault/clarodrive"
)

// VaultRoot resolves
// ${ENGRAM_VAULT_ROOT:-~/Projects/ClaroDrive/clarodrive-knowledge-mcp/vault/clarodrive},
// the same precedence EvidenceRoot applies for captured evidence.
func VaultRoot() string {
	if v := strings.TrimSpace(os.Getenv(VaultRootEnv)); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return VaultRootDefault
	}
	return filepath.Join(home, VaultRootDefault)
}
