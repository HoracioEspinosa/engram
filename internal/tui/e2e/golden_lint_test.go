package e2e

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/tui/app"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	"github.com/charmbracelet/x/ansi"
)

// The twin of internal/tui/app/golden_lint_test.go, for the documents this
// package freezes.
//
// The rules are duplicated rather than shared for the same reason firstDiff
// is: a test file cannot be imported, and the alternative is a package of
// production code that exists only for two test suites. What the two do share
// is app.SlotSpans — the tab bar rule and the mouse's hitboxes have to read
// the bar the same way, or the lint would bless a bar the pointer cannot use.
//
// A diff against a golden file catches a render that changed and says nothing
// about a render that was always wrong; `-e2e-update` turns any such render
// into the new truth by definition. These are the properties a frozen frame
// has to have whatever it says.

// frozenScene is one "═══ name ═══" block of a golden document.
type frozenScene struct {
	name  string
	lines []string
}

// parseFrozen splits a golden document into its scenes.
func parseFrozen(t *testing.T, document string) []frozenScene {
	t.Helper()

	var scenes []frozenScene
	for _, line := range strings.Split(document, "\n") {
		if name, ok := frozenSceneHeader(line); ok {
			scenes = append(scenes, frozenScene{name: name})
			continue
		}
		if len(scenes) == 0 {
			if strings.TrimSpace(line) == "" {
				continue
			}
			t.Fatalf("the document opens with content before its first scene header: %q", line)
		}
		scenes[len(scenes)-1].lines = append(scenes[len(scenes)-1].lines, line)
	}

	if len(scenes) == 0 {
		t.Fatalf("the document holds no scenes at all")
	}
	for i := range scenes {
		scenes[i].lines = trimTrailingBlank(scenes[i].lines)
	}
	return scenes
}

// frozenSceneHeader reads a "═══ name ═══" line.
func frozenSceneHeader(line string) (string, bool) {
	const fence = "═══"
	if !strings.HasPrefix(line, fence+" ") || !strings.HasSuffix(line, " "+fence) {
		return "", false
	}
	return strings.TrimSpace(strings.Trim(line, fence+" ")), true
}

