package fly

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"hackathonkit.local/agentkit/internal/config"
)

type fakeClassifier struct {
	scores        map[string]float64
	err           error
	gotCategories []string
}

func (f *fakeClassifier) ClassifyTask(_ context.Context, _ string, categories []string) (map[string]float64, error) {
	f.gotCategories = append([]string{}, categories...)
	if f.err != nil {
		return nil, f.err
	}
	return f.scores, nil
}

func TestNewRouterDefaults(t *testing.T) {
	r := NewRouter(config.RoutingSettings{})
	if r.threshold != 0.55 || r.topK != 5 {
		t.Fatalf("defaults = threshold %v topK %d, want 0.55/5", r.threshold, r.topK)
	}
	r = NewRouter(config.RoutingSettings{FlyThreshold: 0.7, TopK: 2})
	if r.threshold != 0.7 || r.topK != 2 {
		t.Fatalf("configured values = %v/%d", r.threshold, r.topK)
	}
}

func TestStringSliceVariants(t *testing.T) {
	if got := stringSlice([]string{"CSV"}); len(got) != 1 || got[0] != "csv" {
		t.Fatalf("[]string must be lowercased like the other branches, got %v", got)
	}
	if got := stringSlice([]any{"A", 3, "B"}); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("[]any = %v", got)
	}
	if got := stringSlice("MiXeD"); len(got) != 1 || got[0] != "mixed" {
		t.Fatalf("string = %v", got)
	}
	if got := stringSlice(42); got != nil {
		t.Fatalf("unsupported type = %v, want nil", got)
	}
	if got := stringSlice(nil); got != nil {
		t.Fatalf("nil = %v, want nil", got)
	}
}

func TestApplyMetadataBoostsAllTypes(t *testing.T) {
	cases := []struct {
		name string
		meta map[string]any
		want string
	}{
		{"csv", map[string]any{"types": []string{"csv"}}, "data"},
		{"pdf list", map[string]any{"types": []any{"pdf"}}, "documents"},
		{"pdf string", map[string]any{"types": "docx"}, "documents"},
		{"audio", map[string]any{"types": []any{"mp3"}}, "speech"},
		{"image", map[string]any{"types": []any{"jpg"}}, "vision"},
		{"video", map[string]any{"types": []any{"mkv"}}, "media"},
		{"log", map[string]any{"types": []any{"logs"}}, "classification"},
		{"timestamp", map[string]any{"has_timestamp": true}, "timeseries"},
		{"large rows", map[string]any{"rows": float64(1000)}, "data"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRouter(config.RoutingSettings{FlyThreshold: 0.55, TopK: 5})
			res := r.Recommend(context.Background(), "process the attachment", tc.meta, nil)
			if res.Route != tc.want {
				t.Fatalf("route = %q, want %q (scores %v)", res.Route, tc.want, res.CategoryScores)
			}
		})
	}

	r := NewRouter(config.RoutingSettings{})
	res := r.Recommend(context.Background(), "process the attachment", map[string]any{
		"rows":          10,
		"has_timestamp": "yes",
	}, nil)
	if res.Route != "unknown" {
		t.Fatalf("small rows and non-bool timestamp must be ignored, route = %q (%v)", res.Route, res.CategoryScores)
	}
}

func TestRecommendTopKTruncation(t *testing.T) {
	r := NewRouter(config.RoutingSettings{FlyThreshold: 0.55, TopK: 1})
	res := r.Recommend(context.Background(), "csv pdf telemetry", nil, nil)
	if len(res.Recommendations) != 1 {
		t.Fatalf("topK=1 must truncate recommendations, got %+v", res.Recommendations)
	}
}

func TestLLMCategoryScoresFailures(t *testing.T) {
	ctx := context.Background()
	if _, ok := NewRouter(config.RoutingSettings{}).llmCategoryScores(ctx, "x"); ok {
		t.Fatal("a router without a classifier must not score")
	}
	r := NewRouter(config.RoutingSettings{}).WithLLM(nil)
	if _, ok := r.llmCategoryScores(ctx, "x"); ok {
		t.Fatal("a nil classifier must not score")
	}
	r = r.WithLLM(&fakeClassifier{err: errors.New("boom")})
	if _, ok := r.llmCategoryScores(ctx, "x"); ok {
		t.Fatal("a failing classifier must not score")
	}
	r = r.WithLLM(&fakeClassifier{scores: map[string]float64{}})
	if _, ok := r.llmCategoryScores(ctx, "x"); ok {
		t.Fatal("an empty classifier result must not score")
	}
	classifier := &fakeClassifier{scores: map[string]float64{"data": 0.5}}
	r = r.WithLLM(classifier)
	scores, ok := r.llmCategoryScores(ctx, "x")
	if !ok || scores["data"] != 0.5 {
		t.Fatalf("llm scores = %v ok=%v", scores, ok)
	}
	if len(classifier.gotCategories) != len(categoryCapabilities) || !sort.StringsAreSorted(classifier.gotCategories) {
		t.Fatalf("classifier categories must be sorted and complete: %v", classifier.gotCategories)
	}
}

func TestRecommendUsesLLMScores(t *testing.T) {
	classifier := &fakeClassifier{scores: map[string]float64{"documents": 0.9}}
	r := NewRouter(config.RoutingSettings{FlyThreshold: 0.55, TopK: 5}).WithLLM(classifier)
	res := r.Recommend(context.Background(), "qqq zzz", nil, nil)
	if !strings.Contains(res.Encoder, "llm") {
		t.Fatalf("encoder = %q, want an llm signal", res.Encoder)
	}
	if res.Route != "documents" {
		t.Fatalf("route = %q, want documents (scores %v)", res.Route, res.CategoryScores)
	}
}

