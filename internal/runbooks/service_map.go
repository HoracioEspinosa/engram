// Package runbooks turns a checkout of the knowledge vault into the entries
// that store.SyncRunbookIndex consumes, so the `vault-fs` source of the
// runbook index does not depend on the knowledge MCP server being reachable.
package runbooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// servicesFileName is the vault-relative service map file: a checkout that
// wants its own `service:` values recognised drops one at
// <vault>/Runbooks/servicesFileName.
const servicesFileName = "services.json"

// servicesFileEnv points NewServiceMap at a services file directly, for a
// caller with no vault checkout at hand (the "knowledge-mcp" source) or one
// that wants to override the vault-relative default.
const servicesFileEnv = "ENGRAM_RUNBOOK_SERVICES"

// legacyDefaultServices is the fifteen-slug list the index shipped with
// before the service map became data-driven. It is the last-resort tier of
// every ServiceMap's resolution order, so a vault that has not adopted
// services.json yet — or a caller that wires neither a file nor a resolver —
// keeps resolving exactly as before.
var legacyDefaultServices = map[string]string{
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

// ServiceResolver reports whether slug names a known service, for a caller
// that can answer from its own state — for example, "does a project card
// with this slug exist". internal/runbooks never imports internal/store to
// make that check itself: the caller (index or CLI) wires whatever backs the
// answer through ServiceMapOptions.Resolver.
type ServiceResolver func(slug string) bool

// ServiceMapOptions configures NewServiceMap.
type ServiceMapOptions struct {
	// VaultDir is a vault checkout root. When set, and ServicesFile and
	// ENGRAM_RUNBOOK_SERVICES are both empty, NewServiceMap looks for
	// <VaultDir>/Runbooks/services.json.
	VaultDir string
	// ServicesFile, when set, is read instead of ENGRAM_RUNBOOK_SERVICES and
	// instead of the vault-relative default. It is mainly for tests and for
	// a caller that already knows exactly which file to read.
	ServicesFile string
	// Resolver is consulted for any raw value the services file does not
	// cover, before ServiceMap falls back to the compatibility default.
	Resolver ServiceResolver
}

// ServiceMap resolves a vault `service:` frontmatter value to the project
// slug the runbook index is keyed by.
type ServiceMap struct {
	fileAliases map[string]string // normalized service value -> slug
	resolver    ServiceResolver
}

// NewServiceMap builds a ServiceMap from opts. Canonical tries, in order:
//
//  1. the services file — opts.ServicesFile, else the path in
//     ENGRAM_RUNBOOK_SERVICES, else <opts.VaultDir>/Runbooks/services.json —
//     matching a declared slug or one of its aliases;
//  2. opts.Resolver, when the file (if any) did not cover the value;
//  3. the fifteen-slug compatibility default.
//
// A services file that is absent, unreadable or fails to parse is not an
// error: NewServiceMap treats it the same as no file at all, so a vault that
// has not adopted services.json yet behaves exactly as it did before this
// map existed.
func NewServiceMap(opts ServiceMapOptions) *ServiceMap {
	sm := &ServiceMap{resolver: opts.Resolver}

	path := strings.TrimSpace(opts.ServicesFile)
	if path == "" {
		path = strings.TrimSpace(os.Getenv(servicesFileEnv))
	}
	if path == "" && strings.TrimSpace(opts.VaultDir) != "" {
		path = filepath.Join(opts.VaultDir, runbooksDir, servicesFileName)
	}
	if path != "" {
		sm.fileAliases = loadServiceMapFile(path)
	}
	return sm
}

// Canonical resolves raw to a project slug through this map's configured
// sources, falling through file, resolver and compatibility default in that
// order. A nil ServiceMap behaves like one built from ServiceMapOptions{}.
func (m *ServiceMap) Canonical(raw string) (string, bool) {
	key := normalizeServiceKey(raw)
	if key == "" {
		return "", false
	}
	if m != nil {
		if slug, ok := m.fileAliases[key]; ok {
			return slug, true
		}
		if m.resolver != nil && m.resolver(key) {
			return key, true
		}
	}
	if slug, ok := legacyDefaultServices[key]; ok {
		return slug, true
	}
	return "", false
}

// CanonicalService resolves a frontmatter service value with no vault or
// resolver context: through ENGRAM_RUNBOOK_SERVICES if set, otherwise the
// fifteen-slug compatibility default. A caller scanning a specific vault
// checkout should build its own ServiceMap with NewServiceMap instead, so a
// services.json next to that vault's Runbooks/ folder is honored.
func CanonicalService(raw string) (string, bool) {
	return NewServiceMap(ServiceMapOptions{}).Canonical(raw)
}

// CanonicalServices returns the compatibility default's accepted service
// values, sorted for stable diagnostics output.
func CanonicalServices() []string {
	out := make([]string, 0, len(legacyDefaultServices))
	for k := range legacyDefaultServices {
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

// normalizeServiceKey folds a raw frontmatter value into map-lookup shape:
// trimmed and lower-cased, the same normalization CanonicalService has
// always applied.
func normalizeServiceKey(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

// serviceMapFile is the services.json shape:
// {"services":[{"slug":"koi-garden","aliases":["koi","garden"]}]}.
type serviceMapFile struct {
	Services []serviceMapFileEntry `json:"services"`
}

type serviceMapFileEntry struct {
	Slug    string   `json:"slug"`
	Aliases []string `json:"aliases"`
}

// loadServiceMapFile reads and parses a services.json file into a normalized
// alias map, keyed by normalizeServiceKey and valued by the slug as declared
// in the file. A missing file, an unreadable one, one that fails to parse,
// or one with no usable entries all return nil — the caller falls through to
// the resolver and the compatibility default, same as a vault that never had
// the file.
func loadServiceMapFile(path string) map[string]string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var parsed serviceMapFile
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil
	}

	aliases := make(map[string]string)
	for _, entry := range parsed.Services {
		slug := strings.TrimSpace(entry.Slug)
		if slug == "" {
			continue
		}
		aliases[normalizeServiceKey(slug)] = slug
		for _, alias := range entry.Aliases {
			key := normalizeServiceKey(alias)
			if key == "" {
				continue
			}
			aliases[key] = slug
		}
	}
	if len(aliases) == 0 {
		return nil
	}
	return aliases
}
