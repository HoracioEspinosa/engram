package store

import (
	"fmt"
	"testing"
)

// The hot paths measured here are the ones a workspace reads on every screen:
// full-text search, the task list, the card counters, the card selector, the
// runbook search and the global stats. They are seeded from the same store so a
// change to one of them is comparable against the others in the same run.
const (
	benchProjects = 8
	benchTasksPer = 12
	benchObs      = 600
)

// benchRunbookStatuses cycles the three statuses SyncRunbookIndex accepts, so
// the seeded index carries the same mix of stale and fresh rows a real vault
// does rather than a single uniform row repeated.
var benchRunbookStatuses = []string{"verified", "draft", "outdated"}

// benchRunbookCategories cycles the categories the index accepts, so the
// category filter has something to narrow.
var benchRunbookCategories = []string{"auth", "database", "queue", "network", "performance", "data-integrity", "registration"}

// benchTaskKinds cycles the kinds the schema accepts, so the seeded task list
// filters on a real mix instead of one repeated value.
var benchTaskKinds = []string{"feature", "bugfix", "refactor", "incident", "migration", "spike"}

// benchWords feeds the observation bodies. FTS5 ranks by term rarity, so a
// single repeated sentence would make every query degenerate into the same
// posting list and measure nothing about the index.
var benchWords = []string{
	"pond", "koi", "bridge", "lantern", "filter", "pump", "algae", "spawn",
	"heron", "netting", "sluice", "gravel", "aerator", "quarantine", "moss",
}

// seedBenchStore fills a throwaway store with projects cards, tasksPer tasks
// under each card, obs observations spread across the cards, one evidence row
// per task and one runbook per card, then returns it ready to read. Seeding is
// done before the caller resets the timer, so none of it is measured.
func seedBenchStore(b *testing.B, projects, tasksPer, obs int) *Store {
	b.Helper()

	cfg := mustDefaultBenchConfig(b)
	cfg.DataDir = b.TempDir()
	s, err := New(cfg)
	if err != nil {
		b.Fatalf("new store: %v", err)
	}
	b.Cleanup(func() { _ = s.Close() })

	slugs := make([]string, projects)
	for i := range slugs {
		slugs[i] = fmt.Sprintf("koi-project-%02d", i)
	}

	entries := make([]RunbookIndexEntryInput, 0, projects)
	for i, slug := range slugs {
		if _, _, err := s.UpsertProjectCard(UpsertProjectCardParams{Slug: slug}); err != nil {
			b.Fatalf("UpsertProjectCard %q: %v", slug, err)
		}
		if err := s.CreateSession(fmt.Sprintf("bench-session-%02d", i), slug, ""); err != nil {
			b.Fatalf("CreateSession for %q: %v", slug, err)
		}
		entries = append(entries, RunbookIndexEntryInput{
			ID:        fmt.Sprintf("RB-%03d", i+1),
			VaultPath: fmt.Sprintf("Runbooks/%s.md", slug),
			Title:     fmt.Sprintf("%s %s runbook", benchWords[i%len(benchWords)], slug),
			Service:   slug,
			Category:  benchRunbookCategories[i%len(benchRunbookCategories)],
			Status:    benchRunbookStatuses[i%len(benchRunbookStatuses)],
			Symptoms:  []string{benchWords[(i+1)%len(benchWords)], benchWords[(i+2)%len(benchWords)]},
		})

		for t := 0; t < tasksPer; t++ {
			key := fmt.Sprintf("KOI-%d%03d", i, t)
			title := fmt.Sprintf("%s %s task %s", benchWords[t%len(benchWords)], slug, key)
			kind := benchTaskKinds[t%len(benchTaskKinds)]
			res, err := s.UpsertTask(UpsertTaskParams{
				Project: slug,
				JiraKey: &key,
				Title:   &title,
				Kind:    &kind,
			})
			if err != nil {
				b.Fatalf("UpsertTask %q: %v", key, err)
			}
			if _, _, _, err := s.AddEvidence(AddEvidenceParams{
				Task:   res.Task,
				Path:   fmt.Sprintf("%s/%s/screenshots/run.png", slug, key),
				SHA256: fmt.Sprintf("%064x", i*tasksPer+t+1),
				Kind:   "png",
				Proves: fmt.Sprintf("%s run for %s", benchWords[(t+3)%len(benchWords)], key),
			}); err != nil {
				b.Fatalf("AddEvidence for %q: %v", key, err)
			}
		}
	}

	if _, err := s.SyncRunbookIndex(RunbookIndexSyncParams{Source: "vault-fs", Entries: entries}); err != nil {
		b.Fatalf("SyncRunbookIndex: %v", err)
	}

	for o := 0; o < obs; o++ {
		i := o % projects
		if _, err := s.AddObservation(AddObservationParams{
			SessionID: fmt.Sprintf("bench-session-%02d", i),
			Type:      "discovery",
			Project:   slugs[i],
			Title:     fmt.Sprintf("%s note %d", benchWords[o%len(benchWords)], o),
			Content: fmt.Sprintf("%s %s %s observation %d under %s",
				benchWords[o%len(benchWords)],
				benchWords[(o+5)%len(benchWords)],
				benchWords[(o+9)%len(benchWords)],
				o, slugs[i]),
		}); err != nil {
			b.Fatalf("AddObservation %d: %v", o, err)
		}
	}

	return s
}

func mustDefaultBenchConfig(b *testing.B) Config {
	b.Helper()
	cfg, err := DefaultConfig()
	if err != nil {
		b.Fatalf("DefaultConfig: %v", err)
	}
	return cfg
}

func benchProjectSlug() string { return fmt.Sprintf("koi-project-%02d", benchProjects/2) }

func BenchmarkSearch(b *testing.B) {
	s := seedBenchStore(b, benchProjects, benchTasksPer, benchObs)
	opts := SearchOptions{Project: benchProjectSlug(), Limit: 10}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.Search("pond filter", opts); err != nil {
			b.Fatalf("Search: %v", err)
		}
	}
}

func BenchmarkListTasks(b *testing.B) {
	s := seedBenchStore(b, benchProjects, benchTasksPer, benchObs)
	slug := benchProjectSlug()
	filter := TaskListFilter{Limit: 20, StaleAfterHours: 24}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := s.ListTasks(slug, filter); err != nil {
			b.Fatalf("ListTasks: %v", err)
		}
	}
}

func BenchmarkProjectCardCounts(b *testing.B) {
	s := seedBenchStore(b, benchProjects, benchTasksPer, benchObs)
	slug := benchProjectSlug()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.ProjectCardCounts(slug); err != nil {
			b.Fatalf("ProjectCardCounts: %v", err)
		}
	}
}

func BenchmarkListProjectCards(b *testing.B) {
	s := seedBenchStore(b, benchProjects, benchTasksPer, benchObs)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := s.ListProjectCards(true); err != nil {
			b.Fatalf("ListProjectCards: %v", err)
		}
	}
}

func BenchmarkFindRunbooks(b *testing.B) {
	s := seedBenchStore(b, benchProjects, benchTasksPer, benchObs)
	params := RunbookFindParams{Query: "pond koi", IncludeStale: true, Limit: 10}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := s.FindRunbooks(params); err != nil {
			b.Fatalf("FindRunbooks: %v", err)
		}
	}
}

func BenchmarkStats(b *testing.B) {
	s := seedBenchStore(b, benchProjects, benchTasksPer, benchObs)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.Stats(); err != nil {
			b.Fatalf("Stats: %v", err)
		}
	}
}
