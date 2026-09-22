package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hackathonkit.local/agentkit/internal/learning"
	"hackathonkit.local/agentkit/internal/plans"
	"hackathonkit.local/agentkit/internal/resources"
)

func mcpTestRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("POSTGRES_DSN", "")
	t.Setenv("MINIO_ENDPOINT", "")
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("NVIDIA_API_KEY", "")
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
		"resources: not configured - ask the user (system.resources)",
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
	if got := countFiles(dir, io.Discard); got != 2 {
		t.Fatalf("countFiles = %d, want 2", got)
	}
	if got := countFiles(filepath.Join(dir, "missing"), io.Discard); got != 0 {
		t.Fatalf("countFiles(missing, io.Discard) = %d", got)
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

func TestRunSessionContextResourcesProfile(t *testing.T) {
	root := mcpTestRoot(t)
	t.Setenv("OPENAI_API_KEY", "sk-test")
	if err := resources.Save(root, resources.Profile{
		Track:   "08",
		Claimed: map[string]bool{"openai_api": true},
	}); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runMCP(t, "--repo-root", root, "--session-context")
	if code != 0 {
		t.Fatalf("session-context code = %d, stderr = %s", code, stderr)
	}
	for _, want := range []string{
		"resources: openai_api=yes",
		"track=08-innovation",
		"(profile v1)",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("session context missing %q:\n%s", want, stdout)
		}
	}
}

func TestRunResourcesFlags(t *testing.T) {
	root := mcpTestRoot(t)
	code, stdout, stderr := runMCP(t, "--repo-root", root, "--resources-set", "openai_api=true,codex_pro=true,track=08")
	if code != 0 {
		t.Fatalf("resources-set code = %d, stderr = %s", code, stderr)
	}
	var view map[string]any
	if err := json.Unmarshal([]byte(stdout), &view); err != nil {
		t.Fatalf("resources-set JSON: %v\n%s", err, stdout)
	}
	profile := view["profile"].(map[string]any)
	if profile["present"] != true || profile["track"] != "08-innovation" || profile["codex_pro"] != true {
		t.Fatalf("profile = %+v", profile)
	}

	code, stdout, stderr = runMCP(t, "--repo-root", root, "--resources")
	if code != 0 || !strings.Contains(stdout, "08-innovation") {
		t.Fatalf("resources code = %d, stdout = %s, stderr = %s", code, stdout, stderr)
	}

	if code, _, stderr := runMCP(t, "--repo-root", root, "--resources-set", "track=99-nope"); code != 1 || !strings.Contains(stderr, "unknown track") {
		t.Fatalf("bad track: code = %d, stderr = %s", code, stderr)
	}
	if code, _, stderr := runMCP(t, "--repo-root", root, "--resources-set", "openai_api=maybe"); code != 1 || !strings.Contains(stderr, "true or false") {
		t.Fatalf("bad value: code = %d, stderr = %s", code, stderr)
	}
	if code, _, _ := runMCP(t, "--repo-root", root, "--resources-set", "="); code != 1 {
		t.Fatalf("empty key: code = %d", code)
	}
}

func TestRedactBackendError(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "key value with space delimiter",
			input: "connect failed: password=hunter2 host=db",
			want:  "connect failed: password=[redacted] host=db",
		},
		{
			name:  "quoted value containing spaces",
			input: `auth error: password="a b c" user=admin`,
			want:  `auth error: password="[redacted]" user=admin`,
		},
		{
			name:  "url userinfo credentials",
			input: "pgx: cannot parse `postgres://user:s3cr3t@db:5432/app`: bad dsn",
			want:  "pgx: cannot parse `postgres://[redacted]@db:5432/app`: bad dsn",
		},
		{
			name:  "minio endpoint with credentials",
			input: "minio: endpoint http://key:secret@minio:9000 rejected",
			want:  "minio: endpoint http://[redacted]@minio:9000 rejected",
		},
		{
			name:  "url password containing sub-delims",
			input: "postgres://user:p;ss,word@db:5432/app",
			want:  "postgres://[redacted]@db:5432/app",
		},
		{
			name:  "uppercase marker and access key",
			input: "API_KEY=abc123; access_key=akey",
			want:  "API_KEY=[redacted] access_key=[redacted]",
		},
		{
			name:  "no credentials unchanged",
			input: "connection refused to db:5432",
			want:  "connection refused to db:5432",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := redactBackendError(fmt.Errorf("%s", tc.input))
			if got != tc.want {
				t.Fatalf("redactBackendError(%q) = %q, want %q", tc.input, got, tc.want)
			}
			if strings.Contains(got, "s3cr3t") || strings.Contains(got, "hunter2") || strings.Contains(got, "abc123") || strings.Contains(got, "akey") || strings.Contains(got, "secret") || strings.Contains(got, "p;ss") || strings.Contains(got, "a,b") {
				t.Fatalf("secret leaked in %q", got)
			}
		})
	}
}
