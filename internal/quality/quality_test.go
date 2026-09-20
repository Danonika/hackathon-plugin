package quality

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveAndLatest(t *testing.T) {
	root := t.TempDir()
	report := Report{
		SHA:       "abc123",
		OK:        true,
		StartedAt: time.Now().UTC(),
		Checks: []Check{
			{Name: "build", Status: "ok"},
			{Name: "test", Status: "ok"},
		},
		Summary: map[string]int{"ok": 2},
	}
	path, err := Save(root, report)
	if err != nil {
		t.Fatal(err)
	}
	if path != "evals/quality/abc123/gate.json" {
		t.Fatalf("path = %q", path)
	}
	latest, ok := Latest(root)
	if !ok || latest.SHA != "abc123" || !latest.OK {
		t.Fatalf("latest = %+v ok=%v", latest, ok)
	}
	if len(FailedChecks(latest)) != 0 {
		t.Fatalf("unexpected failures %v", FailedChecks(latest))
	}
}

func TestLatestWithoutEvidence(t *testing.T) {
	if _, ok := Latest(t.TempDir()); ok {
		t.Fatal("empty root must not report evidence")
	}
}

func TestFailedChecks(t *testing.T) {
	report := Report{Checks: []Check{
		{Name: "vet", Status: "failed"},
		{Name: "build", Status: "ok"},
		{Name: "test", Status: "timeout"},
	}}
	failed := FailedChecks(report)
	if len(failed) != 2 || failed[0] != "test (timeout)" || failed[1] != "vet (failed)" {
		t.Fatalf("failed = %v", failed)
	}
}

func TestGitSHA(t *testing.T) {
	root := t.TempDir()
	const sha = "0123456789abcdef0123456789abcdef01234567"
	if err := os.MkdirAll(filepath.Join(root, ".git", "refs", "heads"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "refs", "heads", "main"), []byte(sha+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := GitSHA(root); got != sha {
		t.Fatalf("GitSHA = %q, want %q", got, sha)
	}
	if got := GitSHA(t.TempDir()); got != "unknown" {
		t.Fatalf("GitSHA without git = %q", got)
	}
}
