package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLearningRecordRecallPromote(t *testing.T) {
	reg, _ := testRegistry(t)

	var id string
	for i := 0; i < 3; i++ {
		env := call(t, reg, "learning.record", map[string]any{
			"trigger":  "when a tool handler returns an error",
			"insight":  "return the shared envelope with a trace id",
			"evidence": []any{"test-output"},
		})
		if env.Status != "ok" {
			t.Fatalf("record %d: %s %+v", i, env.Status, env.Warnings)
		}
		id, _ = env.Data.(map[string]any)["id"].(string)
		if i == 0 && env.Data.(map[string]any)["created"] != true {
			t.Fatalf("first record must be created: %+v", env.Data)
		}
	}

	recall := call(t, reg, "learning.recall", map[string]any{"query": "tool handler error envelope"})
	if recall.Status != "ok" {
		t.Fatalf("recall: %s %+v", recall.Status, recall.Warnings)
	}
	data := recall.Data.(map[string]any)
	if data["count"].(int) != 1 {
		t.Fatalf("recall count = %v, want 1", data["count"])
	}

	if id == "" {
		t.Fatal("record must return an id")
	}
	promote := call(t, reg, "learning.promote", map[string]any{"id": id})
	if promote.Status != "ok" || promote.Data.(map[string]any)["promoted"] != true {
		t.Fatalf("eligible learning must be promoted: %+v", promote.Data)
	}

	missing := call(t, reg, "learning.promote", map[string]any{"id": "l-does-not-exist"})
	if missing.Status != "error" {
		t.Fatalf("unknown id must fail: %+v", missing)
	}
}

func TestLearningRecallEmptyIsPartial(t *testing.T) {
	reg, _ := testRegistry(t)
	env := call(t, reg, "learning.recall", map[string]any{"query": "nothing recorded yet"})
	if env.Status != "partial" {
		t.Fatalf("expected partial for an empty store, got %s", env.Status)
	}
	if len(env.Warnings) == 0 {
		t.Fatal("empty recall must carry a warning")
	}
}

func TestContextBudgetReportsToolsAndSkills(t *testing.T) {
	reg, root := testRegistry(t)
	mustWrite(t, filepath.Join(root, "plugins", "hackathonkit", "skills", "demo-skill", "SKILL.md"),
		"---\nname: demo-skill\ndescription: demo\n---\n\n# Demo\n\n"+strings.Repeat("line\n", 500))

	env := call(t, reg, "system.context_budget", map[string]any{})
	if env.Status != "ok" {
		t.Fatalf("status %s data %+v", env.Status, env.Data)
	}
	data := env.Data.(map[string]any)
	if data["tool_count"].(int) != 40 {
		t.Fatalf("tool_count = %v, want 40", data["tool_count"])
	}
	if data["total_tokens"].(int) <= 0 {
		t.Fatalf("total_tokens must be positive: %+v", data)
	}
	if len(env.Warnings) == 0 {
		t.Fatal("a 500-line skill must be flagged")
	}
	recommendations := data["recommendations"].([]contextRecommendation)
	if len(recommendations) == 0 {
		t.Fatal("expected recommendations for the oversized skill")
	}
	found := false
	for _, item := range data["items"].([]contextItem) {
		if item.Name == "demo-skill/SKILL.md" && item.Flag != "" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected the demo skill to be flagged in items")
	}
	if _, err := os.Stat(filepath.Join(root, "plugins", "hackathonkit", "skills", "demo-skill", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
}
