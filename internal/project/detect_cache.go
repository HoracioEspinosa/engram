package project

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Detection is the most expensive thing an MCP call does before it does any
// work: DetectProjectFull shells out to git — twice when the repository has no
// remote — and every tool in a session repeats it from the same directory. A
// long session was measured issuing over a thousand git processes for an answer
// that had not changed since the first one.
//
// A Detector keeps that answer per directory. It is not a plain time cache: an
// entry is also checked against the two files whose change is what actually
// makes a stale answer wrong — the repository's HEAD and the nearest
// .engram/config.json — so a branch switch or a project lock being written is
// picked up on the next call rather than a minute later.
const (
	// DefaultDetectionTTL is how long a resolved detection is reused. A branch
	// switch and a new config are caught by their own stamps, so this only
	// bounds how long a change nothing stamps (a remote being renamed, a repo
	// appearing beside the directory) can go unnoticed.
	DefaultDetectionTTL = 60 * time.Second

	// unresolvedDetectionTTL applies to an answer the detector is not happy
	// with: an ambiguous directory, a directory whose name was guessed, a
	// config that did not parse. Those are the states a user is most likely to
	// be fixing right now, so they are held for seconds, not a minute.
	unresolvedDetectionTTL = 5 * time.Second

	// DetectionCacheEnv disables the cache when set to "0", sending every call
	// straight to DetectProjectFull. It is an escape hatch for diagnosing a
	// wrong answer, not a tuning knob.
	DetectionCacheEnv = "ENGRAM_PROJECT_CACHE"
)

// Detector resolves projects the way DetectProjectFull does and remembers the
// answer per working directory. The zero value is not usable; use NewDetector.
// A nil *Detector still answers — it simply never caches — so a caller that has
// not been given one is never a crash.
type Detector struct {
	ttl time.Duration
	// now is the clock, so a test can age an entry without sleeping.
	now func() time.Time

	mu      sync.Mutex
	entries map[string]detectionEntry
}

// detectionEntry is one remembered answer plus what has to stay true for it to
// keep being the answer.
type detectionEntry struct {
	result    DetectionResult
	expiresAt time.Time
	stamps    []pathStamp
}

// pathStamp records what a file looked like when an entry was made. A file that
// did not exist is stamped too: a config appearing where there was none changes
// the answer just as much as one being edited.
type pathStamp struct {
	path    string
	exists  bool
	modTime time.Time
	size    int64
}

func (p pathStamp) matches(other pathStamp) bool {
	return p.exists == other.exists && p.size == other.size && p.modTime.Equal(other.modTime)
}

// NewDetector returns a Detector holding resolved answers for ttl. A ttl of
// zero or less takes DefaultDetectionTTL.
func NewDetector(ttl time.Duration) *Detector {
	if ttl <= 0 {
		ttl = DefaultDetectionTTL
	}
	return &Detector{ttl: ttl, now: time.Now, entries: map[string]detectionEntry{}}
}

// Detect resolves the project for dir, reusing a remembered answer when one is
// still good. The DetectionResult is exactly what DetectProjectFull would have
// returned; the error is that result's own Error, returned separately so a
// caller can branch on it without reaching into the struct.
func (d *Detector) Detect(dir string) (DetectionResult, error) {
	if d == nil || !detectionCacheEnabled() {
		res := DetectProjectFull(dir)
		return res, res.Error
	}

	key := detectionCacheKey(dir)
	now := d.clock()
	if res, ok := d.lookup(key, now); ok {
		return res, res.Error
	}

	res := DetectProjectFull(dir)
	d.remember(key, res, now)
	return res, res.Error
}

// Forget drops every remembered answer. It exists for a caller that knows it
// has just changed the filesystem in a way no stamp covers.
func (d *Detector) Forget() {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.entries = map[string]detectionEntry{}
}

func (d *Detector) clock() time.Time {
	if d.now != nil {
		return d.now()
	}
	return time.Now()
}

