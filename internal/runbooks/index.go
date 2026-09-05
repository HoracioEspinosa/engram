package runbooks

import "github.com/Gentleman-Programming/engram/internal/store"

// Persister is the store capability SyncIndex needs. Keeping it an interface
// lets a test drive the filters without opening a database, and keeps the
// package free of any other store surface.
type Persister interface {
	SyncRunbookIndex(store.RunbookIndexSyncParams) (store.RunbookSyncResult, error)
}

// SyncIndex is the single domain entry point of the runbook index (RFC §9.3).
// Both sources go through it — the `knowledge-mcp` route, whose entries the
// agent assembles from search_by_metadata/get_document/get_stale_docs, and
// the `vault-fs` route, whose entries ScanVault reads off a checkout — so the
// filters and the skip vocabulary are decided in exactly one place.
//
// Its own contribution is the service map: an entry naming a service outside
// the canonical list is skipped with `unknown_service` instead of quietly
// creating a project card for a typo. Every other filter (template, malformed
// id, status outside the enum) belongs to the store, which applies them in
// its documented order before this one.
func SyncIndex(p Persister, params store.RunbookIndexSyncParams) (store.RunbookSyncResult, error) {
	params.ResolveService = CanonicalService
	return p.SyncRunbookIndex(params)
}
