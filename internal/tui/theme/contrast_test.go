package theme

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// TestContrastRatioKnownPairs pins ContrastRatio against ratios that can be
// checked by hand: pure black on pure white is the maximum possible (21:1),
// identical colours are the minimum possible (1:1), and WCAG's own worked
// example (#767676 on white is exactly the 4.5:1 boundary) confirms the
// formula, not just its extremes.
func TestContrastRatioKnownPairs(t *testing.T) {
	tests := []struct {
		name    string
		fg, bg  lipgloss.Color
		want    float64
		epsilon float64
	}{
		{"black on white is maximal", "#000000", "#ffffff", 21.0, 0.01},
		{"white on black is maximal, order does not matter", "#ffffff", "#000000", 21.0, 0.01},
		{"identical colours are minimal", "#89b4fa", "#89b4fa", 1.0, 0.001},
		{"WCAG's own AA boundary example", "#767676", "#ffffff", 4.5, 0.05},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ContrastRatio(tc.fg, tc.bg)
			if err != nil {
				t.Fatalf("ContrastRatio(%q, %q): %v", tc.fg, tc.bg, err)
			}
			if math.Abs(got-tc.want) > tc.epsilon {
				t.Errorf("ContrastRatio(%q, %q) = %.3f, want %.3f ± %.3f", tc.fg, tc.bg, got, tc.want, tc.epsilon)
			}
		})
	}
}

func TestContrastRatioRejectsNonHexColour(t *testing.T) {
	if _, err := ContrastRatio(lipgloss.Color("240"), lipgloss.Color("#1e1e2e")); err == nil {
		t.Fatal("ContrastRatio accepted a bare ANSI index instead of a #rrggbb literal")
	}
}

// contrastIssue names one foreground role that fails MinContrastRatio
// against one background plane.
type contrastIssue struct {
	role  string
	bg    string
	ratio float64
}

// key identifies the (role, background) pair independent of which palette
// produced it, so knownContrastDebt can be keyed without repeating ratios
// that shift if a palette's hex values change.
func (i contrastIssue) key() string { return i.role + "/" + i.bg }

// bar returns the ratio this role has to clear. Overlay is never painted as
// text — theme.go only ever hands it to BorderForeground — so WCAG 1.4.11's
// 3:1 for non-text contrast is the criterion that applies to it, and holding
// it to 1.4.3's 4.5:1 would be citing the wrong rule at it.
func (i contrastIssue) bar() float64 {
	if i.role == "Overlay" {
		return MinOverlayContrastRatio
	}
	return MinContrastRatio
}

func (i contrastIssue) String() string {
	return fmt.Sprintf("%s on %s: contrast %.2f:1, want >= %.1f:1", i.role, i.bg, i.ratio, i.bar())
}

// paletteRoles lists every role that is drawn on top of a plane, paired with
// the colour the palette holds for it. The ten text roles come first, then the
// separator, which answers to its own bar.
func paletteRoles(p Palette) []struct {
	name  string
	color lipgloss.Color
} {
	return []struct {
		name  string
		color lipgloss.Color
	}{
		{"Text", p.Text},
		{"Subtext", p.Subtext},
		{"Primary", p.Primary},
		{"Secondary", p.Secondary},
		{"Accent", p.Accent},
		{"Highlight", p.Highlight},
		{"Success", p.Success},
		{"Warning", p.Warning},
		{"Danger", p.Danger},
		{"Info", p.Info},
		{"Overlay", p.Overlay},
	}
}

// legibilityIssues checks every role a screen paints against both background
// planes it can land on — Base (the app frame) and Surface (panels and cards,
// theme.go's StatCard/TimelineFocus/SearchInput borders sit on either) — and
// reports every pair under the bar that role answers to.
func legibilityIssues(p Palette) []contrastIssue {
	backgrounds := []struct {
		name  string
		color lipgloss.Color
	}{
		{"Base", p.Base},
		{"Surface", p.Surface},
	}

	var issues []contrastIssue
	for _, bg := range backgrounds {
		for _, fg := range paletteRoles(p) {
			issue := contrastIssue{role: fg.name, bg: bg.name}
			ratio, err := ContrastRatio(fg.color, bg.color)
			if err != nil {
				// A malformed hex value is its own failure, at an
				// impossible ratio so it always sorts as a violation.
				issues = append(issues, issue)
				continue
			}
			issue.ratio = ratio
			if ratio < issue.bar() {
				issues = append(issues, issue)
			}
		}
	}
	return issues
}

