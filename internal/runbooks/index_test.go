package runbooks

import (
	"errors"
	"testing"

	"github.com/Gentleman-Programming/engram/internal/store"
)

// fakePersister records what SyncIndex forwarded, so the filter can be tested
// without a database. It answers the way the store does: nothing skipped,
// every entry upserted.
type fakePersister struct {
	got  store.RunbookIndexSyncParams
	err  error
	seen []store.RunbookIndexEntryInput
}

func (f *fakePersister) SyncRunbookIndex(p store.RunbookIndexSyncParams) (store.RunbookSyncResult, error) {
	f.got = p
	f.seen = p.Entries
	if f.err != nil {
		return store.RunbookSyncResult{}, f.err
	}
	// Mirror the store: apply the resolver it was handed, so a test that
	// forgets to wire it shows up as a missing skip rather than as nothing.
	var result store.RunbookSyncResult
	for _, e := range p.Entries {
		if p.ResolveService == nil {
			result.Upserted++
			continue
		}
		if _, ok := p.ResolveService(e.Service); !ok {
			result.Skipped = append(result.Skipped, store.RunbookSkipped{ID: e.ID, VaultPath: e.VaultPath, Reason: "unknown_service"})
			continue
		}
		result.Upserted++
	}
	return result, nil
}

func TestSyncIndex_WiresTheCanonicalServiceMap(t *testing.T) {
	p := &fakePersister{}
	entries := []store.RunbookIndexEntryInput{
		{ID: "RB-003", VaultPath: "Runbooks/Performance/RB-003 Preview.md", Service: "nextcloud", Status: "verified"},
		{ID: "RB-900", VaultPath: "Runbooks/Other/RB-900 Ghost.md", Service: "not-a-service", Status: "verified"},
	}

	result, err := SyncIndex(p, store.RunbookIndexSyncParams{Source: "knowledge-mcp", Entries: entries})
	if err != nil {
		t.Fatalf("SyncIndex: %v", err)
	}
	if p.got.ResolveService == nil {
		t.Fatal("SyncIndex did not hand the store a service resolver")
	}
	if result.Upserted != 1 {
		t.Fatalf("upserted = %d, want 1", result.Upserted)
	}
	if len(result.Skipped) != 1 || result.Skipped[0].Reason != "unknown_service" || result.Skipped[0].ID != "RB-900" {
		t.Fatalf("skipped = %+v, want one unknown_service for RB-900", result.Skipped)
	}
	if len(p.seen) != 2 {
		t.Fatalf("SyncIndex filtered entries itself (%d forwarded); the store owns the skip order", len(p.seen))
	}
}

func TestSyncIndex_PassesEverythingElseThrough(t *testing.T) {
	p := &fakePersister{}
	params := store.RunbookIndexSyncParams{
		Project:      "nextcloud",
		Source:       "vault-fs",
		PruneMissing: true,
		Entries:      []store.RunbookIndexEntryInput{{ID: "RB-003", Service: "nextcloud", Status: "verified"}},
	}
	if _, err := SyncIndex(p, params); err != nil {
		t.Fatalf("SyncIndex: %v", err)
	}
	if p.got.Project != "nextcloud" || p.got.Source != "vault-fs" || !p.got.PruneMissing {
		t.Fatalf("SyncIndex altered the params it forwards: %+v", p.got)
	}
}

func TestSyncIndex_PropagatesStoreErrors(t *testing.T) {
	boom := errors.New("boom")
	p := &fakePersister{err: boom}
	if _, err := SyncIndex(p, store.RunbookIndexSyncParams{
		Entries: []store.RunbookIndexEntryInput{{ID: "RB-003", Service: "nextcloud"}},
	}); !errors.Is(err, boom) {
		t.Fatalf("SyncIndex swallowed the store error: %v", err)
	}
}
