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

func TestPlanSlugValidationAndStatusNormalization(t *testing.T) {
	reg, root := testRegistry(t)

	if env := call(t, reg, "plan.create", map[string]any{"slug": "!!!"}); env.Status != "error" {
		t.Fatalf("symbolic slug must be rejected, got %s", env.Status)
	}
	if env := call(t, reg, "plan.update", map[string]any{"slug": "!!!", "stage": "S0", "status": "done"}); env.Status != "error" {
		t.Fatalf("symbolic update slug must be rejected, got %s", env.Status)
	}

	create := call(t, reg, "plan.create", map[string]any{
		"slug": "mixed-case",
		"stages": []any{
			map[string]any{"id": "S0", "goal": "kick off", "status": "Doing"},
		},
	})
	if create.Status != "ok" {
		t.Fatalf("create: %s %+v", create.Status, create.Warnings)
	}
	stages := create.Data.(map[string]any)["stages"].([]plans.Stage)
	if stages[0].Status != "doing" {
		t.Fatalf("stage status = %q, want doing", stages[0].Status)
	}

	update := call(t, reg, "plan.update", map[string]any{"slug": "mixed-case", "stage": "S0", "status": "DONE"})
	if update.Status != "ok" {
		t.Fatalf("update: %s %+v", update.Status, update.Warnings)
	}
	if got := update.Data.(map[string]any)["status"]; got != "done" {
		t.Fatalf("reported status = %v, want done", got)
	}
	plan, err := plans.Load(root, "mixed-case")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Stages[0].Status != "done" {
		t.Fatalf("persisted status = %q, want done", plan.Stages[0].Status)
	}
}
