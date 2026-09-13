package runbooks

import "testing"

func TestResolveEditorHonoursTheEnvOverride(t *testing.T) {
	t.Setenv(editorEnv, "nvim")

	if got := resolveEditor(); got != "nvim" {
		t.Fatalf("resolveEditor() = %q, want the $EDITOR override", got)
	}
}

func TestResolveEditorFallsBackWhenUnset(t *testing.T) {
	t.Setenv(editorEnv, "")

	if got := resolveEditor(); got != defaultEditorFallback {
		t.Fatalf("resolveEditor() = %q, want the fallback %q", got, defaultEditorFallback)
	}
}

func TestResolveEditorTrimsBlankValues(t *testing.T) {
	t.Setenv(editorEnv, "   ")

	if got := resolveEditor(); got != defaultEditorFallback {
		t.Fatalf("resolveEditor() = %q, want the fallback for a blank value", got)
	}
}

// TestDefaultExecEditorBuildsACommandWithoutRunningIt exercises the real
// (non-injected) execEditor implementation directly. Calling the returned
// tea.Cmd here is safe without suspending the test for a real editor:
// tea.ExecProcess wraps the *exec.Cmd into an execMsg{} value (bubbletea's
// exec.go) and only the running Program's event loop ever executes it —
// invoking the tea.Cmd func outside of a Program, as this test does, only
// builds and returns that message.
func TestDefaultExecEditorBuildsACommandWithoutRunningIt(t *testing.T) {
	cmd := defaultExecEditor("/tmp/RB-900.md")
	if cmd == nil {
		t.Fatal("defaultExecEditor should return a non-nil tea.Cmd")
	}
	if msg := cmd(); msg == nil {
		t.Fatal("invoking the command should produce a non-nil message describing the exec request")
	}
}