// knownContrastDebt lists (role, background) pairs that fail their bar today
// under hues chosen elsewhere: elephant is the palette that shipped before
// theming existed — theme.go's Elephant doc comment fixes its hues so "an
// upgrade never surprises anyone" — and kanagawa's are rfc-tui.md §8.1's own
// table, transcribed faithfully rather than adjusted here.
//
// Every entry belongs to one of those two inherited palettes. The four koi
// palettes are this workspace's own and carry none: a palette designed here
// that cannot clear its own bar is a palette to fix, not to excuse.
//
// TestKnownContrastDebtIsStillReal asserts every pair listed here still
// genuinely fails, so a future colour fix that raises a ratio above the bar
// is caught as stale debt instead of this allowlist quietly doing nothing.
var knownContrastDebt = map[string]map[string]bool{
	"elephant": {
		"Info/Base":    true,
		"Info/Surface": true,
	},
	"catppuccin-mocha": {
		// Mocha's separator clears the ground but not the panel colour it
		// was picked to sit beside.
		"Overlay/Surface": true,
	},
	"kanagawa": {
		"Subtext/Base":      true,
		"Subtext/Surface":   true,
		"Danger/Base":       true,
		"Danger/Surface":    true,
		"Secondary/Surface": true,
		// Kanagawa's separator cannot draw a panel border against either of
		// its own grounds.
		"Overlay/Base":    true,
		"Overlay/Surface": true,
	},
}

// TestRegisteredPalettesAreLegible is the contrast test rfc-tui.md T-10.06
// asks for: every colour the registry exposes through --theme /
// ENGRAM_TUI_THEME / tui.theme must clear MinContrastRatio (WCAG 2.1 AA,
// 4.5:1) for every semantic foreground role against both background planes,
// except the pre-existing debt knownContrastDebt names and tracks. Any
// other violation — including one in a role or palette not listed there —
// fails the test.
//
// This test's own sensitivity is proven by
// TestLegibilityIssuesCatchesAnIllegiblePalette below: a palette outside the
// registry that is genuinely illegible is genuinely reported, not waved
// through.
func TestRegisteredPalettesAreLegible(t *testing.T) {
	for name, ctor := range registry {
		t.Run(name, func(t *testing.T) {
			debt := knownContrastDebt[name]
			for _, issue := range legibilityIssues(ctor()) {
				if debt[issue.key()] {
					t.Logf("known contrast debt, reported to the architect rather than fixed here: %s", issue)
					continue
				}
				t.Error(issue)
			}
		})
	}
}

// TestKnownContrastDebtIsStillReal keeps knownContrastDebt honest: every
// pair it excuses must still actually fail, or the excuse has gone stale and
// TestRegisteredPalettesAreLegible is silently under-checking that palette.
func TestKnownContrastDebtIsStillReal(t *testing.T) {
	for name, debt := range knownContrastDebt {
		ctor, ok := registry[name]
		if !ok {
			t.Errorf("knownContrastDebt names %q, which is not a registered palette", name)
			continue
		}
		seen := map[string]bool{}
		for _, issue := range legibilityIssues(ctor()) {
			seen[issue.key()] = true
		}
		for key := range debt {
			if !seen[key] {
				t.Errorf("%s: %q no longer fails MinContrastRatio — remove it from knownContrastDebt", name, key)
			}
		}
	}
}

// TestOverlayIsVisibleAgainstBothPlanes is the separator's own criterion,
// separated out from the ten text roles so a failure reads as what it is: a
// panel whose border a reader cannot find.
//
// It matters more here than in an interface that fills its panels. Nothing in
// this workspace paints a background — a panel is a border in Overlay and
// nothing else — so an invisible separator does not merely look weak, it
// removes the only thing telling a reader where one panel stops and the next
// begins.
func TestOverlayIsVisibleAgainstBothPlanes(t *testing.T) {
	for _, name := range paletteNames() {
		t.Run(name, func(t *testing.T) {
			p := registry[name]()
			debt := knownContrastDebt[name]
			for _, plane := range []struct {
				name  string
				color lipgloss.Color
			}{{"Base", p.Base}, {"Surface", p.Surface}} {
				ratio, err := ContrastRatio(p.Overlay, plane.color)
				if err != nil {
					t.Fatalf("Overlay on %s: %v", plane.name, err)
				}
				issue := contrastIssue{role: "Overlay", bg: plane.name, ratio: ratio}
				if ratio >= MinOverlayContrastRatio {
					continue
				}
				if debt[issue.key()] {
					t.Logf("known contrast debt on an inherited palette: %s", issue)
					continue
				}
				t.Error(issue)
			}
		})
	}
}

// translucentDesktops are the two extremes a desktop behind a terminal window
// can be. Anything a real wallpaper does sits between them, so a palette that
// clears its bar against both clears it against everything in between.
var translucentDesktops = []struct {
	name  string
	color lipgloss.Color
}{
	{"black", "#000000"},
	{"white", "#ffffff"},
}

