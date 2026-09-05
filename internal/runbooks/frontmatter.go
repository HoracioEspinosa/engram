package runbooks

import (
	"bufio"
	"strings"
)

// frontmatter is the parsed YAML header of a vault note. Only the subset the
// vault actually uses is understood: top-level scalars, block sequences
// (`key:` followed by indented `- item` lines) and inline flow sequences
// (`key: [a, b]`). Nested mappings are skipped rather than flattened, because
// no runbook field the index needs is nested.
type frontmatter struct {
	scalars map[string]string
	lists   map[string][]string
}

func (f frontmatter) str(key string) string { return f.scalars[key] }

func (f frontmatter) list(key string) []string { return f.lists[key] }

// hasFrontmatter reports whether the document opens with a `---` fence. A
// note without one carries no metadata and is not indexable.
func hasFrontmatter(doc string) bool {
	return strings.HasPrefix(doc, "---\n") || strings.HasPrefix(doc, "---\r\n")
}

// parseFrontmatter reads the YAML header of doc. It never fails: a malformed
// header yields whatever keys were readable, and the caller's required-field
// checks reject the note.
func parseFrontmatter(doc string) frontmatter {
	fm := frontmatter{scalars: map[string]string{}, lists: map[string][]string{}}
	if !hasFrontmatter(doc) {
		return fm
	}

	sc := bufio.NewScanner(strings.NewReader(doc))
	// Vault notes carry long root_cause lines; the default 64 KiB token limit
	// is enough, but a runbook with a very long line must not truncate the
	// rest of the header silently.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	inHeader := false
	currentList := ""
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if !inHeader {
			if strings.TrimSpace(line) == "---" {
				inHeader = true
			}
			continue
		}
		if strings.TrimSpace(line) == "---" {
			break
		}

		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Indented line: either an item of the sequence opened by the last
		// key, or part of a nested mapping we do not model.
		if line != trimmed {
			if currentList != "" && strings.HasPrefix(trimmed, "- ") {
				fm.lists[currentList] = append(fm.lists[currentList], unquote(trimmed[2:]))
			}
			continue
		}

		key, value, ok := splitKeyValue(trimmed)
		if !ok {
			continue
		}
		currentList = ""
		switch {
		case value == "":
			// `key:` opens a block sequence (or a nested map, which the
			// indented branch above ignores).
			currentList = key
			if _, exists := fm.lists[key]; !exists {
				fm.lists[key] = nil
			}
		case strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]"):
			fm.lists[key] = splitFlowSequence(value)
		default:
			fm.scalars[key] = unquote(value)
		}
	}
	return fm
}

// splitKeyValue splits `key: value` on the first colon that is followed by a
// space or ends the line, so `title: "Preview: slow"` keeps its colon.
func splitKeyValue(line string) (key, value string, ok bool) {
	idx := strings.Index(line, ":")
	if idx <= 0 {
		return "", "", false
	}
	rest := line[idx+1:]
	if rest != "" && !strings.HasPrefix(rest, " ") {
		return "", "", false
	}
	return strings.TrimSpace(line[:idx]), strings.TrimSpace(rest), true
}

// splitFlowSequence parses `[a, b, "c, d"]` honouring quoted items.
func splitFlowSequence(value string) []string {
	inner := strings.TrimSpace(value[1 : len(value)-1])
	if inner == "" {
		return nil
	}
	var out []string
	var buf strings.Builder
	quote := byte(0)
	for i := 0; i < len(inner); i++ {
		c := inner[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
				continue
			}
			buf.WriteByte(c)
		case c == '"' || c == '\'':
			quote = c
		case c == ',':
			out = append(out, strings.TrimSpace(buf.String()))
			buf.Reset()
		default:
			buf.WriteByte(c)
		}
	}
	if last := strings.TrimSpace(buf.String()); last != "" {
		out = append(out, last)
	}
	return out
}

// unquote strips one layer of matching quotes and trailing inline comments
// are left alone: vault values legitimately contain `#` inside prose.
func unquote(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}
