package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfigFile(t *testing.T, root, name, content string) {
	t.Helper()
	dir := filepath.Join(root, "configs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverRootFromEnv(t *testing.T) {
	abs := t.TempDir()
	t.Setenv("HACKATHONKIT_REPO_ROOT", abs)
	if got := DiscoverRoot(); got != abs {
		t.Fatalf("DiscoverRoot = %q, want %q", got, abs)
	}

	t.Setenv("HACKATHONKIT_REPO_ROOT", "relative-root")
	got := DiscoverRoot()
	if !filepath.IsAbs(got) || filepath.Base(got) != "relative-root" {
		t.Fatalf("relative env root = %q", got)
	}
}

func TestDiscoverRootWalksUp(t *testing.T) {
	t.Setenv("HACKATHONKIT_REPO_ROOT", "")
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "docker-compose.yml"), []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nested)
	if got := DiscoverRoot(); got != root {
		t.Fatalf("DiscoverRoot = %q, want %q", got, root)
	}
}

func TestDiscoverRootRoutingMarker(t *testing.T) {
	t.Setenv("HACKATHONKIT_REPO_ROOT", "")
	root := t.TempDir()
	writeRoutingFile(t, root, "routing:\n  mode: normal\n")
	nested := filepath.Join(root, "deep", "dir")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(nested)
	if got := DiscoverRoot(); got != root {
		t.Fatalf("DiscoverRoot = %q, want %q", got, root)
	}
}

func TestDiscoverRootNoMarker(t *testing.T) {
	t.Setenv("HACKATHONKIT_REPO_ROOT", "")
	root := t.TempDir()
	t.Chdir(root)
	if got := DiscoverRoot(); got != root {
		t.Fatalf("DiscoverRoot = %q, want %q", got, root)
	}
}

func TestManagerRoot(t *testing.T) {
	root := t.TempDir()
	if got := NewManager(root).Root(); got != root {
		t.Fatalf("Root = %q", got)
	}
}

func TestMergeFileAllFields(t *testing.T) {
	root := t.TempDir()
	content := `openai:
  chat_model: chat-x
  vision_model: vision-x
  transcribe_model: transcribe-x
  embedding_model: embed-x
storage:
  input_dir: in
  artifact_dir: art
  index_dir: idx
tools:
  default_timeout_sec: 5
  max_text_chars: 10
routing:
  fly_threshold: 0.9
  top_k: 9
`
	writeConfigFile(t, root, "config.yaml", content)
	cfg := NewManager(root).Config()
	if cfg.OpenAI.ChatModel != "chat-x" || cfg.OpenAI.VisionModel != "vision-x" ||
		cfg.OpenAI.TranscribeModel != "transcribe-x" || cfg.OpenAI.EmbeddingModel != "embed-x" {
		t.Fatalf("openai = %+v", cfg.OpenAI)
	}
	if cfg.Storage.InputDir != "in" || cfg.Storage.ArtifactDir != "art" || cfg.Storage.IndexDir != "idx" {
		t.Fatalf("storage = %+v", cfg.Storage)
	}
	if cfg.Tools.DefaultTimeoutSec != 5 || cfg.Tools.MaxTextChars != 10 {
		t.Fatalf("tools = %+v", cfg.Tools)
	}
	if cfg.Routing.FlyThreshold != 0.9 || cfg.Routing.TopK != 9 {
		t.Fatalf("routing = %+v", cfg.Routing)
	}
}

func TestMergeNumericLearningAndZeros(t *testing.T) {
	root := t.TempDir()
	content := `tools:
  max_text_chars: -5
routing:
  fly_threshold: 0
  top_k: 0
learning:
  enabled: false
  min_confidence: 0
  inject_min_confidence: 0
  max_inject: 0
  promote_after: 0
`
	writeConfigFile(t, root, "config.yaml", content)
	cfg := NewManager(root).Config()
	if cfg.Tools.MaxTextChars != 60000 {
		t.Fatalf("max_text_chars = %d", cfg.Tools.MaxTextChars)
	}
	if cfg.Routing.FlyThreshold != 0 || cfg.Routing.TopK != 0 {
		t.Fatalf("explicit routing zeros not applied: %+v", cfg.Routing)
	}
	if cfg.Learning.Enabled {
		t.Fatal("enabled false not applied")
	}
	if cfg.Learning.MinConfidence != 0 || cfg.Learning.InjectMinConfidence != 0 || cfg.Learning.MaxInject != 0 {
		t.Fatalf("learning zeros not applied: %+v", cfg.Learning)
	}
	if cfg.Learning.PromoteAfter != 3 {
		t.Fatalf("promote_after 0 must be ignored, got %d", cfg.Learning.PromoteAfter)
	}
}