func trimTrailingBlank(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// lintFrozenScene applies every structural rule to one scene and reports what
// it found. hasTabBar says whether the scene drew one at all, so the suite can
// tell a document where the rule passed from one where it never ran.
func lintFrozenScene(scene frozenScene, width, height int) (problems []string, hasTabBar bool) {
	if len(scene.lines) == 0 {
		return []string{scene.name + ": the scene rendered nothing"}, false
	}

	problems = append(problems, lintFrozenHeight(scene, height)...)
	for i, line := range scene.lines {
		problems = append(problems, lintFrozenLine(scene.name, i+1, line, width)...)
	}

	barProblems, hasTabBar := lintFrozenTabBar(scene, width)
	problems = append(problems, barProblems...)
	problems = append(problems, lintFrozenStatusBar(scene)...)
	return problems, hasTabBar
}

// lintFrozenHeight requires the frame to fit the terminal it was drawn for.
//
// The geometry a scene is frozen at is a width and a height, and only the
// width used to be measured. A frame taller than its terminal is not clipped
// at the bottom by the renderer: bubbletea's standard renderer drops the rows
// that do not fit from the TOP, so the reader loses the tab bar and the header
// while the tail of a list keeps the screen. That is a screen with no chrome
// on it, and it looks like a different application rather than like a
// truncation.
func lintFrozenHeight(scene frozenScene, height int) (problems []string) {
	if rows := len(scene.lines); rows > height {
		problems = append(problems, fmt.Sprintf(
			"%s: the frame is %d rows tall, past the %d the geometry declares; the renderer drops the excess from the top, taking the tab bar with it",
			scene.name, rows, height))
	}
	return problems
}

// lintFrozenLine is the per-row half: the geometry, and the two marks a render
// leaves behind when it has gone wrong.
func lintFrozenLine(scene string, number int, line string, width int) (problems []string) {
	// Trailing blanks are the padding lipgloss gives every block up to its
	// widest row; they are not content, and the width test the layout already
	// ships measures the same way.
	if w := ansi.StringWidth(strings.TrimRight(line, " ")); w > width {
		problems = append(problems, fmt.Sprintf("%s line %d is %d cells wide, past the %d the geometry declares:\n%s",
			scene, number, w, width, line))
	}

	if strings.ContainsRune(line, '\uFFFD') {
		problems = append(problems, fmt.Sprintf("%s line %d carries a replacement character, so something was cut mid-rune:\n%s",
			scene, number, line))
	}

	return append(problems, lintFrozenElisions(scene, number, line)...)
}

// elisionMarkers are the two ways a render says "there was more".
var elisionMarkers = []string{"…", "..."}

// lintFrozenElisions requires that nothing be glued to the right of an elision
// that ends a field.
//
// Cutting mid-word is what truncation does, so the text to the left of a
// marker proves nothing. The text to the right does: a marker is the end of
// its field, and a character immediately after one is the next column having
// run into it. That is the collapsed column this lint exists to catch, and it
// is invisible in a diff — the row still looks like a row.
//
// A marker with a blank to its left is a leading elision ("…and 1 more"),
// where text following it is the whole point.
func lintFrozenElisions(scene string, number int, line string) (problems []string) {
	for _, marker := range elisionMarkers {
		for offset := 0; ; {
			at := strings.Index(line[offset:], marker)
			if at < 0 {
				break
			}
			at += offset
			offset = at + len(marker)

			before := line[:at]
			if before == "" || strings.HasSuffix(before, " ") {
				continue
			}
			after := line[at+len(marker):]
			if after == "" || strings.HasPrefix(after, " ") {
				continue
			}
			problems = append(problems, fmt.Sprintf("%s line %d has a column running into an elision at cell %d:\n%s",
				scene, number, ansi.StringWidth(before), line))
		}
	}
	return problems
}

// lintFrozenTabBar checks the separation the bar declares between its slots.
//
// The bar is the one piece of chrome with a target on it, and the mouse reads
// its hitboxes out of exactly this row. Two slots that have run together read
// as one word and click as one target, so the row is required to still parse
// as consecutively numbered slots with blank cells between them.
func lintFrozenTabBar(scene frozenScene, width int) (problems []string, hasTabBar bool) {
	row := frozenTabBarRow(scene)
	if row == "" {
		return nil, false
	}

	bracketed := 0
	for i, span := range app.SlotSpans(row) {
		text := strings.TrimSpace(frozenCells(row, span))
		if strings.HasPrefix(text, "[") {
			bracketed++
		}

		// One slot carries one digit: its own. A second digit inside the same
		// span is the next slot, drawn without the blank cells that separate
		// them, and a slot whose digit is not its own is one the previous span
		// already swallowed.
		digit, digits := slotDigit(text)
		if digits != 1 || digit != i {
			return []string{fmt.Sprintf("%s: tab bar slot %d reads %q, so the slots have run together:\n%s",
				scene.name, i, text, row)}, true
		}
	}
	if bracketed != 1 {
		problems = append(problems, fmt.Sprintf("%s: the tab bar brackets %d slots, want exactly the active one:\n%s",
			scene.name, bracketed, row))
	}
	if w := ansi.StringWidth(strings.TrimRight(row, " ")); w > width {
		problems = append(problems, fmt.Sprintf("%s: the tab bar is %d cells wide, past the %d the geometry declares",
			scene.name, w, width))
	}
	return problems, true
}

// frozenTabBarRow returns the scene's tab bar, or "" for a screen that draws
// none — the project selector keeps a header of its own, since there is no
// project yet to number tabs for. The bar sits on the frame's second row: the
// first is the app frame's own top padding.
func frozenTabBarRow(scene frozenScene) string {
	if len(scene.lines) < 2 {
		return ""
	}
	row := scene.lines[1]
	spans := app.SlotSpans(row)
	if len(spans) < 2 {
		return ""
	}
	if digit, digits := slotDigit(frozenCells(row, spans[0])); digits != 1 || digit != 0 {
		return ""
	}
	return row
}

// slotDigit reads the digit one bar slot carries and how many it carries at
// all.
//
// A slot is drawn as its glyph, its digit and — above the breakpoint — its
// label, so the digit is neither the first rune nor a field of its own. What
// the rule needs is that there is exactly one of them and that it is the
// slot's: two digits in one span are two slots that have run together, and a
// digit that is not the slot's own is a slot the span before it swallowed.
func slotDigit(text string) (digit, digits int) {
	digit = -1
	for _, r := range text {
		if r < '0' || r > '9' {
			continue
		}
		if digits == 0 {
			digit = int(r - '0')
		}
		digits++
	}
	return digit, digits
}

// lintFrozenStatusBar requires the frame's bottom line, which is the one row
// every screen shares. Its rightmost segment is the palette's name — the
// segment that gives ground last but one — so a scene that has lost it has
// lost the bar.
//
// The blank cell before the name is the separation the bar declares between
// its two groups: without it the hints and the theme read as one word.
func lintFrozenStatusBar(scene frozenScene) (problems []string) {
	last := ""
	for i := len(scene.lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(scene.lines[i]) != "" {
			last = strings.TrimRight(scene.lines[i], " ")
			break
		}
	}

	name := theme.DefaultThemeName
	if !strings.HasSuffix(last, name) {
		return []string{fmt.Sprintf("%s: the last row carries no status bar:\n%s", scene.name, last)}
	}
	if at := len(last) - len(name); at == 0 || last[at-1] != ' ' {
		problems = append(problems, fmt.Sprintf("%s: the status bar's right group has run into what precedes it:\n%s",
			scene.name, last))
	}
	return problems
}

// frozenCells returns the text of one half-open span of cells.
func frozenCells(line string, span [2]int) string {
	var b strings.Builder
	cell := 0
	for _, r := range line {
		if cell >= span[0] && cell < span[1] {
			b.WriteRune(r)
		}
		cell += ansi.StringWidth(string(r))
	}
	return b.String()
}

// lintFrozenDocument lints every scene of one document and fails with what it
// found.
func lintFrozenDocument(t *testing.T, where, document string, width, height int) {
	t.Helper()

	bars := 0
	for _, scene := range parseFrozen(t, document) {
		problems, hasTabBar := lintFrozenScene(scene, width, height)
		if hasTabBar {
			bars++
		}
		for _, problem := range problems {
			t.Errorf("%s/%s", where, problem)
		}
	}
	if bars == 0 {
		t.Errorf("%s: not one scene draws a tab bar, so the rule that checks it never ran", where)
	}
}

// TestTeatestGoldenScreensAreStructurallySound lints the frozen documents.
func TestTeatestGoldenScreensAreStructurallySound(t *testing.T) {
	for _, size := range goldenSizes {
		t.Run(size.name, func(t *testing.T) {
			path := teatestGoldenPath(size)
			document, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden: %v", err)
			}
			lintFrozenDocument(t, path, string(document), size.width, size.height)
		})
	}
}

// TestTeatestRenderedScreensAreStructurallySound runs the same rules against
// the scenes as the real program draws them now, not as they were frozen.
//
// This is the half that makes `-e2e-update` safe: the document it writes is
// this render, so a regeneration that would freeze a broken screen fails on
// the same run rather than being reviewed as a diff nobody can read
// structurally.
func TestTeatestRenderedScreensAreStructurallySound(t *testing.T) {
	for _, size := range goldenSizes {
		t.Run(size.name, func(t *testing.T) {
			var b strings.Builder
			for _, screen := range teatestScreens() {
				fmt.Fprintf(&b, "═══ %s ═══\n", screen.name)
				b.WriteString(renderTeatestScene(t, screen, size))
				b.WriteString("\n")
			}
			lintFrozenDocument(t, "rendered/"+size.name, b.String(), size.width, size.height)
		})
	}
}
