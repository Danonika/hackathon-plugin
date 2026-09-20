package tools

import (
	"os"
	"path/filepath"
	"testing"

	"hackathonkit.local/agentkit/internal/evaluation"
)

func inlineReport(judgeID, sha string, total float64) map[string]any {
	return map[string]any{
		"judge_id":     judgeID,
		"snapshot_sha": sha,
		"scores": map[string]any{
			"value":                total * 0.25,
			"result_quality":       total * 0.20,
			"innovation":           total * 0.15,
			"scalability":          total * 0.20,
			"presentation_demo_qa": total * 0.20,
		},
		"evidence": []any{map[string]any{
			"criterion": "value",
			"finding":   "scenario works",
			"source":    "smoke-results.json",
		}},
	}
}

func TestEvalAggregatePersists(t *testing.T) {
	reg, root := testRegistry(t)
	env := call(t, reg, "eval.aggregate", map[string]any{
		"reports_json": []any{
			inlineReport("judge-01", "sha1", 80),
			inlineReport("judge-02", "sha1", 90),
			inlineReport("judge-03", "sha1", 70),
		},
		"snapshot_sha": "sha1",
	})
	if env.Status != "ok" {
		t.Fatalf("status %s data %+v warnings %v", env.Status, env.Data, env.Warnings)
	}
	data := env.Data.(map[string]any)
	if data["valid_judges"].(int) != 3 {
		t.Fatalf("valid_judges = %v, want 3", data["valid_judges"])
	}
	if data["mean_total"].(float64) != 80 {
		t.Fatalf("mean_total = %v, want 80", data["mean_total"])
	}
	path := data["aggregate_path"].(string)
	if path != "evals/judges/sha1/aggregate.json" {
		t.Fatalf("aggregate_path = %q", path)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
		t.Fatalf("aggregate artifact missing: %v", err)
	}
}

func TestEvalAggregateInconclusive(t *testing.T) {
	reg, _ := testRegistry(t)
	env := call(t, reg, "eval.aggregate", map[string]any{
		"reports_json": []any{
			inlineReport("judge-01", "sha1", 80),
		},
		"snapshot_sha": "sha1",
		"persist":      false,
	})
	if env.Status != "partial" {
		t.Fatalf("status %s, want partial", env.Status)
	}
	if !env.Data.(map[string]any)["inconclusive"].(bool) {
		t.Fatal("expected inconclusive aggregate")
	}
}

func TestEvalAggregateDropsInvalidAndRejectsTraversal(t *testing.T) {
	reg, _ := testRegistry(t)
	noEvidence := inlineReport("judge-02", "sha1", 90)
	delete(noEvidence, "evidence")
	env := call(t, reg, "eval.aggregate", map[string]any{
		"reports_json": []any{inlineReport("judge-01", "sha1", 80), noEvidence},
		"reports":      []any{"../escape.json"},
		"snapshot_sha": "sha1",
		"persist":      false,
	})
	if env.Status != "partial" {
		t.Fatalf("status %s, want partial", env.Status)
	}
	data := env.Data.(map[string]any)
	if got := len(data["dropped_invalid"].([]evaluation.InvalidReport)); got != 2 {
		t.Fatalf("dropped_invalid = %d, want 2", got)
	}
}

func TestEvalCalibrate(t *testing.T) {
	reg, root := testRegistry(t)
	env := call(t, reg, "eval.calibrate", map[string]any{
		"run1_json": []any{
			inlineReport("judge-01", "sha1", 80),
			inlineReport("judge-02", "sha1", 80),
			inlineReport("judge-03", "sha1", 80),
		},
		"run2_json": []any{
			inlineReport("judge-01", "sha1", 83),
			inlineReport("judge-02", "sha1", 83),
			inlineReport("judge-03", "sha1", 83),
		},
		"snapshot_sha": "sha1",
	})
	if env.Status != "ok" {
		t.Fatalf("status %s data %+v warnings %v", env.Status, env.Data, env.Warnings)
	}
	cal, ok := env.Data.(map[string]any)["calibration"].(evaluation.Calibration)
	if !ok {
		t.Fatalf("calibration missing: %+v", env.Data)
	}
	if cal.RetestDelta != 3 {
		t.Fatalf("retest_delta = %v, want 3", cal.RetestDelta)
	}
	if cal.NoiseFloor != 6 {
		t.Fatalf("noise_floor = %v, want 6", cal.NoiseFloor)
	}
	path := env.Data.(map[string]any)["calibration_path"].(string)
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
		t.Fatalf("calibration artifact missing: %v", err)
	}
}

func TestEvalCompare(t *testing.T) {
	reg, _ := testRegistry(t)
	reached := call(t, reg, "eval.compare", map[string]any{
		"mode": "technical", "candidate": 96.0, "best": 80.0, "target": 95.0,
	})
	if reached.Data.(map[string]any)["decision"] != evaluation.DecisionReached {
		t.Fatalf("expected target_reached, got %+v", reached.Data)
	}
	rollback := call(t, reg, "eval.compare", map[string]any{
		"mode": "technical", "candidate": 90.0, "best": 90.0, "mandatory_scenario_failed": true,
	})
	if rollback.Data.(map[string]any)["decision"] != evaluation.DecisionRollback {
		t.Fatalf("expected rollback, got %+v", rollback.Data)
	}
	if len(rollback.Warnings) == 0 {
		t.Fatal("rollback must carry a warning")
	}
	judge := call(t, reg, "eval.compare", map[string]any{
		"mode": "judge", "candidate": 87.0, "best": 90.0, "noise_floor": 2.0,
	})
	if judge.Data.(map[string]any)["decision"] != evaluation.DecisionRollback {
		t.Fatalf("expected judge rollback, got %+v", judge.Data)
	}
}

func TestEvalFreezeBundle(t *testing.T) {
	reg, root := testRegistry(t)
	mustWrite(t, filepath.Join(root, "logs", "mcp-calls.jsonl"), "{}\n")
	mustWrite(t, filepath.Join(root, "README.md"), "# demo\n")

	env := call(t, reg, "eval.freeze_bundle", map[string]any{"model": "test-model"})
	if env.Status != "ok" {
		t.Fatalf("status %s data %+v warnings %v", env.Status, env.Data, env.Warnings)
	}
	data := env.Data.(map[string]any)
	if data["bundle_path"] != "judge-bundle" {
		t.Fatalf("bundle_path = %v", data["bundle_path"])
	}
	hash := data["bundle_hash"].(string)
	if len(hash) != 64 {
		t.Fatalf("bundle_hash = %q", hash)
	}
	if _, err := os.Stat(filepath.Join(root, "judge-bundle", "README.md")); err != nil {
		t.Fatalf("bundle missing README: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "judge-bundle", "logs")); !os.IsNotExist(err) {
		t.Fatal("logs must be excluded from the bundle")
	}
	if len(env.Warnings) == 0 {
		t.Fatal("missing git sha should warn")
	}
}
