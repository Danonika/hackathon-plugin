package tools

import (
	"strings"
	"testing"

	"hackathonkit.local/agentkit/internal/mcp"
)

func TestLearningDisabledStore(t *testing.T) {
	reg := mcp.NewRegistry()
	registerLearning(reg, Deps{})
	calls := map[string]map[string]any{
		"learning.record":  {"trigger": "t", "insight": "i"},
		"learning.recall":  {"query": "q"},
		"learning.promote": {"id": "l-1"},
	}
	for name, args := range calls {
		env := call(t, reg, name, args)
		if env.Status != "error" {
			t.Fatalf("%s: status %s, want error", name, env.Status)
		}
		if !strings.Contains(env.Data.(map[string]any)["error"].(string), "disabled") {
			t.Fatalf("%s: error = %+v", name, env.Data)
		}
	}
}

func TestLearningPromoteReasons(t *testing.T) {
	reg, _ := testRegistry(t)

	global := call(t, reg, "learning.record", map[string]any{
		"trigger": "global trigger", "insight": "global insight", "scope": "global",
	})
	if global.Status != "ok" {
		t.Fatalf("global record: %+v", global)
	}
	globalID := global.Data.(map[string]any)["id"].(string)
	already := call(t, reg, "learning.promote", map[string]any{"id": globalID})
	if already.Status != "ok" || already.Data.(map[string]any)["promoted"] != false {
		t.Fatalf("already-global promote: %+v", already.Data)
	}
	if already.Data.(map[string]any)["reason"] != "already global" {
		t.Fatalf("reason = %v", already.Data.(map[string]any)["reason"])
	}

	project := call(t, reg, "learning.record", map[string]any{
		"trigger": "project trigger", "insight": "project insight",
	})
	projectID := project.Data.(map[string]any)["id"].(string)
	pending := call(t, reg, "learning.promote", map[string]any{"id": projectID})
	if pending.Status != "ok" || pending.Data.(map[string]any)["promoted"] != false {
		t.Fatalf("early promote: %+v", pending.Data)
	}
	reason := pending.Data.(map[string]any)["reason"].(string)
	if !strings.Contains(reason, "not eligible") {
		t.Fatalf("reason = %q", reason)
	}

	recall := call(t, reg, "learning.recall", map[string]any{"query": "project trigger", "limit": float64(1), "min_confidence": 0.0})
	if recall.Status != "ok" || recall.Data.(map[string]any)["count"].(int) == 0 {
		t.Fatalf("recall with explicit minimum: %+v", recall)
	}
	none := call(t, reg, "learning.recall", map[string]any{"query": "project trigger", "min_confidence": 2.0})
	if none.Status != "partial" {
		t.Fatalf("recall above every confidence must be partial: %+v", none)
	}
}
