package runbooks

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/HoracioEspinosa/engram/internal/tui/shared"

	"github.com/charmbracelet/glamour"
)

// defaultRenderWidth is used when the tab has not yet received a
// tea.WindowSizeMsg (a markdown load raced ahead of the first resize).
const defaultRenderWidth = 80

// absoluteVaultPath resolves a runbook_index.vault_path — relative to
// shared.VaultRoot(), per rfc-tui.md §9.1's documented contract — to a real
// filesystem path. It mirrors tabs/evidence/model.go's absolutePath: reading
// a runbook's Markdown file is not part of data.RunbookReader (see its doc
// comment), the same way tabs/evidence keeps manifest.json off
// data.EvidenceReader — plain filesystem access with nothing to query the
// store for belongs to the tab, not the reader.
func absoluteVaultPath(vaultPath string) string {
	return filepath.Join(shared.VaultRoot(), vaultPath)
}

// readRunbookMarkdown reads vaultPath's file relative to shared.VaultRoot().
//
// A missing file is not an error — rfc-tui.md §9.4 documents it as the
// normal state of a checkout of cd-knowledge-mcp that was never cloned
// locally — it is reported through exists=false so S9 can show the index
// fields plus the instruction to clone instead of a raw I/O error.
func readRunbookMarkdown(vaultPath string) (content string, exists bool, err error) {
	raw, err := os.ReadFile(absoluteVaultPath(vaultPath))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return "", false, err
	}
	return string(raw), true, nil
}

// renderMarkdown converts source into the ANSI-styled text glamour produces,
// wrapped to width (rfc-tui.md §9.4: "la vista Markdown... lo renderiza con
// glamour").
//
// It uses glamour's automatic light/dark detection (WithAutoStyle) rather
// than the palette-driven ansi.StyleConfig rfc-tui.md §8 describes
// (glamour.WithStyles built from theme.Palette): that mapping belongs with
// the rest of the theming work (T-10.06), which is what introduces the
// tokens a glamour.StyleConfig would be built from. WithColorProfile is the
// seam T-10.07's teatest golden files will need to pin a stable ASCII
// profile, the same way ADR-028 §7 fixes lipgloss.SetColorProfile for the
// rest of the TUI.
func renderMarkdown(source string, width int) (string, error) {
	if width <= 0 {
		width = defaultRenderWidth
	}
	renderer, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return source, err
	}
	out, err := renderer.Render(source)
	if err != nil {
		return source, err
	}
	return out, nil
}
