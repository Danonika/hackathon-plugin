package fly

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"hackathonkit.local/agentkit/internal/config"
)

func saveTestBrain(t *testing.T, brain *MBrain) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "brain.json")
	if err := brain.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	return path
}

func countActive(code []float64) (int, float64) {
	active := 0
	norm := 0.0
	for _, v := range code {
		if v != 0 {
			active++
			norm += v
		}
	}
	return active, norm
}

func TestNewMBrainDefaultsAndReadout(t *testing.T) {
	brain := NewMBrain(0, 0.06, []string{"data", "reports"}, 7)
	if brain.KC != MBKenyonCells || brain.MBON != MBMBONCount {
		t.Fatalf("defaults not applied: kc=%d mbon=%d", brain.KC, brain.MBON)
	}
	if brain.Model != "mushroom-body" || brain.Seed != 7 || brain.Source == "" {
		t.Fatalf("brain metadata missing: %+v", brain)
	}
	if len(brain.Categories) != 2 || len(brain.Readout) != 2 {
		t.Fatalf("categories/readout not initialized: %+v", brain)
	}
	if len(brain.Readout["data"]) != MBKenyonCells {
		t.Fatalf("readout dim = %d, want %d", len(brain.Readout["data"]), MBKenyonCells)
	}
	if brain.Bias == nil {
		t.Fatal("bias map must be initialized")
	}
}

func TestSplitMix64DeterministicAndBounded(t *testing.T) {
	first, second := uint64(42), uint64(42)
	for i := 0; i < 200; i++ {
		a, b := splitMix64(&first), splitMix64(&second)
		if a != b {
			t.Fatalf("splitMix64 is not deterministic at step %d: %v vs %v", i, a, b)
		}
		if a < -1 || a >= 1 {
			t.Fatalf("splitMix64 out of range at step %d: %v", i, a)
		}
	}
}

func TestMBrainActivateSparsityAndScores(t *testing.T) {
	brain := NewMBrain(128, 0.1, []string{"alpha", "beta"}, 21)
	vec := []float64{1, 0.5, -0.25}
	code := brain.activate(vec)
	if len(code) != 128 {
		t.Fatalf("code dim = %d, want 128", len(code))
	}
	const wantActive = 12
	active, norm := countActive(code)
	if active == 0 {
		t.Fatal("activation produced an all-zero code")
	}
	if active > wantActive {
		t.Fatalf("active cells = %d, want at most %d", active, wantActive)
	}
	if math.Abs(norm-1) > 1e-9 {
		t.Fatalf("active code must sum to 1, got %v", norm)
	}

	scores, gotCode, ok := brain.scores(vec)
	if !ok || len(gotCode) != 128 || len(scores) != 2 {
		t.Fatalf("scores failed: ok=%v code=%d scores=%v", ok, len(gotCode), scores)
	}
	total := 0.0
	for _, v := range scores {
		total += v
	}
	if math.Abs(total-1) > 1e-9 {
		t.Fatalf("softmax scores must sum to 1, got %v", total)
	}
}

func TestMBrainActivateDefaultSparsity(t *testing.T) {
	for _, sparsity := range []float64{0, 1, 2, -1} {
		brain := NewMBrain(100, sparsity, []string{"a"}, 5)
		code := brain.activate([]float64{1})
		active, _ := countActive(code)
		if active == 0 {
			t.Fatalf("sparsity %v produced an all-zero code", sparsity)
		}
		if active > int(100*MBDefaultSparse) {
			t.Fatalf("sparsity %v: active cells = %d, want at most %d", sparsity, active, int(100*MBDefaultSparse))
		}
	}
	if code := (&MBrain{KC: 0}).activate([]float64{1}); code != nil {
		t.Fatalf("a brain without Kenyon cells must return a nil code, got %v", code)
	}
}

func TestMBrainScoresFailureModes(t *testing.T) {
	if _, _, ok := (&MBrain{KC: 0}).scores([]float64{1}); ok {
		t.Fatal("a brain without Kenyon cells must not score")
	}
	if _, _, ok := (&MBrain{KC: 4}).scores([]float64{1}); ok {
		t.Fatal("a brain without categories must not score")
	}
	brain := NewMBrain(32, 0.5, []string{"a"}, 2)
	brain.Readout["a"] = []float64{1}
	if _, _, ok := brain.scores([]float64{1}); ok {
		t.Fatal("a readout shorter than the code must be skipped")
	}
}

