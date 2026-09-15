package theme

import "strings"

// This file is the only place in the repository that spells a glyph, the same
// way nothing outside this package spells a hex value. A view asks for a
// logical icon — IconTaskBlocked, IconProjectUmbrella — and the active Set
// decides which of the three spellings the terminal in front of the reader can
// actually draw.
//
// Every glyph in the catalogue below occupies exactly one terminal cell in
// every mode. That is not a convention, it is what makes the column solver in
// shared/columns.go able to reserve a single cell for a marker and have the
// rest of the row line up; icons_test.go measures all three modes with the
// same ansi.StringWidth the renderer pads with.

// IconMode is how much a terminal can be trusted to draw.
type IconMode string

const (
	// IconModeUnicode is the default: the basic multilingual plane, which
	// any UTF-8 terminal draws without a patched font.
	IconModeUnicode IconMode = "unicode"
	// IconModeNerd uses the private use codepoints a Nerd Font patches its
	// icons into. It is never inferred — see ResolveIconMode.
	IconModeNerd IconMode = "nerd"
	// IconModeASCII is the last resort for a terminal that cannot be
	// trusted with anything above 0x7e.
	IconModeASCII IconMode = "ascii"
)

// Icon is a logical marker, independent of how it is drawn. Views name these;
// nothing outside this file names a codepoint.
type Icon int

// The catalogue, grouped the way the workspace reads: decorations, then the
// seven store vocabularies, then the chrome.
const (
	// Decorations. The koi, the water it swims in, the lotus above it and the
	// pond around it: the workspace's own marks, used in the wordmark and in
	// empty states.
	IconKoi Icon = iota
	IconWater
	IconLotus
	IconPond

	// Task states, in the order a task moves through them, followed by the
	// three the schema added alongside them.
	IconTaskOpen
	IconTaskAnalysis
	IconTaskInProgress
	IconTaskReview
	IconTaskVerified
	IconTaskDone
	IconTaskBlocked
	IconTaskCancelled
	IconTaskPending
	IconTaskArchived
	IconTaskUnverified

	// Task kinds.
	IconTaskFeature
	IconTaskBugfix
	IconTaskRefactor
	IconTaskIncident
	IconTaskMigration
	IconTaskSpike

	// Evidence categories: the eleven folders a vault's evidence lands in.
	IconEvidenceAnalysis
	IconEvidencePlans
	IconEvidenceRunbooks
	IconEvidenceReports
	IconEvidencePatches
	IconEvidenceEvidences
	IconEvidenceQA
	IconEvidenceBenchmarks
	IconEvidenceScripts
	IconEvidenceAssets
	IconEvidenceExports

	// Evidence media. The nineteen file kinds the schema admits collapse onto
	// these six: a reader scanning a list wants to know whether a row is a
	// picture or a log, not which of five image formats it is.
	IconEvidenceImage
	IconEvidenceVideo
	IconEvidenceData
	IconEvidenceText
	IconEvidenceDiff
	IconEvidenceFile

	// Runbook categories.
	IconRunbookAuth
	IconRunbookDatabase
	IconRunbookQueue
	IconRunbookNetwork
	IconRunbookPerformance
	IconRunbookDataIntegrity
	IconRunbookRegistration

	// Project kinds.
	IconProjectUmbrella
	IconProjectRepo
	IconProjectInstance
	IconProjectService
	IconProjectDataset
	IconProjectKnowledge

	// Sync lifecycle, matching the six values store.ProjectHealth reports.
	IconSyncHealthy
	IconSyncPending
	IconSyncRunning
	IconSyncIdle
	IconSyncDisabled
	IconSyncDegraded

	// Tabs, in tab-bar order.
	IconTabHome
	IconTabMemory
	IconTabTasks
	IconTabEvidence
	IconTabBenchmarks
	IconTabRunbooks
	IconTabGraph
	IconTabSettings

	// Chrome.
	IconChevronRight
	IconChevronDown
	IconTree
	IconSearch
	IconFilter
	IconRefresh
	IconLink
	IconCopy
	IconTrendUp
	IconTrendDown
	IconTrendFlat
	IconFresh
	IconStale
	IconGodNode

	// Typography. The marks that punctuate a line rather than stand for a
	// thing: the bullet between two hints, the dot between two fields of
	// metadata, the dash before a subtitle, the delta of a benchmark column
	// and the two arrows a key label names. They are here for the same
	// reason every other glyph is — under IconModeASCII the terminal cannot
	// draw them, and a separator that arrives as a replacement character is
	// as unreadable as a state marker that does.
	IconHintSeparator
	IconMetaSeparator
	IconEmDash
	IconDelta
	IconArrowUp
	IconArrowDown

	// IconUnknown is what every vocabulary lookup falls back to. It is a
	// visible mark on purpose: a value this build has never heard of — a row
	// written by a newer one — should read as "something is here that I
	// cannot name", never as an empty cell.
	IconUnknown

	// iconCount bounds the catalogue. It is deliberately the last constant,
	// so adding an icon above it extends the array without touching anything
	// else.
	iconCount
)

