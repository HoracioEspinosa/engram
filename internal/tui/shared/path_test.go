package shared

import "testing"

func TestAbbreviateHomeShortensAPathUnderHome(t *testing.T) {
	t.Setenv("HOME", "/tmp/home-x")

	if got := AbbreviateHome("/tmp/home-x/.clarodrive"); got != "~/.clarodrive" {
		t.Fatalf("AbbreviateHome() = %q, want %q", got, "~/.clarodrive")
	}
}

func TestAbbreviateHomeShortensANestedPathUnderHome(t *testing.T) {
	t.Setenv("HOME", "/tmp/home-x")

	if got := AbbreviateHome("/tmp/home-x/.clarodrive/evidence"); got != "~/.clarodrive/evidence" {
		t.Fatalf("AbbreviateHome() = %q, want %q", got, "~/.clarodrive/evidence")
	}
}

func TestAbbreviateHomeCollapsesTheHomeDirectoryItselfToATilde(t *testing.T) {
	t.Setenv("HOME", "/tmp/home-x")

	if got := AbbreviateHome("/tmp/home-x"); got != "~" {
		t.Fatalf("AbbreviateHome() = %q, want %q", got, "~")
	}
}

// TestAbbreviateHomeLeavesAPathOutsideHomeIntact pins the boundary: a path
// this workspace did not derive from $HOME (a mounted vault, say) must reach
// the screen unchanged, not truncated on a coincidental prefix match.
func TestAbbreviateHomeLeavesAPathOutsideHomeIntact(t *testing.T) {
	t.Setenv("HOME", "/tmp/home-x")

	const outside = "/mnt/external/evidence"
	if got := AbbreviateHome(outside); got != outside {
		t.Fatalf("AbbreviateHome() = %q, want it unchanged", got)
	}
}

// TestAbbreviateHomeDoesNotMatchASiblingThatSharesAPrefix guards the naive
// strings.HasPrefix implementation this is not: "/tmp/home-x-other" is a
// sibling of "/tmp/home-x", not a child of it, and must not be cut at the
// character level.
func TestAbbreviateHomeDoesNotMatchASiblingThatSharesAPrefix(t *testing.T) {
	t.Setenv("HOME", "/tmp/home-x")

	const sibling = "/tmp/home-x-other/file"
	if got := AbbreviateHome(sibling); got != sibling {
		t.Fatalf("AbbreviateHome() = %q, want it unchanged", got)
	}
}

func TestAbbreviateHomeLeavesAnEmptyPathIntact(t *testing.T) {
	t.Setenv("HOME", "/tmp/home-x")

	if got := AbbreviateHome(""); got != "" {
		t.Fatalf("AbbreviateHome() = %q, want empty", got)
	}
}

// TestAbbreviateHomeLeavesThePathIntactWhenHomeCannotBeResolved covers the
// os.UserHomeDir error branch: with HOME unset, a path has nothing to be
// abbreviated against, so it must come back exactly as given rather than
// panic or silently mangle it.
func TestAbbreviateHomeLeavesThePathIntactWhenHomeCannotBeResolved(t *testing.T) {
	t.Setenv("HOME", "")

	const path = "/tmp/home-x/.clarodrive"
	if got := AbbreviateHome(path); got != path {
		t.Fatalf("AbbreviateHome() = %q, want it unchanged", got)
	}
}
