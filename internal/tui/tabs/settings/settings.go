// Package settings is the Settings tab: everything the workspace remembers
// about itself, in one place.
//
// It absorbed the Cloud tab. Sync configuration was a tab of its own holding
// four menu items, which cost a slot in the bar and left "where do I change
// things" with two answers. It is a row here now, and the tab it used to be
// is gone.
package settings

import (
	"errors"
	"os"
	"strings"

	"github.com/HoracioEspinosa/engram/internal/tui/data"
	"github.com/HoracioEspinosa/engram/internal/tui/shared"
	"github.com/HoracioEspinosa/engram/internal/tui/tabs"
	"github.com/HoracioEspinosa/engram/internal/tui/theme"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// IconSettingKey is where the chosen glyph vocabulary is remembered. It is
// the same key theme.ResolveIconMode reads above config.json, so cycling it
// here and setting it on the command line are the same act.
const IconSettingKey = "tui.icons"

// Screen is the tab's own screen enum: the settings list, or the sync
// sub-screen the Cloud tab used to be.
type Screen int

const (
	// ScreenList is the settings themselves.
	ScreenList Screen = iota
	// ScreenCloud is sync configuration.
	ScreenCloud
)

// row names one line of the settings list.
type row int

const (
	rowTheme row = iota
	rowIcons
	rowVaultRoot
	rowEvidenceDir
	rowCloud
	rowDoctor
	rowCount
)

// OpenThemePickerMsg asks the root to open the ctrl+t overlay.
//
// The picker is root state — choosing a theme repaints every tab — so this
// tab asks for it rather than owning it, the same way a tab asks for a
// navigation instead of switching itself.
type OpenThemePickerMsg struct{}

// TabOwner names the tab this message came from. It is the root that acts on
// it, but the tab is still where it belongs.
func (OpenThemePickerMsg) TabOwner() tabs.ID { return tabs.Settings }

// iconModeSavedMsg reports the settings write for the icon mode.
type iconModeSavedMsg struct {
	mode theme.IconMode
	err  error
}

// TabOwner names the tab this message belongs to.
func (iconModeSavedMsg) TabOwner() tabs.ID { return tabs.Settings }

// errNoSettingsStore is what a cycle reports with nowhere to write it down:
// the vocabulary still changes, the choice simply does not survive a restart.
var errNoSettingsStore = errors.New("no settings store is bound to this workspace")

// iconModes is the cycle "enter" walks on the Icons row, in the order
// §6.9 declares them.
var iconModes = []theme.IconMode{theme.IconModeUnicode, theme.IconModeNerd, theme.IconModeASCII}

// cloudItems are the sync entry points the Cloud tab used to list, kept in
// its order so the screen a reader knew is the screen they find.
var cloudItems = []string{
	"Configure server",
	"View status",
	"Enroll projects",
	"Back",
}

// Model is the Settings tab's state.
type Model struct {
	styles   theme.Styles
	settings data.SettingsWriter
	doctor   data.SettingsReader

	Screen Screen
	Width  int
	Height int

	// Cursor is the highlighted row of whichever screen is showing.
	Cursor int

	// IconMode is the vocabulary the workspace draws with. It is the tab's
	// own copy of what the root resolved, cycled here and remembered in
	// settings; the repaint itself is the root's job.
	IconMode theme.IconMode

	Notice shared.Notice
}

// New creates the Settings tab.
func New() Model {
	return Model{styles: theme.Default(), IconMode: theme.IconModeUnicode}
}

// WithStyles returns a copy of m painted with styles instead of the default,
// and reading the icon mode those styles were built with.
func (m Model) WithStyles(styles theme.Styles) Model {
	m.styles = styles
	m.IconMode = styles.Icons.Mode()
	return m
}

// Styles exposes the tab's current style set for app-level tests that assert
// every tab paints with the same resolved palette.
func (m Model) Styles() theme.Styles { return m.styles }

// WithSettings binds the tab to the store it writes a remembered choice to,
// and to the reader the Doctor row summarises. A tab built without them still
// renders; the rows then say what they cannot do rather than doing nothing.
func (m Model) WithSettings(w data.SettingsWriter, r data.SettingsReader) Model {
	m.settings = w
	m.doctor = r
	return m
}

// Title is the label the tab bar shows for this tab.
func (Model) Title() string { return "Settings" }

// Init has nothing to load: every row reads its own source when drawn.
func (Model) Init() tea.Cmd { return nil }

// Refresh has nothing to reload for the same reason.
func (Model) Refresh() tea.Cmd { return nil }

// CapturingText is always false: Settings has no text input.
func (Model) CapturingText() bool { return false }

// Update advances the Settings tab.
func (m Model) Update(msg tea.Msg) (tabs.Tab, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width, m.Height = msg.Width, msg.Height
		return m, nil

	case iconModeSavedMsg:
		if msg.err != nil {
			// The vocabulary is still what the reader chose; what failed is
			// remembering it, and saying which is what keeps the two apart.
			m.Notice = shared.Warn("could not remember the icon mode: " + msg.err.Error())
			return m, nil
		}
		m.Notice = shared.Info("icons: " + string(msg.mode))
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg.String())
	}
	return m, nil
}

