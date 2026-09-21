package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hackathonkit.local/agentkit/internal/evaluation"
)

func TestEvalAggregateReportFiles(t *testing.T) {
	reg, root := testRegistry(t)
	valid, err := json.Marshal(inlineReport("judge-01", "sha1", 80))
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(root, "evals", "judges", "j1.json"), string(valid))
	mustWrite(t, filepath.Join(root, "evals", "judges", "bad.json"), "{not json")

	env := call(t, reg, "eval.aggregate", map[string]any{
		"reports":      []any{"evals/judges/j1.json", "evals/judges/bad.json", "evals/judges/missing.json"},
		"snapshot_sha": "sha1",
		"persist":      false,
	})
	if env.Status != "partial" {
		t.Fatalf("status %s data %+v", env.Status, env.Data)
	}
	data := env.Data.(map[string]any)
	if data["valid_judges"].(int) != 1 {
		t.Fatalf("valid_judges = %v", data["valid_judges"])
	}
	if got := len(data["dropped_invalid"].([]evaluation.InvalidReport)); got != 2 {
		t.Fatalf("dropped_invalid = %d, want 2", got)
	}
	if !strings.Contains(strings.Join(env.Warnings, "|"), "dropped as invalid") {
		t.Fatalf("warnings = %v", env.Warnings)
	}
}

func TestEvalAggregateKindPersists(t *testing.T) {
	reg, root := testRegistry(t)
	env := call(t, reg, "eval.aggregate", map[string]any{
		"reports_json": []any{
			inlineReport("judge-01", "sha2", 80),
			inlineReport("judge-02", "sha2", 82),
			inlineReport("judge-03", "sha2", 84),
		},
		"snapshot_sha": "sha2",
		"kind":         "custom kind",
	})
	if env.Status != "ok" {
		t.Fatalf("status %s data %+v", env.Status, env.Data)
	}
	path := env.Data.(map[string]any)["aggregate_path"].(string)
	if path != "evals/custom-kind/sha2/aggregate.json" {
		t.Fatalf("aggregate_path = %q", path)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
		t.Fatalf("aggregate file missing: %v", err)
	}
	if env := call(t, reg, "eval.aggregate", map[string]any{}); env.Status != "error" {
		t.Fatalf("aggregate without reports must fail: %+v", env)
	}
}

func TestEvalRankAggregatePaths(t *testing.T) {
	reg, root := testRegistry(t)
	fixture, err := json.Marshal(aggregateFixture("a", 16, 20, 16))
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(root, "evals", "aggregates", "a.json"), string(fixture))
	mustWrite(t, filepath.Join(root, "evals", "aggregates", "bad.json"), "{oops")

	env := call(t, reg, "eval.rank", map[string]any{
		"aggregates": []any{"evals/aggregates/a.json"},
		"persist":    false,
	})
	if env.Status != "ok" {
		t.Fatalf("file aggregate rank: %s %+v", env.Status, env.Warnings)
	}

	for name, args := range map[string]map[string]any{
		"bad json file":  {"aggregates": []any{"evals/aggregates/bad.json"}},
		"missing file":   {"aggregates": []any{"evals/aggregates/missing.json"}},
		"traversal":      {"aggregates": []any{"../escape.json"}},
		"inline garbage": {"aggregates_json": []any{"not-an-object"}},
	} {
		if got := call(t, reg, "eval.rank", args); got.Status != "error" {
			t.Fatalf("%s: status %s, want error (%+v)", name, got.Status, got)
		}
	}
}

func TestEvalCalibrateMissingRuns(t *testing.T) {
	reg, _ := testRegistry(t)
	report := inlineReport("judge-01", "sha1", 80)
	if env := call(t, reg, "eval.calibrate", map[string]any{"run1_json": []any{report}, "persist": false}); env.Status != "error" {
		t.Fatalf("missing run2 must fail: %+v", env)
	}
	if env := call(t, reg, "eval.calibrate", map[string]any{"run2_json": []any{report}, "persist": false}); env.Status != "error" {
		t.Fatalf("missing run1 must fail: %+v", env)
	}
	if env := call(t, reg, "eval.calibrate", map[string]any{"run1": []any{}, "run2_json": []any{report}, "persist": false}); env.Status != "error" {
		t.Fatalf("empty run1 must fail: %+v", env)
	}
}