// glyphs is one icon's three spellings. name exists for failure messages: a
// test that says "icon 47" tells nobody anything.
type glyphs struct {
	name    string
	nerd    string
	unicode string
	ascii   string
}

// catalog is the whole vocabulary. The nerd column is verified against the
// cmap of the patched font the workspace is developed on, so each entry is a
// codepoint that exists rather than one that ought to; the unicode column
// stays in the basic multilingual plane and out of the private use areas, and
// the ascii column is one printable byte.
var catalog = [iconCount]glyphs{
	IconKoi:   {"koi", "", "◈", ">"},            // nf-fa-fish
	IconWater: {"water", "\U000f078d", "≈", "~"}, // nf-md-waves
	IconLotus: {"lotus", "\U000f024a", "❀", "*"}, // nf-md-flower
	IconPond:  {"pond", "\U000f0ef3", "∪", "u"},  // nf-md-fishbowl

	IconTaskOpen:       {"task-open", "", "○", "o"},        // nf-oct-issue_opened
	IconTaskAnalysis:   {"task-analysis", "", "◔", "a"},    // nf-cod-telescope
	IconTaskInProgress: {"task-in-progress", "", "◑", ">"}, // nf-cod-debug_start
	IconTaskReview:     {"task-review", "", "◕", "r"},      // nf-cod-eye
	IconTaskVerified:   {"task-verified", "", "◉", "v"},    // nf-cod-verified_filled
	IconTaskDone:       {"task-done", "", "●", "+"},        // nf-oct-issue_closed
	IconTaskBlocked:    {"task-blocked", "", "⊘", "!"},     // nf-cod-circle_slash
	IconTaskCancelled:  {"task-cancelled", "", "⊗", "x"},   // nf-cod-close
	IconTaskPending:    {"task-pending", "", "◒", "p"},     // nf-cod-history
	IconTaskArchived:   {"task-archived", "", "⊖", "#"},    // nf-cod-archive
	IconTaskUnverified: {"task-unverified", "", "◌", "?"},  // nf-cod-question

	IconTaskFeature:   {"task-feature", "", "✦", "+"},   // nf-cod-rocket
	IconTaskBugfix:    {"task-bugfix", "", "✗", "x"},    // nf-cod-bug
	IconTaskRefactor:  {"task-refactor", "", "⇄", "~"},  // nf-cod-tools
	IconTaskIncident:  {"task-incident", "", "✹", "!"},  // nf-cod-flame
	IconTaskMigration: {"task-migration", "", "⋔", ">"}, // nf-cod-git_merge
	IconTaskSpike:     {"task-spike", "", "↯", "?"},     // nf-cod-lightbulb

	IconEvidenceAnalysis:   {"evidence-analysis", "", "◎", "a"},            // nf-cod-telescope
	IconEvidencePlans:      {"evidence-plans", "", "≡", "p"},               // nf-cod-milestone
	IconEvidenceRunbooks:   {"evidence-runbooks", "", "▤", "k"},            // nf-cod-book
	IconEvidenceReports:    {"evidence-reports", "", "▦", "r"},             // nf-cod-report
	IconEvidencePatches:    {"evidence-patches", "", "≠", "d"},             // nf-cod-diff
	IconEvidenceEvidences:  {"evidence-evidences", "", "▪", "e"},           // nf-cod-file_media
	IconEvidenceQA:         {"evidence-qa", "", "✓", "q"},                  // nf-cod-check_all
	IconEvidenceBenchmarks: {"evidence-benchmarks", "\U000f012a", "∿", "b"}, // nf-md-chart_line
	IconEvidenceScripts:    {"evidence-scripts", "", "❯", "s"},             // nf-cod-terminal
	IconEvidenceAssets:     {"evidence-assets", "", "▨", "m"},              // nf-cod-layers
	IconEvidenceExports:    {"evidence-exports", "", "↥", "x"},             // nf-cod-export

	IconEvidenceImage: {"evidence-image", "", "▣", "i"}, // nf-cod-file_media
	IconEvidenceVideo: {"evidence-video", "", "▸", "v"}, // nf-cod-play
	IconEvidenceData:  {"evidence-data", "", "≣", "d"},  // nf-cod-database
	IconEvidenceText:  {"evidence-text", "", "¶", "t"},  // nf-cod-note
	IconEvidenceDiff:  {"evidence-diff", "", "≠", "p"},  // nf-cod-diff
	IconEvidenceFile:  {"evidence-file", "", "▫", "f"},  // nf-cod-file

	IconRunbookAuth:          {"runbook-auth", "", "⊛", "a"},           // nf-cod-key
	IconRunbookDatabase:      {"runbook-database", "", "≣", "d"},       // nf-cod-database
	IconRunbookQueue:         {"runbook-queue", "", "⇶", "q"},          // nf-cod-inbox
	IconRunbookNetwork:       {"runbook-network", "", "⇅", "n"},        // nf-cod-radio_tower
	IconRunbookPerformance:   {"runbook-performance", "", "∿", "f"},    // nf-cod-pulse
	IconRunbookDataIntegrity: {"runbook-data-integrity", "", "⊡", "i"}, // nf-cod-shield
	IconRunbookRegistration:  {"runbook-registration", "", "⊕", "g"},   // nf-cod-account

	IconProjectUmbrella:  {"project-umbrella", "", "▲", "u"},  // nf-cod-project
	IconProjectRepo:      {"project-repo", "", "◆", "r"},      // nf-cod-repo
	IconProjectInstance:  {"project-instance", "", "◇", "i"},  // nf-cod-repo_forked
	IconProjectService:   {"project-service", "", "⊞", "s"},   // nf-cod-server
	IconProjectDataset:   {"project-dataset", "", "≣", "d"},   // nf-cod-database
	IconProjectKnowledge: {"project-knowledge", "", "▤", "k"}, // nf-cod-book

	IconSyncHealthy:  {"sync-healthy", "\U000f0160", "✓", "y"},  // nf-md-cloud_check
	IconSyncPending:  {"sync-pending", "", "↑", "^"},           // nf-cod-cloud_upload
	IconSyncRunning:  {"sync-running", "", "↓", "v"},           // nf-cod-cloud_download
	IconSyncIdle:     {"sync-idle", "", "○", "o"},              // nf-cod-cloud
	IconSyncDisabled: {"sync-disabled", "\U000f0164", "⊘", "-"}, // nf-md-cloud_off_outline
	IconSyncDegraded: {"sync-degraded", "", "⊗", "!"},          // nf-cod-error

	IconTabHome:       {"tab-home", "", "⌂", "0"},                // nf-cod-home
	IconTabMemory:     {"tab-memory", "", "≣", "1"},              // nf-cod-database
	IconTabTasks:      {"tab-tasks", "", "✓", "2"},               // nf-cod-tasklist
	IconTabEvidence:   {"tab-evidence", "", "▪", "3"},            // nf-cod-file_media
	IconTabBenchmarks: {"tab-benchmarks", "\U000f0128", "∿", "4"}, // nf-md-chart_bar
	IconTabRunbooks:   {"tab-runbooks", "", "▤", "5"},            // nf-cod-book
	IconTabGraph:      {"tab-graph", "", "⋈", "6"},               // nf-cod-graph
	IconTabSettings:   {"tab-settings", "", "⌘", "7"},            // nf-cod-settings_gear

	IconChevronRight: {"chevron-right", "", "›", ">"},       // nf-cod-chevron_right
	IconChevronDown:  {"chevron-down", "", "⌄", "v"},        // nf-cod-chevron_down
	IconTree:         {"tree", "", "├", "|"},                // nf-cod-list_tree
	IconSearch:       {"search", "", "⌕", "?"},              // nf-cod-search
	IconFilter:       {"filter", "", "▽", "="},              // nf-cod-filter
	IconRefresh:      {"refresh", "", "↻", "@"},             // nf-cod-refresh
	IconLink:         {"link", "", "⇔", "&"},                // nf-cod-link
	IconCopy:         {"copy", "", "⧉", "c"},                // nf-fa-files_o
	IconTrendUp:      {"trend-up", "\U000f0535", "⇑", "^"},   // nf-md-trending_up
	IconTrendDown:    {"trend-down", "\U000f0533", "⇓", "_"}, // nf-md-trending_down
	IconTrendFlat:    {"trend-flat", "\U000f0534", "⇒", "-"}, // nf-md-trending_neutral
	IconFresh:        {"fresh", "", "●", "+"},               // nf-cod-pass_filled
	IconStale:        {"stale", "", "▲", "!"},               // nf-cod-warning
	IconGodNode:      {"god-node", "", "★", "*"},            // nf-cod-star_full

	// The typographic marks keep their Unicode spelling under nerd: they are
	// punctuation, not icons a patched font redraws, and swapping them for a
	// private-use codepoint would only make them font-dependent for nothing.
	IconHintSeparator: {"hint-separator", "\u2022", "\u2022", "*"},
	IconMetaSeparator: {"meta-separator", "\u00b7", "\u00b7", "-"},
	IconEmDash:        {"em-dash", "\u2014", "\u2014", "-"},
	IconDelta:         {"delta", "\u0394", "\u0394", "d"},
	IconArrowUp:       {"arrow-up", "\u2191", "\u2191", "^"},
	IconArrowDown:     {"arrow-down", "\u2193", "\u2193", "v"},

	IconUnknown: {"unknown", "", "·", "."}, // nf-cod-dash
}

