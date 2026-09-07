package app

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Gentleman-Programming/engram/internal/setup"
	"github.com/Gentleman-Programming/engram/internal/store"
	"github.com/Gentleman-Programming/engram/internal/tui/tabs"
	"github.com/Gentleman-Programming/engram/internal/tui/tabs/memory"
	"github.com/Gentleman-Programming/engram/internal/tui/theme"
	"github.com/Gentleman-Programming/engram/internal/version"

	tea "github.com/charmbracelet/bubbletea"
)

// The golden suite freezes the ASCII rendering of every TUI screen at two
// terminal geometries. It is the oracle for structural refactors: package
// layout may change, rendered bytes may not.
//
// Regenerate after an intentional UI change:
//
//	go test ./internal/tui/... -run TestGoldenScreens -update
//
// Determinism relies on three things:
//   - lipgloss falls back to the Ascii color profile because `go test` never
//     attaches a TTY to stdout; renderNoANSI asserts it instead of assuming it.
//   - ENGRAM_TIMEZONE pins timeutil.FormatLocal, so timestamps do not drift
//     with the machine's zone.
//   - every fixture is a literal; no clock, no store, no network is touched.

var updateGolden = flag.Bool("update", false, "rewrite the golden files under testdata/")

const goldenVersion = "1.20.1-golden"

// goldenSize is a terminal geometry the snapshots are frozen at.
type goldenSize struct {
	name          string
	width, height int
}

var goldenSizes = []goldenSize{
	{name: "120x40", width: 120, height: 40},
	{name: "80x24", width: 80, height: 24},
}

// goldenScene is one named screen state rendered through the root View.
type goldenScene struct {
	name  string
	build func(m Model) Model
}

func goldenPtr(s string) *string { return &s }

func goldenStats() *store.Stats {
	return &store.Stats{
		TotalSessions:     12,
		TotalObservations: 348,
		TotalPrompts:      57,
		Projects:          []string{"engram", "clarodrive", "middleware", "portal", "lookup", "mailing"},
	}
}

func goldenObservations() []store.Observation {
	return []store.Observation{
		{ID: 101, Type: "decision", Title: "Adopt WAL mode for the local store", Content: "SQLite WAL keeps readers unblocked while the MCP server writes.", CreatedAt: "2026-01-15 09:30:00", Project: goldenPtr("engram"), SessionID: "session-alpha"},
		{ID: 102, Type: "bugfix", Title: "Session delete refused when observations remain", Content: "Guard the delete path so orphan observations cannot appear.", CreatedAt: "2026-01-15 10:05:00", Project: goldenPtr("engram"), SessionID: "session-alpha", ReviewAfter: goldenPtr("2026-03-01 00:00:00")},
		{ID: 103, Type: "pattern", Title: "Two-line list item", Content: "Header line carries id, type and badges; preview carries content.", CreatedAt: "2026-01-15 11:45:00", Project: goldenPtr("clarodrive"), SessionID: "session-beta", Pinned: true},
		{ID: 104, Type: "note", Title: "Golden files are the refactor oracle", Content: "Rendered bytes must survive a package move untouched.", CreatedAt: "2026-01-16 08:00:00", SessionID: "session-beta"},
	}
}

func goldenSearchResults() []store.SearchResult {
	obs := goldenObservations()
	results := make([]store.SearchResult, 0, len(obs))
	for i, o := range obs {
		results = append(results, store.SearchResult{Observation: o, Rank: float64(i)})
	}
	return results
}

func goldenSessions() []store.SessionSummary {
	return []store.SessionSummary{
		{ID: "session-alpha", Project: "engram", StartedAt: "2026-01-15 09:00:00", Summary: goldenPtr("Refactored the TUI into tab packages"), ObservationCount: 8},
		{ID: "session-beta", Project: "clarodrive", StartedAt: "2026-01-16 07:40:00", ObservationCount: 3},
		{ID: "session-gamma", Project: "middleware", StartedAt: "2026-01-17 12:15:00", Summary: goldenPtr("Traced the recovery lookup"), ObservationCount: 11},
		{ID: "session-delta", Project: "portal", StartedAt: "2026-01-18 16:20:00", ObservationCount: 1},
	}
}

