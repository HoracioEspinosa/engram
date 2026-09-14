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
