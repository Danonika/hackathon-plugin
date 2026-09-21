package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hackathonkit.local/agentkit/internal/learning"
	"hackathonkit.local/agentkit/internal/plans"
)

func mcpTestRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("POSTGRES_DSN", "")
	t.Setenv("MINIO_ENDPOINT", "")
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	return root
}

func runMCP(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr strings.Builder
	code := run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestRunListTools(t *testing.T) {
	root := mcpTestRoot(t)
	code, stdout, stderr := runMCP(t, "--repo-root", root, "--list-tools")
	if code != 0 {
		t.Fatalf("list-tools code = %d, stderr = %s", code, stderr)
	}
	var descriptors []map[string]any
	if err := json.Unmarshal([]byte(stdout), &descriptors); err != nil {
		t.Fatalf("descriptors JSON: %v\n%s", err, stdout)
	}
	if len(descriptors) == 0 {
		t.Fatal("no tool descriptors returned")
	}
	if !strings.Contains(stdout, "system.context") {
		t.Fatalf("descriptors missing system.context")
	}
}

func TestRunSelftest(t *testing.T) {
	root := mcpTestRoot(t)
	code, stdout, stderr := runMCP(t, "--repo-root", root, "--selftest")
	if code != 0 {
		t.Fatalf("selftest code = %d, stderr = %s", code, stderr)
	}
	var summary struct {
		OK        bool `json:"ok"`
		ToolCount int  `json:"tool_count"`
	}
	if err := json.Unmarshal([]byte(stdout), &summary); err != nil {
		t.Fatalf("selftest JSON: %v\n%s", err, stdout)
	}
	if !summary.OK || summary.ToolCount == 0 {
		t.Fatalf("selftest summary = %+v", summary)
	}
}

func TestRunSetRoutingMode(t *testing.T) {
	root := mcpTestRoot(t)
	code, stdout, stderr := runMCP(t, "--repo-root", root, "--set-routing-mode", "fly")
	if code != 0 {
		t.Fatalf("set-routing-mode code = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(stdout, "routing mode set to fly (version 2)") {
		t.Fatalf("set-routing-mode output = %q", stdout)
	}
	data, err := os.ReadFile(filepath.Join(root, "configs", "routing.yaml"))
	if err != nil || !strings.Contains(string(data), "mode: fly") {
		t.Fatalf("routing.yaml = %q err=%v", data, err)
	}

	code, stdout, _ = runMCP(t, "--repo-root", root, "--set-routing-mode", "normal")
	if code != 0 || !strings.Contains(stdout, "version 3") {
		t.Fatalf("second set code=%d output=%q", code, stdout)
	}

	code, _, stderr = runMCP(t, "--repo-root", root, "--set-routing-mode", "bogus")
	if code != 1 || !strings.Contains(stderr, "invalid routing mode") {
		t.Fatalf("invalid mode code=%d stderr=%q", code, stderr)
	}
}

func TestRunSessionContext(t *testing.T) {
	root := mcpTestRoot(t)
	store := learning.NewStore(filepath.Join(root, "data", "index", "learnings.json"), learning.ProjectKey(root))
	for i := 0; i < 2; i++ {
		if _, err := store.Record(context.Background(), learning.RecordInput{
			Trigger: "when running session context tests",
			Insight: "seed learnings before printing the context block",
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := plans.Save(root, plans.Plan{
		Slug:   "context-plan",
		Stages: []plans.Stage{{ID: "S0", Goal: "kick off", Status: "doing"}},
	}); err != nil {
		t.Fatal(err)
	}
	decisions := filepath.Join(root, "evals", "decisions")
	if err := os.MkdirAll(decisions, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"one.json", "two.json", "note.txt"} {
		if err := os.WriteFile(filepath.Join(decisions, name), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	code, stdout, stderr := runMCP(t, "--repo-root", root, "--session-context")
	if code != 0 {
		t.Fatalf("session-context code = %d, stderr = %s", code, stderr)
	}
	for _, want := range []string{
		"Hackathon Agent Kit session context",
		"routing: normal",
		"learnings:",
		"when running session context tests",
		"plan: context-plan r1 stage S0",
		"pending eval decisions: 2",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("session context missing %q:\n%s", want, stdout)
		}
	}
}

func TestRunSessionContextLearningDisabled(t *testing.T) {
	root := mcpTestRoot(t)
	if err := os.MkdirAll(filepath.Join(root, "configs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "configs", "config.yaml"), []byte("learning:\n  enabled: false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runMCP(t, "--repo-root", root, "--session-context")
	if code != 0 {
		t.Fatalf("session-context code = %d, stderr = %s", code, stderr)
	}
	if strings.Contains(stdout, "learnings:") {
		t.Fatalf("learnings must be hidden when disabled:\n%s", stdout)
	}
}

func TestRunValidateSkills(t *testing.T) {
	root := mcpTestRoot(t)
	code, _, stderr := runMCP(t, "--repo-root", root, "--validate-skills")
	if code != 1 || !strings.Contains(stderr, "skills directory") {
		t.Fatalf("missing catalog code=%d stderr=%q", code, stderr)
	}

	skillDir := filepath.Join(root, "plugins", "hackathonkit", "skills", "broken-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# no frontmatter\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ := runMCP(t, "--repo-root", root, "--validate-skills")
	if code != 1 {
		t.Fatalf("invalid catalog code = %d", code)
	}
	var report struct {
		OK      bool `json:"ok"`
		Summary struct {
			Failed int `json:"failed"`
		} `json:"summary"`
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("report JSON: %v\n%s", err, stdout)
	}
	if report.OK || report.Summary.Failed != 1 {
		t.Fatalf("report = %+v", report)
	}
}

func TestRunFlagHandling(t *testing.T) {
	root := mcpTestRoot(t)
	code, _, _ := runMCP(t, "--repo-root", root, "--not-a-flag")
	if code != 2 {
		t.Fatalf("unknown flag code = %d, want 2", code)
	}
	code, _, _ = runMCP(t, "-h")
	if code != 0 {
		t.Fatalf("-h code = %d, want 0", code)
	}
}

func TestCountFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "nested", "c.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "d.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := countFiles(dir); got != 2 {
		t.Fatalf("countFiles = %d, want 2", got)
	}
	if got := countFiles(filepath.Join(dir, "missing")); got != 0 {
		t.Fatalf("countFiles(missing) = %d", got)
	}
}

func TestClip(t *testing.T) {
	if got := clip("  hello   world  ", 40); got != "hello world" {
		t.Fatalf("clip collapse = %q", got)
	}
	if got := clip("abcdef", 3); got != "abc..." {
		t.Fatalf("clip long = %q", got)
	}
	if got := clip("abc", 3); got != "abc" {
		t.Fatalf("clip exact = %q", got)
	}
}
