package fly

import (
	"context"
	"strings"
	"testing"

	"hackathonkit.local/agentkit/internal/config"
)

type fakeEmbedder struct{}

func (fakeEmbedder) Embed(ctx context.Context, texts []string) ([][]float64, error) {
	out := make([][]float64, len(texts))
	for i, t := range texts {
		l := strings.ToLower(t)
		switch {
		case strings.Contains(l, "audio") || strings.Contains(l, "recording") || strings.Contains(l, "запис"):
			out[i] = []float64{0, 1}
		case strings.Contains(l, "anomal") || strings.Contains(l, "telemetry") || strings.Contains(l, "датчик"):
			out[i] = []float64{1, 0}
		default:
			out[i] = []float64{0.5, 0.5}
		}
	}
	return out, nil
}

func TestRecommendWithEmbeddings(t *testing.T) {
	r := NewRouter(config.RoutingSettings{FlyThreshold: 0.55, TopK: 5}).
		WithEmbeddings(fakeEmbedder{}, t.TempDir()+"/emb.json", "fake-model")

	available := []string{"audio.transcribe", "timeseries.analyze", "documents.extract", "reports.create"}
	res := r.Recommend(context.Background(), "process this recording from the meeting", nil, available)
	if res.Encoder != "keyword+embedding" {
		t.Fatalf("expected keyword+embedding encoder, got %q", res.Encoder)
	}
	if res.Route != "speech" {
		t.Fatalf("expected speech route, got %q (scores %v)", res.Route, res.CategoryScores)
	}
	if len(res.Recommendations) == 0 || res.Recommendations[0].Capability != "audio.transcribe" {
		t.Fatalf("expected audio.transcribe first, got %+v", res.Recommendations)
	}

	res2 := r.Recommend(context.Background(), "process this recording from the meeting", nil, available)
	if res2.Route != "speech" {
		t.Fatalf("cached run expected speech route, got %q", res2.Route)
	}
}

func TestEmbeddingFallsBackToKeywords(t *testing.T) {
	r := NewRouter(config.RoutingSettings{FlyThreshold: 0.55, TopK: 5}).
		WithEmbeddings(failingEmbedder{}, t.TempDir()+"/emb.json", "fake-model")
	res := r.Recommend(context.Background(), "find anomalies in telemetry csv", nil,
		[]string{"timeseries.detect_anomalies", "documents.extract"})
	if res.Encoder != "keyword" {
		t.Fatalf("expected keyword fallback, got %q", res.Encoder)
	}
	if res.Route != "timeseries" {
		t.Fatalf("expected timeseries route, got %q", res.Route)
	}
}

type failingEmbedder struct{}

func (failingEmbedder) Embed(ctx context.Context, texts []string) ([][]float64, error) {
	return nil, context.DeadlineExceeded
}
