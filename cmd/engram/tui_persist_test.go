package main

import (
	"testing"

	"github.com/HoracioEspinosa/engram/internal/project"
	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"
)

// persistStore opens a store over a temporary data dir and seeds the
// remembered settings the case needs.
func persistStore(t *testing.T, settings map[string]string) *store.Store {
	t.Helper()

	s, err := store.New(testConfig(t))
	if err != nil {
		t.Fatalf("open the store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	for key, value := range settings {
		if err := s.SetSetting(key, value); err != nil {
			t.Fatalf("set %s: %v", key, err)
		}
	}
	return s
}

// withDetectedProject makes cwd detection answer with slug, as a git checkout
// would, so the tier below it can be shown to lose.
func withDetectedProject(t *testing.T, slug string) {
	t.Helper()
	old := detectProjectFull
	detectProjectFull = func(string) project.DetectionResult {
		return project.DetectionResult{Project: slug, Source: project.SourceGitRemote}
	}
	t.Cleanup(func() { detectProjectFull = old })
}

// withoutDetectedProject makes cwd detection answer with a directory-name
// guess, which ADR-057 §3 treats as no detection at all.
func withoutDetectedProject(t *testing.T) {
	t.Helper()
	old := detectProjectFull
	detectProjectFull = func(string) project.DetectionResult {
		return project.DetectionResult{Project: "tmp", Source: project.SourceDirBasename}
	}
	t.Cleanup(func() { detectProjectFull = old })
}

// TestResolveTUIProjectPrefersDetectedOverPersisted is the tier that matters:
// opening a terminal inside a repository and being shown a different project's
// workspace is the one failure a remembered choice must not cause.
func TestResolveTUIProjectPrefersDetectedOverPersisted(t *testing.T) {
	withArgs(t, "engram", "tui")
	t.Setenv("ENGRAM_PROJECT", "")
	withDetectedProject(t, "clarodrive")
	s := persistStore(t, map[string]string{lastProjectSettingKey: "engram"})

	if got := resolveTUIProject(s); got != "clarodrive" {
		t.Fatalf("resolveTUIProject = %q, want the detected clarodrive", got)
	}
}

// TestResolveTUIProjectFallsBackToPersisted: a directory that is nobody's
// project reopens on the project the last run was doing work in, rather than
// on an empty selector to pick from again.
func TestResolveTUIProjectFallsBackToPersisted(t *testing.T) {
	withArgs(t, "engram", "tui")
	t.Setenv("ENGRAM_PROJECT", "")
	withoutDetectedProject(t)
	s := persistStore(t, map[string]string{lastProjectSettingKey: "engram"})

	if got := resolveTUIProject(s); got != "engram" {
		t.Fatalf("resolveTUIProject = %q, want the remembered engram", got)
	}
}

// TestResolveTUIProjectStaysEmptyWithNothingRemembered: without a tier that
// answers, the workspace opens on the selector, which is rfc-tui.md §9.1's
// fallback.
func TestResolveTUIProjectStaysEmptyWithNothingRemembered(t *testing.T) {
	withArgs(t, "engram", "tui")
	t.Setenv("ENGRAM_PROJECT", "")
	withoutDetectedProject(t)
	s := persistStore(t, nil)

	if got := resolveTUIProject(s); got != "" {
		t.Fatalf("resolveTUIProject = %q, want the empty selector", got)
	}
}

// TestTheFlagAndTheEnvironmentStillBeatThePersistedProject: the new tier goes
// below every tier that was already there, not beside them.
func TestTheFlagAndTheEnvironmentStillBeatThePersistedProject(t *testing.T) {
	s := persistStore(t, map[string]string{lastProjectSettingKey: "engram"})
	withoutDetectedProject(t)

	t.Run("flag", func(t *testing.T) {
		withArgs(t, "engram", "tui", "--project", "middleware")
		t.Setenv("ENGRAM_PROJECT", "")
		if got := resolveTUIProject(s); got != "middleware" {
			t.Fatalf("resolveTUIProject = %q, want middleware", got)
		}
	})

	t.Run("environment", func(t *testing.T) {
		withArgs(t, "engram", "tui")
		t.Setenv("ENGRAM_PROJECT", "portal")
		if got := resolveTUIProject(s); got != "portal" {
			t.Fatalf("resolveTUIProject = %q, want portal", got)
		}
	})
}

// TestTheRememberedTabOnlyAppliesToItsOwnProject: a tab is a place inside a
// project, not a global preference, so opening a different project on the last
// one's tab would show a screen about somebody else's work.
func TestTheRememberedTabOnlyAppliesToItsOwnProject(t *testing.T) {
	s := persistStore(t, map[string]string{
		lastProjectSettingKey: "engram",
		lastTabSettingKey:     "runbooks",
	})

	if got := resolveTUITab(s, "engram"); got != "runbooks" {
		t.Fatalf("resolveTUITab for the remembered project = %q, want runbooks", got)
	}
	if got := resolveTUITab(s, "clarodrive"); got != "" {
		t.Fatalf("resolveTUITab for another project = %q, want nothing", got)
	}
	if got := resolveTUITab(s, ""); got != "" {
		t.Fatalf("resolveTUITab with no project = %q, want nothing", got)
	}
}

// TestResolveTUIIconsFollowsItsPrecedence: the environment first, then the
// remembered setting, then what the terminal can be trusted to draw. Nerd Font
// icons are never inferred.
func TestResolveTUIIconsFollowsItsPrecedence(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("LANG", "en_US.UTF-8")

	t.Run("the environment wins", func(t *testing.T) {
		t.Setenv("ENGRAM_TUI_ICONS", "nerd")
		s := persistStore(t, map[string]string{iconsSettingKey: "ascii"})
		if got := resolveTUIIcons(s); got != theme.IconModeNerd {
			t.Fatalf("resolveTUIIcons = %v, want nerd", got)
		}
	})

	t.Run("the remembered setting is next", func(t *testing.T) {
		t.Setenv("ENGRAM_TUI_ICONS", "")
		s := persistStore(t, map[string]string{iconsSettingKey: "nerd"})
		if got := resolveTUIIcons(s); got != theme.IconModeNerd {
			t.Fatalf("resolveTUIIcons = %v, want nerd", got)
		}
	})

	t.Run("a terminal that cannot draw falls to ascii", func(t *testing.T) {
		t.Setenv("ENGRAM_TUI_ICONS", "")
		t.Setenv("TERM", "dumb")
		s := persistStore(t, nil)
		if got := resolveTUIIcons(s); got != theme.IconModeASCII {
			t.Fatalf("resolveTUIIcons = %v, want ascii", got)
		}
	})

	t.Run("nerd is never inferred", func(t *testing.T) {
		t.Setenv("ENGRAM_TUI_ICONS", "")
		s := persistStore(t, nil)
		if got := resolveTUIIcons(s); got != theme.IconModeUnicode {
			t.Fatalf("resolveTUIIcons = %v, want unicode", got)
		}
	})
}

// TestOpeningTheWorkspaceRemembersNothingByItself: cmdTUI reads the remembered
// choices; writing them is the running workspace's job, so a start that never
// switched a tab must leave the rows exactly as it found them.
func TestOpeningTheWorkspaceRemembersNothingByItself(t *testing.T) {
	cfg := testConfig(t)
	stubRuntimeHooks(t)
	withArgs(t, "engram", "tui")
	t.Setenv("ENGRAM_TUI_THEME", "")
	t.Setenv("ENGRAM_PROJECT", "")

	captureOutput(t, func() { cmdTUI(cfg) })

	s, err := store.New(cfg)
	if err != nil {
		t.Fatalf("open the store: %v", err)
	}
	defer s.Close()

	for _, key := range []string{lastProjectSettingKey, lastTabSettingKey} {
		if _, ok, err := s.Setting(key); err != nil {
			t.Fatalf("read %s: %v", key, err)
		} else if ok {
			t.Fatalf("%s was written by merely opening the workspace", key)
		}
	}
}