func TestMBrainScoresSoftmaxWithBias(t *testing.T) {
	brain := NewMBrain(64, 0.5, []string{"a", "b"}, 3)
	brain.Bias["a"] = 2
	scores, _, ok := brain.scores([]float64{1, 1})
	if !ok {
		t.Fatal("scores must succeed")
	}
	if scores["a"] <= scores["b"] {
		t.Fatalf("the biased category must win: %v", scores)
	}
}

func TestMBrainAdjustClampsWeights(t *testing.T) {
	brain := NewMBrain(16, 0.5, []string{"a"}, 1)
	code := make([]float64, 16)
	code[0] = 1
	brain.adjust("a", code, 100)
	if brain.Readout["a"][0] != mbWeightLimit {
		t.Fatalf("positive weight = %v, want clamp at %v", brain.Readout["a"][0], mbWeightLimit)
	}
	brain.adjust("a", code, -1e9)
	if brain.Readout["a"][0] != -mbWeightLimit {
		t.Fatalf("negative weight = %v, want clamp at %v", brain.Readout["a"][0], -mbWeightLimit)
	}
	brain.adjust("missing", code, 1)
	brain.adjust("a", make([]float64, 17), 1)
	if brain.Readout["a"][0] != -mbWeightLimit {
		t.Fatal("an oversized code must leave the weights untouched")
	}
}

func TestMBrainTrainUpdatesWeightsAndRank(t *testing.T) {
	brain := NewMBrain(64, 0.25, []string{"a", "b"}, 7)
	brain.Bias["a"] = 5
	brain.Train([]float64{1, 1}, "b", 1)

	bSum := 0.0
	for _, w := range brain.Readout["b"] {
		bSum += w
	}
	if bSum <= 0 {
		t.Fatalf("the trained category must gain weight, sum=%v", bSum)
	}
	aSum := 0.0
	for _, w := range brain.Readout["a"] {
		aSum += w
	}
	if aSum >= 0 {
		t.Fatalf("the ranking winner must be punished, sum=%v", aSum)
	}
	if math.Abs(brain.Bias["b"]-0.1) > 1e-12 {
		t.Fatalf("bias = %v, want eta*0.1", brain.Bias["b"])
	}
	ranking := brain.Rank([]float64{1, 1})
	if len(ranking) != 2 || ranking[0] != "a" {
		t.Fatalf("ranking = %v, want [a b]", ranking)
	}
}

func TestMBrainTrainEdgeCases(t *testing.T) {
	(&MBrain{KC: 8}).Train([]float64{1}, "a", 1)

	brain := NewMBrain(32, 0.25, []string{"a"}, 3)
	brain.Train([]float64{1}, "b", 0.5)
	if len(brain.Categories) != 2 || brain.Categories[1] != "b" {
		t.Fatalf("new category not registered: %v", brain.Categories)
	}
	if len(brain.Readout["b"]) != 32 {
		t.Fatalf("new readout dim = %d, want 32", len(brain.Readout["b"]))
	}
	brain.Train([]float64{1}, "", 1)
	if len(brain.Categories) != 2 {
		t.Fatalf("an empty category must be ignored: %v", brain.Categories)
	}

	ghost := NewMBrain(16, 0.5, []string{"a"}, 1)
	ghost.Readout["ghost"] = make([]float64, 16)
	ghost.Train([]float64{1}, "ghost", 0.5)
	if len(ghost.Categories) != 1 {
		t.Fatalf("a category with an existing readout must not be appended: %v", ghost.Categories)
	}

	noKC := NewMBrain(8, 0.5, []string{"a"}, 1)
	noKC.KC = 0
	noKC.Train([]float64{1}, "a", 1)
}

func TestMBrainRankFailureModes(t *testing.T) {
	if got := (&MBrain{KC: 0}).Rank([]float64{1}); got != nil {
		t.Fatalf("rank without Kenyon cells = %v, want nil", got)
	}
	if got := (&MBrain{KC: 4}).Rank([]float64{1}); got != nil {
		t.Fatalf("rank without categories = %v, want nil", got)
	}
}

func TestMBrainSaveToAndLoadBrain(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "brain.json")
	brain := NewMBrain(32, 0.2, []string{"data", "reports"}, 9)
	brain.Bias["data"] = 1.5
	brain.Dopamine = mbDopamine{PAM: 2, PPL1: 1}
	if err := brain.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	if brain.UpdatedAt == "" {
		t.Fatal("SaveTo must stamp updated_at")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var decoded MBrain
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.KC != 32 || decoded.Bias["data"] != 1.5 || decoded.Dopamine.PAM != 2 {
		t.Fatalf("persisted brain lost data: %+v", decoded)
	}

	r := NewRouter(config.RoutingSettings{}).WithBrain(path)
	loaded := r.loadBrain()
	if loaded == nil || loaded.KC != 32 || loaded.Dopamine.PAM != 2 {
		t.Fatalf("loadBrain = %+v", loaded)
	}
	if r.loadBrain() != loaded {
		t.Fatal("loadBrain must cache the parsed brain")
	}
}

