package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hackathonkit.local/agentkit/internal/routing/fly"
)

func runBench(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr strings.Builder
	code := run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func benchRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_BASE_URL", "http://127.0.0.1:1")
	return root
}

func writeSuite(t *testing.T, root, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRunMissingSuite(t *testing.T) {
	root := benchRoot(t)
	code, _, stderr := runBench(t, "--root", root, "--suite", "missing.jsonl")
	if code != 1 || !strings.Contains(stderr, "error:") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
}

func TestRunInvalidSuiteLine(t *testing.T) {
	root := benchRoot(t)
	writeSuite(t, root, "bad.jsonl", "not-json\n")
	code, _, stderr := runBench(t, "--root", root, "--suite", "bad.jsonl")
	if code != 1 || !strings.Contains(stderr, "suite line") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
}

func TestRunSuiteDirectory(t *testing.T) {
	root := benchRoot(t)
	if err := os.MkdirAll(filepath.Join(root, "suite-dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := runBench(t, "--root", root, "--suite", "suite-dir")
	if code != 1 || !strings.Contains(stderr, "error:") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
}

func TestRunEmptySuite(t *testing.T) {
	root := benchRoot(t)
	writeSuite(t, root, "empty.jsonl", "# nothing\n\n")
	code, _, stderr := runBench(t, "--root", root, "--suite", "empty.jsonl")
	if code != 1 || !strings.Contains(stderr, "suite has no tasks") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
}

func TestRunKeywordOnlyWithHashFallback(t *testing.T) {
	root := benchRoot(t)
	writeSuite(t, root, "suite.jsonl", strings.Join([]string{
		`{"task":"find anomalies in sensor telemetry","expected":["timeseries.analyze"]}`,
		`{"task":"build a report with findings","expected":["reports.create"]}`,
		`{"task":"please help me","expected":["reports.create"]}`,
	}, "\n")+"\n")

	code, stdout, stderr := runBench(t, "--root", root, "--suite", "suite.jsonl")
	if code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	for _, want := range []string{"fly-bench: 3 tasks", "baseline recall@k", "evidence: evals/fly-ab/unknown.json"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout missing %q:\n%s", want, stdout)
		}
	}
	evidencePath := filepath.Join(root, "evals", "fly-ab", "unknown.json")
	data, err := os.ReadFile(evidencePath)
	if err != nil {
		t.Fatalf("evidence file: %v", err)
	}
	var rep report
	if err := json.Unmarshal(data, &rep); err != nil {
		t.Fatalf("evidence JSON: %v", err)
	}
	if rep.Summary.Tasks != 3 || len(rep.Rows) != 3 {
		t.Fatalf("report = %+v", rep.Summary)
	}
	if !strings.Contains(rep.Rows[0].FlyEncoder, "embedding") {
		t.Fatalf("hash fallback encoder = %q", rep.Rows[0].FlyEncoder)
	}
}

func TestRunJSONOnly(t *testing.T) {
	root := benchRoot(t)
	writeSuite(t, root, "suite.jsonl", `{"task":"analyze the csv table","expected":["data.inspect"]}`+"\n")
	code, stdout, stderr := runBench(t, "--root", root, "--suite", "suite.jsonl", "--json")
	if code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	var rep report
	if err := json.Unmarshal([]byte(stdout), &rep); err != nil {
		t.Fatalf("json-only output: %v\n%s", err, stdout)
	}
	if rep.Summary.Tasks != 1 || len(rep.Rows) != 1 {
		t.Fatalf("report = %+v", rep)
	}
	if strings.Contains(stdout, "fly-bench:") {
		t.Fatalf("json-only output contains prose: %q", stdout)
	}
}

func TestRunFlagHandling(t *testing.T) {
	code, _, _ := runBench(t, "--not-a-flag")
	if code != 2 {
		t.Fatalf("unknown flag code = %d, want 2", code)
	}
	code, _, _ = runBench(t, "-h")
	if code != 0 {
		t.Fatalf("-h code = %d, want 0", code)
	}
}

func TestSave(t *testing.T) {
	root := t.TempDir()
	rep := report{SHA: "abc123", Suite: "suite.jsonl"}
	rel, err := save(root, rep)
	if err != nil {
		t.Fatal(err)
	}
	if rel != "evals/fly-ab/abc123.json" {
		t.Fatalf("rel = %q", rel)
	}
	data, err := os.ReadFile(filepath.Join(root, "evals", "fly-ab", "abc123.json"))
	if err != nil {
		t.Fatal(err)
	}
	var decoded report
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.SHA != "abc123" {
		t.Fatalf("decoded = %+v", decoded)
	}

	fileRoot := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(fileRoot, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := save(fileRoot, rep); err == nil {
		t.Fatal("save with a file root must fail")
	}
}

func TestSummarizeEmptyAndRecallEdges(t *testing.T) {
	out := summarize(nil, 3)
	if out.Tasks != 0 || out.TopK != 3 || out.FlyRecallAtK != 0 {
		t.Fatalf("empty summary = %+v", out)
	}
	recs := []fly.Recommendation{{Capability: "a"}, {Capability: "b"}}
	if got := recall(nil, recs, 3); got != 0 {
		t.Fatalf("recall(nil) = %v", got)
	}
	if got := recall([]string{"a"}, recs, 0); got != 1 {
		t.Fatalf("recall(k<=0) = %v", got)
	}
	if got := recall([]string{"c"}, recs, 10); got != 0 {
		t.Fatalf("recall(k>len) = %v", got)
	}
	if got := recall([]string{"a"}, nil, 3); got != 0 {
		t.Fatalf("recall(no recs) = %v", got)
	}
	if got := round2(1.2349); got != 1.23 {
		t.Fatalf("round2 = %v", got)
	}
}
