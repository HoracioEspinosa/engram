package app

import (
	"time"

	"github.com/HoracioEspinosa/engram/internal/tui/tabs"
)

// refreshTTL is how long a tab's data is taken at face value before a switch
// back to it reloads. It is short enough that a change made elsewhere — by
// the CLI, by an MCP tool, by another engram process — shows up on the next
// visit, and long enough that flicking between tabs is not five queries.
const refreshTTL = 30 * time.Second

// tabFreshness records when each tab last loaded, and which ones are known
// to be out of date.
//
// It is an array rather than a map on purpose: the root is a value, copied on
// every Update, and a map field would be shared by every copy — a reload
// recorded on a model the runtime then discarded would still be visible on
// the one it kept.
type tabFreshness struct {
	at    [tabCount]time.Time
	dirty [tabCount]bool
}

// tabCount is how many IDs tabs declares. A tab added there without widening
// this is caught by tabFreshness.index, which reports an unknown ID rather
// than indexing past the end.
const tabCount = int(tabs.Cloud) + 1

// index reports id's slot, and false when this build has never heard of it.
func (tabFreshness) index(id tabs.ID) (int, bool) {
	i := int(id)
	return i, i >= 0 && i < tabCount
}

// loaded records that id's data is current as of now.
func (f tabFreshness) loaded(id tabs.ID) tabFreshness {
	i, ok := f.index(id)
	if !ok {
		return f
	}
	f.at[i] = time.Now()
	f.dirty[i] = false
	return f
}

// invalidate marks id's data as out of date, so the next switch to it
// reloads whatever its TTL says.
func (f tabFreshness) invalidate(id tabs.ID) tabFreshness {
	i, ok := f.index(id)
	if !ok {
		return f
	}
	f.dirty[i] = true
	return f
}

// invalidateAll marks every tab out of date. Switching projects does this:
// nothing a tab is holding belongs to the project now active.
func (f tabFreshness) invalidateAll() tabFreshness {
	for i := range f.dirty {
		f.dirty[i] = true
	}
	return f
}

// stale reports whether id should reload: never loaded, explicitly
// invalidated, or older than the TTL.
func (f tabFreshness) stale(id tabs.ID, now time.Time) bool {
	i, ok := f.index(id)
	if !ok {
		return true
	}
	if f.dirty[i] || f.at[i].IsZero() {
		return true
	}
	return now.Sub(f.at[i]) >= refreshTTL
}