func TestLoadBrainRejectsInvalidFiles(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{
		"null":       `null`,
		"invalid":    `{`,
		"no kc":      `{"readout":{"data":[1]}}`,
		"no readout": `{"kc":64}`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name+".json")
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			r := NewRouter(config.RoutingSettings{}).WithBrain(path)
			if brain := r.loadBrain(); brain != nil {
				t.Fatalf("brain = %+v, want nil", brain)
			}
		})
	}
	r := NewRouter(config.RoutingSettings{}).WithBrain(filepath.Join(dir, "missing.json"))
	if brain := r.loadBrain(); brain != nil {
		t.Fatalf("a missing file must not load a brain: %+v", brain)
	}
	if brain := NewRouter(config.RoutingSettings{}).loadBrain(); brain != nil {
		t.Fatalf("a router without a brain path must not load a brain: %+v", brain)
	}
}

func TestSaveBrainPersistsAndReportsErrors(t *testing.T) {
	dir := t.TempDir()
	r := NewRouter(config.RoutingSettings{})
	if err := r.saveBrain(NewMBrain(8, 0.5, []string{"a"}, 1)); err != nil {
		t.Fatalf("a router without a brain path must be a no-op: %v", err)
	}

	path := filepath.Join(dir, "state", "brain.json")
	r = r.WithBrain(path)
	brain := NewMBrain(16, 0.25, []string{"a"}, 2)
	if err := r.saveBrain(brain); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("saveBrain did not write the file: %v", err)
	}
	if brain.UpdatedAt == "" {
		t.Fatal("saveBrain must stamp updated_at")
	}

	bad := NewMBrain(8, 0.5, []string{"a"}, 1)
	bad.Readout["a"][0] = math.NaN()
	if err := r.saveBrain(bad); err == nil {
		t.Fatal("NaN weights must fail to marshal")
	}

	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := r.WithBrain(filepath.Join(blocker, "brain.json")).saveBrain(brain); err == nil {
		t.Fatal("saveBrain must fail when the directory cannot be created")
	}
}

func TestSaveToReportsDirectoryErrors(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	brain := NewMBrain(8, 0.5, []string{"a"}, 1)
	if err := brain.SaveTo(filepath.Join(blocker, "brain.json")); err == nil {
		t.Fatal("SaveTo must fail when the parent is a file")
	}
}

func TestDopamineStats(t *testing.T) {
	r := NewRouter(config.RoutingSettings{})
	if stats := r.Dopamine(); stats != (DopamineStats{}) {
		t.Fatalf("dopamine without a brain = %+v, want zero", stats)
	}
	path := filepath.Join(t.TempDir(), "brain.json")
	brain := NewMBrain(16, 0.25, []string{"data"}, 4)
	brain.Dopamine = mbDopamine{PAM: 3, PPL1: 2}
	if err := brain.SaveTo(path); err != nil {
		t.Fatal(err)
	}
	stats := NewRouter(config.RoutingSettings{}).WithBrain(path).Dopamine()
	if stats.PAM != 3 || stats.PPL1 != 2 || stats.Total != 5 {
		t.Fatalf("dopamine stats = %+v, want pam=3 ppl1=2 total=5", stats)
	}
}

