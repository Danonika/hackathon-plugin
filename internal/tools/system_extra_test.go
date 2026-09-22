package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSystemCapabilities(t *testing.T) {
	reg, _ := testRegistry(t)
	env := call(t, reg, "system.capabilities", map[string]any{})
	if env.Status != "ok" {
		t.Fatalf("status %s data %+v", env.Status, env.Data)
	}
	data := env.Data.(map[string]any)
	groups := data["groups"].(map[string]bool)
	for _, name := range []string{
		"documents", "rag", "speech", "vision", "data_analysis", "timeseries",
		"classification", "charts", "reports", "evaluation", "learning",
		"context_budget", "telegram", "fly_router_available",
	} {
		if !groups[name] {
			t.Fatalf("group %q must be available: %+v", name, groups)
		}
	}
	experimental := data["experimental"].(map[string]any)
	if experimental["fly_router"] != true {
		t.Fatalf("experimental = %+v", experimental)
	}
	if tools := data["tools"].([]string); len(tools) == 0 {
		t.Fatal("expected a tool list")
	}
}

func TestFlyFeedbackTool(t *testing.T) {
	reg, root := testRegistry(t)

	if env := call(t, reg, "routing.fly_feedback", map[string]any{}); env.Status != "error" {
		t.Fatalf("missing task/chosen must fail: %+v", env)
	}

	env := call(t, reg, "routing.fly_feedback", map[string]any{
		"task":        "analyze telemetry csv and chart it",
		"chosen":      []any{"timeseries.analyze", "charts.create"},
		"recommended": []any{"timeseries.analyze"},
		"source":      "codex-correction",
		"input_metadata": map[string]any{
			"has_timestamp": true,
		},
	})
	if env.Status != "ok" {
		t.Fatalf("status %s data %+v warnings %v", env.Status, env.Data, env.Warnings)
	}
	data := env.Data.(map[string]any)
	if data["learned"] != false || data["source"] != "codex-correction" {
		t.Fatalf("data = %+v", data)
	}
	if data["stored"].(int) < 1 {
		t.Fatalf("stored = %v", data["stored"])
	}
	if env.Metrics["memory_size"] == nil {
		t.Fatalf("metrics = %+v", env.Metrics)
	}
	if _, err := os.Stat(filepath.Join(root, "logs", "fly-feedback.jsonl")); err != nil {
		t.Fatalf("feedback log missing: %v", err)
	}
}

func TestFlyRecommendCustomCapabilities(t *testing.T) {
	reg, _ := testRegistry(t)
	env := call(t, reg, "routing.fly_recommend", map[string]any{
		"task":                   "render a monthly revenue chart",
		"available_capabilities": []any{"charts.create", "reports.create"},
		"input_metadata":         map[string]any{"types": []any{"csv"}},
	})
	if env.Status != "ok" && env.Status != "partial" {
		t.Fatalf("status %s data %+v", env.Status, env.Data)
	}
	data := env.Data.(map[string]any)
	if data["route"] == nil || data["recommendations"] == nil {
		t.Fatalf("data = %+v", data)
	}
}
