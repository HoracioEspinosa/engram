package app

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/shared"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

// themeSettingKey is where the chosen theme is remembered. It is the same key
// `engram theme use` writes and the same one theme.Selection reads above
// config.json, so picking a theme here and picking one on the command line are
// the same act.
const themeSettingKey = "tui.theme"

// themePickerKeys are the bindings the picker owns. They live here rather than
// in the global key map because every one of them means something only while
// the overlay is open; the key that opens it is a global and lives there.
var themePickerKeys = struct {
	Move, Apply, Filter, Reload, Close key.Binding
}{
	Move: key.NewBinding(
		key.WithKeys("up", "k", "down", "j"),
		key.WithHelp("j/k", "preview"),
	),
	Apply: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "use"),
	),
	// Filter is the list's own binding, declared here so the footer names it.
	Filter: key.NewBinding(
		key.WithKeys("/"),
		key.WithHelp("/", "filter"),
	),
	Reload: key.NewBinding(
		key.WithKeys("R"),
		key.WithHelp("R", "reload"),
	),
	Close: key.NewBinding(
		key.WithKeys("esc", "ctrl+t"),
		key.WithHelp("esc", "cancel"),
	),
}

// themePickerHelp lists the overlay's own bindings in the order its footer
// leads with them. The footer is rendered from this declaration, the same way
// every screen's is, so the keys the overlay answers to and the keys it
// advertises cannot drift apart.
func themePickerHelp() []key.Binding {
	return []key.Binding{
		themePickerKeys.Move,
		themePickerKeys.Apply,
		themePickerKeys.Filter,
		themePickerKeys.Reload,
		themePickerKeys.Close,
	}
}

// themeItem is one row of the picker: a stored theme, the palette decoded out
// of it, and why it cannot be used if it cannot.
type themeItem struct {
	name    string
	variant string
	source  string
	palette theme.Palette
	// problem is empty for a theme that can be applied, and otherwise says
	// what is wrong with it. A theme with a problem is still listed — hiding
	// it would leave somebody wondering where their theme went — but it is
	// drawn in Danger and refuses to be chosen.
	problem string
}

func (i themeItem) FilterValue() string { return i.name }

// themesLoadedMsg carries the result of reading the themes table.
type themesLoadedMsg struct {
	items []themeItem
	err   error
}

// themePreviewMsg asks the root to repaint in a palette without remembering
// it. It is what moving the cursor emits: a theme is judged by looking at the
// workspace in it, not by reading its name.
type themePreviewMsg struct {
	palette theme.Palette
}

// themeAppliedMsg reports a theme chosen and written to settings.
type themeAppliedMsg struct {
	name    string
	palette theme.Palette
	err     error
}

// themePickerModel is the ctrl+t overlay.
type themePickerModel struct {
	open   bool
	list   list.Model
	styles theme.Styles

	themes   data.ThemeReader
	settings data.SettingsWriter

	// original is the palette that was showing when the overlay opened, so
	// closing without choosing puts the workspace back exactly as it was
	// rather than leaving the last previewed theme in place.
	original theme.Palette

	// notice explains a refusal or a failure: an unreadable theme, a store
	// that would not answer, a setting that would not save.
	notice string
}

// newThemePickerModel builds the overlay's own state. The list is created
// empty and filled from the store when the overlay opens, so a theme added
// by `engram theme import` while the workspace is running appears without a
// restart.
func newThemePickerModel(styles theme.Styles) themePickerModel {
	l := list.New(nil, themeItemDelegate{styles: styles}, 0, 0)
	l.Title = "Theme"
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetShowPagination(false)
	l.SetFilteringEnabled(true)
	return themePickerModel{list: l, styles: styles}
}

// WithThemePicker binds the overlay to the store it reads themes from and
// writes the chosen one to. A root built without it still opens; ctrl+t then
// reports that there is nothing to read rather than doing nothing at all.
func (m Model) WithThemePicker(themes data.ThemeReader, settings data.SettingsWriter) Model {
	m.themePicker.themes = themes
	m.themePicker.settings = settings
	return m
}