func TestRecommendUsesMBrainSignals(t *testing.T) {
	dir := t.TempDir()
	brainPath := filepath.Join(dir, "brain.json")
	brain := NewMBrain(64, 0.25, []string{"timeseries", "speech"}, 8)
	brain.Dopamine = mbDopamine{PAM: 2}
	if err := brain.SaveTo(brainPath); err != nil {
		t.Fatal(err)
	}
	r := NewRouter(config.RoutingSettings{FlyThreshold: 0.55, TopK: 5}).
		WithEmbeddings(fakeEmbedder{}, filepath.Join(dir, "emb.json"), "fake-model").
		WithBrain(brainPath)
	res := r.Recommend(context.Background(), "analyze telemetry data", nil, nil)
	if !strings.Contains(res.Encoder, "mb") {
		t.Fatalf("encoder = %q, want an mb signal", res.Encoder)
	}
	if !strings.Contains(res.Encoder, "dopamine") {
		t.Fatalf("encoder = %q, want a dopamine signal", res.Encoder)
	}
	if len(res.CategoryScores) == 0 {
		t.Fatal("mbrain signals must contribute category scores")
	}
}

func TestMemorySizeTracksExamples(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	memPath := filepath.Join(dir, "mem.json")
	r := NewRouter(config.RoutingSettings{}).
		WithEmbeddings(fakeEmbedder{}, "", "fake-model").
		WithMemory(memPath)
	if r.MemorySize() != 0 {
		t.Fatalf("empty memory size = %d, want 0", r.MemorySize())
	}
	if _, err := r.AddFeedback(ctx, FeedbackEvent{
		Task:   "extract text from a pdf contract",
		Chosen: []string{"documents.extract"},
		Source: "accepted",
	}); err != nil {
		t.Fatal(err)
	}
	if r.MemorySize() != 1 {
		t.Fatalf("memory size = %d, want 1", r.MemorySize())
	}
	if err := os.WriteFile(memPath, []byte("{invalid"), 0o644); err != nil {
		t.Fatal(err)
	}
	if r.MemorySize() != 0 {
		t.Fatalf("an unreadable memory file must report 0, got %d", r.MemorySize())
	}
}

func TestAddFeedbackSaveError(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewRouter(config.RoutingSettings{}).WithMemory(filepath.Join(blocker, "mem.json"))
	size, err := r.AddFeedback(context.Background(), FeedbackEvent{
		Task:   "analyze the csv table",
		Chosen: []string{"data.inspect"},
		Source: "accepted",
	})
	if err == nil {
		t.Fatal("AddFeedback must surface a save failure")
	}
	if size != 1 {
		t.Fatalf("size = %d, want 1 even when saving fails", size)
	}
}

func TestMemoryCategoryScoresVotes(t *testing.T) {
	memPath := filepath.Join(t.TempDir(), "mem.json")
	mem := memoryFile{Examples: []FeedbackExample{
		{Hash: "1", Categories: []string{"data"}, Vector: []float64{1, 0}},
		{Hash: "2", Categories: []string{"speech", "data"}, Vector: []float64{0.5, 0.5}},
	}}
	data, err := json.Marshal(mem)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(memPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewRouter(config.RoutingSettings{}).WithMemory(memPath)
	scores, ok := r.memoryCategoryScores([]float64{1, 0}, 10)
	if !ok {
		t.Fatal("memory scores must succeed")
	}
	if math.Abs(scores["data"]-1) > 1e-9 {
		t.Fatalf("data vote = %v, want 1", scores["data"])
	}
	wantSpeech := math.Sqrt(0.5) / (1 + math.Sqrt(0.5))
	if math.Abs(scores["speech"]-wantSpeech) > 1e-9 {
		t.Fatalf("speech vote = %v, want %v", scores["speech"], wantSpeech)
	}
}

func TestMemoryCategoryScoresRejectsUnusableExamples(t *testing.T) {
	memPath := filepath.Join(t.TempDir(), "mem.json")
	mem := memoryFile{Examples: []FeedbackExample{
		{Hash: "no-vector", Categories: []string{"data"}},
		{Hash: "zero-vector", Categories: []string{"data"}, Vector: []float64{0, 0}},
		{Hash: "no-category", Vector: []float64{1, 0}},
	}}
	data, err := json.Marshal(mem)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(memPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewRouter(config.RoutingSettings{}).WithMemory(memPath)
	if _, ok := r.memoryCategoryScores([]float64{1, 0}, 0); ok {
		t.Fatal("examples without a positive similarity must not produce votes")
	}
	if _, ok := NewRouter(config.RoutingSettings{}).memoryCategoryScores([]float64{1, 0}, 5); ok {
		t.Fatal("a router without memory must not produce votes")
	}
}

func TestCapabilityToCategory(t *testing.T) {
	if got := capabilityToCategory("timeseries.analyze"); got != "timeseries" {
		t.Fatalf("timeseries.analyze -> %q", got)
	}
	if got := capabilityToCategory("audio.transcribe"); got != "media" {
		t.Fatalf("audio.transcribe -> %q, want the first sorted category", got)
	}
	if got := capabilityToCategory("unknown.cap"); got != "" {
		t.Fatalf("unknown capability -> %q, want empty", got)
	}
}
