package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/HoracioEspinosa/engram/internal/store"
	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// storedTheme renders a palette the way the themes table holds one.
func storedTheme(t *testing.T, name, variant string, palette theme.Palette) data.ThemeRecord {
	t.Helper()
	document, err := theme.MarshalTheme(name, variant, palette)
	if err != nil {
		t.Fatalf("MarshalTheme(%s): %v", name, err)
	}
	return data.ThemeRecord{ThemeRecord: store.ThemeRecord{
		Name:    name,
		Variant: variant,
		Palette: json.RawMessage(document),
		Builtin: true,
		Source:  "builtin",
	}}
}

// pickerFixture builds a workspace on koi-pond with a store offering the four
// koi palettes, and the fakes the assertions read back.
func pickerFixture(t *testing.T, extra ...data.ThemeRecord) (Model, *data.FakeTheme, *data.FakeSettings) {
	t.Helper()
	records := []data.ThemeRecord{
		storedTheme(t, "koi-pond", theme.ThemeVariantDark, theme.KoiPond()),
		storedTheme(t, "koi-day", theme.ThemeVariantLight, theme.KoiDay()),
		storedTheme(t, "ogon", theme.ThemeVariantDark, theme.Ogon()),
		storedTheme(t, "showa", theme.ThemeVariantDark, theme.Showa()),
	}
	records = append(records, extra...)

	themes := &data.FakeTheme{Themes: records}
	settings := &data.FakeSettings{}
	m := New(nil, nil, nil, nil, nil, "test", theme.New(theme.KoiPond()), "").
		WithThemePicker(themes, settings)
	m.width, m.height = 120, 40
	return m, themes, settings
}

// press sends one key through the root and returns the model and the message
// its command produced, so a test can follow the round trip a real program
// makes without running one.
func press(t *testing.T, m Model, key tea.KeyMsg) (Model, tea.Msg) {
	t.Helper()
	updated, cmd := m.Update(key)
	next, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want app.Model", updated)
	}
	if cmd == nil {
		return next, nil
	}
	return next, cmd()
}

// deliver feeds a message back into the root, the way the program's event loop
// would.
func deliver(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	updated, _ := m.Update(msg)
	next, ok := updated.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want app.Model", updated)
	}
	return next
}

var (
	keyCtrlT = tea.KeyMsg{Type: tea.KeyCtrlT}
	keyDown  = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	keyEnter = tea.KeyMsg{Type: tea.KeyEnter}
	keyEsc   = tea.KeyMsg{Type: tea.KeyEsc}
)

// openPicker opens the overlay and lets the load command's message land, so a
// test starts from a filled list.
func openPicker(t *testing.T, m Model) Model {
	t.Helper()
	m, msg := press(t, m, keyCtrlT)
	if !m.themePicker.open {
		t.Fatal("ctrl+t did not open the theme picker")
	}
	loaded, ok := msg.(themesLoadedMsg)
	if !ok {
		t.Fatalf("opening the picker produced %T, want themesLoadedMsg", msg)
	}
	if loaded.err != nil {
		t.Fatalf("loading the themes failed: %v", loaded.err)
	}
	return deliver(t, m, loaded)
}

// TestThemePreviewDoesNotPersist is the line between looking and choosing.
// Moving the cursor repaints the whole workspace so a theme can be judged by
// what it looks like, and writes nothing: a reader who browses the list and
// presses esc must open tomorrow on the theme they had, not on the last one
// their cursor happened to rest on.
func TestThemePreviewDoesNotPersist(t *testing.T) {
	m, _, settings := pickerFixture(t)
	m = openPicker(t, m)

	before := m.styles.Palette.Name
	m, msg := press(t, m, keyDown)
	preview, ok := msg.(themePreviewMsg)
	if !ok {
		t.Fatalf("moving the cursor produced %T, want themePreviewMsg", msg)
	}
	m = deliver(t, m, preview)

	if m.styles.Palette.Name == before {
		t.Fatalf("the workspace still shows %q after previewing another theme", before)
	}
	if len(settings.SetCalls()) != 0 {
		t.Errorf("previewing wrote %v to settings", settings.SetCalls())
	}
}

