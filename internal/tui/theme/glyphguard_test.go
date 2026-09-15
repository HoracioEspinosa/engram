package theme

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

// TestNoGlyphIsSpelledOutsideTheThemePackage walks the TUI's production
// sources and fails on any string or rune literal holding a character above
// 0x7e.
//
// The rule it enforces is the one icons.go opens with: this package is the
// only place that spells a glyph, the same way it is the only place that
// spells a hex value. A separator, a dash or a state marker written inline
// somewhere else looks harmless until a reader opens the workspace on a
// terminal that cannot draw it, because that reader is exactly who the ascii
// mode exists for and an inline literal never reaches it. Routing every mark
// through Set.Glyph is what makes the mode mean something.
//
// Test files are exempt on purpose. A width test that measures an ideograph,
// or a clipboard test that round-trips an accent, is feeding a character in as
// data rather than drawing one, and a guard that forbade it would only be
// worked around.
func TestNoGlyphIsSpelledOutsideTheThemePackage(t *testing.T) {
	root := ".."

	var offences []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case "theme", "testdata":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		fset := token.NewFileSet()
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return parseErr
		}
		ast.Inspect(file, func(node ast.Node) bool {
			lit, ok := node.(*ast.BasicLit)
			if !ok || (lit.Kind != token.STRING && lit.Kind != token.CHAR) {
				return true
			}
			value, unquoteErr := strconv.Unquote(lit.Value)
			if unquoteErr != nil {
				// A literal this test cannot read is one it cannot judge;
				// the compiler has already accepted it.
				return true
			}
			for _, r := range value {
				if r > unicode.MaxASCII {
					offences = append(offences, filepath.ToSlash(path)+":"+
						strconv.Itoa(fset.Position(lit.Pos()).Line)+": "+
						strconv.QuoteRune(r)+" in "+lit.Value)
					break
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}

	if len(offences) > 0 {
		t.Errorf("a glyph is spelled outside the theme package (%d):\n  %s\n"+
			"add it to the catalogue in icons.go and ask for it through Set.Glyph, "+
			"so the ascii and nerd modes have an answer for it too",
			len(offences), strings.Join(offences, "\n  "))
	}
}

// TestTheGlyphGuardReadsTheTree fails if the walk above finds nothing to read.
//
// A guard that silently inspects zero files reports the same green as one that
// inspects every file and finds nothing, and the first is what a moved package
// or a renamed directory turns it into.
func TestTheGlyphGuardReadsTheTree(t *testing.T) {
	var seen int
	err := filepath.WalkDir("..", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case "theme", "testdata":
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			seen++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking ..: %v", err)
	}
	// The workspace has a root package, data, shared, app and nine tab
	// packages; a count this low means the walk stopped somewhere it should
	// not have.
	if seen < 40 {
		t.Fatalf("the glyph guard read %d production files, want at least 40", seen)
	}
}