// Ellipsis is the mark that says text was cut.
//
// It is a constant rather than a catalogue entry, and it is the one mark this
// package spells the same way in all three modes. Two things read it and
// neither has a mode to read it with: shared.Truncate, a pure width helper
// reached from the column solver and from paths that carry no style set, pays
// for the marker out of the same budget it caps the text at; and the golden
// structural lint recognises a truncated field by this exact rune. Making the
// spelling depend on the terminal would push a style set through the column
// solver, and make a frozen screen's lint depend on the environment that drew
// it, to replace a mark every UTF-8 terminal already draws in one cell.
const Ellipsis = "…"

// Separator is the mark that joins two fields of metadata on one line, with
// the blanks around it the caller would otherwise have to remember.
func (s Set) Separator() string { return " " + s.Glyph(IconMetaSeparator) + " " }

// HintSeparator is the mark that joins two hints in a footer or in the status
// bar, blanks included.
func (s Set) HintSeparator() string { return " " + s.Glyph(IconHintSeparator) + " " }

// Dash is the mark that separates a heading from what qualifies it, blanks
// included.
func (s Set) Dash() string { return " " + s.Glyph(IconEmDash) + " " }

// Set draws one mode's spelling of the catalogue.
//
// It holds nothing but the mode: the catalogue itself is package state that
// never changes, so a Set copies by value the way a Styles does and two tabs
// carrying one can never disagree.
type Set struct {
	mode IconMode
}