func goldenTimeline() *store.TimelineResult {
	return &store.TimelineResult{
		Focus:        store.Observation{ID: 102, Type: "bugfix", Title: "Session delete refused when observations remain", Content: "Guard the delete path so orphan observations cannot appear in the store."},
		Before:       []store.TimelineEntry{{ID: 101, Type: "decision", Title: "Adopt WAL mode for the local store"}},
		After:        []store.TimelineEntry{{ID: 103, Type: "pattern", Title: "Two-line list item"}},
		SessionInfo:  &store.Session{ID: "session-alpha", Project: "engram"},
		TotalInRange: 3,
	}
}

func goldenAgents() []setup.Agent {
	return []setup.Agent{
		{Name: "claude-code", Description: "Claude Code", InstallDir: "/Users/dev/.claude/plugins"},
		{Name: "opencode", Description: "OpenCode", InstallDir: "/Users/dev/.config/opencode/plugins"},
	}
}

// goldenScenes covers every screen of the router plus the branches that change
// the frame: error banner, copy notice, empty states and modal prompts.
func goldenScenes() []goldenScene {
	return []goldenScene{
		{name: "dashboard-loading", build: func(m Model) Model {
			m.memory.Screen = memory.ScreenDashboard
			return m
		}},
		{name: "dashboard", build: func(m Model) Model {
			m.memory.Screen = memory.ScreenDashboard
			m.memory.Stats = goldenStats()
			m.memory.Cursor = 1
			return m
		}},
		{name: "dashboard-update-available", build: func(m Model) Model {
			m.memory.Screen = memory.ScreenDashboard
			m.memory.Stats = goldenStats()
			m.memory.UpdateStatus = version.StatusUpdateAvailable
			m.memory.UpdateMsg = "Update available: 1.20.1 -> 1.21.0"
			return m
		}},
		{name: "dashboard-update-failed", build: func(m Model) Model {
			m.memory.Screen = memory.ScreenDashboard
			m.memory.Stats = goldenStats()
			m.memory.UpdateStatus = version.StatusCheckFailed
			m.memory.UpdateMsg = "Could not check for updates: GitHub took too long to respond."
			return m
		}},
		{name: "dashboard-with-error", build: func(m Model) Model {
			m.memory.Screen = memory.ScreenDashboard
			m.memory.Stats = goldenStats()
			m.memory.ErrorMsg = "database is locked"
			return m
		}},
		{name: "search-empty-input", build: func(m Model) Model {
			m.memory.Screen = memory.ScreenSearch
			return m
		}},
		{name: "search-typed-query", build: func(m Model) Model {
			m.memory.Screen = memory.ScreenSearch
			m.memory.SearchInput.SetValue("wal mode")
			return m
		}},
		{name: "search-results", build: func(m Model) Model {
			m.memory.Screen = memory.ScreenSearchResults
			m.memory.SearchQuery = "wal"
			m.memory.SearchResults = goldenSearchResults()
			m.memory.Cursor = 1
			return m
		}},
		{name: "search-results-scrolled", build: func(m Model) Model {
			m.memory.Screen = memory.ScreenSearchResults
			m.memory.SearchQuery = "wal"
			m.memory.SearchResults = goldenSearchResults()
			m.memory.Cursor = 3
			m.memory.Scroll = 1
			return m
		}},
		{name: "search-results-empty", build: func(m Model) Model {
			m.memory.Screen = memory.ScreenSearchResults
			m.memory.SearchQuery = "no-such-thing"
			return m
		}},
		{name: "recent", build: func(m Model) Model {
			m.memory.Screen = memory.ScreenRecent
			m.memory.RecentObservations = goldenObservations()
			m.memory.Cursor = 2
			return m
		}},
		{name: "recent-empty", build: func(m Model) Model {
			m.memory.Screen = memory.ScreenRecent
			return m
		}},
		{name: "recent-copy-feedback", build: func(m Model) Model {
			m.memory.Screen = memory.ScreenRecent
			m.memory.RecentObservations = goldenObservations()
			m.memory.CopyFeedback = "✓ Copied!"
			return m
		}},
		{name: "observation-detail-loading", build: func(m Model) Model {
			m.memory.Screen = memory.ScreenObservationDetail
			return m
		}},
		{name: "observation-detail", build: func(m Model) Model {
			m.memory.Screen = memory.ScreenObservationDetail
			obs := goldenObservations()[1]
			obs.ToolName = goldenPtr("Bash")
			obs.Content = strings.Repeat("Guard the delete path so orphan observations cannot appear. ", 6)
			m.memory.SelectedObservation = &obs
			return m
		}},
		{name: "observation-detail-scrolled", build: func(m Model) Model {
			m.memory.Screen = memory.ScreenObservationDetail
			obs := goldenObservations()[1]
			obs.Content = strings.Repeat("Guard the delete path so orphan observations cannot appear. ", 6)
			m.memory.SelectedObservation = &obs
			m.memory.DetailScroll = 2
			return m
		}},
		{name: "timeline-loading", build: func(m Model) Model {
			m.memory.Screen = memory.ScreenTimeline
			return m
		}},
		{name: "timeline", build: func(m Model) Model {
			m.memory.Screen = memory.ScreenTimeline
			m.memory.Timeline = goldenTimeline()
			return m
		}},
		{name: "sessions", build: func(m Model) Model {
			m.memory.Screen = memory.ScreenSessions
			m.memory.Sessions = goldenSessions()
			m.memory.Cursor = 1
			return m
		}},
		{name: "sessions-empty", build: func(m Model) Model {
			m.memory.Screen = memory.ScreenSessions
			return m
		}},
		{name: "sessions-delete-prompt", build: func(m Model) Model {
			m.memory.Screen = memory.ScreenSessions
			m.memory.Sessions = goldenSessions()
			m.memory.SessionDeleteState = memory.SessionDeleteStatePrompt
			m.memory.SessionDeleteID = "session-beta"
			m.memory.SessionDeleteProject = "clarodrive"
			return m
		}},
		{name: "sessions-deleting", build: func(m Model) Model {
			m.memory.Screen = memory.ScreenSessions
			m.memory.Sessions = goldenSessions()
			m.memory.SessionDeleteState = memory.SessionDeleteStateDeleting
			m.memory.SessionDeleteID = "session-beta"
			return m
		}},
		{name: "session-detail", build: func(m Model) Model {
			m.memory.Screen = memory.ScreenSessionDetail
			m.memory.Sessions = goldenSessions()
			m.memory.SelectedSessionIdx = 0
			m.memory.SessionObservations = goldenObservations()
			m.memory.Cursor = 1
			return m
		}},
		{name: "session-detail-empty", build: func(m Model) Model {
			m.memory.Screen = memory.ScreenSessionDetail
			m.memory.Sessions = goldenSessions()
			m.memory.SelectedSessionIdx = 1
			return m
		}},
		{name: "session-detail-not-found", build: func(m Model) Model {
			m.memory.Screen = memory.ScreenSessionDetail
			m.memory.SelectedSessionIdx = 9
			return m
		}},
		{name: "setup-select", build: func(m Model) Model {
			m.memory.Screen = memory.ScreenSetup
			m.memory.SetupAgents = goldenAgents()
			return m
		}},
		{name: "setup-installing-opencode", build: func(m Model) Model {
			m.memory.Screen = memory.ScreenSetup
			m.memory.SetupAgents = goldenAgents()
			m.memory.SetupInstalling = true
			m.memory.SetupInstallingName = "opencode"
			return m
		}},
		{name: "setup-installing-claude-code", build: func(m Model) Model {
			m.memory.Screen = memory.ScreenSetup
			m.memory.SetupAgents = goldenAgents()
			m.memory.SetupInstalling = true
			m.memory.SetupInstallingName = "claude-code"
			return m
		}},
		{name: "setup-allowlist-prompt", build: func(m Model) Model {
			m.memory.Screen = memory.ScreenSetup
			m.memory.SetupAllowlistPrompt = true
			m.memory.SetupResult = &setup.Result{Agent: "claude-code", Destination: "claude plugin system"}
			return m
		}},
		{name: "setup-done-opencode", build: func(m Model) Model {
			m.memory.Screen = memory.ScreenSetup
			m.memory.SetupDone = true
			m.memory.SetupResult = &setup.Result{Agent: "opencode", Destination: "/Users/dev/.config/opencode/plugins", Files: 2}
			return m
		}},
		{name: "setup-done-claude-allowlisted", build: func(m Model) Model {
			m.memory.Screen = memory.ScreenSetup
			m.memory.SetupDone = true
			m.memory.SetupResult = &setup.Result{Agent: "claude-code", Destination: "claude plugin system"}
			m.memory.SetupAllowlistApplied = true
			return m
		}},
		{name: "setup-done-claude-allowlist-error", build: func(m Model) Model {
			m.memory.Screen = memory.ScreenSetup
			m.memory.SetupDone = true
			m.memory.SetupResult = &setup.Result{Agent: "claude-code", Destination: "claude plugin system"}
			m.memory.SetupAllowlistError = "permission denied"
			return m
		}},
		{name: "setup-failed", build: func(m Model) Model {
			m.memory.Screen = memory.ScreenSetup
			m.memory.SetupDone = true
			m.memory.SetupError = "network unreachable"
			return m
		}},
		{name: "cloud-settings", build: func(m Model) Model {
			m.active = tabs.Cloud
			m.cloud.Cursor = 2
			return m
		}},
		{name: "unknown-screen", build: func(m Model) Model {
			m.memory.Screen = memory.Screen(999)
			return m
		}},
	}
}