// lookup returns the remembered answer for key when it has not expired and
// every file it was stamped against still looks the same.
func (d *Detector) lookup(key string, now time.Time) (DetectionResult, bool) {
	d.mu.Lock()
	entry, ok := d.entries[key]
	d.mu.Unlock()
	if !ok || now.After(entry.expiresAt) {
		return DetectionResult{}, false
	}
	for _, stamp := range entry.stamps {
		if !statStamp(stamp.path).matches(stamp) {
			return DetectionResult{}, false
		}
	}
	return entry.result, true
}

func (d *Detector) remember(key string, res DetectionResult, now time.Time) {
	ttl := d.ttl
	if !isSettledDetection(res) && unresolvedDetectionTTL < ttl {
		ttl = unresolvedDetectionTTL
	}
	entry := detectionEntry{
		result:    res,
		expiresAt: now.Add(ttl),
		stamps:    detectionStamps(key, res),
	}
	d.mu.Lock()
	if d.entries == nil {
		d.entries = map[string]detectionEntry{}
	}
	d.entries[key] = entry
	d.mu.Unlock()
}

// isSettledDetection reports whether the detector actually knows the answer,
// rather than having guessed it or given up on it.
func isSettledDetection(res DetectionResult) bool {
	if res.Error != nil {
		return false
	}
	switch res.Source {
	case SourceConfig, SourceGitRemote, SourceGitRoot, SourceGitChild:
		return true
	default:
		return false
	}
}

// detectionStamps lists the files whose change makes a remembered answer wrong.
//
// The config nearest the directory is always stamped, present or not: writing
// one is how a user overrides detection, and an override that takes a minute to
// take effect reads as a bug. A git-backed answer also stamps the repository's
// HEAD, so a branch switch is seen immediately, and the config at the repository
// root, which is the other place a project lock is written.
func detectionStamps(dir string, res DetectionResult) []pathStamp {
	paths := []string{configPathIn(dir)}
	switch res.Source {
	case SourceConfig:
		paths = append(paths, configPathIn(res.Path))
	case SourceGitRemote, SourceGitRoot, SourceGitChild:
		paths = append(paths, configPathIn(res.Path))
		if head := gitHeadPath(res.Path); head != "" {
			paths = append(paths, head)
		}
	}

	seen := make(map[string]bool, len(paths))
	stamps := make([]pathStamp, 0, len(paths))
	for _, path := range paths {
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		stamps = append(stamps, statStamp(path))
	}
	return stamps
}

func configPathIn(dir string) string {
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, ".engram", "config.json")
}

// gitHeadPath returns the HEAD file of the repository rooted at root, following
// the indirection a worktree or a submodule puts in place of a .git directory.
func gitHeadPath(root string) string {
	if root == "" {
		return ""
	}
	gitPath := filepath.Join(root, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return ""
	}
	if info.IsDir() {
		return filepath.Join(gitPath, "HEAD")
	}
	data, err := os.ReadFile(gitPath)
	if err != nil {
		return ""
	}
	const prefix = "gitdir:"
	line := strings.TrimSpace(string(data))
	if !strings.HasPrefix(line, prefix) {
		return ""
	}
	gitDir := strings.TrimSpace(strings.TrimPrefix(line, prefix))
	if gitDir == "" {
		return ""
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(root, gitDir)
	}
	return filepath.Join(gitDir, "HEAD")
}

// statStamp describes path as it is now. A path that cannot be stat'ed is
// stamped as absent rather than as an error: "not there" is a state the cache
// can compare against later, and it is the state a missing config is in.
func statStamp(path string) pathStamp {
	info, err := os.Stat(path)
	if err != nil {
		return pathStamp{path: path}
	}
	return pathStamp{path: path, exists: true, modTime: info.ModTime(), size: info.Size()}
}

// detectionCacheKey mirrors DetectProjectFull's own handling of an empty
// directory, so Detect("") and Detect(".") share one entry rather than running
// the same detection twice.
func detectionCacheKey(dir string) string {
	if dir == "" {
		return "."
	}
	return dir
}

func detectionCacheEnabled() bool {
	return strings.TrimSpace(os.Getenv(DetectionCacheEnv)) != "0"
}