// terminalOpacity is the background opacity the workspace is designed to stay
// legible at — a terminal pane showing ten percent of whatever is behind it.
const terminalOpacity = 0.90

// koiPaletteNames lists the palettes this workspace designed, in the order
// they are offered. The inherited three are deliberately absent: they were
// composed for opaque terminals by other people and holding them to a
// translucency budget they never had would be inventing a failure.
var koiPaletteNames = []string{"koi-pond", "koi-day", "showa", "ogon"}

// TestKoiPalettesStayLegibleOverTranslucentBackgrounds is the check that makes
// the koi palettes usable on the terminals they were designed for.
//
// A pane at ninety percent opacity does not show Base. It shows Base mixed
// with the desktop, and that mixed plane is what a reader actually reads text
// against — so a palette validated only against its own hex values is
// validated against a colour nobody sees. Both extremes are checked, because a
// dark palette in front of a bright desktop is the case that washes out and a
// light one in front of a dark desktop is the case that muddies.
//
// Surface is deliberately not composited. Nothing in this workspace paints a
// panel background: a panel is a border in Overlay, a selected row is a cursor
// glyph in Primary. Surface is a role the palette carries for the contrast
// budget and for the one place a background survives — glamour's H1 — so
// compositing it would measure a plane that is never drawn, and would fail on
// arithmetic rather than on anything a reader could see.
func TestKoiPalettesStayLegibleOverTranslucentBackgrounds(t *testing.T) {
	var report strings.Builder
	fmt.Fprintf(&report, "koi palettes at %.0f%% opacity, text >= %.1f:1, overlay >= %.1f:1\n",
		terminalOpacity*100, MinContrastRatio, MinOverlayContrastRatio)

	for _, name := range koiPaletteNames {
		ctor, ok := registry[name]
		if !ok {
			t.Fatalf("koiPaletteNames names %q, which is not a registered palette", name)
		}
		p := ctor()

		t.Run(name, func(t *testing.T) {
			for _, desktop := range translucentDesktops {
				plane, err := Composite(p.Base, desktop.color, terminalOpacity)
				if err != nil {
					t.Fatalf("Composite(%s, %s): %v", p.Base, desktop.color, err)
				}
				worst := math.Inf(1)
				worstRole := ""
				for _, role := range paletteRoles(p) {
					ratio, err := ContrastRatio(role.color, plane)
					if err != nil {
						t.Errorf("%s over %s: %v", role.name, desktop.name, err)
						continue
					}
					issue := contrastIssue{role: role.name, bg: "Base over " + desktop.name, ratio: ratio}
					if ratio < issue.bar() {
						t.Error(issue)
					}
					if margin := ratio - issue.bar(); margin < worst {
						worst, worstRole = margin, role.name
					}
					fmt.Fprintf(&report, "  %-9s %-15s %-10s %s %6.2f:1\n",
						name, "Base/"+desktop.name, plane, pad(role.name), ratio)
				}
				fmt.Fprintf(&report, "  %-9s %-15s %-10s tightest margin %+.2f (%s)\n",
					name, "Base/"+desktop.name, plane, worst, worstRole)
			}
		})
	}
	writeContrastReport(t, report.String())
}

// pad widens a role name to a fixed column so the report lines up when it is
// read in a terminal rather than diffed.
func pad(role string) string {
	const width = 10
	for len(role) < width {
		role += " "
	}
	return role
}

// writeContrastReport drops the measured ratios next to the rest of a run's
// evidence when one is being collected, and does nothing otherwise. A gate
// wants the numbers, not just a pass; a developer running `go test` wants
// neither a file appearing in their working tree nor a failure because a
// directory they never heard of does not exist.
func writeContrastReport(t *testing.T, report string) {
	t.Helper()
	dir := strings.TrimSpace(os.Getenv("ENGRAM_TEST_OUT"))
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Logf("contrast report not written: %v", err)
		return
	}
	path := filepath.Join(dir, "theme-contrast.log")
	if err := os.WriteFile(path, []byte(report), 0o644); err != nil {
		t.Logf("contrast report not written: %v", err)
		return
	}
	t.Logf("contrast report written to %s", path)
}

// TestCompositeIsTheIdentityAtFullOpacity and its siblings pin the blend
// itself, so the translucency test above is measuring what it claims to.
func TestCompositeIsTheIdentityAtFullOpacity(t *testing.T) {
	got, err := Composite("#0d1b21", "#ffffff", 1)
	if err != nil {
		t.Fatalf("Composite: %v", err)
	}
	if got != lipgloss.Color("#0d1b21") {
		t.Fatalf("Composite at full opacity = %s, want the background unchanged", got)
	}
}

