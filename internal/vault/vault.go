// Package vault reads the knowledge tree the user keeps outside any
// repository, shaped as <project>/<task>/<category>.
//
// It is pure filesystem code: it never imports internal/store, so the scanner
// stays usable from the CLI, the MCP tools and the TUI without dragging a
// database along. Every path it is about to read is resolved through
// filepath.EvalSymlinks and compared against the restricted roots by
// containment of the real paths, never by string prefix, so a symlink planted
// inside the vault cannot smuggle a restricted directory into a scan.
package vault

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DefaultMaxBytes is the size beyond which a file is recorded but not hashed.
const DefaultMaxBytes int64 = 64 << 20

// Skip reasons reported on File.SkipReason.
const (
	// SkipRestricted marks an entry whose real path lives under a restricted
	// root. The entry is never opened, hashed or descended into.
	SkipRestricted = "restricted"
	// SkipOversize marks a file larger than Options.MaxBytes. Its size and
	// modification time are reported; its content is never read.
	SkipOversize = "oversize"
)

// ErrRootUnresolved is returned by Root when no candidate carries a path.
var ErrRootUnresolved = errors.New("vault: root unresolved")

// ErrRestrictedPath is returned when a scan is asked to start at a path whose
// real location sits under a restricted root.
var ErrRestrictedPath = errors.New("vault: restricted path")

// Options tunes a scan.
type Options struct {
	// Restricted lists the roots whose contents must never be read. An empty
	// list falls back to DefaultRestrictedRoots.
	Restricted []string
	// MaxBytes is the size beyond which a file is recorded as skipped instead
	// of hashed. Zero or negative falls back to DefaultMaxBytes.
	MaxBytes int64
}

// normalized fills in the defaults so the scanners can use the options
// directly without repeating the fallbacks.
func (o Options) normalized() Options {
	out := o
	if out.MaxBytes <= 0 {
		out.MaxBytes = DefaultMaxBytes
	}
	if len(out.Restricted) == 0 {
		out.Restricted = DefaultRestrictedRoots()
	}
	return out
}

// DefaultRestrictedRoots names the directories excluded when Options leaves
// the list empty: the secrets tree the user keeps beside the vault.
func DefaultRestrictedRoots() []string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return nil
	}
	return []string{filepath.Join(home, ".clarodrive-restricted")}
}

// Root resolves the vault root from the three candidates, in precedence
// order: an explicit argument, the knowledge hub path recorded on a project
// card, and the ENGRAM_VAULT_ROOT environment variable. Blank candidates are
// ignored; when none carries a path the caller gets ErrRootUnresolved instead
// of a silent default, because guessing a knowledge root is how a scanner ends
// up indexing the wrong tree.
func Root(explicit, hubPath, env string) (string, error) {
	for _, candidate := range []string{explicit, hubPath, env} {
		if trimmed := strings.TrimSpace(candidate); trimmed != "" {
			return filepath.Clean(trimmed), nil
		}
	}
	return "", ErrRootUnresolved
}

// IsRestricted reports whether path resolves, after following every symlink,
// to a location inside one of roots. Both sides are resolved with
// filepath.EvalSymlinks first, so neither a symlink nor a sibling directory
// whose name merely starts with a restricted root's name can fool the check.
//
// Roots that do not exist cannot contain anything and are skipped. A path that
// does not exist is resolved through its closest existing ancestor, which is
// what makes the check usable before creating a file.
func IsRestricted(path string, roots []string) (bool, error) {
	if strings.TrimSpace(path) == "" || len(roots) == 0 {
		return false, nil
	}
	real, err := realPath(path)
	if err != nil {
		return false, err
	}
	for _, root := range roots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		realRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return false, fmt.Errorf("vault: resolve restricted root %s: %w", root, err)
		}
		rel, err := filepath.Rel(realRoot, real)
		if err != nil {
			continue
		}
		if rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
			return true, nil
		}
	}
	return false, nil
}

// realPath resolves every symlink in path. When path itself does not exist,
// the closest existing ancestor is resolved and the remainder appended, so the
// answer still reflects the real location rather than the requested spelling.
func realPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("vault: absolute path for %s: %w", path, err)
	}
	remainder := ""
	current := abs
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			if remainder == "" {
				return resolved, nil
			}
			return filepath.Join(resolved, remainder), nil
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("vault: resolve %s: %w", path, err)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return abs, nil
		}
		remainder = filepath.Join(filepath.Base(current), remainder)
		current = parent
	}
}