func TestMergeNumericLearningValues(t *testing.T) {
	root := t.TempDir()
	content := `learning:
  enabled: true
  min_confidence: 0.25
  inject_min_confidence: 0.75
  max_inject: 2
  promote_after: 5
`
	writeConfigFile(t, root, "config.yaml", content)
	cfg := NewManager(root).Config()
	if !cfg.Learning.Enabled || cfg.Learning.MinConfidence != 0.25 ||
		cfg.Learning.InjectMinConfidence != 0.75 || cfg.Learning.MaxInject != 2 ||
		cfg.Learning.PromoteAfter != 5 {
		t.Fatalf("learning = %+v", cfg.Learning)
	}
}

func TestNewManagerUnreadableConfig(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "configs")
	if err := os.MkdirAll(filepath.Join(dir, "config.yaml"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := NewManager(root).Config()
	if cfg.Tools.DefaultTimeoutSec != 120 {
		t.Fatalf("defaults not used: %+v", cfg.Tools)
	}
}

func TestNewManagerInvalidYAML(t *testing.T) {
	root := t.TempDir()
	writeConfigFile(t, root, "config.yaml", "tools: [not, a, map\n")
	cfg := NewManager(root).Config()
	if cfg.Tools.DefaultTimeoutSec != 120 {
		t.Fatalf("defaults not used: %+v", cfg.Tools)
	}
}

func TestApplyEvaluationPatchAllFields(t *testing.T) {
	root := t.TempDir()
	content := `evaluation:
  demo_day_target: 80
  max_judge_rounds: 4
  roast_agents: 5
  parallel_roast: false
  full_cleanroom_each_iteration: true
  full_cleanroom_on_final_freeze: false
  technical_target: 0
  max_technical_iterations: 0
  finalization_reserve_minutes: 0
  min_delta: 0
`
	writeConfigFile(t, root, "evaluation.yaml", content)
	settings := NewManager(root).Evaluation()
	if settings.DemoDayTarget != 80 || settings.MaxJudgeRounds != 4 || settings.RoastAgents != 5 {
		t.Fatalf("settings = %+v", settings)
	}
	if settings.ParallelRoast || !settings.FullCleanroomEachIteration || settings.FullCleanroomOnFinalFreeze {
		t.Fatalf("booleans = %+v", settings)
	}
	if settings.TechnicalTarget != 0 || settings.MaxTechnicalIterations != 0 ||
		settings.FinalizationReserveMinutes != 0 || settings.MinDelta != 0 {
		t.Fatalf("zeros = %+v", settings)
	}
}

func TestEvaluationCorruptFileUsesDefaults(t *testing.T) {
	root := t.TempDir()
	writeConfigFile(t, root, "evaluation.yaml", "evaluation: [broken\n")
	settings := NewManager(root).Evaluation()
	if settings.Judges != 3 {
		t.Fatalf("defaults not used: %+v", settings)
	}
}

func TestEvaluationUnreadablePathUsesDefaults(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "configs", "evaluation.yaml"), 0o755); err != nil {
		t.Fatal(err)
	}
	settings := NewManager(root).Evaluation()
	if settings.Judges != 3 {
		t.Fatalf("defaults not used: %+v", settings)
	}
}

func TestEvaluationCachedAfterRemoval(t *testing.T) {
	root := t.TempDir()
	writeConfigFile(t, root, "evaluation.yaml", "evaluation:\n  judges: 5\n")
	mgr := NewManager(root)
	if got := mgr.Evaluation().Judges; got != 5 {
		t.Fatalf("judges = %d", got)
	}
	if err := os.Remove(filepath.Join(root, "configs", "evaluation.yaml")); err != nil {
		t.Fatal(err)
	}
	if got := mgr.Evaluation().Judges; got != 5 {
		t.Fatalf("cached judges = %d", got)
	}
}

