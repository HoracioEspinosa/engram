package shared

import "testing"

func TestTruncate(t *testing.T) {
	tests := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{name: "unchanged", in: "short", max: 10, want: "short"},
		{name: "replaces newlines", in: "a\nb", max: 10, want: "a b"},
		{name: "truncated", in: "abcdefghijklmnopqrstuvwxyz", max: 5, want: "abcde..."},
		{name: "spanish accents", in: "Decisión de arquitectura", max: 8, want: "Decisión..."},
		{name: "emoji", in: "🐛🔧🚀✨🎉💡", max: 3, want: "🐛🔧🚀..."},
		{name: "mixed ascii and multibyte", in: "café☕latte", max: 5, want: "café☕..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Truncate(tt.in, tt.max)
			if got != tt.want {
				t.Fatalf("Truncate() = %q, want %q", got, tt.want)
			}
		})
	}
}
