// Package version checks for newer engram releases on GitHub.
package version

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"
)

// repoOwner and repoName point the update check at this fork by default.
// They are plain vars (not consts) so a downstream build can retarget them
// with -ldflags "-X <module>/internal/version.repoOwner=... -X
// <module>/internal/version.repoName=...", the same mechanism main.version
// already uses to stamp the release — no source change needed to point a
// different fork's build at its own repository.
var (
	repoOwner = "HoracioEspinosa"
	repoName  = "engram"
)

// githubReleasesListURL hits the releases *list*, not /releases/latest.
// GitHub's "latest release" endpoint excludes prereleases by design (see
// docs.github.com/en/rest/releases/releases#get-the-latest-release), and
// every tag this fork has published carries a prerelease identifier, so that
// endpoint 404s forever (ADR-045 §2). The list includes prereleases and is
// sorted by creation date, so the first entry is the one to compare against.
var (
	checkTimeout          = 2 * time.Second
	githubReleasesListURL = fmt.Sprintf("https://api.github.com/repos/%s/%s/releases", repoOwner, repoName)
	httpClient            = http.DefaultClient
)

type CheckStatus string

const (
	StatusUpToDate        CheckStatus = "up_to_date"
	StatusUpdateAvailable CheckStatus = "update_available"
	StatusCheckFailed     CheckStatus = "check_failed"
)

type CheckResult struct {
	Status  CheckStatus
	Message string
}

// githubRelease is the subset of the GitHub releases API we care about.
type githubRelease struct {
	TagName string `json:"tag_name"`
}

// CheckLatest compares the running version against the latest GitHub release.
// It distinguishes between up-to-date, update available, and check failures.
func CheckLatest(current string) CheckResult {
	switch current {
	case "":
		return checkFailed("Could not check for updates: current version is unknown.")
	case "dev":
		return checkFailed("Could not check for updates: development builds do not map to a release version.")
	}

	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubReleasesListURL, nil)
	if err != nil {
		return checkFailed("Could not check for updates: could not create the GitHub request.")
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token := githubToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return checkFailed("Could not check for updates: GitHub took too long to respond.")
		}
		return checkFailed(fmt.Sprintf("Could not check for updates: %v.", err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return checkFailed(nonOKStatusMessage(resp.Status))
	}

	var releases []githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return checkFailed("Could not check for updates: could not read the GitHub response.")
	}
	if len(releases) == 0 {
		return checkFailed("Could not check for updates: GitHub did not return a release version.")
	}

	latest := normalizeVersion(releases[0].TagName)
	running := normalizeVersion(current)

	if latest == "" {
		return checkFailed("Could not check for updates: GitHub did not return a release version.")
	}

	if latest == running {
		return CheckResult{Status: StatusUpToDate}
	}

	if !isNewer(latest, running) {
		return CheckResult{Status: StatusUpToDate}
	}

	return CheckResult{
		Status: StatusUpdateAvailable,
		Message: fmt.Sprintf(
			"Update available: %s -> %s\nTo update:\n%s",
			running, latest, updateInstructions(),
		),
	}
}

// normalizeVersion strips a leading "v" prefix.
func normalizeVersion(v string) string {
	return strings.TrimPrefix(strings.TrimSpace(v), "v")
}

// isNewer returns true if latest > current using simple semver comparison.
func isNewer(latest, current string) bool {
	latestParts := splitVersion(latest)
	currentParts := splitVersion(current)

	for i := 0; i < 3; i++ {
		if latestParts[i] > currentParts[i] {
			return true
		}
		if latestParts[i] < currentParts[i] {
			return false
		}
	}
	return false
}

// splitVersion splits "1.8.1" into [1, 8, 1]. Returns [0,0,0] on parse failure.
func splitVersion(v string) [3]int {
	var parts [3]int
	segments := strings.SplitN(v, ".", 3)
	for i, s := range segments {
		if i >= 3 {
			break
		}
		for _, c := range s {
			if c >= '0' && c <= '9' {
				parts[i] = parts[i]*10 + int(c-'0')
			} else {
				break
			}
		}
	}
	return parts
}

// updateInstructions returns platform-appropriate update commands.
//
// `go install <repoOwner>/<repoName>/...@latest` is deliberately not offered
// here: this module's own go.mod still declares the upstream import path, so
// a network `go install` built from repoOwner/repoName fails with a "module
// declares its path as" mismatch unless repoOwner/repoName are overridden
// back to the module's declared owner. Building from a local clone (`go
// install ./cmd/engram`, documented in docs/INSTALLATION.md) does not hit
// that mismatch, but it is not a one-line update command, so it is left out
// of this in-app message.
func updateInstructions() string {
	switch runtime.GOOS {
	case "darwin", "linux":
		return fmt.Sprintf("  brew update && brew upgrade %s/tap/engram-custom", repoOwner)
	default:
		return fmt.Sprintf("  Download the latest release: https://github.com/%s/%s/releases/latest", repoOwner, repoName)
	}
}

func githubToken() string {
	if token := strings.TrimSpace(os.Getenv("GH_TOKEN")); token != "" {
		return token
	}
	return strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
}

func nonOKStatusMessage(status string) string {
	msg := fmt.Sprintf("Could not check for updates: GitHub API returned %s.", status)
	if strings.HasPrefix(status, "401") || strings.HasPrefix(status, "403") {
		msg += " Set GH_TOKEN or GITHUB_TOKEN to reduce rate limits."
	}
	return msg
}

func checkFailed(message string) CheckResult {
	return CheckResult{Status: StatusCheckFailed, Message: message}
}
