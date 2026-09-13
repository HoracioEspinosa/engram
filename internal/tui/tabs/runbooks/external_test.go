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
