package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func writeRoutingFile(t *testing.T, root, content string) {
	t.Helper()
	dir := filepath.Join(root, "configs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "routing.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRoutingLiveRead(t *testing.T) {
	root := t.TempDir()
	writeRoutingFile(t, root, "routing:\n  mode: normal\n  version: 1\n")
	mgr := NewManager(root)
	if got := mgr.Routing().Mode; got != "normal" {
		t.Fatalf("expected normal, got %s", got)
	}
	writeRoutingFile(t, root, "routing:\n  mode: fly\n  version: 2\n")
	if got := mgr.Routing().Mode; got != "fly" {
		t.Fatalf("expected live re-read to return fly, got %s", got)
	}
}

func TestSetRoutingMode(t *testing.T) {
	root := t.TempDir()
	writeRoutingFile(t, root, "routing:\n  mode: normal\n  version: 1\n")
	mgr := NewManager(root)
	state, err := mgr.SetRoutingMode("fly")
	if err != nil {
		t.Fatal(err)
	}
	if state.Mode != "fly" || state.Version != 2 {
		t.Fatalf("unexpected state %+v", state)
	}
	if got := mgr.Routing().Mode; got != "fly" {
		t.Fatalf("expected cached state fly, got %s", got)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(mgr.RoutingPath())
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o644 {
			t.Fatalf("routing.yaml mode = %v, want 0644", info.Mode().Perm())
		}
	}
	if _, err := mgr.SetRoutingMode("turbo"); err == nil {
		t.Fatal("expected error for invalid mode")
	}
}

func TestRoutingModeNormalizationOnRead(t *testing.T) {
	root := t.TempDir()
	writeRoutingFile(t, root, "routing:\n  mode: \" Fly \"\n  version: 3\n")
	mgr := NewManager(root)
	if got := mgr.Routing().Mode; got != "fly" {
		t.Fatalf("expected normalized mode fly, got %q", got)
	}

	root2 := t.TempDir()
	writeRoutingFile(t, root2, "routing:\n  mode: turbo\n  version: 4\n")
	mgr2 := NewManager(root2)
	if got := mgr2.Routing().Mode; got != "normal" {
		t.Fatalf("invalid mode must fall back to normal, got %q", got)
	}
}

func TestConfigExplicitZeroIsPreserved(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "configs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("routing:\n  top_k: 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(root)
	if got := mgr.Config().Routing.TopK; got != 0 {
		t.Fatalf("explicit top_k: 0 must override the default, got %d", got)
	}
}

func TestConfigNonPositiveMaxTextCharsIgnored(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "configs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("tools:\n  max_text_chars: 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(root)
	if got := mgr.Config().Tools.MaxTextChars; got != 60000 {
		t.Fatalf("non-positive max_text_chars must keep the default 60000, got %d", got)
	}
}

func TestEvaluationDefaultsAndLivePatch(t *testing.T) {
	root := t.TempDir()
	mgr := NewManager(root)
	settings := mgr.Evaluation()
	if settings.TechnicalTarget != 95 || settings.DemoDayTarget != 90 {
		t.Fatalf("unexpected default targets %+v", settings)
	}
	if !settings.ParallelJudges || !settings.FullCleanroomOnFinalFreeze || settings.FullCleanroomEachIteration {
		t.Fatalf("unexpected default booleans %+v", settings)
	}
	if settings.MinDelta != 0.5 || settings.Judges != 3 || settings.FinalizationReserveMinutes != 45 {
		t.Fatalf("unexpected default numbers %+v", settings)
	}

	dir := filepath.Join(root, "configs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "evaluation:\n  technical_target: 90\n  parallel_judges: false\n  max_technical_iterations: 3\n"
	if err := os.WriteFile(filepath.Join(dir, "evaluation.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	settings = mgr.Evaluation()
	if settings.TechnicalTarget != 90 {
		t.Fatalf("live re-read must pick up technical_target 90, got %v", settings.TechnicalTarget)
	}
	if settings.ParallelJudges {
		t.Fatal("explicit parallel_judges: false must override the default true")
	}
	if settings.MaxTechnicalIterations != 3 {
		t.Fatalf("max_technical_iterations = %d, want 3", settings.MaxTechnicalIterations)
	}
	if settings.DemoDayTarget != 90 || settings.MinDelta != 0.5 {
		t.Fatalf("absent keys must keep defaults, got %+v", settings)
	}
}

func TestEvaluationInvalidValuesIgnored(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "configs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "evaluation:\n  judges: 0\n  min_delta: -1\n  finalization_reserve_minutes: -5\n"
	if err := os.WriteFile(filepath.Join(dir, "evaluation.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(root)
	settings := mgr.Evaluation()
	if settings.Judges != 3 || settings.MinDelta != 0.5 || settings.FinalizationReserveMinutes != 45 {
		t.Fatalf("invalid values must be ignored, got %+v", settings)
	}
}

func TestEvaluationMinValidJudges(t *testing.T) {
	root := t.TempDir()
	mgr := NewManager(root)
	if got := mgr.Evaluation().MinValidJudges; got != 3 {
		t.Fatalf("default min_valid_judges = %d, want 3", got)
	}

	dir := filepath.Join(root, "configs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "evaluation.yaml"), []byte("evaluation:\n  min_valid_judges: 4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := mgr.Evaluation().MinValidJudges; got != 4 {
		t.Fatalf("min_valid_judges = %d, want 4", got)
	}

	if err := os.WriteFile(filepath.Join(dir, "evaluation.yaml"), []byte("evaluation:\n  min_valid_judges: 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := mgr.Evaluation().MinValidJudges; got != 3 {
		t.Fatalf("invalid min_valid_judges must fall back to the default 3, got %d", got)
	}
}
