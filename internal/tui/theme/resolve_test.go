package theme

import "testing"

// TestResolvePrecedence pins rfc-tui.md §8.2's precedence chain: flag beats
// env beats config beats the default, mirroring cmd/engram's
// resolveTUIProject and its TestCmdTUIResolvesProjectPrecedence.
//
// Resolve, registry and DefaultThemeName did not exist before this task, so
// there is no pre-existing bug for this test to have caught red — it was
// written alongside the implementation it pins, not against a prior defect.
func TestResolvePrecedence(t *testing.T) {
	tests := []struct {
		name              string
		flag, env, config string
		want              string
	}{
		{"flag wins with no env or config", "kanagawa", "", "", "kanagawa"},
		{"env is used without a flag", "", "kanagawa", "", "kanagawa"},
		{"config is used without a flag or env", "", "", "elephant", "elephant"},
		{"flag overrides env", "kanagawa", "elephant", "", "kanagawa"},
		{"flag overrides config", "kanagawa", "", "elephant", "kanagawa"},
		{"env overrides config", "", "kanagawa", "elephant", "kanagawa"},
		{"nothing set falls back to the default", "", "", "", DefaultThemeName},
		{"whitespace-only tiers count as unset", "  ", "\t", "", DefaultThemeName},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Resolve(tc.flag, tc.env, tc.config).Name
			if got != tc.want {
				t.Errorf("Resolve(%q, %q, %q).Name = %q, want %q", tc.flag, tc.env, tc.config, got, tc.want)
			}
		})
	}
}

// TestResolveRejectsUnknownNameWithoutFallingThrough pins the documented
// behaviour that an invalid winning tier resolves straight to the default,
// rather than silently trying the next tier down — a typo'd --theme must
// not have ENGRAM_TUI_THEME quietly take over.
func TestResolveRejectsUnknownNameWithoutFallingThrough(t *testing.T) {
	got := Resolve("not-a-real-theme", "kanagawa", "elephant").Name
	if got != DefaultThemeName {
		t.Fatalf("Resolve with an invalid flag = %q, want the default %q (not env's %q)", got, DefaultThemeName, "kanagawa")
	}
}

// TestResolveEveryRegisteredName confirms every name registry.go exposes
// round-trips through Resolve to the palette of that name.
func TestResolveEveryRegisteredName(t *testing.T) {
	for _, name := range paletteNames() {
		t.Run(name, func(t *testing.T) {
			if got := Resolve(name, "", "").Name; got != name {
				t.Errorf("Resolve(%q, ..) = %q, want %q", name, got, name)
			}
		})
	}
}

// TestUnknownName pins rfc-tui.md §10.1's smoke test line: "engram tui
// --theme desconocido cae al default con aviso" — falling back is Resolve's
// job, but a caller needs to know an invalid name was actually requested in
// order to print that "aviso" (warning). cmd/engram's cmdTUI is the actual
// caller; this pins the pure decision UnknownName makes for it.
func TestUnknownName(t *testing.T) {
	tests := []struct {
		name              string
		flag, env, config string
		want              string
	}{
		{"a registered flag reports nothing", "kanagawa", "", "", ""},
		{"an unregistered flag is reported", "not-a-real-theme", "", "", "not-a-real-theme"},
		{"an unregistered env is reported when there is no flag", "", "not-a-real-theme", "", "not-a-real-theme"},
		{"an unregistered config is reported when there is no flag or env", "", "", "not-a-real-theme", "not-a-real-theme"},
		{"a valid flag hides an invalid lower tier", "kanagawa", "not-a-real-theme", "", ""},
		{"nothing set reports nothing", "", "", "", ""},
		{"whitespace-only counts as nothing set", "  ", "", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := UnknownName(tc.flag, tc.env, tc.config); got != tc.want {
				t.Errorf("UnknownName(%q, %q, %q) = %q, want %q", tc.flag, tc.env, tc.config, got, tc.want)
			}
		})
	}
}
