package theme

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// legiblePalette is a palette built to pass every rule, so a table test can
// break one rule at a time and know the failure it gets is the one it caused.
func legiblePalette() Palette {
	return Palette{
		Name:      "probe",
		Base:      "#000000",
		Surface:   "#111111",
		Overlay:   "#7a7a7a",
		Text:      "#ffffff",
		Subtext:   "#cccccc",
		Primary:   "#ff9e5e",
		Secondary: "#f4a8c0",
		Accent:    "#ecc369",
		Highlight: "#7fe0d4",
		Success:   "#96cf7f",
		Warning:   "#e9b949",
		Danger:    "#f4787f",
		Info:      "#74bde0",
		LogoGradient: [logoRows]lipgloss.Color{
			"#e6edef", "#ecc369", "#ff9e5e", "#f4787f", "#7fe0d4",
		},
	}
}

// TestMarshalUnmarshalRoundTripsEveryRegisteredPalette pins that a palette the
// binary ships survives a trip through the document: the thirteen roles and
// the five gradient stops come back byte for byte, whatever the palette.
func TestMarshalUnmarshalRoundTripsEveryRegisteredPalette(t *testing.T) {
	for _, builtin := range Builtins() {
		t.Run(builtin.Name, func(t *testing.T) {
			encoded, err := MarshalTheme(builtin.Name, builtin.Variant, builtin.Palette)
			if err != nil {
				t.Fatalf("MarshalTheme: %v", err)
			}

			name, variant, decoded, err := UnmarshalTheme(encoded)
			if err != nil {
				t.Fatalf("UnmarshalTheme: %v", err)
			}
			if name != builtin.Name {
				t.Errorf("name = %q, want %q", name, builtin.Name)
			}
			if variant != builtin.Variant {
				t.Errorf("variant = %q, want %q", variant, builtin.Variant)
			}

			original := builtin.Palette
			originalFields := paletteFields(&original)
			decodedFields := paletteFields(&decoded)
			for _, role := range Roles() {
				want := strings.ToLower(string(*originalFields[role]))
				if got := string(*decodedFields[role]); got != want {
					t.Errorf("role %s = %q, want %q", role, got, want)
				}
			}
			for i, stop := range original.LogoGradient {
				if got := string(decoded.LogoGradient[i]); got != strings.ToLower(string(stop)) {
					t.Errorf("gradient stop %d = %q, want %q", i, got, stop)
				}
			}

			// A second trip through the document must be byte-identical, which
			// is what makes an exported theme diffable.
			again, err := MarshalTheme(name, variant, decoded)
			if err != nil {
				t.Fatalf("second MarshalTheme: %v", err)
			}
			if string(again) != string(encoded) {
				t.Errorf("the document is not stable across a round trip:\n%s\n%s", encoded, again)
			}
		})
	}
}

// TestMarshalThemeWritesEveryRoleByName pins the document's shape: thirteen
// named roles and five gradient stops, so somebody editing it with json_set
// knows what to address.
func TestMarshalThemeWritesEveryRoleByName(t *testing.T) {
	encoded, err := MarshalTheme("probe", "dark", legiblePalette())
	if err != nil {
		t.Fatalf("MarshalTheme: %v", err)
	}

	var doc themeDocument
	if err := json.Unmarshal(encoded, &doc); err != nil {
		t.Fatalf("the document is not JSON: %v", err)
	}
	if len(doc.Palette) != 13 {
		t.Fatalf("the document carries %d roles, want 13", len(doc.Palette))
	}
	for _, role := range Roles() {
		if _, ok := doc.Palette[role]; !ok {
			t.Errorf("the document does not name the role %s", role)
		}
	}
	if len(doc.LogoGradient) != logoRows {
		t.Fatalf("the document carries %d gradient stops, want %d", len(doc.LogoGradient), logoRows)
	}
}

// TestMarshalThemePrefersTheGivenName pins that a palette exported under a new
// name says so inside the file, rather than leaving two themes claiming to be
// the same one.
func TestMarshalThemePrefersTheGivenName(t *testing.T) {
	encoded, err := MarshalTheme("koi-copy", "dark", legiblePalette())
	if err != nil {
		t.Fatalf("MarshalTheme: %v", err)
	}
	name, _, _, err := UnmarshalTheme(encoded)
	if err != nil {
		t.Fatalf("UnmarshalTheme: %v", err)
	}
	if name != "koi-copy" {
		t.Fatalf("name = %q, want koi-copy", name)
	}
}

