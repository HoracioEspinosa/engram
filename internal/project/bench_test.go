package project

import (
	"os/exec"
	"testing"
)

// BenchmarkDetectProjectFull measures the detection every MCP tool call runs
// before it can name a project. The repository is a real one with a remote,
// because that is the branch that shells out to git — the cost the cache in
// front of this function has to remove.
func BenchmarkDetectProjectFull(b *testing.B) {
	dir := b.TempDir()
	run := func(args ...string) {
		b.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			b.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "bench@example.com")
	run("config", "user.name", "Bench User")
	run("remote", "add", "origin", "git@github.com:koi/koi-garden.git")

	if res := DetectProjectFull(dir); res.Project != "koi-garden" {
		b.Fatalf("DetectProjectFull = %+v; want project koi-garden", res)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if res := DetectProjectFull(dir); res.Project == "" {
			b.Fatalf("DetectProjectFull returned no project: %+v", res)
		}
	}
}

// BenchmarkDetectProjectCached measures the same detection through the cache an
// MCP session actually uses. It is the number that says what a tool call pays
// for naming its project on every call but the first.
func BenchmarkDetectProjectCached(b *testing.B) {
	dir := b.TempDir()
	run := func(args ...string) {
		b.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			b.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "bench@example.com")
	run("config", "user.name", "Bench User")
	run("remote", "add", "origin", "git@github.com:koi/koi-garden.git")

	d := NewDetector(0)
	if res, err := d.Detect(dir); err != nil || res.Project != "koi-garden" {
		b.Fatalf("Detect = %+v err=%v; want project koi-garden", res, err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		res, err := d.Detect(dir)
		if err != nil || res.Project == "" {
			b.Fatalf("Detect returned no project: %+v err=%v", res, err)
		}
	}
}
