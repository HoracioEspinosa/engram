package shared

import (
	"os"
	"strings"
)

// VaultRootEnv names the environment variable that points at the local
// clone of the knowledge vault that runbook_index.vault_path (and
// project_cards.knowledge_hub_path) are relative to. It has no default:
// every developer's clone of cd-knowledge-mcp lives at a different path, and
// a guessed default that happens not to exist on a given machine fails
// silently — the runbook Markdown view would report the runbook as "not
// cloned locally" when the actual problem is that the variable was never
// set. Making the variable mandatory turns that silent failure into a named
// one.
const VaultRootEnv = "ENGRAM_VAULT_ROOT"

// VaultRoot resolves ENGRAM_VAULT_ROOT. ok is false when the variable is
// unset or blank, in which case root is empty and callers must not use it
// to build a path. Distinguishing !ok ("not configured") from ok-but-the-
// path-does-not-exist-on-disk ("configured, missing checkout") is left to
// the caller, because only the caller — the runbook Markdown view's
// viewMarkdown, for instance — knows which of the two messages a reader
// needs.
func VaultRoot() (root string, ok bool) {
	v := strings.TrimSpace(os.Getenv(VaultRootEnv))
	if v == "" {
		return "", false
	}
	return v, true
}