// TestUnmarshalThemeRejectsDocumentsThatAreNotThemes pins what the decoder
// checks: the shape of a theme, not the taste of one.
func TestUnmarshalThemeRejectsDocumentsThatAreNotThemes(t *testing.T) {
	full, err := MarshalTheme("probe", "dark", legiblePalette())
	if err != nil {
		t.Fatalf("MarshalTheme: %v", err)
	}
	var doc themeDocument
	if err := json.Unmarshal(full, &doc); err != nil {
		t.Fatalf("decode the reference document: %v", err)
	}

	encode := func(t *testing.T, bend func(*themeDocument)) []byte {
		t.Helper()
		copy := themeDocument{
			Name:         doc.Name,
			Variant:      doc.Variant,
			Palette:      map[string]string{},
			LogoGradient: append([]string(nil), doc.LogoGradient...),
		}
		for k, v := range doc.Palette {
			copy.Palette[k] = v
		}
		bend(&copy)
		raw, err := json.Marshal(copy)
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		return raw
	}

	cases := []struct {
		name string
		doc  []byte
		want string
	}{
		{"not JSON at all", []byte("{"), "parse document"},
		{"no name", encode(t, func(d *themeDocument) { d.Name = "" }), "needs a name"},
		{"a name nothing else accepts", encode(t, func(d *themeDocument) { d.Name = "Koi Pond" }), "not a theme name"},
		{"a variant that is neither", encode(t, func(d *themeDocument) { d.Variant = "sepia" }), "neither dark nor light"},
		{"a role left out", encode(t, func(d *themeDocument) { delete(d.Palette, RoleAccent) }), "missing the role(s) accent"},
		{"a role left blank", encode(t, func(d *themeDocument) { d.Palette[RoleDanger] = "  " }), "missing the role(s) danger"},
		{"a gradient of four", encode(t, func(d *themeDocument) { d.LogoGradient = d.LogoGradient[:4] }), "gradient stop(s)"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, err := UnmarshalTheme(tc.doc)
			if err == nil {
				t.Fatal("expected the document to be refused")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

// TestUnmarshalThemeDefaultsTheVariantToDark pins that a document that forgets
// to say which variant it is opens on the safer of the two.
func TestUnmarshalThemeDefaultsTheVariantToDark(t *testing.T) {
	raw := []byte(`{"name":"probe","palette":{` +
		`"base":"#000000","surface":"#111111","overlay":"#7a7a7a","text":"#ffffff","subtext":"#cccccc",` +
		`"primary":"#ff9e5e","secondary":"#f4a8c0","accent":"#ecc369","highlight":"#7fe0d4",` +
		`"success":"#96cf7f","warning":"#e9b949","danger":"#f4787f","info":"#74bde0"},` +
		`"logo_gradient":["#e6edef","#ecc369","#ff9e5e","#f4787f","#7fe0d4"]}`)

	_, variant, _, err := UnmarshalTheme(raw)
	if err != nil {
		t.Fatalf("UnmarshalTheme: %v", err)
	}
	if variant != "dark" {
		t.Fatalf("variant = %q, want dark", variant)
	}
}

// TestMarshalThemeRefusesANameNothingElseAccepts pins that the encoder holds
// the same rule the decoder does, so a file it writes can always be read back.
func TestMarshalThemeRefusesANameNothingElseAccepts(t *testing.T) {
	if _, err := MarshalTheme("Koi Pond", "dark", legiblePalette()); err == nil {
		t.Fatal("expected the name to be refused")
	}
	if _, err := MarshalTheme("probe", "sepia", legiblePalette()); err == nil {
		t.Fatal("expected the variant to be refused")
	}
}

// TestRoleReadsEveryRoleAndNothingElse pins that the role accessor answers for
// all thirteen names and refuses anything that is not one, so a caller walking
// the roles cannot silently read a zero colour.
func TestRoleReadsEveryRoleAndNothingElse(t *testing.T) {
	p := legiblePalette()
	for _, role := range Roles() {
		colour, ok := p.Role(role)
		if !ok {
			t.Errorf("Role(%q) reported the name is not a role", role)
			continue
		}
		if colour == "" {
			t.Errorf("Role(%q) returned no colour", role)
		}
	}
	if colour, ok := p.Role("BASE"); !ok || colour != p.Base {
		t.Errorf("Role is case-sensitive: got %q, %v", colour, ok)
	}
	if _, ok := p.Role("chartreuse"); ok {
		t.Error("Role accepted a name that is not a role")
	}
}

// TestBuiltinsExposeEveryRegisteredPalette pins that seeding is driven by the
// registry rather than by a second list somebody has to remember to update.
func TestBuiltinsExposeEveryRegisteredPalette(t *testing.T) {
	builtins := Builtins()
	if len(builtins) != len(registry) {
		t.Fatalf("Builtins returned %d palettes, want the %d registered", len(builtins), len(registry))
	}
	for i := 1; i < len(builtins); i++ {
		if builtins[i-1].Name >= builtins[i].Name {
			t.Fatalf("Builtins is not in name order: %s before %s", builtins[i-1].Name, builtins[i].Name)
		}
	}
	for _, builtin := range builtins {
		if _, ok := registry[builtin.Name]; !ok {
			t.Errorf("%s is not a registered palette", builtin.Name)
		}
		if builtin.Variant != "dark" && builtin.Variant != "light" {
			t.Errorf("%s declares variant %q", builtin.Name, builtin.Variant)
		}
	}

	if _, ok := Builtin(DefaultThemeName); !ok {
		t.Fatalf("Builtin(%q) found nothing", DefaultThemeName)
	}
	if _, ok := Builtin("no-such-theme"); ok {
		t.Fatal("Builtin invented a palette that is not registered")
	}
}