// renderScene builds the scene at the given size and returns its ASCII frame.
func renderScene(t *testing.T, scene goldenScene, size goldenSize) string {
	t.Helper()

	m := New(nil, goldenVersion, theme.Default(), "")
	sized, _ := m.Update(tea.WindowSizeMsg{Width: size.width, Height: size.height})
	m = sized.(Model)
	m = scene.build(m)

	out := m.View()
	if strings.ContainsRune(out, 0x1b) {
		t.Fatalf("scene %q rendered ANSI escapes: the color profile is not Ascii, so the golden files would not be portable", scene.name)
	}
	return out
}

// renderAll concatenates every scene into one document per terminal size.
func renderAll(t *testing.T, size goldenSize) string {
	t.Helper()

	var b strings.Builder
	for _, scene := range goldenScenes() {
		fmt.Fprintf(&b, "═══ %s ═══\n", scene.name)
		b.WriteString(renderScene(t, scene, size))
		b.WriteString("\n")
	}
	return b.String()
}

func goldenPath(size goldenSize) string {
	return filepath.Join("testdata", "screens-"+size.name+".golden")
}

func TestGoldenScreens(t *testing.T) {
	t.Setenv("ENGRAM_TIMEZONE", "UTC")

	for _, size := range goldenSizes {
		t.Run(size.name, func(t *testing.T) {
			got := renderAll(t, size)
			path := goldenPath(size)

			if *updateGolden {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatalf("create testdata dir: %v", err)
				}
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
				t.Logf("wrote %s (%d bytes)", path, len(got))
				return
			}

			wantBytes, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden (run with -update to create it): %v", err)
			}
			want := string(wantBytes)
			if got == want {
				return
			}

			t.Errorf("rendering drifted from %s\n%s", path, firstDiff(want, got))
		})
	}
}