// CapturingText reports whether an overlay the root owns has the keyboard.
//
// The picker filters with a text input, so while it is open a keystroke is a
// character and not a command. Reporting it here is what lets whatever
// composes this model — today the program, later an overlay compositor — apply
// the same rule to the root that the root already applies to its tabs.
func (m Model) CapturingText() bool {
	return m.themePicker.open && m.themePicker.list.FilterState() == list.Filtering
}

// withStyles repaints the overlay itself. The picker draws in the palette
// currently on screen, which during a preview is the palette being previewed —
// that is the preview.
func (p themePickerModel) withStyles(s theme.Styles) themePickerModel {
	p.styles = s
	p.list.SetDelegate(themeItemDelegate{styles: s})
	p.list.Styles.Title = s.Title
	p.list.Styles.NoItems = s.NoResults
	return p
}

// updateThemePicker is the root's single entry point for the overlay. It
// reports whether it consumed the key, so Update can hand it every keystroke
// and let the picker decide.
func (m Model) updateThemePicker(msg tea.KeyMsg) (bool, Model, tea.Cmd) {
	if !m.themePicker.open {
		if !key.Matches(msg, globalKeys.ThemePicker) || m.showHelp {
			return false, m, nil
		}
		// A focused text input owns the keyboard, the same rule the tabs
		// play by (rfc-tui.md §7.1).
		if tab := m.tab(m.active); tab != nil && tab.CapturingText() && m.screen == screenTab {
			return false, m, nil
		}
		m.themePicker.open = true
		m.themePicker.original = m.styles.Palette
		m.themePicker.notice = ""
		return true, m, loadThemes(m.themePicker.themes)
	}

	// While filtering, every key is a character: only the list may have them.
	if m.themePicker.list.FilterState() == list.Filtering {
		updated, cmd := m.themePicker.list.Update(msg)
		m.themePicker.list = updated
		return true, m, cmd
	}

	switch {
	case key.Matches(msg, themePickerKeys.Close):
		m.themePicker.open = false
		m.themePicker.notice = ""
		// Restore rather than repaint from m.styles: what is on screen right
		// now is whatever was last previewed.
		return true, m.withStyles(theme.New(m.themePicker.original)), nil

	case key.Matches(msg, themePickerKeys.Reload):
		return true, m, loadThemes(m.themePicker.themes)

	case key.Matches(msg, themePickerKeys.Apply):
		item, ok := m.themePicker.selected()
		if !ok {
			return true, m, nil
		}
		if item.problem != "" {
			m.themePicker.notice = item.name + ": " + item.problem
			return true, m, nil
		}
		m.themePicker.open = false
		return true, m, applyTheme(m.themePicker.settings, item)
	}

	before := m.themePicker.list.Index()
	updated, cmd := m.themePicker.list.Update(msg)
	m.themePicker.list = updated
	if m.themePicker.list.Index() == before {
		return true, m, cmd
	}
	// The cursor moved: show the workspace in the theme under it, without
	// writing anything down.
	item, ok := m.themePicker.selected()
	if !ok || item.problem != "" {
		return true, m, cmd
	}
	return true, m, tea.Batch(cmd, preview(item.palette))
}

// updateThemeMessage folds the overlay's own messages back into the root.
func (m Model) updateThemeMessage(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case themesLoadedMsg:
		m.themePicker = m.themePicker.applyLoaded(msg)
		return m, nil

	case themePreviewMsg:
		return m.withStyles(theme.New(msg.palette)), nil

	case themeAppliedMsg:
		if msg.err != nil {
			// The palette is still worth showing — the failure is about
			// remembering the choice, not about the colours.
			m.themePicker.notice = "could not remember " + msg.name + ": " + msg.err.Error()
			return m.withStyles(theme.New(msg.palette)), nil
		}
		m.themePicker.notice = ""
		m.themePicker.original = msg.palette
		return m.withStyles(theme.New(msg.palette)), nil
	}
	return m, nil
}

// applyLoaded fills the list and puts the cursor on the theme currently
// showing, so the overlay opens on where the reader already is.
func (p themePickerModel) applyLoaded(msg themesLoadedMsg) themePickerModel {
	if msg.err != nil {
		p.notice = "could not read the themes: " + msg.err.Error()
		return p
	}
	p.notice = ""
	items := make([]list.Item, 0, len(msg.items))
	current := 0
	for i, item := range msg.items {
		if item.name == p.styles.Palette.Name {
			current = i
		}
		items = append(items, item)
	}
	p.list.SetItems(items)
	p.list.Select(current)
	return p
}