func TestEvalCompareStopAndPersist(t *testing.T) {
	reg, root := testRegistry(t)
	env := call(t, reg, "eval.compare", map[string]any{
		"mode":         "technical",
		"candidate":    80.4,
		"best":         80.0,
		"target":       95.0,
		"noise_floor":  0.5,
		"persist":      true,
		"snapshot_sha": "sha-z",
	})
	if env.Status != "ok" {
		t.Fatalf("status %s data %+v", env.Status, env.Data)
	}
	data := env.Data.(map[string]any)
	if data["decision"] != evaluation.DecisionStop {
		t.Fatalf("decision = %v, want stop", data["decision"])
	}
	if len(env.Warnings) == 0 {
		t.Fatal("stop must warn")
	}
	path := data["decision_path"].(string)
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
		t.Fatalf("decision artifact missing: %v", err)
	}

	if env := call(t, reg, "eval.compare", map[string]any{}); env.Status != "error" {
		t.Fatalf("missing candidate must fail: %+v", env)
	}
	judge := call(t, reg, "eval.compare", map[string]any{"mode": "judge", "candidate": 70.0, "persist": false})
	if judge.Status != "ok" || judge.Data.(map[string]any)["mode"] != evaluation.ModeJudge {
		t.Fatalf("judge compare: %+v", judge)
	}
}

func TestEvalFreezeBundleErrors(t *testing.T) {
	reg, _ := testRegistry(t)
	if env := call(t, reg, "eval.freeze_bundle", map[string]any{"out": "../escape"}); env.Status != "error" {
		t.Fatalf("traversal out must fail: %+v", env)
	}
	if env := call(t, reg, "eval.freeze_bundle", map[string]any{"out": "/absolute"}); env.Status != "error" {
		t.Fatalf("absolute out must fail: %+v", env)
	}
	env := call(t, reg, "eval.freeze_bundle", map[string]any{
		"out":       "data/bundles",
		"read_only": false,
		"exclude":   []any{"*.log"},
		"profile":   "technical",
		"model":     "test-model",
	})
	if env.Status != "ok" {
		t.Fatalf("status %s data %+v warnings %v", env.Status, env.Data, env.Warnings)
	}
}

func TestPathSegmentAndResolveRepoPath(t *testing.T) {
	if got := pathSegment(""); got != "unknown" {
		t.Fatalf("pathSegment empty = %q", got)
	}
	if got := pathSegment("!!!"); got != "unknown" {
		t.Fatalf("pathSegment unsafe-only = %q", got)
	}
	if got := pathSegment("a b/c"); got != "a-b-c" {
		t.Fatalf("pathSegment slashes = %q", got)
	}
	long := strings.Repeat("a", 80)
	if got := pathSegment(long); len(got) != 64 {
		t.Fatalf("pathSegment truncation = %d", len(got))
	}

	root := t.TempDir()
	if _, err := resolveRepoPath(root, ""); err == nil {
		t.Fatal("empty path must fail")
	}
	if _, err := resolveRepoPath(root, "/abs"); err == nil {
		t.Fatal("absolute path must fail")
	}
	if _, err := resolveRepoPath(root, "C:/abs"); err == nil {
		t.Fatal("drive path must fail")
	}
	if _, err := resolveRepoPath(root, ".."); err == nil {
		t.Fatal("parent path must fail")
	}
	if _, err := resolveRepoPath(root, "../escape"); err == nil {
		t.Fatal("traversal must fail")
	}
	if got, err := resolveRepoPath(root, "a/b.json"); err != nil || got != filepath.Join(root, "a", "b.json") {
		t.Fatalf("resolveRepoPath = %q, %v", got, err)
	}
	if _, err := writeRepoJSON(root, "../escape.json", map[string]any{"a": 1}); err == nil {
		t.Fatal("writeRepoJSON traversal must fail")
	}
	rel, err := writeRepoJSON(root, "tmp/out.json", map[string]any{"a": 1})
	if err != nil || rel != "tmp/out.json" {
		t.Fatalf("writeRepoJSON = %q, %v", rel, err)
	}
	if _, err := os.Stat(filepath.Join(root, "tmp", "out.json")); err != nil {
		t.Fatalf("written file missing: %v", err)
	}
}