// TestEscapeRestoresTheThemeThePickerOpenedOn is the other half of the same
// promise: cancelling puts back what was there, not what was last previewed.
func TestEscapeRestoresTheThemeThePickerOpenedOn(t *testing.T) {
	m, _, settings := pickerFixture(t)
	original := m.styles.Palette.Name
	m = openPicker(t, m)

	m, msg := press(t, m, keyDown)
	if preview, ok := msg.(themePreviewMsg); ok {
		m = deliver(t, m, preview)
	}
	if m.styles.Palette.Name == original {
		t.Fatal("the preview never took effect, so this test would pass vacuously")
	}

	m, _ = press(t, m, keyEsc)
	if m.themePicker.open {
		t.Error("esc left the picker open")
	}
	if got := m.styles.Palette.Name; got != original {
		t.Errorf("after esc the workspace shows %q, want the original %q", got, original)
	}
	if len(settings.SetCalls()) != 0 {
		t.Errorf("cancelling wrote %v to settings", settings.SetCalls())
	}
}

// TestThemeAppliedPersistsSetting pins what choosing means: the workspace
// repaints and the choice is written where the next start reads it from.
func TestThemeAppliedPersistsSetting(t *testing.T) {
	m, _, settings := pickerFixture(t)
	m = openPicker(t, m)

	m, msg := press(t, m, keyDown)
	if preview, ok := msg.(themePreviewMsg); ok {
		m = deliver(t, m, preview)
	}
	chosen, ok := m.themePicker.selected()
	if !ok {
		t.Fatal("no theme under the cursor")
	}

	m, msg = press(t, m, keyEnter)
	applied, ok := msg.(themeAppliedMsg)
	if !ok {
		t.Fatalf("enter produced %T, want themeAppliedMsg", msg)
	}
	if applied.err != nil {
		t.Fatalf("applying the theme failed: %v", applied.err)
	}
	m = deliver(t, m, applied)

	if m.themePicker.open {
		t.Error("choosing a theme left the picker open")
	}
	if got := m.styles.Palette.Name; got != chosen.name {
		t.Errorf("the workspace shows %q, want the chosen %q", got, chosen.name)
	}
	if len(settings.SetCalls()) != 1 {
		t.Fatalf("choosing wrote %d settings, want exactly one", len(settings.SetCalls()))
	}
	if got := settings.SetCalls()[0]; got.Key != themeSettingKey || got.Value != chosen.name {
		t.Errorf("wrote %s=%s, want %s=%s", got.Key, got.Value, themeSettingKey, chosen.name)
	}
}

// TestInvalidThemeFallsBackWithNotice covers a row somebody broke by hand. The
// themes table is meant to be edited with SQL, so a palette that no longer
// parses is a thing people do — it must cost them that one theme and say so,
// never the workspace.
func TestInvalidThemeFallsBackWithNotice(t *testing.T) {
	broken := data.ThemeRecord{
		ThemeRecord: store.ThemeRecord{
			Name:    "broken",
			Variant: theme.ThemeVariantDark,
			Palette: json.RawMessage(`{"name":"broken","variant":"dark","palette":{},"logo_gradient":[]}`),
			Source:  "sql",
		},
		Invalid: "its palette has no colour roles in it",
	}
	m, _, settings := pickerFixture(t, broken)
	original := m.styles.Palette.Name
	m = openPicker(t, m)

	// Walk to the broken row. It is listed rather than hidden: a theme that
	// vanished would be harder to diagnose than one that explains itself.
	found := false
	for i := 0; i < len(m.themePicker.list.Items()); i++ {
		m.themePicker.list.Select(i)
		if item, ok := m.themePicker.selected(); ok && item.name == "broken" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("the broken theme is not listed at all")
	}

	m, msg := press(t, m, keyEnter)
	if msg != nil {
		t.Fatalf("choosing a broken theme produced %T, want nothing to happen", msg)
	}
	if !m.themePicker.open {
		t.Error("the picker closed on a theme it refused to apply")
	}
	if got := m.styles.Palette.Name; got != original {
		t.Errorf("the workspace repainted to %q, want to stay on %q", got, original)
	}
	if len(settings.SetCalls()) != 0 {
		t.Errorf("a refused theme was written to settings: %v", settings.SetCalls())
	}
	if !strings.Contains(m.themePicker.notice, broken.Invalid) {
		t.Errorf("notice = %q, want it to carry %q", m.themePicker.notice, broken.Invalid)
	}
	if rendered := ansi.Strip(m.View()); !strings.Contains(rendered, broken.Invalid) {
		t.Errorf("the refusal never reaches the screen:\n%s", rendered)
	}
}

// TestUnreadableThemeDocumentIsRefusedToo covers the same failure arriving
// through the other door: a row the store's own JSON check accepted but that
// is not a theme document.
func TestUnreadableThemeDocumentIsRefusedToo(t *testing.T) {
	item := themeItemFrom(data.ThemeRecord{ThemeRecord: store.ThemeRecord{
		Name:    "half-done",
		Palette: json.RawMessage(`{"name":"half-done","variant":"dark","palette":{"base":"#000000"},"logo_gradient":[]}`),
	}})
	if item.problem == "" {
		t.Fatal("a document missing twelve roles was accepted as a theme")
	}
}

