package resources

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDefaultMatrixIsValidAndComplete(t *testing.T) {
	matrix := DefaultMatrix()
	if err := matrix.Validate(); err != nil {
		t.Fatalf("default matrix invalid: %v", err)
	}
	if len(matrix.Tracks) != 10 {
		t.Fatalf("tracks = %d, want 10", len(matrix.Tracks))
	}
	for _, track := range Tracks() {
		if _, ok := matrix.Tracks[track.ID]; !ok {
			t.Fatalf("track %s missing from the default matrix", track.ID)
		}
	}
}

func TestRepoMatrixMatchesDefaults(t *testing.T) {
	repoRoot := filepath.Join("..", "..")
	loaded, err := LoadMatrix(repoRoot)
	if err != nil {
		t.Fatalf("LoadMatrix(repo) = %v", err)
	}
	if !reflect.DeepEqual(loaded, DefaultMatrix()) {
		t.Fatal("configs/resource-matrix.yaml and resources.DefaultMatrix() drifted; update both together")
	}
}

func TestLoadMatrixFallbackAndErrors(t *testing.T) {
	matrix, err := LoadMatrix(t.TempDir())
	if err != nil || !reflect.DeepEqual(matrix, DefaultMatrix()) {
		t.Fatalf("missing file must fall back to defaults: %v", err)
	}

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "configs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(MatrixPath(root), []byte("capabilities: ["), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadMatrix(root); err == nil {
		t.Fatal("invalid YAML must fail")
	}
	if err := os.WriteFile(MatrixPath(root), []byte("capabilities:\n  x:\n    - id: deterministic\ntracks:\n  99-nope:\n    label: x\n    capabilities: [x]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadMatrix(root); err == nil || !strings.Contains(err.Error(), "not a known track") {
		t.Fatalf("unknown track must fail, got %v", err)
	}
}

func TestPlanWithoutProviders(t *testing.T) {
	clearProviderEnv(t)
	matrix := DefaultMatrix()
	statuses := Statuses(Profile{})
	track, decisions, err := Plan(matrix, statuses, "08")
	if err != nil {
		t.Fatal(err)
	}
	if track.ID != "08-innovation" || !strings.Contains(track.Scenario, "photo") {
		t.Fatalf("track plan = %+v", track)
	}
	if len(decisions) != 5 {
		t.Fatalf("decisions = %d, want 5", len(decisions))
	}
	byCapability := map[string]Decision{}
	for _, decision := range decisions {
		byCapability[decision.Capability] = decision
	}
	if byCapability["tabular"].Chosen.ID != "deterministic" || byCapability["tabular"].Degraded {
		t.Fatalf("tabular = %+v", byCapability["tabular"])
	}
	vision := byCapability["vision"]
	if vision.Chosen.ID != "deterministic" || !vision.Degraded {
		t.Fatalf("vision = %+v", vision)
	}
	if len(vision.Missing) == 0 || vision.Missing[0] != "NVIDIA_API_KEY" {
		t.Fatalf("vision missing = %v", vision.Missing)
	}
	if len(vision.Alternatives) != 2 {
		t.Fatalf("vision alternatives = %+v", vision.Alternatives)
	}
	reasoning := byCapability["reasoning"]
	if reasoning.Chosen.ID != "deterministic" || !reasoning.Degraded {
		t.Fatalf("reasoning = %+v", reasoning)
	}
	if summary := DecisionSummary(decisions); !strings.Contains(summary, "reasoning=deterministic") {
		t.Fatalf("summary = %q", summary)
	}
	if degraded := DegradedCapabilities(decisions); len(degraded) != 3 {
		t.Fatalf("degraded = %v, want vision+reasoning+artifacts", degraded)
	}
}

func TestPlanWithProviders(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test")
	t.Setenv("TELEGRAM_BOT_TOKEN", "bot")
	t.Setenv("TELEGRAM_CHAT_ID", "42")
	statuses := Statuses(Profile{Claimed: map[string]bool{"openai_api": true, "telegram": true}})
	matrix := DefaultMatrix()

	_, decisions, err := Plan(matrix, statuses, "08")
	if err != nil {
		t.Fatal(err)
	}
	byCapability := map[string]Decision{}
	for _, decision := range decisions {
		byCapability[decision.Capability] = decision
	}
	if byCapability["vision"].Chosen.Provider != "openai_api" || byCapability["vision"].Degraded {
		t.Fatalf("vision = %+v", byCapability["vision"])
	}
	if byCapability["reasoning"].Chosen.Provider != "openai_api" {
		t.Fatalf("reasoning = %+v", byCapability["reasoning"])
	}

	_, decisions, err = Plan(matrix, statuses, "09")
	if err != nil {
		t.Fatal(err)
	}
	for _, decision := range decisions {
		if decision.Capability == "notifications" && decision.Chosen.ID != "telegram" {
			t.Fatalf("notifications = %+v", decision)
		}
	}

	if _, _, err := Plan(matrix, statuses, "99-nope"); err == nil {
		t.Fatal("unknown track must fail")
	}
	if _, _, err := Plan(Matrix{Capabilities: map[string][]Variant{"x": {{ID: "deterministic"}}}, Tracks: map[string]TrackPlan{}}, statuses, "08"); err == nil {
		t.Fatal("track without a playbook must fail")
	}
}