// TestGoldenScreensAreStable renders twice in the same process and requires an
// identical result, so a snapshot can never encode a clock or a random seed.
func TestGoldenScreensAreStable(t *testing.T) {
	t.Setenv("ENGRAM_TIMEZONE", "UTC")

	for _, size := range goldenSizes {
		first := renderAll(t, size)
		second := renderAll(t, size)
		if first != second {
			t.Errorf("%s rendering is not deterministic across two runs\n%s", size.name, firstDiff(first, second))
		}
	}
}

// firstDiff reports the first line where want and got diverge, with the scene
// header that line belongs to, so a failure names the screen that drifted.
func firstDiff(want, got string) string {
	wantLines := strings.Split(want, "\n")
	gotLines := strings.Split(got, "\n")

	scene := "(before the first scene header)"
	limit := len(wantLines)
	if len(gotLines) > limit {
		limit = len(gotLines)
	}

	for i := 0; i < limit; i++ {
		var w, g string
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if i < len(gotLines) {
			g = gotLines[i]
		}
		if strings.HasPrefix(w, "═══ ") {
			scene = strings.Trim(w, "═ ")
		}
		if w != g {
			return fmt.Sprintf("scene %q, line %d:\n  want: %q\n  got:  %q", scene, i+1, w, g)
		}
	}
	return "files differ but no differing line was found (trailing newline?)"
}