// selected returns the theme under the cursor.
func (p themePickerModel) selected() (themeItem, bool) {
	item, ok := p.list.SelectedItem().(themeItem)
	return item, ok
}

// loadThemes reads every stored theme and decodes each one's palette.
func loadThemes(themes data.ThemeReader) tea.Cmd {
	return func() tea.Msg {
		if themes == nil {
			return themesLoadedMsg{err: errors.New("no theme store is bound to this workspace")}
		}
		records, err := themes.ListThemes()
		if err != nil {
			return themesLoadedMsg{err: err}
		}
		items := make([]themeItem, 0, len(records))
		for _, record := range records {
			items = append(items, themeItemFrom(record))
		}
		return themesLoadedMsg{items: items}
	}
}

// themeItemFrom turns a stored row into a row of the picker, deciding here —
// once, off the render path — whether it can be applied at all.
func themeItemFrom(record data.ThemeRecord) themeItem {
	item := themeItem{name: record.Name, variant: record.Variant, source: record.Source}
	if record.Invalid != "" {
		item.problem = record.Invalid
		return item
	}
	_, _, palette, err := theme.UnmarshalTheme(record.Palette)
	if err != nil {
		item.problem = "its palette is not a theme document"
		return item
	}
	palette.Name = record.Name
	item.palette = palette
	return item
}

// preview asks the root to repaint without remembering.
func preview(palette theme.Palette) tea.Cmd {
	return func() tea.Msg { return themePreviewMsg{palette: palette} }
}

// applyTheme remembers a choice and reports it back.
//
// The write happens before the repaint reaches the root, so a setting that
// will not save says so instead of leaving the workspace painted in a theme
// the next start will not open on.
func applyTheme(settings data.SettingsWriter, item themeItem) tea.Cmd {
	return func() tea.Msg {
		applied := themeAppliedMsg{name: item.name, palette: item.palette}
		if settings == nil {
			applied.err = errors.New("no settings store is bound to this workspace")
			return applied
		}
		applied.err = settings.SetSetting(themeSettingKey, item.name)
		return applied
	}
}

// themeItemDelegate draws one theme per row, in the palette currently on
// screen.
type themeItemDelegate struct {
	styles theme.Styles
}

func (d themeItemDelegate) Height() int                         { return 1 }
func (d themeItemDelegate) Spacing() int                        { return 0 }
func (d themeItemDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (d themeItemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	item, ok := listItem.(themeItem)
	if !ok {
		return
	}

	cursor := "  "
	style := d.styles.ListItem
	if index == m.Index() {
		// A cursor glyph and weight, never an inverted background: the
		// workspace has to stay readable on a translucent terminal, where a
		// filled row punches an opaque block through the window.
		cursor = d.styles.Icons.Glyph(theme.IconChevronRight) + " "
		style = d.styles.ListSelected
	}
	if item.problem != "" {
		style = d.styles.DangerInline
	}

	detail := item.variant
	if item.source != "" && item.source != "builtin" {
		detail += " · " + item.source
	}
	if item.problem != "" {
		detail = item.problem
	}

	fmt.Fprint(w, cursor+style.Render(item.name)+"  "+d.styles.Timestamp.Render(detail))
}

// viewThemePicker draws the overlay.
//
// It replaces the body rather than compositing over it, the same way the "?"
// overlay does, because there is no layering primitive in this line of bubbles
// yet. A full-screen swap also keeps the list readable at eighty columns,
// which a floating panel would not be.
func (m Model) viewThemePicker() string {
	l := m.themePicker.list
	height := m.height - 8
	if height < 3 {
		height = 3
	}
	width := m.width - 6
	if width < 20 {
		width = 20
	}
	l.SetSize(width, height)

	var b strings.Builder
	b.WriteString(l.View())
	if m.themePicker.notice != "" {
		b.WriteString("\n" + m.styles.Error.Render(m.themePicker.notice))
	}
	if hints := shared.HintsFrom(m.styles, themePickerHelp(), m.width); hints != "" {
		b.WriteString("\n" + hints)
	}
	return b.String()
}
