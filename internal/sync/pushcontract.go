package sync

import (
	"strings"

	"github.com/HoracioEspinosa/engram/internal/store"
)

// The typed collections of a cloud chunk — sessions, observations, prompts —
// are upserts. Everything the cloud needs to apply one has to be on the row, so
// a row that cannot be upserted has no business in them; the rules that decide
// that live here, in one place, because the exporter that fills the collections
// and the server that accepts them have to agree or the push fails on rows the
// client believed were fine.

// ObservationIsSoftDeleted reports whether an observation row is a tombstone.
//
// A soft delete keeps the row so the deletion can replicate, and in a cloud
// chunk the deletion travels as its own `delete` mutation. The tombstone
// therefore adds nothing to the typed collections, and it actively harms:
// deletion makes no promise about the row's remaining fields, so a row deleted
// because it had no title is offered to the cloud as an upsert without one and
// the whole chunk is rejected.
//
// This is a cloud-chunk rule. A local filesystem chunk carries no delete
// mutations, so there the tombstone in the collection is the deletion signal
// and the import path turns it back into a delete.
func ObservationIsSoftDeleted(o store.Observation) bool {
	return o.DeletedAt != nil && strings.TrimSpace(*o.DeletedAt) != ""
}
