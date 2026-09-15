package project

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// gitRepo makes a repository under t.TempDir with origin pointing at remote,
// and returns its root. Detection is what is under test, so the repository is
// built with git itself rather than by writing .git by hand.
func gitRepo(t *testing.T, remote string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git is not available: %v", err)
	}
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	if remote != "" {
		run("remote", "add", "origin", remote)
	}
	return root
}

func writeConfig(t *testing.T, dir, projectName string, modTime time.Time) {
	t.Helper()
	configDir := filepath.Join(dir, ".engram")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", configDir, err)
	}
	path := filepath.Join(configDir, "config.json")
	if err := os.WriteFile(path, []byte(`{"project_name":"`+projectName+`"}`), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	// The stamp compares modification times, and two writes inside the same
	// filesystem tick can share one. Setting it explicitly makes the test
	// describe the change it means rather than depend on timer granularity.
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

func TestDetectionCacheHitsWithinTTL(t *testing.T) {
	root := gitRepo(t, "git@github.com:koi/first-name.git")
	d := NewDetector(time.Minute)
	base := time.Now()
	d.now = func() time.Time { return base }

	first, err := d.Detect(root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if first.Project != "first-name" || first.Source != SourceGitRemote {
		t.Fatalf("expected the remote to name the project, got %+v", first)
	}

	// Renaming the remote changes what DetectProjectFull would say, and touches
	// neither HEAD nor any config — so within the TTL the cached answer must
	// still be the one that comes back. That it does is the proof git was not
	// run a second time.
	cmd := exec.Command("git", "-C", root, "remote", "set-url", "origin", "git@github.com:koi/second-name.git")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git remote set-url: %v\n%s", err, out)
	}

	cached, err := d.Detect(root)
	if err != nil {
		t.Fatalf("Detect (cached): %v", err)
	}
	if cached.Project != "first-name" {
		t.Fatalf("expected the cached answer within the TTL, got %q", cached.Project)
	}

	d.now = func() time.Time { return base.Add(time.Minute + time.Second) }
	expired, err := d.Detect(root)
	if err != nil {
		t.Fatalf("Detect (expired): %v", err)
	}
	if expired.Project != "second-name" {
		t.Fatalf("expected the TTL to expire into the new remote, got %q", expired.Project)
	}
}

func TestDetectionCacheInvalidatesOnConfigMtime(t *testing.T) {
	root := gitRepo(t, "git@github.com:koi/ignored-remote.git")
	base := time.Now()
	writeConfig(t, root, "locked-one", base.Add(-time.Hour))

	d := NewDetector(time.Minute)
	d.now = func() time.Time { return base }

	first, err := d.Detect(root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if first.Project != "locked-one" || first.Source != SourceConfig {
		t.Fatalf("expected the config to win, got %+v", first)
	}

	// Same TTL, same HEAD: only the config changed, and that alone has to be
	// enough. A project lock that takes a minute to take effect reads as a bug.
	writeConfig(t, root, "locked-two", base.Add(-time.Minute))

	second, err := d.Detect(root)
	if err != nil {
		t.Fatalf("Detect (after rewrite): %v", err)
	}
	if second.Project != "locked-two" {
		t.Fatalf("expected the rewritten config to be read, got %q", second.Project)
	}
}

func TestDetectionCacheInvalidatesOnHeadChange(t *testing.T) {
	root := gitRepo(t, "")
	d := NewDetector(time.Minute)
	base := time.Now()
	d.now = func() time.Time { return base }

	first, err := d.Detect(root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if first.Source != SourceGitRoot {
		t.Fatalf("expected git_root for a repo with no remote, got %+v", first)
	}

	head := filepath.Join(root, ".git", "HEAD")
	before, err := os.Stat(head)
	if err != nil {
		t.Fatalf("stat HEAD: %v", err)
	}

	cmd := exec.Command("git", "-C", root, "checkout", "-b", "koi-branch")
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git checkout -b: %v\n%s", err, out)
	}
	after, err := os.Stat(head)
	if err != nil {
		t.Fatalf("stat HEAD after checkout: %v", err)
	}
	if after.ModTime().Equal(before.ModTime()) && after.Size() == before.Size() {
		t.Skip("this filesystem did not record the HEAD rewrite; nothing to detect")
	}

	// The project name does not change on a branch switch, so what is asserted
	// is that the entry was dropped: after the checkout the detector must go
	// back to the filesystem instead of answering from the stamp it no longer
	// matches.
	if _, ok := d.lookup(detectionCacheKey(root), base); ok {
		t.Fatal("expected the HEAD rewrite to invalidate the cached detection")
	}
}

func TestDetectionCacheDisabledByEnv(t *testing.T) {
	root := gitRepo(t, "git@github.com:koi/before-rename.git")
	t.Setenv(DetectionCacheEnv, "0")

	d := NewDetector(time.Minute)
	base := time.Now()
	d.now = func() time.Time { return base }

	if first, err := d.Detect(root); err != nil || first.Project != "before-rename" {
		t.Fatalf("Detect: %+v err=%v", first, err)
	}

	cmd := exec.Command("git", "-C", root, "remote", "set-url", "origin", "git@github.com:koi/after-rename.git")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git remote set-url: %v\n%s", err, out)
	}

	second, err := d.Detect(root)
	if err != nil {
		t.Fatalf("Detect (disabled): %v", err)
	}
	if second.Project != "after-rename" {
		t.Fatalf("expected no caching with %s=0, got %q", DetectionCacheEnv, second.Project)
	}
}

func TestAmbiguousDetectionExpiresQuickly(t *testing.T) {
	parent := t.TempDir()
	for _, name := range []string{"one", "two"} {
		child := filepath.Join(parent, name)
		if err := os.MkdirAll(filepath.Join(child, ".git"), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", child, err)
		}
	}

	d := NewDetector(time.Minute)
	base := time.Now()
	d.now = func() time.Time { return base }

	first, err := d.Detect(parent)
	if err == nil {
		t.Fatalf("expected an ambiguous directory to report one, got %+v", first)
	}
	if first.Source != SourceAmbiguous {
		t.Fatalf("expected source %q, got %+v", SourceAmbiguous, first)
	}

	if err := os.RemoveAll(filepath.Join(parent, "two")); err != nil {
		t.Fatalf("remove child repo: %v", err)
	}

	// Still ambiguous a second later: the answer is held, just not for long.
	d.now = func() time.Time { return base.Add(time.Second) }
	if held, err := d.Detect(parent); err == nil {
		t.Fatalf("expected the ambiguous answer to still be held after a second, got %+v", held)
	}

	// An unresolved answer is the state a user is most likely fixing right now,
	// so it expires in seconds rather than in a minute.
	d.now = func() time.Time { return base.Add(unresolvedDetectionTTL + time.Second) }
	resolved, err := d.Detect(parent)
	if err != nil {
		t.Fatalf("Detect (after the short TTL): %v", err)
	}
	if resolved.Source != SourceGitChild || resolved.Project != "one" {
		t.Fatalf("expected the remaining child to be promoted, got %+v", resolved)
	}
}

func TestNilDetectorStillDetects(t *testing.T) {
	root := gitRepo(t, "git@github.com:koi/nil-safe.git")
	var d *Detector
	res, err := d.Detect(root)
	if err != nil {
		t.Fatalf("Detect on a nil detector: %v", err)
	}
	if res.Project != "nil-safe" {
		t.Fatalf("expected detection to still work, got %+v", res)
	}
}
