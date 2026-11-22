package fly

import (
	"context"
	"testing"

	"hackathonkit.local/agentkit/internal/config"
)

func TestRecommendTelemetry(t *testing.T) {
	r := NewRouter(config.RoutingSettings{FlyThreshold: 0.55, TopK: 5})
	available := []string{
		"documents.extract", "knowledge.search", "data.inspect", "data.analyze",
		"timeseries.analyze", "timeseries.detect_anomalies", "audio.transcribe",
		"vision.analyze", "charts.create", "reports.create", "classification.classify",
	}
	res := r.Recommend(
		context.Background(),
		"Find anomalies in equipment telemetry",
		map[string]any{
			"types":         []any{"csv"},
			"rows":          float64(120000),
			"has_timestamp": true,
		},
		available,
	)
	if res.Route != "timeseries" {
		t.Fatalf("expected route timeseries, got %q (scores=%v)", res.Route, res.CategoryScores)
	}
	if res.MaxScore < 0.55 {
		t.Fatalf("expected max score above threshold, got %v", res.MaxScore)
	}
	found := false
	for _, rec := range res.Recommendations {
		if rec.Capability == "timeseries.detect_anomalies" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected timeseries.detect_anomalies in recommendations, got %+v", res.Recommendations)
	}
}

func TestRecommendAudio(t *testing.T) {
	r := NewRouter(config.RoutingSettings{FlyThreshold: 0.55, TopK: 5})
	available := []string{"audio.transcribe", "vision.analyze", "documents.extract", "reports.create"}
	res := r.Recommend(context.Background(), "Generate subtitles for this interview audio", map[string]any{
		"types": []any{"audio"},
	}, available)
	if res.Route != "speech" {
		t.Fatalf("expected route speech, got %q (scores=%v)", res.Route, res.CategoryScores)
	}
	if len(res.Recommendations) == 0 || res.Recommendations[0].Capability != "audio.transcribe" {
		t.Fatalf("expected audio.transcribe first, got %+v", res.Recommendations)
	}
}

func TestRecommendFiltersUnavailable(t *testing.T) {
	r := NewRouter(config.RoutingSettings{FlyThreshold: 0.55, TopK: 5})
	res := r.Recommend(context.Background(), "find anomalies in telemetry csv", nil, []string{"documents.extract"})
	for _, rec := range res.Recommendations {
		if rec.Capability != "documents.extract" {
			t.Fatalf("unexpected capability outside available list: %s", rec.Capability)
		}
	}
}

func TestRecommendZeroSignalIsUnknown(t *testing.T) {
	r := NewRouter(config.RoutingSettings{FlyThreshold: 0.55, TopK: 5})
	res := r.Recommend(context.Background(), "qqq zzz", nil, nil)
	if res.Route != "unknown" {
		t.Fatalf("zero-signal task must route to unknown, got %q (scores=%v)", res.Route, res.CategoryScores)
	}
	if len(res.Recommendations) != 0 {
		t.Fatalf("zero-signal task must not recommend capabilities, got %+v", res.Recommendations)
	}
}

func TestRecommendKeywordTieBreakIsDeterministic(t *testing.T) {
	r := NewRouter(config.RoutingSettings{FlyThreshold: 0.55, TopK: 5})
	// "csv" (data) and "policy" (knowledge) both score 0.3; the tie must resolve
	// the same way on every run regardless of map iteration order.
	first := r.Recommend(context.Background(), "csv policy", nil, nil).Route
	for i := 0; i < 50; i++ {
		if got := r.Recommend(context.Background(), "csv policy", nil, nil).Route; got != first {
			t.Fatalf("tie-break is not deterministic: %q vs %q", got, first)
		}
	}
}

func TestUnmappedLearnedCategoryCannotRoute(t *testing.T) {
	if categoryAvailable("unmapped-category", map[string]bool{"data.inspect": true}) {
		t.Fatal("a route must map to executable capabilities")
	}
	if n := centroidDims(map[string][]float64{"valid": {1, 2}, "missing": {}}); n != 0 {
		t.Fatal("empty centroid accepted", n)
	}
}
