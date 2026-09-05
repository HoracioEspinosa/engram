package store

import (
	"errors"
	"fmt"
	"strings"
)

// Sentinel errors of the knowledge_ref shape rule (RFC §9.1/§9.2). They map
// 1:1 to the error `code` the MCP and HTTP layers return, the same way the
// sentinels in projects.go do.
var (
	// ErrKnowledgeRefNotCurated rejects a pointer into the bridge's own
	// export folder: `90 - Engram/` holds observations engram itself wrote,
	// so accepting it would let a memory cite itself as curated knowledge.
	ErrKnowledgeRefNotCurated = errors.New("knowledge_ref points at 90 - Engram/, which is not curated knowledge")
	// ErrKnowledgeRefAbsolute rejects a filesystem path. A knowledge_ref is
	// vault-relative because it has to mean the same thing on every checkout.
	ErrKnowledgeRefAbsolute = errors.New("knowledge_ref must be vault-relative, not an absolute path")
	// ErrKnowledgeRefInvalid covers every remaining shape violation: no .md
	// suffix, an empty path, or a `..` segment.
	ErrKnowledgeRefInvalid = errors.New("knowledge_ref must be a vault-relative .md path")
)

// knowledgeVaultLinkPrefix is the prefix Obsidian wikilinks carry inside the
// vault (`[[Work/Claro drive/Services/…]]`). The MCP tools return `file_path`
// without it, and that shorter form is the one engram stores.
const knowledgeVaultLinkPrefix = "Work/Claro drive/"

// knowledgeMemoryFolder is the bridge's export folder inside the vault.
const knowledgeMemoryFolder = "90 - Engram/"

// NormalizeKnowledgeRef turns whatever an agent pasted into the canonical
// form engram stores (RFC §9.1): it strips `[[`/`]]`, strips the
// `Work/Claro drive/` wikilink prefix, and keeps an optional `#Anchor`
// verbatim after the path.
//
// It validates shape only. engram never reads the vault on a write path, so
// it cannot know whether the document exists; that is the agent's job with
// get_document, and the doctor check `knowledge_ref_dangling` catches the
// references whose target later disappeared from the checkout.
func NormalizeKnowledgeRef(raw string) (string, error) {
	ref := strings.TrimSpace(raw)
	if ref == "" {
		return "", fmt.Errorf("%w: empty", ErrKnowledgeRefInvalid)
	}

	// A pasted wikilink arrives as `[[Work/Claro drive/Doc.md#Anchor]]`, and
	// occasionally with an alias (`[[Doc.md|Doc]]`), which is display text
	// and never part of the reference.
	if strings.HasPrefix(ref, "[[") && strings.HasSuffix(ref, "]]") {
		ref = strings.TrimSpace(ref[2 : len(ref)-2])
		if pipe := strings.Index(ref, "|"); pipe >= 0 {
			ref = strings.TrimSpace(ref[:pipe])
		}
	}
	ref = strings.TrimPrefix(ref, knowledgeVaultLinkPrefix)
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", fmt.Errorf("%w: empty", ErrKnowledgeRefInvalid)
	}

	// Split the anchor off before any path check: `#` is legal inside a
	// heading fragment and illegal nowhere else.
	path, anchor := ref, ""
	if hash := strings.Index(ref, "#"); hash >= 0 {
		path, anchor = ref[:hash], ref[hash:]
	}
	path = strings.TrimSpace(path)

	if strings.HasPrefix(path, "/") || strings.HasPrefix(path, "~") {
		return "", fmt.Errorf("%w: %q", ErrKnowledgeRefAbsolute, raw)
	}
	if path == "" {
		return "", fmt.Errorf("%w: %q has no path before the anchor", ErrKnowledgeRefInvalid, raw)
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == ".." {
			return "", fmt.Errorf("%w: %q escapes the vault", ErrKnowledgeRefInvalid, raw)
		}
	}
	if !strings.EqualFold(pathExt(path), ".md") {
		return "", fmt.Errorf("%w: %q is not a .md document", ErrKnowledgeRefInvalid, raw)
	}
	if strings.HasPrefix(path, knowledgeMemoryFolder) {
		return "", fmt.Errorf("%w: %q", ErrKnowledgeRefNotCurated, raw)
	}

	return path + anchor, nil
}

// normalizeKnowledgeRefPtr applies NormalizeKnowledgeRef to an optional
// field, leaving nil and the explicit empty string alone: nil means "do not
// touch this column" and "" means "clear it".
func normalizeKnowledgeRefPtr(ref *string) (*string, error) {
	if ref == nil || strings.TrimSpace(*ref) == "" {
		return ref, nil
	}
	normalized, err := NormalizeKnowledgeRef(*ref)
	if err != nil {
		return nil, err
	}
	return &normalized, nil
}

// pathExt returns the extension of a slash-separated vault path. filepath.Ext
// is not used because a vault path is always POSIX, whatever the host OS.
func pathExt(path string) string {
	base := path
	if slash := strings.LastIndex(path, "/"); slash >= 0 {
		base = path[slash+1:]
	}
	dot := strings.LastIndex(base, ".")
	if dot <= 0 {
		return ""
	}
	return base[dot:]
}