// TestEveryTabRepaintsOnThemeSwap is the whole point of routing the swap
// through the root: a tab that missed the fan-out keeps rendering in the
// palette it was built with, and no test of that tab alone can see it.
//
// It walks `registered` rather than a list written here, so a tab added to the
// workspace without a way to read its palette back fails this test instead of
// silently going unchecked.
func TestEveryTabRepaintsOnThemeSwap(t *testing.T) {
	m, _, _ := pickerFixture(t)

	paletteOf := map[tabs.ID]func(Model) string{
		tabs.Home:       func(m Model) string { return m.home.Styles().Palette.Name },
		tabs.Memory:     func(m Model) string { return m.memory.Styles().Palette.Name },
		tabs.Tasks:      func(m Model) string { return m.tasks.Styles().Palette.Name },
		tabs.Evidence:   func(m Model) string { return m.evidence.Styles().Palette.Name },
		tabs.Benchmarks: func(m Model) string { return m.benchmarks.Styles().Palette.Name },
		tabs.Runbooks:   func(m Model) string { return m.runbooks.Styles().Palette.Name },
		tabs.Graph:      func(m Model) string { return m.graph.Styles().Palette.Name },
		tabs.Settings:   func(m Model) string { return m.settings.Styles().Palette.Name },
	}

	const swapped = "koi-day"
	m = deliver(t, m, themePreviewMsg{palette: theme.KoiDay()})

	if got := m.styles.Palette.Name; got != swapped {
		t.Fatalf("the root itself did not repaint: %q", got)
	}
	for _, id := range registered {
		read, ok := paletteOf[id]
		if !ok {
			t.Errorf("tab %v is registered but this test cannot read its palette — add it here", id)
			continue
		}
		if got := read(m); got != swapped {
			t.Errorf("tab %v still renders in %q after the swap to %q", id, got, swapped)
		}
	}
}

// TestThemePickerReportsAStoreThatWillNotAnswer keeps a failed read from
// looking like an empty list of themes.
func TestThemePickerReportsAStoreThatWillNotAnswer(t *testing.T) {
	m := New(nil, nil, nil, nil, nil, "test", theme.New(theme.KoiPond()), "")
	m.width, m.height = 120, 40

	m, msg := press(t, m, keyCtrlT)
	loaded, ok := msg.(themesLoadedMsg)
	if !ok {
		t.Fatalf("opening the picker with no store produced %T, want themesLoadedMsg", msg)
	}
	if loaded.err == nil {
		t.Fatal("opening the picker with no store reported success")
	}
	m = deliver(t, m, loaded)
	if m.themePicker.notice == "" {
		t.Error("a store that would not answer left no notice")
	}
}

// TestAFailedSettingWriteStillPaintsTheTheme separates the two things that can
// go wrong: the colours are already correct, so only the remembering failed,
// and saying so is more useful than refusing the theme.
func TestAFailedSettingWriteStillPaintsTheTheme(t *testing.T) {
	m, _, settings := pickerFixture(t)
	settings.SetErr(errStub)
	m = openPicker(t, m)

	m, msg := press(t, m, keyDown)
	if preview, ok := msg.(themePreviewMsg); ok {
		m = deliver(t, m, preview)
	}
	chosen, _ := m.themePicker.selected()

	m, msg = press(t, m, keyEnter)
	applied, ok := msg.(themeAppliedMsg)
	if !ok {
		t.Fatalf("enter produced %T, want themeAppliedMsg", msg)
	}
	if applied.err == nil {
		t.Fatal("the failing settings writer reported success")
	}
	m = deliver(t, m, applied)

	if got := m.styles.Palette.Name; got != chosen.name {
		t.Errorf("the workspace shows %q, want the chosen %q despite the failed write", got, chosen.name)
	}
	if !strings.Contains(m.themePicker.notice, "could not remember") {
		t.Errorf("notice = %q, want it to name the failure", m.themePicker.notice)
	}
}

// TestPickerOpensOnTheThemeAlreadyShowing keeps the cursor where the reader
// already is, so moving it is a step away from the current theme rather than
// an arbitrary jump.
func TestPickerOpensOnTheThemeAlreadyShowing(t *testing.T) {
	m, _, _ := pickerFixture(t)
	m = openPicker(t, m)

	item, ok := m.themePicker.selected()
	if !ok {
		t.Fatal("nothing under the cursor after loading")
	}
	if item.name != m.styles.Palette.Name {
		t.Errorf("the cursor opened on %q, want the showing %q", item.name, m.styles.Palette.Name)
	}
}

