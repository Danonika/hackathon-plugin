package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"hackathonkit.local/agentkit/internal/quality"
)

func TestQualityGateWithoutEvidence(t *testing.T) {
	reg, _ := testRegistry(t)
	env := call(t, reg, "quality.gate", map[string]any{})
	if env.Status != "partial" {
		t.Fatalf("expected partial without evidence, got %s", env.Status)
	}
	if len(env.Warnings) == 0 {
		t.Fatal("missing evidence must carry a warning")
	}
}

func TestQualityGateReadsEvidence(t *testing.T) {
	reg, root := testRegistry(t)
	report := quality.Report{
		SHA:       "abc123",
		OK:        false,
		StartedAt: time.Now().UTC(),
		Checks: []quality.Check{
			{Name: "build", Status: "ok"},
			{Name: "test", Status: "failed", Output: "FAIL"},
		},
		Summary: map[string]int{"ok": 1, "failed": 1},
	}
	if _, err := quality.Save(root, report); err != nil {
		t.Fatal(err)
	}
	env := call(t, reg, "quality.gate", map[string]any{})
	if env.Status != "ok" {
		t.Fatalf("status %s data %+v", env.Status, env.Data)
	}
	data := env.Data.(map[string]any)
	if data["sha"] != "abc123" || data["ok"] != false {
		t.Fatalf("unexpected data %+v", data)
	}
	if len(env.Warnings) == 0 {
		t.Fatal("failing checks must produce a warning")
	}
	raw, _ := json.Marshal(data["failed"])
	if string(raw) == "[]" {
		t.Fatalf("failed checks missing: %+v", data)
	}
	if _, err := os.Stat(filepath.Join(root, "evals", "quality", "abc123", "gate.json")); err != nil {
		t.Fatal(err)
	}
}
