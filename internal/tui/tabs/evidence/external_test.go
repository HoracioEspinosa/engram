package evidence

import "testing"

// TestOpenerForPicksTheRightBinaryOnEveryPlatform pins openerFor's full
// darwin/not-darwin decision without depending on runtime.GOOS: unlike
// defaultOpenFile, this makes both branches verifiable from a single CI
// platform, so the coverage gate cannot pass on one OS by accident of which
// branch that OS's runtime.GOOS happens to take.
func TestOpenerForPicksTheRightBinaryOnEveryPlatform(t *testing.T) {
	tests := []struct {
		goos string
		want string
	}{
		{"darwin", "open"},
		{"linux", "xdg-open"},
		{"windows", "xdg-open"},
		{"freebsd", "xdg-open"},
		{"", "xdg-open"},
	}
	for _, tt := range tests {
		if got := openerFor(tt.goos); got != tt.want {
			t.Errorf("openerFor(%q) = %q, want %q", tt.goos, got, tt.want)
		}
	}
}
