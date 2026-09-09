package main

import (
	"errors"

	"github.com/HoracioEspinosa/engram/internal/store"
)

// projKnowledgeRefFail answers a rejected --knowledge-ref with the same error
// codes the MCP tools and the HTTP API return (RFC §9.1/§9.2), and reports
// false for any other error so the caller falls through to its own mapping.
func projKnowledgeRefFail(jsonOut bool, err error) bool {
	switch {
	case errors.Is(err, store.ErrKnowledgeRefNotCurated):
		projFail(jsonOut, "knowledge_ref_not_curated", err.Error(),
			map[string]any{"hint": "point --knowledge-ref at a curated document; 90 - Engram/ holds engram's own export"})
	case errors.Is(err, store.ErrKnowledgeRefAbsolute):
		projFail(jsonOut, "absolute_path_rejected", err.Error(),
			map[string]any{"hint": "use the vault-relative form the knowledge tools return, e.g. Services/Nextcloud/Architecture.md"})
	case errors.Is(err, store.ErrKnowledgeRefInvalid):
		projFail(jsonOut, "invalid_knowledge_ref", err.Error(),
			map[string]any{"hint": "a knowledge_ref is a vault-relative .md path with an optional #Anchor"})
	default:
		return false
	}
	return true
}