func (m Model) handleKey(pressed string) (tabs.Tab, tea.Cmd) {
	if m.Screen == ScreenCloud {
		return m.handleCloudKey(pressed)
	}

	switch pressed {
	case "up", "k":
		if m.Cursor > 0 {
			m.Cursor--
		}
	case "down", "j":
		if m.Cursor < int(rowCount)-1 {
			m.Cursor++
		}
	case "g":
		m.Cursor = 0
	case "G":
		m.Cursor = int(rowCount) - 1
	case "enter", " ":
		return m.activateRow()
	case "esc", "q":
		// The same way out every other tab's root screen offers.
		return m, tabs.Navigate(tabs.Home)
	}
	return m, nil
}

func (m Model) handleCloudKey(pressed string) (tabs.Tab, tea.Cmd) {
	switch pressed {
	case "up", "k":
		if m.Cursor > 0 {
			m.Cursor--
		}
	case "down", "j":
		if m.Cursor < len(cloudItems)-1 {
			m.Cursor++
		}
	case "enter", " ":
		if m.Cursor != len(cloudItems)-1 { // anything but "Back"
			return m, nil
		}
		return m.leaveCloud(), nil
	case "esc", "q":
		return m.leaveCloud(), nil
	}
	return m, nil
}

// leaveCloud returns to the settings list with the cursor back on the row
// that opened the sub-screen.
func (m Model) leaveCloud() Model {
	m.Screen = ScreenList
	m.Cursor = int(rowCloud)
	return m
}

// activateRow answers "enter" on the settings list.
func (m Model) activateRow() (tabs.Tab, tea.Cmd) {
	switch row(m.Cursor) {
	case rowTheme:
		return m, func() tea.Msg { return OpenThemePickerMsg{} }
	case rowIcons:
		m.IconMode = nextIconMode(m.IconMode)
		m.styles = m.styles.WithIcons(m.IconMode)
		return m, saveIconMode(m.settings, m.IconMode)
	case rowCloud:
		m.Screen = ScreenCloud
		m.Cursor = 0
		return m, nil
	}
	// The remaining rows are readings, not actions: a vault root, an evidence
	// directory and a doctor summary are what the workspace found, and the
	// place to change them is the environment they were read from.
	return m, nil
}

// nextIconMode cycles unicode → nerd → ascii → unicode.
func nextIconMode(current theme.IconMode) theme.IconMode {
	for i, mode := range iconModes {
		if mode == current {
			return iconModes[(i+1)%len(iconModes)]
		}
	}
	return iconModes[0]
}

// saveIconMode remembers the chosen vocabulary.
func saveIconMode(w data.SettingsWriter, mode theme.IconMode) tea.Cmd {
	return func() tea.Msg {
		if w == nil {
			return iconModeSavedMsg{mode: mode, err: errNoSettingsStore}
		}
		return iconModeSavedMsg{mode: mode, err: w.SetSetting(IconSettingKey, string(mode))}
	}
}

