package quality

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRunCheckStatuses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses POSIX utilities (true/false/sleep)")
	}
	root := t.TempDir()
	cases := []struct {
		name string
		spec checkSpec
		want string
	}{
		{"ok", checkSpec{name: "true", command: []string{"true"}, timeout: 5 * time.Second}, "ok"},
		{"failed", checkSpec{name: "false", command: []string{"false"}, timeout: 5 * time.Second}, "failed"},
		{"missing binary", checkSpec{name: "nope", command: []string{"definitely-not-a-binary-xyz"}, timeout: 5 * time.Second}, "failed"},
		{"timeout", checkSpec{name: "sleep", command: []string{"sleep", "2"}, timeout: 20 * time.Millisecond}, "timeout"},
		{"gofmt output", checkSpec{name: "gofmt", command: []string{"echo", "file.go"}, timeout: 5 * time.Second}, "failed"},
		{"gofmt clean", checkSpec{name: "gofmt", command: []string{"true"}, timeout: 5 * time.Second}, "ok"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			check := runCheck(context.Background(), root, tc.spec)
			if check.Status != tc.want {
				t.Fatalf("status = %q, want %q (output %q)", check.Status, tc.want, check.Output)
			}
			if check.Name != tc.spec.name {
				t.Fatalf("name = %q", check.Name)
			}
			if check.Command == "" {
				t.Fatal("command must be recorded")
			}
		})
	}
}

func TestRunCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	report := Run(ctx, t.TempDir())
	if report.OK {
		t.Fatal("canceled run must not be OK")
	}
	if len(report.Checks) != len(defaultChecks) {
		t.Fatalf("checks = %d, want %d", len(report.Checks), len(defaultChecks))
	}
	// A canceled parent context is reported as "canceled", not "failed".
	if report.Summary["canceled"] != len(defaultChecks) {
		t.Fatalf("summary = %v", report.Summary)
	}
	if report.StartedAt.IsZero() || report.FinishedAt.IsZero() {
		t.Fatal("timestamps must be set")
	}
}

func TestTail(t *testing.T) {
	if got := tail("short\n", 10); got != "short" {
		t.Fatalf("tail = %q", got)
	}
	long := strings.Repeat("x", 30)
	got := tail(long, 10)
	if !strings.HasPrefix(got, "...") || len(got) != 13 {
		t.Fatalf("tail = %q (len %d)", got, len(got))
	}
}

func TestPathDefaultSHA(t *testing.T) {
	root := t.TempDir()
	want := filepath.Join(Dir(root), "unknown", "gate.json")
	if got := Path(root, "   "); got != want {
		t.Fatalf("Path = %q, want %q", got, want)
	}
}

func TestSaveMkdirError(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "evals"), []byte("file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Save(root, Report{SHA: "abc"}); err == nil {
		t.Fatal("expected mkdir error")
	}
}

func TestSaveRenameError(t *testing.T) {
	root := t.TempDir()
	target := Path(root, "abc")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "occupied"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Save(root, Report{SHA: "abc"}); err == nil {
		t.Fatal("expected rename error when gate.json is a directory")
	}
}

func TestSaveCreateTempError(t *testing.T) {
	if os.Geteuid() == 0 || runtime.GOOS == "windows" {
		t.Skip("permission checks do not apply to root or Windows")
	}
	root := t.TempDir()
	dir := filepath.Dir(Path(root, "abc"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if _, err := Save(root, Report{SHA: "abc"}); err == nil {
		t.Fatal("expected create-temp error in a read-only directory")
	}
}

func TestLatestSkipsInvalidEntries(t *testing.T) {
	root := t.TempDir()
	base := Dir(root)
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "loose.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(base, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(base, "corrupt"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "corrupt", "gate.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := Latest(root); ok {
		t.Fatal("no valid report must be found")
	}
}

func TestLatestPicksNewest(t *testing.T) {
	root := t.TempDir()
	base := Dir(root)
	writeReport := func(sha string, when time.Time) {
		t.Helper()
		dir := filepath.Join(base, sha)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "gate.json")
		if err := os.WriteFile(path, []byte(`{"sha":"`+sha+`"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, when, when); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(dir, when, when); err != nil {
			t.Fatal(err)
		}
	}
	writeReport("old", time.Now().Add(-time.Hour))
	writeReport("new", time.Now())

	report, ok := Latest(root)
	if !ok || report.SHA != "new" {
		t.Fatalf("latest = %+v ok=%v", report, ok)
	}
}

func TestLatestDirIsFile(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(Dir(root)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Dir(root), []byte("file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := Latest(root); ok {
		t.Fatal("a file at the evidence dir must not yield a report")
	}
}

func TestGitSHADetached(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	sha := strings.Repeat("a", 40)
	if err := os.WriteFile(filepath.Join(root, ".git", "HEAD"), []byte(sha+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := GitSHA(root); got != sha {
		t.Fatalf("GitSHA = %q, want %q", got, sha)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "HEAD"), []byte("abc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := GitSHA(root); got != "unknown" {
		t.Fatalf("short detached HEAD = %q", got)
	}
}

func TestGitSHAPackedRefs(t *testing.T) {
	root := t.TempDir()
	gitDir := filepath.Join(root, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sha := strings.Repeat("b", 40)
	packed := "# pack-refs with: peeled fully-peeled sorted\n" +
		sha + " refs/heads/main\n" +
		"^" + strings.Repeat("c", 40) + "\n" +
		strings.Repeat("d", 40) + " refs/heads/other\n"
	if err := os.WriteFile(filepath.Join(gitDir, "packed-refs"), []byte(packed), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := GitSHA(root); got != sha {
		t.Fatalf("GitSHA = %q, want %q", got, sha)
	}

	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/missing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := GitSHA(root); got != "unknown" {
		t.Fatalf("unmatched packed ref = %q", got)
	}
}

func TestBuildEnvDisablesVCSStamping(t *testing.T) {
	// The gate runs in containers as root over bind-mounted repos whose .git is
	// owned by another user; git refuses and Go's VCS stamping fails with exit
	// status 128 unless -buildvcs=false is set.
	t.Setenv("GOFLAGS", "-mod=mod")
	flags := ""
	count := 0
	for _, kv := range buildEnv() {
		if strings.HasPrefix(kv, "GOFLAGS=") {
			flags = strings.TrimPrefix(kv, "GOFLAGS=")
			count++
		}
	}
	if count != 1 {
		t.Fatalf("GOFLAGS entries = %d, want 1", count)
	}
	if !strings.Contains(flags, "-buildvcs=false") || !strings.Contains(flags, "-mod=mod") {
		t.Fatalf("GOFLAGS = %q, want merged flags with -buildvcs=false", flags)
	}

	t.Setenv("GOFLAGS", "-buildvcs=true")
	for _, kv := range buildEnv() {
		if strings.HasPrefix(kv, "GOFLAGS=") {
			flags = strings.TrimPrefix(kv, "GOFLAGS=")
		}
	}
	if flags != "-buildvcs=true" {
		t.Fatalf("explicit GOFLAGS must be preserved, got %q", flags)
	}
}
