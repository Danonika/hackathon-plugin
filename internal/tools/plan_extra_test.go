package tools

import (
	"testing"

	"hackathonkit.local/agentkit/internal/plans"
)

func TestPlanCreateWithExplicitStages(t *testing.T) {
	reg, _ := testRegistry(t)
	env := call(t, reg, "plan.create", map[string]any{
		"slug": "custom stages",
		"task": "input -> output",
		"stages": []any{
			map[string]any{"id": "S1", "goal": "discover", "status": "doing", "timebox_minutes": float64(30)},
			map[string]any{"id": "S2", "status": ""},
			map[string]any{"goal": "no id must be skipped"},
			"not-a-map",
		},
	})
	if env.Status != "ok" {
		t.Fatalf("status %s data %+v warnings %v", env.Status, env.Data, env.Warnings)
	}
	data := env.Data.(map[string]any)
	if data["slug"] != "custom-stages" {
		t.Fatalf("slug = %v", data["slug"])
	}
	stages := data["stages"].([]plans.Stage)
	if len(stages) != 2 {
		t.Fatalf("stages = %+v", stages)
	}
	if stages[0].ID != "S1" || stages[0].Status != "doing" || stages[0].TimeboxMinute != 30 {
		t.Fatalf("first stage = %+v", stages[0])
	}
	if stages[1].ID != "S2" || stages[1].Status != "todo" {
		t.Fatalf("second stage = %+v", stages[1])
	}
	if data["summary"].(string) == "" {
		t.Fatal("summary missing")
	}
}

func TestPlanCreateInvalidStageStatus(t *testing.T) {
	reg, _ := testRegistry(t)
	env := call(t, reg, "plan.create", map[string]any{
		"slug":   "bad-stage",
		"stages": []any{map[string]any{"id": "S1", "status": "bogus"}},
	})
	if env.Status != "error" {
		t.Fatalf("invalid stage status must fail: %+v", env)
	}
}

func TestPlanUpdateGoalEvidenceAndStatus(t *testing.T) {
	reg, _ := testRegistry(t)
	if env := call(t, reg, "plan.create", map[string]any{"slug": "lifecycle"}); env.Status != "ok" {
		t.Fatalf("create: %+v", env)
	}

	update := call(t, reg, "plan.update", map[string]any{
		"slug":     "lifecycle",
		"stage":    "S3",
		"status":   "done",
		"goal":     "build the mandatory scenario",
		"evidence": []any{"artifact-1", "test-output"},
	})
	if update.Status != "ok" {
		t.Fatalf("update status %s data %+v warnings %v", update.Status, update.Data, update.Warnings)
	}
	data := update.Data.(map[string]any)
	if data["revision"].(int) != 2 || data["stage"] != "S3" || data["status"] != "done" {
		t.Fatalf("update data = %+v", data)
	}
	if data["summary"].(string) == "" {
		t.Fatal("summary missing")
	}

	if env := call(t, reg, "plan.update", map[string]any{"slug": "lifecycle", "stage": "S3"}); env.Status != "error" {
		t.Fatalf("missing status must fail: %+v", env)
	}
	if env := call(t, reg, "plan.update", map[string]any{"slug": "nope", "stage": "S3", "status": "doing"}); env.Status != "error" {
		t.Fatalf("missing plan must fail: %+v", env)
	}

	bySlug := call(t, reg, "plan.status", map[string]any{"slug": "lifecycle"})
	if bySlug.Status != "ok" {
		t.Fatalf("status by slug: %+v", bySlug)
	}
	if _, ok := bySlug.Data.(map[string]any)["current_stage"]; !ok {
		t.Fatal("current_stage missing")
	}
	latest := call(t, reg, "plan.status", map[string]any{})
	if latest.Status != "ok" {
		t.Fatalf("latest plan status: %+v", latest)
	}
	if env := call(t, reg, "plan.status", map[string]any{"slug": "does-not-exist"}); env.Status != "error" {
		t.Fatalf("unknown slug must fail: %+v", env)
	}
}