// Help lists whichever screen is showing's own bindings.
func (m Model) Help() []key.Binding {
	if m.Screen == ScreenCloud {
		return []key.Binding{
			key.NewBinding(key.WithKeys("up", "k", "down", "j"), key.WithHelp("j/k", "navigate")),
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
			key.NewBinding(key.WithKeys("esc", "q"), key.WithHelp("esc/q", "back")),
		}
	}
	return []key.Binding{
		key.NewBinding(key.WithKeys("up", "k", "down", "j"), key.WithHelp("j/k", "navigate")),
		key.NewBinding(key.WithKeys("g", "G"), key.WithHelp("g/G", "top/bottom")),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "change")),
		key.NewBinding(key.WithKeys("esc", "q"), key.WithHelp("esc/q", "back")),
	}
}

// ─── View ────────────────────────────────────────────────────────────────────

// labelCells is the width of the settings list's label column.
const labelCells = 16

// View renders the tab.
func (m Model) View() string {
	if m.Screen == ScreenCloud {
		return m.viewCloud()
	}
	return m.viewList()
}

func (m Model) viewList() string {
	var b strings.Builder

	b.WriteString(m.styles.Header.Render("  Settings"))
	b.WriteString("\n")
	if !m.Notice.Empty() {
		b.WriteString(m.Notice.Render(m.styles))
		b.WriteString("\n")
	}

	vaultRoot, vaultOK := shared.VaultRoot()
	evidenceRoot := shared.EvidenceRoot()

	rows := [rowCount]struct{ label, value string }{
		rowTheme:       {"theme", m.styles.Palette.Name},
		rowIcons:       {"icons", string(m.IconMode)},
		rowVaultRoot:   {"vault root", m.presence(vaultRoot, vaultOK && dirExists(vaultRoot))},
		rowEvidenceDir: {"evidence dir", m.presence(evidenceRoot, dirExists(evidenceRoot))},
		rowCloud:       {"cloud", "sync configuration"},
		rowDoctor:      {"doctor", m.doctorSummary()},
	}

	for i, r := range rows {
		selected := i == m.Cursor
		style := m.styles.ListItem
		if selected {
			style = m.styles.ListSelected
		}
		b.WriteString(strings.TrimRight(shared.RowCursor(m.styles, selected)+
			style.Render(shared.Field(r.label, labelCells))+" "+
			m.styles.DetailValue.Render(r.value), " ") + "\n")
	}
	return b.String()
}

// presence marks a configured path with whether it is actually there. A vault
// root pointing at a directory nobody created is the single most common
// reason the workspace looks empty, and it is invisible until somebody says
// so.
func (m Model) presence(path string, exists bool) string {
	if strings.TrimSpace(path) == "" {
		return m.styles.StaleBadge.Render(m.styles.Icons.Glyph(theme.IconUnknown) + " not configured")
	}
	if exists {
		return path + "  " + m.styles.SuccessInline.Render(m.styles.Icons.Glyph(theme.IconFresh)+" exists")
	}
	return path + "  " + m.styles.DangerInline.Render(m.styles.Icons.Glyph(theme.IconStale)+" missing")
}

// doctorSummary is what the workspace remembers about itself, counted. The
// full diagnostic is `engram doctor`; this row says whether there is anything
// to look at.
func (m Model) doctorSummary() string {
	if m.doctor == nil {
		return "no settings store bound"
	}
	remembered, err := m.doctor.Settings("tui.")
	if err != nil {
		return "unreadable: " + err.Error()
	}
	if len(remembered) == 0 {
		return "nothing remembered yet"
	}
	return plural(len(remembered), "remembered setting")
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return itoa(n) + " " + noun + "s"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// dirExists reports whether path is a directory that is actually there.
func dirExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func (m Model) viewCloud() string {
	var b strings.Builder

	b.WriteString(m.styles.Header.Render("  Cloud sync settings"))
	b.WriteString("\n\n")
	b.WriteString(shared.Menu(m.styles, cloudItems, m.Cursor))
	return b.String()
}