// Icons returns the set for a mode. An unrecognised mode is the default one —
// a caller that mis-spells a mode gets readable glyphs, not blank cells.
func Icons(mode IconMode) Set { return Set{mode: normalizeIconMode(mode)} }

// Mode reports which spelling this set draws.
func (s Set) Mode() IconMode { return normalizeIconMode(s.mode) }

// Glyph returns one icon's spelling, always exactly one terminal cell wide.
// An icon outside the catalogue draws the neutral marker.
func (s Set) Glyph(icon Icon) string {
	if icon < 0 || icon >= iconCount {
		icon = IconUnknown
	}
	entry := catalog[icon]
	switch normalizeIconMode(s.mode) {
	case IconModeNerd:
		return entry.nerd
	case IconModeASCII:
		return entry.ascii
	default:
		return entry.unicode
	}
}

// normalizeIconMode folds a mode to one of the three, defaulting to unicode.
func normalizeIconMode(mode IconMode) IconMode {
	switch IconMode(strings.ToLower(strings.TrimSpace(string(mode)))) {
	case IconModeNerd:
		return IconModeNerd
	case IconModeASCII:
		return IconModeASCII
	default:
		return IconModeUnicode
	}
}

// ResolveIconMode picks the mode from the four places that may have an
// opinion, in the order the design fixes: ENGRAM_TUI_ICONS, then
// settings['tui.icons'], then ascii for a terminal that cannot draw more,
// then unicode.
//
// Nerd is never inferred. A terminal naming a patched font in TERM, or a
// locale that mentions one, proves only that somebody named it — not that the
// font is installed on the machine actually drawing the screen. Guessing wrong
// costs a screen of replacement characters, and the reader has no way to know
// that is what happened, so the mode stays opt-in through one of the two tiers
// a person sets deliberately. What the environment may do is push the mode
// *down*: a dumb terminal or a locale without UTF-8 cannot draw the default,
// and that is a fact rather than a guess.
func ResolveIconMode(env, setting, term, lang string) IconMode {
	for _, candidate := range []string{env, setting} {
		trimmed := strings.ToLower(strings.TrimSpace(candidate))
		if trimmed == "" {
			continue
		}
		switch IconMode(trimmed) {
		case IconModeUnicode, IconModeNerd, IconModeASCII:
			return IconMode(trimmed)
		}
		// An unrecognised value is not an instruction. Fall through to the
		// next tier rather than honouring a typo as if it were a choice.
	}
	if !terminalDrawsUnicode(term, lang) {
		return IconModeASCII
	}
	return IconModeUnicode
}

