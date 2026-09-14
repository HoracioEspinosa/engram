package app

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
)

// rootHelpKeyMap adapts two plain []key.Binding slices — the chrome's own
// global bindings and whichever screen is active's own — into help.KeyMap,
// so bubbles/help's FullHelpView renders them as two columns instead of one
// flat list.
type rootHelpKeyMap struct {
	global []key.Binding
	screen []key.Binding
}

func (k rootHelpKeyMap) ShortHelp() []key.Binding {
	return append(append([]key.Binding{}, k.global...), k.screen...)
}

func (k rootHelpKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{k.global, k.screen}
}

// activeScreenHelp returns whichever screen is on display's own bindings: the
// project tree's while its overlay has the keyboard, and the active tab's
// Help() otherwise (rfc-tui.md §7.1: "lista los atajos que esa pantalla
// declara, no una lista fija").
func (m Model) activeScreenHelp() []key.Binding {
	if m.tree.open {
		return treeHelp()
	}
	if tab := m.tab(m.active); tab != nil {
		return tab.Help()
	}
	return nil
}

// viewHelpOverlay renders the "?" overlay (rfc-tui.md §7.1) with
// bubbles/help: the chrome's own global bindings alongside whichever screen
// is active's own, styled from the resolved palette rather than a default
// one.
func (m Model) viewHelpOverlay() string {
	h := help.New()
	h.ShowAll = true
	// The overlay is composited over the body now, so it has to fit inside
	// the frame that is still drawn around it.
	if width := m.width - overlayChromeCells; width > 0 {
		h.Width = width
	}
	h.Styles.FullKey = m.styles.TabActive
	h.Styles.FullDesc = m.styles.DetailValue
	h.Styles.FullSeparator = m.styles.Help

	km := rootHelpKeyMap{global: globalHelpBindings(), screen: m.activeScreenHelp()}

	return m.styles.Title.Render("Help") + "\n" +
		h.View(km) + "\n" +
		m.styles.Help.Render("  ? / esc / q  close")
}
