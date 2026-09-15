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

// activeScreenHelp returns the bindings of whatever currently answers the
// keyboard: the overlay holding it, in the order the overlays take it, and the
// active tab's Help() when none is open. The list is whatever that screen
// declares, never a fixed one.
//
// An overlay is modal, so the hints under it name its keys and not the
// screen's: the body stays visible behind the panel, and hints naming keys
// that currently do nothing would be the one part of it that lies.
func (m Model) activeScreenHelp() []key.Binding {
	if m.themePicker.open {
		return themePickerHelp()
	}
	if m.palette.open {
		return paletteHelp(m.styles.Icons)
	}
	if m.tree.open {
		return treeHelp()
	}
	if tab := m.tab(m.active); tab != nil {
		return tab.Help()
	}
	return nil
}

// viewHelpOverlay renders the "?" overlay with bubbles/help: the chrome's own
// global bindings alongside whichever screen is active's own, styled from the
// resolved palette rather than a default one.
func (m Model) viewHelpOverlay() string {
	h := help.New()
	h.ShowAll = true
	// The overlay is composited over the body, so it has to fit inside the
	// frame drawn around it.
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
