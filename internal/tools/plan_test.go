package tools

import (
	"os"
	"path/filepath"
	"testing"

	"hackathonkit.local/agentkit/internal/plans"
)

func TestPlanTools(t *testing.T) {
	reg, root := testRegistry(t)

	create := call(t, reg, "plan.create", map[string]any{
		"slug": "Telemetry Anomalies",
		"task": "csv -> anomalies report",
		"mandatory_scenario": map[string]any{
			"input":    "data/input/sample-telemetry.csv",
			"command":  "detect anomalies and build a report",
			"expected": "anomaly list + chart + report artifact",
		},
	})
	if create.Status != "ok" {
		t.Fatalf("create: %s %+v", create.Status, create.Warnings)
	}
	data := create.Data.(map[string]any)
	if data["slug"] != "telemetry-anomalies" {
		t.Fatalf("slug = %v", data["slug"])
	}
	if data["path"] != "evals/plans/telemetry-anomalies.json" {
		t.Fatalf("path = %v", data["path"])
	}
	stages, ok := data["stages"].([]plans.Stage)
	if !ok || len(stages) != 7 {
		t.Fatalf("expected 7 default stages, got %+v", data["stages"])
	}
	if _, err := os.Stat(filepath.Join(root, "evals", "plans", "telemetry-anomalies.json")); err != nil {
		t.Fatal(err)
	}

	update := call(t, reg, "plan.update", map[string]any{
		"slug":     "telemetry-anomalies",
		"stage":    "S3",
		"status":   "doing",
		"evidence": []any{"artifact-1"},
	})
	if update.Status != "ok" {
		t.Fatalf("update: %s %+v", update.Status, update.Warnings)
	}
	updated := update.Data.(map[string]any)
	if updated["revision"].(int) != 2 {
		t.Fatalf("revision = %v, want 2", updated["revision"])
	}
	if updated["summary"].(string) == "" {
		t.Fatal("summary missing")
	}

	status := call(t, reg, "plan.status", map[string]any{"slug": "telemetry-anomalies"})
	if status.Status != "ok" {
		t.Fatalf("status: %s", status.Status)
	}
	if _, ok := status.Data.(map[string]any)["current_stage"]; !ok {
		t.Fatal("current_stage missing")
	}

	if bad := call(t, reg, "plan.update", map[string]any{"slug": "telemetry-anomalies", "stage": "S3", "status": "bogus"}); bad.Status != "error" {
		t.Fatalf("invalid status must fail: %+v", bad)
	}
	if missing := call(t, reg, "plan.update", map[string]any{"slug": "telemetry-anomalies", "stage": "S99", "status": "doing"}); missing.Status != "error" {
		t.Fatalf("unknown stage must fail: %+v", missing)
	}
}

func TestPlanStatusWithoutPlanIsPartial(t *testing.T) {
	reg, _ := testRegistry(t)
	env := call(t, reg, "plan.status", map[string]any{})
	if env.Status != "partial" {
		t.Fatalf("expected partial without a plan, got %s", env.Status)
	}
}