func TestRoutingCachedAfterRemoval(t *testing.T) {
	root := t.TempDir()
	writeRoutingFile(t, root, "routing:\n  mode: fly\n  version: 7\n")
	mgr := NewManager(root)
	if got := mgr.Routing(); got.Mode != "fly" || got.Version != 7 {
		t.Fatalf("routing = %+v", got)
	}
	if err := os.Remove(filepath.Join(root, "configs", "routing.yaml")); err != nil {
		t.Fatal(err)
	}
	if got := mgr.Routing(); got.Mode != "fly" || got.Version != 7 {
		t.Fatalf("cached routing = %+v", got)
	}
}

func TestRoutingUnreadableFileFallsBack(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "configs", "routing.yaml"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := NewManager(root).Routing(); got.Mode != "normal" || got.Version != 1 {
		t.Fatalf("routing = %+v", got)
	}
}

func TestRoutingInvalidYAMLAndModeFallbackVersion(t *testing.T) {
	root := t.TempDir()
	writeRoutingFile(t, root, "routing: [broken\n")
	if got := NewManager(root).Routing(); got.Mode != "normal" || got.Version != 1 {
		t.Fatalf("routing = %+v", got)
	}

	root2 := t.TempDir()
	writeRoutingFile(t, root2, "routing:\n  mode: turbo\n  version: 0\n")
	if got := NewManager(root2).Routing(); got.Mode != "normal" || got.Version != 1 {
		t.Fatalf("fallback version = %+v", got)
	}
}

func TestSetRoutingModeWithoutFile(t *testing.T) {
	root := t.TempDir()
	state, err := NewManager(root).SetRoutingMode("fly")
	if err != nil {
		t.Fatal(err)
	}
	if state.Mode != "fly" || state.Version != 2 {
		t.Fatalf("state = %+v", state)
	}
}

func TestSetRoutingModeInvalidExistingMode(t *testing.T) {
	root := t.TempDir()
	writeRoutingFile(t, root, "routing:\n  mode: turbo\n  version: 4\n")
	state, err := NewManager(root).SetRoutingMode("normal")
	if err != nil {
		t.Fatal(err)
	}
	if state.Mode != "normal" || state.Version != 5 {
		t.Fatalf("state = %+v", state)
	}
}

func TestSetRoutingModeReadOnlyDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission checks do not apply to root")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "configs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if _, err := NewManager(root).SetRoutingMode("fly"); err == nil {
		t.Fatal("expected error when the routing directory is read-only")
	}
}

func TestDiscoverRootGetwdFailure(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HACKATHONKIT_REPO_ROOT", "relative-root")
	if got := DiscoverRoot(); got != "relative-root" {
		t.Fatalf("relative env with deleted cwd = %q", got)
	}
	t.Setenv("HACKATHONKIT_REPO_ROOT", "")
	if got := DiscoverRoot(); got != "." {
		t.Fatalf("deleted cwd = %q, want .", got)
	}
}

func TestSetRoutingModeMkdirError(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "configs"), []byte("file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewManager(root).SetRoutingMode("fly"); err == nil {
		t.Fatal("expected mkdir error when configs is a file")
	}
}

func TestSetRoutingModeRenameError(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "configs", "routing.yaml")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "occupied"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewManager(root).SetRoutingMode("fly"); err == nil {
		t.Fatal("expected rename error when routing.yaml is a non-empty directory")
	}
}

func TestEvaluationCacheHit(t *testing.T) {
	root := t.TempDir()
	writeConfigFile(t, root, "evaluation.yaml", "evaluation:\n  judges: 6\n")
	mgr := NewManager(root)
	if got := mgr.Evaluation().Judges; got != 6 {
		t.Fatalf("judges = %d", got)
	}
	if got := mgr.Evaluation().Judges; got != 6 {
		t.Fatalf("cached judges = %d", got)
	}
}

func TestLoadDotEnvEdgeLines(t *testing.T) {
	root := t.TempDir()
	content := "NOEQUALS\n=novalue\n  \n#comment\n"
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	LoadDotEnv(root)
	if got := os.Getenv("NOEQUALS"); got != "" {
		t.Fatalf("NOEQUALS = %q", got)
	}
}