func TestCompositeIsTheDesktopAtZeroOpacity(t *testing.T) {
	got, err := Composite("#0d1b21", "#ffffff", 0)
	if err != nil {
		t.Fatalf("Composite: %v", err)
	}
	if got != lipgloss.Color("#ffffff") {
		t.Fatalf("Composite at zero opacity = %s, want the desktop", got)
	}
}

func TestCompositeMixesHalfway(t *testing.T) {
	got, err := Composite("#000000", "#ffffff", 0.5)
	if err != nil {
		t.Fatalf("Composite: %v", err)
	}
	// Half of 255 rounds to 128, not 127: the blend rounds rather than
	// truncating, so a pane at fifty percent does not drift darker than it
	// should.
	if got != lipgloss.Color("#808080") {
		t.Fatalf("Composite halfway between black and white = %s, want #808080", got)
	}
}

func TestCompositeClampsAnImpossibleOpacity(t *testing.T) {
	over, err := Composite("#0d1b21", "#ffffff", 4)
	if err != nil {
		t.Fatalf("Composite: %v", err)
	}
	if over != lipgloss.Color("#0d1b21") {
		t.Errorf("an opacity above 1 gave %s, want the background unchanged", over)
	}
	under, err := Composite("#0d1b21", "#ffffff", -2)
	if err != nil {
		t.Fatalf("Composite: %v", err)
	}
	if under != lipgloss.Color("#ffffff") {
		t.Errorf("an opacity below 0 gave %s, want the desktop", under)
	}
}

func TestCompositeRejectsNonHexColours(t *testing.T) {
	if _, err := Composite("240", "#ffffff", 0.9); err == nil {
		t.Error("Composite accepted a bare ANSI index as the background")
	}
	if _, err := Composite("#0d1b21", "white", 0.9); err == nil {
		t.Error("Composite accepted a colour name as the desktop")
	}
}

// TestTranslucencyCheckCatchesAWashedOutPalette proves the translucency test
// is not vacuous: a palette whose text only just clears its bar on an opaque
// ground genuinely fails once the ground is mixed with a bright desktop.
func TestTranslucencyCheckCatchesAWashedOutPalette(t *testing.T) {
	// White-ish text on a near-black ground: 18:1 opaque, which any check
	// waves through.
	const text = "#e6edef"
	const base = "#0d1b21"
	opaque, err := ContrastRatio(text, base)
	if err != nil {
		t.Fatalf("ContrastRatio: %v", err)
	}

	// The same pair at ten percent opacity, which is far past anything the
	// workspace supports, has to come out worse — otherwise Composite is not
	// doing anything and the test above proves nothing.
	plane, err := Composite(base, "#ffffff", 0.10)
	if err != nil {
		t.Fatalf("Composite: %v", err)
	}
	washed, err := ContrastRatio(text, plane)
	if err != nil {
		t.Fatalf("ContrastRatio: %v", err)
	}
	if washed >= opaque {
		t.Fatalf("text on a mostly-white plane scored %.2f:1, no worse than %.2f:1 on the opaque ground",
			washed, opaque)
	}
	if washed >= MinContrastRatio {
		t.Fatalf("text on a mostly-white plane still scored %.2f:1: the check would pass anything", washed)
	}
}

// TestLegibilityIssuesCatchesAnIllegiblePalette proves legibilityIssues is
// not a check that would pass any palette: a synthetic palette with light
// text on a near-white surface (a pair a real author could plausibly
// commit — light grey on light grey, not an absurd extreme) must be
// reported.
func TestLegibilityIssuesCatchesAnIllegiblePalette(t *testing.T) {
	illegible := Palette{
		Name:      "illegible",
		Base:      "#1e1e2e",
		Surface:   "#f0f0f0",
		Overlay:   "#6c7086",
		Text:      "#cdd6f4",
		Subtext:   "#a6adc8",
		Primary:   "#b4befe",
		Secondary: "#cba6f7",
		Accent:    "#fab387",
		Highlight: "#94e2d5",
		Success:   "#a6e3a1",
		Warning:   "#f9e2af",
		Danger:    "#f38ba8",
		Info:      "#89b4fa",
	}

	issues := legibilityIssues(illegible)
	if len(issues) == 0 {
		t.Fatal("legibilityIssues found nothing wrong with light text on a near-white Surface — the check is vacuous")
	}

	found := false
	for _, issue := range issues {
		if issue.role == "Text" && issue.bg == "Surface" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a Text-on-Surface violation among the reported issues, got: %v", issues)
	}
}