// TestRootReportsCapturingTextWhileFiltering pins the rule the root already
// applies to its tabs, applied to the root itself: while the picker's filter
// has the keyboard, a keystroke is a character.
func TestRootReportsCapturingTextWhileFiltering(t *testing.T) {
	m, _, _ := pickerFixture(t)
	if m.CapturingText() {
		t.Fatal("the root claims to be capturing text with no overlay open")
	}
	m = openPicker(t, m)
	if m.CapturingText() {
		t.Error("the root claims to be capturing text before the filter is focused")
	}

	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if !m.CapturingText() {
		t.Error("the root does not report capturing text while the filter is focused")
	}
}

// TestPickerReloadsFromTheStore covers the "R" key: the table is editable from
// outside the workspace, so the list has to be re-readable without a restart.
func TestPickerReloadsFromTheStore(t *testing.T) {
	m, themes, _ := pickerFixture(t)
	m = openPicker(t, m)
	before := len(m.themePicker.list.Items())

	themes.Themes = append(themes.Themes, storedTheme(t, "mine", theme.ThemeVariantDark, theme.Showa()))

	m, msg := press(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	loaded, ok := msg.(themesLoadedMsg)
	if !ok {
		t.Fatalf("R produced %T, want themesLoadedMsg", msg)
	}
	m = deliver(t, m, loaded)

	if got := len(m.themePicker.list.Items()); got != before+1 {
		t.Errorf("after reloading the list has %d themes, want %d", got, before+1)
	}
}

// TestCtrlTIsSuspendedWhileATabCapturesText keeps the picker from stealing a
// keystroke meant for a search box.
func TestCtrlTIsSuspendedWhileATabCapturesText(t *testing.T) {
	m, _, _ := pickerFixture(t)
	m.tree.open = false
	m.active = tabs.Tasks
	m.tasks.Searching = true
	m.tasks.SearchInput.Focus()

	if !m.tasks.CapturingText() {
		t.Fatal("the fixture did not actually focus a text input")
	}
	m, _ = press(t, m, keyCtrlT)
	if m.themePicker.open {
		t.Error("ctrl+t opened the picker over a focused text input")
	}
}

// TestThemePickerTakesTheScreenAndCarriesItsOwnFooter pins the overlay's half
// of the frame contract. The root draws one footer, for whichever screen is on
// display; the overlay replaces that screen outright, so the hints underneath
// would name keys that answer to nothing while it is open.
func TestThemePickerTakesTheScreenAndCarriesItsOwnFooter(t *testing.T) {
	m, _, _ := pickerFixture(t)
	m.tree.open = false
	m.active = tabs.Memory

	beneath := ansi.Strip(m.View())
	under := m.activeScreenHelp()[0].Help()
	if !strings.Contains(beneath, under.Key+" "+under.Desc) {
		t.Fatalf("the screen under the overlay renders no footer:\n%s", beneath)
	}

	view := ansi.Strip(openPicker(t, m).View())
	if strings.Contains(view, under.Key+" "+under.Desc) {
		t.Fatalf("the screen's own footer survived under the overlay:\n%s", view)
	}
	for _, b := range themePickerHelp() {
		h := b.Help()
		if !strings.Contains(view, h.Key+" "+h.Desc) {
			t.Fatalf("the overlay's footer omits %q:\n%s", h.Key+" "+h.Desc, view)
		}
	}
}

// TestThemePickerFooterMatchesItsBindings keeps the overlay as honest as every
// screen: its footer is derived from what it declares, so it cannot advertise
// a key the overlay does not answer.
func TestThemePickerFooterMatchesItsBindings(t *testing.T) {
	m, _, _ := pickerFixture(t)
	m = openPicker(t, m)

	// The panel is composited over the body, so the footer to check is the
	// overlay's own last line, not the last line of the screen.
	lines := strings.Split(ansi.Strip(m.viewThemePicker()), "\n")
	footer := ""
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			footer = line
			break
		}
	}
	if footer == "" {
		t.Fatal("the overlay renders no footer at all")
	}

	declared := make(map[string]bool, len(themePickerHelp()))
	for _, b := range themePickerHelp() {
		h := b.Help()
		declared[h.Key+" "+h.Desc] = true
	}
	for _, hint := range strings.Split(footer, "•") {
		hint = strings.TrimSpace(hint)
		if hint == "" {
			continue
		}
		if !declared[hint] {
			t.Fatalf("footer hint %q is not declared in themePickerHelp()", hint)
		}
	}
}