// terminalDrawsUnicode reports whether the terminal can be expected to draw
// anything above 0x7e: a TERM that is not "dumb", and a locale that says
// UTF-8.
func terminalDrawsUnicode(term, lang string) bool {
	if strings.EqualFold(strings.TrimSpace(term), "dumb") {
		return false
	}
	normalized := strings.ToLower(strings.TrimSpace(lang))
	normalized = strings.ReplaceAll(normalized, "-", "")
	return strings.Contains(normalized, "utf8")
}

// TaskState returns the marker for one of the eleven states a task can be in.
func (s Set) TaskState(state string) string {
	return s.Glyph(lookupIcon(taskStateIcons, state))
}

// TaskKind returns the marker for one of the six kinds of work a task is.
func (s Set) TaskKind(kind string) string {
	return s.Glyph(lookupIcon(taskKindIcons, kind))
}

// EvidenceCategory returns the marker for one of the eleven folders evidence
// is filed under.
func (s Set) EvidenceCategory(category string) string {
	return s.Glyph(lookupIcon(evidenceCategoryIcons, category))
}

// EvidenceKind returns the marker for a file kind, grouped by medium: the
// nineteen kinds the schema admits read as six, because a list of evidence is
// scanned for "is this a picture or a log", not for which image format.
func (s Set) EvidenceKind(kind string) string {
	return s.Glyph(lookupIcon(evidenceKindIcons, kind))
}

// RunbookCategory returns the marker for one of the seven runbook categories.
func (s Set) RunbookCategory(category string) string {
	return s.Glyph(lookupIcon(runbookCategoryIcons, category))
}

// ProjectKind returns the marker for one of the six kinds of project card.
func (s Set) ProjectKind(kind string) string {
	return s.Glyph(lookupIcon(projectKindIcons, kind))
}