func TestApplyDopamineRewardsAndPunishes(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	brainPath := filepath.Join(dir, "brain.json")
	brain := NewMBrain(64, 0.25, []string{"timeseries", "media", "speech", "documents"}, 6)
	if err := brain.SaveTo(brainPath); err != nil {
		t.Fatal(err)
	}
	r := NewRouter(config.RoutingSettings{}).
		WithEmbeddings(fakeEmbedder{}, filepath.Join(dir, "emb.json"), "fake-model").
		WithBrain(brainPath)

	if ok := r.applyDopamine(ctx, FeedbackEvent{
		Task:        "analyze telemetry",
		Chosen:      []string{"timeseries.analyze"},
		Recommended: []Recommendation{{Capability: "audio.transcribe"}, {Capability: "unknown.cap"}},
		Source:      "accepted",
	}); !ok {
		t.Fatal("accepted feedback must apply dopamine")
	}
	stats := r.Dopamine()
	if stats.PAM != 1 || stats.PPL1 != 0 || stats.Total != 1 {
		t.Fatalf("stats after reward = %+v", stats)
	}

	if ok := r.applyDopamine(ctx, FeedbackEvent{
		Task:        "transcribe the audio",
		Chosen:      []string{"timeseries.analyze"},
		Recommended: []Recommendation{{Capability: "audio.transcribe"}},
		Source:      "codex-correction",
	}); !ok {
		t.Fatal("correction feedback must apply dopamine")
	}
	if stats = r.Dopamine(); stats.PPL1 != 1 || stats.PAM != 1 {
		t.Fatalf("stats after punishment = %+v", stats)
	}

	if ok := r.applyDopamine(ctx, FeedbackEvent{
		Task:   "transcribe the audio",
		Chosen: []string{"timeseries.analyze"},
		Source: "fallback",
	}); !ok {
		t.Fatal("fallback feedback must apply dopamine")
	}
	if stats = r.Dopamine(); stats.PPL1 != 2 {
		t.Fatalf("stats after fallback = %+v", stats)
	}

	if r.applyDopamine(ctx, FeedbackEvent{Task: "analyze telemetry", Source: "mystery"}) {
		t.Fatal("an unknown source must not apply dopamine")
	}
	if stats = r.Dopamine(); stats.PPL1 != 2 || stats.PAM != 1 {
		t.Fatalf("an unknown source must not change stats: %+v", stats)
	}

	raw, err := os.ReadFile(brainPath)
	if err != nil {
		t.Fatal(err)
	}
	var persisted MBrain
	if err := json.Unmarshal(raw, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.Dopamine.PAM != 1 || persisted.Dopamine.PPL1 != 2 {
		t.Fatalf("dopamine counters must be persisted: %+v", persisted.Dopamine)
	}
}

func TestApplyDopamineFailureModes(t *testing.T) {
	ctx := context.Background()
	event := FeedbackEvent{Task: "analyze telemetry", Chosen: []string{"timeseries.analyze"}, Source: "accepted"}

	r := NewRouter(config.RoutingSettings{}).WithEmbeddings(fakeEmbedder{}, "", "fake")
	if r.applyDopamine(ctx, event) {
		t.Fatal("dopamine without a brain must not apply")
	}

	brainPath := saveTestBrain(t, NewMBrain(16, 0.25, []string{"timeseries"}, 1))
	if NewRouter(config.RoutingSettings{}).WithBrain(brainPath).applyDopamine(ctx, event) {
		t.Fatal("dopamine without an embedder must not apply")
	}

	r = NewRouter(config.RoutingSettings{}).WithEmbeddings(failingEmbedder{}, "", "fake").WithBrain(brainPath)
	if r.applyDopamine(ctx, event) {
		t.Fatal("a failing embedder must not apply dopamine")
	}

	r = NewRouter(config.RoutingSettings{}).WithEmbeddings(fakeEmbedder{}, "", "fake")
	r.brain = &MBrain{KC: 0}
	if r.applyDopamine(ctx, event) {
		t.Fatal("a brain that cannot score must not apply dopamine")
	}
}

func TestMBCategoryScores(t *testing.T) {
	ctx := context.Background()
	brainPath := saveTestBrain(t, NewMBrain(64, 0.25, []string{"timeseries", "speech"}, 11))

	r := NewRouter(config.RoutingSettings{}).
		WithEmbeddings(fakeEmbedder{}, "", "fake-model").
		WithBrain(brainPath)
	scores, ok := r.mbCategoryScores(ctx, "analyze telemetry")
	if !ok || len(scores) != 2 {
		t.Fatalf("mb scores = %v ok=%v, want two categories", scores, ok)
	}

	if _, ok := NewRouter(config.RoutingSettings{}).WithBrain(brainPath).mbCategoryScores(ctx, "x"); ok {
		t.Fatal("mb scores without an embedder must fail")
	}
	if _, ok := NewRouter(config.RoutingSettings{}).WithEmbeddings(fakeEmbedder{}, "", "fake").mbCategoryScores(ctx, "x"); ok {
		t.Fatal("mb scores without a brain must fail")
	}
	if _, ok := NewRouter(config.RoutingSettings{}).WithEmbeddings(failingEmbedder{}, "", "fake").WithBrain(brainPath).mbCategoryScores(ctx, "x"); ok {
		t.Fatal("mb scores with a failing embedder must fail")
	}
	broken := NewRouter(config.RoutingSettings{}).WithEmbeddings(fakeEmbedder{}, "", "fake")
	broken.brain = &MBrain{KC: 0}
	if _, ok := broken.mbCategoryScores(ctx, "x"); ok {
		t.Fatal("mb scores with a broken brain must fail")
	}
}
