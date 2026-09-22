package fly

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"hackathonkit.local/agentkit/internal/config"
)

func TestFeedbackMemoryLearnsCorrection(t *testing.T) {
	dir := t.TempDir()
	router := NewRouter(config.RoutingSettings{FlyThreshold: 0.55, TopK: 5}).
		WithEmbeddings(fakeEmbedder{}, filepath.Join(dir, "emb.json"), "fake-model").
		WithMemory(filepath.Join(dir, "fly-memory.json"))
	ctx := context.Background()

	size, _, err := router.AddFeedback(ctx, FeedbackEvent{
		Task:   "сделай разбор вложения номер семь",
		Chosen: []string{"timeseries.detect_anomalies", "charts.create"},
		Source: "normal-teacher",
	})
	if err != nil || size != 1 {
		t.Fatalf("feedback stored=%d err=%v", size, err)
	}

	res := router.Recommend(ctx, "сделай разбор вложения номер восемь", nil,
		[]string{"timeseries.detect_anomalies", "charts.create", "audio.transcribe", "documents.extract"})
	if !strings.Contains(res.Encoder, "knn") {
		t.Fatalf("expected knn signal in encoder, got %q", res.Encoder)
	}
	if res.Route != "timeseries" {
		t.Fatalf("expected timeseries route learned from feedback, got %q (%v)", res.Route, res.CategoryScores)
	}

	dedup, _, err := router.AddFeedback(ctx, FeedbackEvent{
		Task:   "сделай разбор вложения номер семь",
		Chosen: []string{"data.inspect"},
		Source: "codex-correction",
	})
	if err != nil || dedup != 1 {
		t.Fatalf("same task must replace the example, size=%d err=%v", dedup, err)
	}
}