// errStub is a failure with no detail beyond being one.
var errStub = stubError("the store said no")

type stubError string

func (e stubError) Error() string { return string(e) }

// keyReload is the picker's own "R": re-read the themes table without leaving
// the overlay.
var keyReload = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}}

// TestReloadingThePickerRepaintsTheWorkspace: "R" exists so an edit made
// outside the workspace shows up in it, and the workspace is what the reader
// is judging the theme by.
//
// A reload that filled the list and left the screen alone put the new palette's
// name in front of the old colours. The only way to see the edit was then to
// move the cursor off the row and back onto it, which is a workaround for a
// screen that has already been handed the answer.
func TestReloadingThePickerRepaintsTheWorkspace(t *testing.T) {
	m, themes, _ := pickerFixture(t)
	m = openPicker(t, m)

	// The same theme, edited the way `engram theme` edits one: a role
	// replaced, the name kept. The replacement is another stored palette's
	// role rather than a literal, so no colour is spelled outside theme/.
	edited := theme.KoiPond()
	edited.Primary = theme.Ogon().Primary
	themes.Themes[0] = storedTheme(t, "koi-pond", theme.ThemeVariantDark, edited)

	before := m.styles.Palette.Primary
	cursor := m.themePicker.list.Index()

	m, msg := press(t, m, keyReload)
	loaded, ok := msg.(themesLoadedMsg)
	if !ok {
		t.Fatalf("R produced %T, want themesLoadedMsg", msg)
	}

	updated, cmd := m.Update(loaded)
	m, ok = updated.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want app.Model", updated)
	}
	if got := m.themePicker.list.Index(); got != cursor {
		t.Fatalf("the reload moved the cursor from %d to %d", cursor, got)
	}
	if cmd == nil {
		t.Fatal("the reload repainted nothing")
	}

	previewed, ok := cmd().(themePreviewMsg)
	if !ok {
		t.Fatalf("the reload produced %T, want themePreviewMsg", cmd())
	}
	if previewed.palette.Primary != edited.Primary {
		t.Fatalf("the reload previewed %v, want the palette it just read", previewed.palette.Primary)
	}

	m = deliver(t, m, previewed)
	if m.styles.Palette.Primary == before {
		t.Fatalf("the workspace still paints in %v, the palette the reload replaced", before)
	}
}

// TestAReloadedPickerPreviewsNothingItCannotApply: a row the picker refuses to
// apply is a row it refuses to paint the workspace in, whether the cursor
// landed on it or a reload put it under one.
func TestAReloadedPickerPreviewsNothingItCannotApply(t *testing.T) {
	broken := data.ThemeRecord{ThemeRecord: store.ThemeRecord{
		Name:    "koi-pond",
		Variant: theme.ThemeVariantDark,
		Palette: json.RawMessage(`{"not":"a theme document"}`),
		Source:  "builtin",
	}}

	m, themes, _ := pickerFixture(t)
	m = openPicker(t, m)
	themes.Themes[0] = broken

	m, msg := press(t, m, keyReload)
	loaded, ok := msg.(themesLoadedMsg)
	if !ok {
		t.Fatalf("R produced %T, want themesLoadedMsg", msg)
	}
	if _, cmd := m.Update(loaded); cmd != nil {
		t.Fatalf("the reload previewed a theme it would refuse to apply: %T", cmd())
	}
}

// TestRepaintingKeepsTheIconVocabulary: a change of palette is not a change of
// glyphs.
//
// theme.New starts every style set at the default vocabulary, so a repaint
// that took it as-is dropped an ascii or nerd workspace back to unicode the
// moment a theme was previewed — a terminal that cannot draw those glyphs
// cannot draw them in another colour either.
func TestRepaintingKeepsTheIconVocabulary(t *testing.T) {
	m, _, _ := pickerFixture(t)
	m = m.WithIcons(theme.IconModeASCII)
	m = openPicker(t, m)

	m, msg := press(t, m, keyDown)
	previewed, ok := msg.(themePreviewMsg)
	if !ok {
		t.Fatalf("moving the cursor produced %T, want themePreviewMsg", msg)
	}
	m = deliver(t, m, previewed)

	if got := m.styles.Icons.Mode(); got != theme.IconModeASCII {
		t.Fatalf("previewing a theme left the workspace drawing in %v, want the vocabulary it had", got)
	}
}
