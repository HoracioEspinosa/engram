package vault

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrPointerUnresolved reports a JSON Pointer that does not address anything
// in the document. Errors wrapping it always name the pointer that failed, so
// a broken benchmark map says which line to fix.
var ErrPointerUnresolved = errors.New("vault: json pointer unresolved")

// JSONPointer resolves an RFC 6901 pointer against a decoded JSON document.
// The empty pointer addresses the whole document. Object keys are unescaped
// ("~1" is "/", "~0" is "~", in that order); array indices must be decimal
// without leading zeros, and "-" (the RFC's "past the end" token) does not
// resolve on a read.
func JSONPointer(doc any, pointer string) (any, error) {
	if pointer == "" {
		return doc, nil
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, fmt.Errorf("%w: %s (must be empty or start with \"/\")", ErrPointerUnresolved, pointer)
	}
	current := doc
	for _, token := range strings.Split(pointer[1:], "/") {
		key := unescapePointerToken(token)
		switch node := current.(type) {
		case map[string]any:
			value, ok := node[key]
			if !ok {
				return nil, fmt.Errorf("%w: %s (no key %q)", ErrPointerUnresolved, pointer, key)
			}
			current = value
		case []any:
			index, err := arrayIndex(key)
			if err != nil || index >= len(node) {
				return nil, fmt.Errorf("%w: %s (no index %q)", ErrPointerUnresolved, pointer, key)
			}
			current = node[index]
		default:
			return nil, fmt.Errorf("%w: %s (%q has no member %q)", ErrPointerUnresolved, pointer, token, key)
		}
	}
	return current, nil
}

// unescapePointerToken applies the RFC 6901 escapes in the order the spec
// mandates: "~1" first, then "~0", so "~01" decodes to "~1" and not to "/".
func unescapePointerToken(token string) string {
	token = strings.ReplaceAll(token, "~1", "/")
	return strings.ReplaceAll(token, "~0", "~")
}

// arrayIndex parses an RFC 6901 array index: decimal digits, no sign, and no
// leading zero unless the index is exactly "0".
func arrayIndex(token string) (int, error) {
	if token == "" || (len(token) > 1 && token[0] == '0') {
		return 0, errors.New("invalid array index")
	}
	for _, r := range token {
		if r < '0' || r > '9' {
			return 0, errors.New("invalid array index")
		}
	}
	return strconv.Atoi(token)
}
