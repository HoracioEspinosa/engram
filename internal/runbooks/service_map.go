// Package runbooks turns a checkout of the knowledge vault into the entries
// that store.SyncRunbookIndex consumes, so the `vault-fs` source of the
// runbook index does not depend on the knowledge MCP server being reachable.
package runbooks

import "strings"

// canonicalServices maps a vault `service:` frontmatter value to the project
// slug the index is keyed by. The first nine are the enum of the vault's own
// Runbook Template.md and map 1:1; the rest are the remaining slugs of the
// canonical service list, accepted even though the template does not list
// them yet.
var canonicalServices = map[string]string{
	"middleware":         "middleware",
	"nextcloud":          "nextcloud",
	"lookup":             "lookup",
	"metadata":           "metadata",
	"notifications":      "notifications",
	"portal":             "portal",
	"imaginary":          "imaginary",
	"mailing":            "mailing",
	"enterprise-server":  "enterprise-server",
	"clarodrive-patches": "clarodrive-patches",
	"metadata-extractor": "metadata-extractor",
	"connector":          "connector",
	"microframework":     "microframework",
	"desktop-client":     "desktop-client",
	"knowledge-mcp":      "knowledge-mcp",
}

// CanonicalService resolves a frontmatter service value to its slug. The
// lookup is case- and whitespace-insensitive; anything outside the map is
// rejected rather than guessed, so an unknown service shows up as a
// `unknown_service` skip that a vault PR can fix.
func CanonicalService(raw string) (string, bool) {
	slug, ok := canonicalServices[strings.ToLower(strings.TrimSpace(raw))]
	return slug, ok
}

// CanonicalServices returns the accepted service values, sorted for stable
// diagnostics output.
func CanonicalServices() []string {
	out := make([]string, 0, len(canonicalServices))
	for k := range canonicalServices {
		out = append(out, k)
	}
	// Insertion sort keeps this dependency-free and the map is tiny.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}
