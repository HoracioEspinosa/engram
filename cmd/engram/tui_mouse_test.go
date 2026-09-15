package main

import (
	"testing"

	"github.com/HoracioEspinosa/engram/internal/store"
)

// TestTheMouseIsOnByDefault: cell motion is what makes a click land on a cell
// the workspace can reason about, so it is the state a plain `engram tui`
// opens in.
func TestTheMouseIsOnByDefault(t *testing.T) {
	withArgs(t, "engram", "tui")

	if !resolveTUIMouse(nil) {
		t.Fatalf("the mouse is off with no flag and no store")
	}
	if got := len(tuiProgramOptions(true)); got != 1 {
		t.Fatalf("tuiProgramOptions(true) returned %d options, want 1", got)
	}
}

// TestNoMouseDropsTheProgramOption is the escape hatch: enabling the mouse
// takes the terminal's own selection away, so the flag has to reach all the
// way to the program options rather than only to a field nothing reads.
func TestNoMouseDropsTheProgramOption(t *testing.T) {
	withArgs(t, "engram", "tui", "--no-mouse")

	if resolveTUIMouse(nil) {
		t.Fatalf("--no-mouse left the mouse on")
	}
	if got := len(tuiProgramOptions(resolveTUIMouse(nil))); got != 0 {
		t.Fatalf("--no-mouse still registered %d program options", got)
	}
}

// TestTheRememberedMouseSettingTurnsItOff covers settings['tui.mouse'], and
// the two ways it must not: a value nobody recognises and a key nobody wrote
// leave the mouse on rather than quietly disabling it.
func TestTheRememberedMouseSettingTurnsItOff(t *testing.T) {
	cases := []struct {
		name  string
		value string
		set   bool
		want  bool
	}{
		{name: "off", value: "off", set: true, want: false},
		{name: "off with padding and case", value: " OFF ", set: true, want: false},
		{name: "on", value: "on", set: true, want: true},
		{name: "unrecognised", value: "sometimes", set: true, want: true},
		{name: "never written", set: false, want: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withArgs(t, "engram", "tui")
			s, err := store.New(testConfig(t))
			if err != nil {
				t.Fatalf("open the store: %v", err)
			}
			defer s.Close()

			if tc.set {
				if err := s.SetSetting(mouseSettingKey, tc.value); err != nil {
					t.Fatalf("set %s: %v", mouseSettingKey, err)
				}
			}

			if got := resolveTUIMouse(s); got != tc.want {
				t.Fatalf("resolveTUIMouse = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestTheFlagBeatsTheRememberedSetting: --no-mouse is the one-off escape
// hatch, so it wins over a row that says the mouse should be on.
func TestTheFlagBeatsTheRememberedSetting(t *testing.T) {
	withArgs(t, "engram", "tui", "--no-mouse")

	s, err := store.New(testConfig(t))
	if err != nil {
		t.Fatalf("open the store: %v", err)
	}
	defer s.Close()
	if err := s.SetSetting(mouseSettingKey, "on"); err != nil {
		t.Fatalf("set %s: %v", mouseSettingKey, err)
	}

	if resolveTUIMouse(s) {
		t.Fatalf("--no-mouse lost to the remembered setting")
	}
}
