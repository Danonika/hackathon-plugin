package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestShortSHA(t *testing.T) {
	if got := shortSHA("0123456789abcdef0123456789abcdef01234567"); got != "01234567" {
		t.Fatalf("shortSHA = %q", got)
	}
	if got := shortSHA("abc"); got != "abc" {
		t.Fatalf("shortSHA(short) = %q", got)
	}
}

func TestLastLines(t *testing.T) {
	if got := lastLines("a\nb\nc\nd", 2); got != "c | d" {
		t.Fatalf("lastLines = %q", got)
	}
	if got := lastLines("only", 3); got != "only" {
		t.Fatalf("lastLines(single) = %q", got)
	}
}

func TestWriteReport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "report.json")
	rep := report{Version: version, SHA: "abc", Steps: []stepResult{{Name: "x", OK: true}}}
	if err := writeReport(path, rep); err != nil {
		t.Fatalf("writeReport: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	var decoded report
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal report: %v", err)
	}
	if decoded.SHA != "abc" || len(decoded.Steps) != 1 {
		t.Fatalf("decoded report = %+v", decoded)
	}
}

func TestFinishExitCodesAndOutput(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "report.json")

	var stdout, stderr bytes.Buffer
	rep := report{StartedAt: time.Now().UTC(), Steps: []stepResult{{Name: "system.context", Tool: "system.context", Status: "ok", OK: true}}}
	if code := finish(rep, dir, outPath, &stdout, &stderr, false, true); code != 0 {
		t.Fatalf("finish(ok) = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "PASS: 1/1 steps") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("report not written: %v", err)
	}

	stdout.Reset()
	rep = report{StartedAt: time.Now().UTC(), Steps: []stepResult{{Name: "clone", OK: false, Error: "boom"}}}
	if code := finish(rep, dir, outPath, &stdout, &stderr, false, false); code != 1 {
		t.Fatalf("finish(fail) = %d, want 1", code)
	}
	if !strings.Contains(stdout.String(), "FAIL: 0/1 steps") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestFinishJSONOnly(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	rep := report{StartedAt: time.Now().UTC(), Steps: []stepResult{}}
	if code := finish(rep, dir, filepath.Join(dir, "r.json"), &stdout, &stderr, true, true); code != 0 {
		t.Fatalf("finish(json) = %d", code)
	}
	var decoded report
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("stdout is not JSON: %v (%q)", err, stdout.String())
	}
}

func TestRunRejectsNonRepository(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--root", t.TempDir()}, &stdout, &stderr); code != 1 {
		t.Fatalf("run(non-repo) = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "does not look like the hackathonkit repository") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestListArtifacts(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "data", "artifacts", "charts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "data", "artifacts", "charts", "a.svg"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := listArtifacts(root)
	if err != nil {
		t.Fatalf("listArtifacts: %v", err)
	}
	if len(got) != 1 || got[0] != "charts/a.svg" {
		t.Fatalf("listArtifacts = %v", got)
	}
	if empty, err := listArtifacts(t.TempDir()); err != nil || len(empty) != 0 {
		t.Fatalf("listArtifacts(empty) = %v, %v", empty, err)
	}
}

func TestRehearsalReportScores(t *testing.T) {
	rep := rehearsalReport("judge-01", 80)
	scores, _ := rep["scores"].(map[string]any)
	sum := 0.0
	for _, value := range scores {
		sum += value.(float64)
	}
	if sum < 79.99 || sum > 80.01 {
		t.Fatalf("score sum = %v, want 80", sum)
	}
}
