package version

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNormalizeVersion(t *testing.T) {
	tests := []struct{ in, want string }{
		{"v1.8.1", "1.8.1"},
		{"1.8.1", "1.8.1"},
		{" v2.0.0 ", "2.0.0"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := normalizeVersion(tt.in); got != tt.want {
			t.Errorf("normalizeVersion(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSplitVersion(t *testing.T) {
	tests := []struct {
		in   string
		want [3]int
	}{
		{"1.8.1", [3]int{1, 8, 1}},
		{"2.0.0", [3]int{2, 0, 0}},
		{"1.0", [3]int{1, 0, 0}},
		{"", [3]int{0, 0, 0}},
		{"1.8.1-beta", [3]int{1, 8, 1}},
	}
	for _, tt := range tests {
		if got := splitVersion(tt.in); got != tt.want {
			t.Errorf("splitVersion(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestIsNewer(t *testing.T) {
	tests := []struct {
		latest, current string
		want            bool
	}{
		{"1.8.1", "1.8.0", true},
		{"2.0.0", "1.9.9", true},
		{"1.8.1", "1.8.1", false},
		{"1.7.0", "1.8.1", false},
		{"1.8.2", "1.8.1", true},
	}
	for _, tt := range tests {
		if got := isNewer(tt.latest, tt.current); got != tt.want {
			t.Errorf("isNewer(%q, %q) = %v, want %v", tt.latest, tt.current, got, tt.want)
		}
	}
}

func TestCheckLatest(t *testing.T) {
	t.Run("dev and empty versions fail honestly", func(t *testing.T) {
		result := CheckLatest("dev")
		if result.Status != StatusCheckFailed {
			t.Fatalf("status = %q, want %q", result.Status, StatusCheckFailed)
		}
		if !strings.Contains(result.Message, "do not map to a release version") {
			t.Fatalf("message = %q", result.Message)
		}

		result = CheckLatest("")
		if result.Status != StatusCheckFailed {
			t.Fatalf("status = %q, want %q", result.Status, StatusCheckFailed)
		}
		if !strings.Contains(result.Message, "current version is unknown") {
			t.Fatalf("message = %q", result.Message)
		}
	})

	t.Run("update available", func(t *testing.T) {
		withCheckServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"tag_name":"v1.10.8"}]`))
		}))

		result := CheckLatest("1.10.7")
		if result.Status != StatusUpdateAvailable {
			t.Fatalf("status = %q, want %q", result.Status, StatusUpdateAvailable)
		}
		if !strings.Contains(result.Message, "Update available: 1.10.7 -> 1.10.8") || !strings.Contains(result.Message, "To update:") {
			t.Fatalf("message = %q", result.Message)
		}
	})

	t.Run("up to date", func(t *testing.T) {
		withCheckServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"tag_name":"v1.10.7"}]`))
		}))

		result := CheckLatest("1.10.7")
		if result.Status != StatusUpToDate {
			t.Fatalf("status = %q, want %q", result.Status, StatusUpToDate)
		}
		if result.Message != "" {
			t.Fatalf("message = %q, want empty", result.Message)
		}
	})

	// This is the exact shape of the bug ADR-045 §2 documents: every tag this
	// fork has ever published is marked prerelease, and the "latest release"
	// endpoint excludes prereleases by design, so it 404s forever. The list
	// endpoint returns them anyway; taking the first entry is the fix. Fed
	// through the pre-fix decoder (a single object, not a list), these
	// fixtures fail with a decode error instead of resolving — confirmed
	// against the pre-fix code before either was kept, so neither is a test
	// that would pass either way.
	//
	// The version numbers differ in minor version (1.21.0 vs 1.20.0), not
	// only in the -cd.N suffix, on purpose: splitVersion only compares the
	// three numeric segments before the first non-digit, so two tags that
	// differ solely in their -cd.N revision (v1.20.0-cd.3 vs v1.20.0-cd.4,
	// the actual shape of every tag this fork has published) both truncate to
	// [1, 20, 0] and compare equal. That is a real, separate bug in
	// isNewer/splitVersion, not the one this fix addresses — out of scope
	// here, reported rather than fixed alongside it.
	t.Run("resolves an all-prerelease repo from the releases list", func(t *testing.T) {
		withCheckServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, "/latest") {
				t.Fatalf("request hit the latest-release endpoint (%s), which excludes prereleases by design", r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[
				{"tag_name":"v1.21.0-cd.1","prerelease":true},
				{"tag_name":"v1.20.0-cd.4","prerelease":true},
				{"tag_name":"v1.20.0-cd.3","prerelease":true}
			]`))
		}))

		result := CheckLatest("1.20.0-cd.4")
		if result.Status != StatusUpdateAvailable {
			t.Fatalf("status = %q, want %q", result.Status, StatusUpdateAvailable)
		}
		if !strings.Contains(result.Message, "Update available: 1.20.0-cd.4 -> 1.21.0-cd.1") {
			t.Fatalf("message = %q", result.Message)
		}
	})

	t.Run("all-prerelease repo already up to date resolves without a 404", func(t *testing.T) {
		withCheckServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[
				{"tag_name":"v1.20.0-cd.4","prerelease":true},
				{"tag_name":"v1.20.0-cd.3","prerelease":true}
			]`))
		}))

		result := CheckLatest("1.20.0-cd.4")
		if result.Status != StatusUpToDate {
			t.Fatalf("status = %q, want %q", result.Status, StatusUpToDate)
		}
	})

	t.Run("empty releases list becomes check failed", func(t *testing.T) {
		withCheckServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		}))

		result := CheckLatest("1.10.7")
		if result.Status != StatusCheckFailed {
			t.Fatalf("status = %q, want %q", result.Status, StatusCheckFailed)
		}
		if !strings.Contains(result.Message, "did not return a release version") {
			t.Fatalf("message = %q", result.Message)
		}
	})

	t.Run("non-200 becomes check failed", func(t *testing.T) {
		withCheckServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "rate limited", http.StatusForbidden)
		}))

		result := CheckLatest("1.10.7")
		if result.Status != StatusCheckFailed {
			t.Fatalf("status = %q, want %q", result.Status, StatusCheckFailed)
		}
		if !strings.Contains(result.Message, "403 Forbidden") || !strings.Contains(result.Message, "GH_TOKEN") {
			t.Fatalf("message = %q", result.Message)
		}
	})

	t.Run("decode error becomes check failed", func(t *testing.T) {
		withCheckServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"tag_name":`))
		}))

		result := CheckLatest("1.10.7")
		if result.Status != StatusCheckFailed {
			t.Fatalf("status = %q, want %q", result.Status, StatusCheckFailed)
		}
		if !strings.Contains(result.Message, "could not read the GitHub response") {
			t.Fatalf("message = %q", result.Message)
		}
	})

	t.Run("missing tag becomes check failed", func(t *testing.T) {
		withCheckServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"tag_name":""}]`))
		}))

		result := CheckLatest("1.10.7")
		if result.Status != StatusCheckFailed {
			t.Fatalf("status = %q, want %q", result.Status, StatusCheckFailed)
		}
		if !strings.Contains(result.Message, "did not return a release version") {
			t.Fatalf("message = %q", result.Message)
		}
	})

	t.Run("timeout becomes check failed", func(t *testing.T) {
		withCheckTimeout(t, 20*time.Millisecond)
		withCheckServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(50 * time.Millisecond)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"tag_name":"v1.10.8"}]`))
		}))

		result := CheckLatest("1.10.7")
		if result.Status != StatusCheckFailed {
			t.Fatalf("status = %q, want %q", result.Status, StatusCheckFailed)
		}
		if !strings.Contains(result.Message, "took too long to respond") {
			t.Fatalf("message = %q", result.Message)
		}
	})
}

func TestCheckLatestUsesGitHubToken(t *testing.T) {
	t.Run("prefers GH_TOKEN", func(t *testing.T) {
		t.Setenv("GH_TOKEN", "gh-token")
		t.Setenv("GITHUB_TOKEN", "github-token")

		withCheckServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if got := r.Header.Get("Authorization"); got != "Bearer gh-token" {
				t.Fatalf("authorization = %q", got)
			}
			if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
				t.Fatalf("accept = %q", got)
			}
			_, _ = w.Write([]byte(`[{"tag_name":"v1.10.7"}]`))
		}))

		_ = CheckLatest("1.10.7")
	})

	t.Run("falls back to GITHUB_TOKEN", func(t *testing.T) {
		t.Setenv("GH_TOKEN", "")
		t.Setenv("GITHUB_TOKEN", "github-token")

		withCheckServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if got := r.Header.Get("Authorization"); got != "Bearer github-token" {
				t.Fatalf("authorization = %q", got)
			}
			_, _ = w.Write([]byte(`[{"tag_name":"v1.10.7"}]`))
		}))

		_ = CheckLatest("1.10.7")
	})

	t.Run("omits authorization header without token", func(t *testing.T) {
		t.Setenv("GH_TOKEN", "")
		t.Setenv("GITHUB_TOKEN", "")

		withCheckServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if got := r.Header.Get("Authorization"); got != "" {
				t.Fatalf("authorization = %q, want empty", got)
			}
			_, _ = w.Write([]byte(`[{"tag_name":"v1.10.7"}]`))
		}))

		_ = CheckLatest("1.10.7")
	})
}

func TestUpdateInstructions(t *testing.T) {
	msg := updateInstructions()
	if msg == "" {
		t.Fatal("expected non-empty update instructions")
	}
	if strings.Contains(msg, "Gentleman-Programming") {
		t.Fatalf("update instructions must not point at the upstream project, got: %q", msg)
	}
	if !strings.Contains(msg, repoOwner) {
		t.Fatalf("update instructions must reference repoOwner (%q), got: %q", repoOwner, msg)
	}
}

func TestRepoOwnerDefaultsToThisFork(t *testing.T) {
	if repoOwner != "HoracioEspinosa" {
		t.Fatalf("repoOwner = %q, want %q", repoOwner, "HoracioEspinosa")
	}
	if repoName != "engram" {
		t.Fatalf("repoName = %q, want %q", repoName, "engram")
	}
	if !strings.Contains(githubReleasesListURL, "HoracioEspinosa/engram") {
		t.Fatalf("githubReleasesListURL = %q, want it to target HoracioEspinosa/engram", githubReleasesListURL)
	}
}

func withCheckServer(t *testing.T, handler http.Handler) {
	t.Helper()

	srv := httptest.NewServer(handler)
	oldURL := githubReleasesListURL
	githubReleasesListURL = srv.URL
	t.Cleanup(func() {
		githubReleasesListURL = oldURL
		srv.Close()
	})
}

func withCheckTimeout(t *testing.T, timeout time.Duration) {
	t.Helper()

	oldTimeout := checkTimeout
	checkTimeout = timeout
	t.Cleanup(func() { checkTimeout = oldTimeout })
}

func TestNonOKStatusMessage(t *testing.T) {
	if got := nonOKStatusMessage(fmt.Sprintf("%d %s", http.StatusUnauthorized, http.StatusText(http.StatusUnauthorized))); !strings.Contains(got, "GH_TOKEN") {
		t.Fatalf("message = %q", got)
	}
}