// SyncState returns the marker for one of the six lifecycle values
// store.ProjectHealth reports.
func (s Set) SyncState(state string) string {
	return s.Glyph(lookupIcon(syncStateIcons, state))
}

// lookupIcon folds a value the way every surface that writes one does —
// trimmed and lowercased — and answers IconUnknown for anything the build has
// not heard of. A vocabulary the store grows past this binary is the case this
// exists for: an unfamiliar value draws a neutral mark instead of nothing.
func lookupIcon(table map[string]Icon, value string) Icon {
	if icon, ok := table[strings.ToLower(strings.TrimSpace(value))]; ok {
		return icon
	}
	return IconUnknown
}

// The seven vocabularies, spelled with the exact values the store's own CHECK
// constraints admit.
var (
	taskStateIcons = map[string]Icon{
		"open":        IconTaskOpen,
		"analysis":    IconTaskAnalysis,
		"in_progress": IconTaskInProgress,
		"review":      IconTaskReview,
		"verified":    IconTaskVerified,
		"done":        IconTaskDone,
		"blocked":     IconTaskBlocked,
		"cancelled":   IconTaskCancelled,
		"pending":     IconTaskPending,
		"archived":    IconTaskArchived,
		"unverified":  IconTaskUnverified,
	}

	taskKindIcons = map[string]Icon{
		"feature":   IconTaskFeature,
		"bugfix":    IconTaskBugfix,
		"refactor":  IconTaskRefactor,
		"incident":  IconTaskIncident,
		"migration": IconTaskMigration,
		"spike":     IconTaskSpike,
	}

	evidenceCategoryIcons = map[string]Icon{
		"analysis":     IconEvidenceAnalysis,
		"plans":        IconEvidencePlans,
		"runbooks":     IconEvidenceRunbooks,
		"reports":      IconEvidenceReports,
		"patches":      IconEvidencePatches,
		"evidences":    IconEvidenceEvidences,
		"evidences-qa": IconEvidenceQA,
		"benchmarks":   IconEvidenceBenchmarks,
		"scripts":      IconEvidenceScripts,
		"assets":       IconEvidenceAssets,
		"exports":      IconEvidenceExports,
	}

	evidenceKindIcons = map[string]Icon{
		"png":   IconEvidenceImage,
		"jpg":   IconEvidenceImage,
		"gif":   IconEvidenceImage,
		"webp":  IconEvidenceImage,
		"svg":   IconEvidenceImage,
		"mp4":   IconEvidenceVideo,
		"webm":  IconEvidenceVideo,
		"json":  IconEvidenceData,
		"csv":   IconEvidenceData,
		"log":   IconEvidenceText,
		"txt":   IconEvidenceText,
		"md":    IconEvidenceText,
		"patch": IconEvidenceDiff,
		"diff":  IconEvidenceDiff,
		"pdf":   IconEvidenceFile,
		"html":  IconEvidenceFile,
		"zip":   IconEvidenceFile,
		"har":   IconEvidenceFile,
		"other": IconEvidenceFile,
	}

	runbookCategoryIcons = map[string]Icon{
		"auth":           IconRunbookAuth,
		"database":       IconRunbookDatabase,
		"queue":          IconRunbookQueue,
		"network":        IconRunbookNetwork,
		"performance":    IconRunbookPerformance,
		"data-integrity": IconRunbookDataIntegrity,
		"registration":   IconRunbookRegistration,
	}

	projectKindIcons = map[string]Icon{
		"umbrella":  IconProjectUmbrella,
		"repo":      IconProjectRepo,
		"instance":  IconProjectInstance,
		"service":   IconProjectService,
		"dataset":   IconProjectDataset,
		"knowledge": IconProjectKnowledge,
	}

	syncStateIcons = map[string]Icon{
		"healthy":  IconSyncHealthy,
		"pending":  IconSyncPending,
		"running":  IconSyncRunning,
		"idle":     IconSyncIdle,
		"disabled": IconSyncDisabled,
		"degraded": IconSyncDegraded,
	}
)
