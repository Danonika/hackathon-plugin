package main

import (
	"os"
	"path/filepath"
	"testing"

	"hackathonkit.local/agentkit/internal/routing/fly"
)

func TestRecallAndTop1(t *testing.T) {
	recs := []fly.Recommendation{
		{Capability: "timeseries.detect_anomalies"},
		{Capability: "timeseries.analyze"},
		{Capability: "charts.create"},
	}
	if got := top1(recs); got != "timeseries.detect_anomalies" {
		t.Fatalf("top1 = %q", got)
	}
	if got := recall([]string{"timeseries.analyze", "charts.create"}, recs, 3); got != 1 {
		t.Fatalf("recall = %v, want 1", got)
	}
	if got := recall([]string{"timeseries.analyze", "reports.create"}, recs, 3); got != 0.5 {
		t.Fatalf("recall = %v, want 0.5", got)
	}
	if got := recall([]string{"timeseries.analyze"}, recs, 1); got != 0 {
		t.Fatalf("recall@1 = %v, want 0", got)
	}
	if got := top1(nil); got != "" {
		t.Fatalf("top1(nil) = %q", got)
	}
}

func TestSummarize(t *testing.T) {
	rows := []rowResult{
		{BaselineRecall: 0.5, FlyRecall: 1, Agreement: true, Deterministic: true, BaselineLatency: 1, FlyLatency: 3},
		{BaselineRecall: 0, FlyRecall: 0.5, Agreement: false, Deterministic: true, BaselineLatency: 1, FlyLatency: 5},
	}
	out := summarize(rows, 5)
	if out.Tasks != 2 || out.TopK != 5 {
		t.Fatalf("summary = %+v", out)
	}
	if out.FlyRecallAtK != 0.75 || out.BaselineRecallAtK != 0.25 {
		t.Fatalf("recall summary = %+v", out)
	}
	if out.AgreementRate != 0.5 || out.DeterminismRate != 1 {
		t.Fatalf("rate summary = %+v", out)
	}
	if out.FlyLatencyMS != 4 {
		t.Fatalf("latency summary = %+v", out)
	}
}

func TestLoadSuite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "suite.jsonl")
	content := "# comment\n\n" +
		`{"task":"one","expected":["a"]}` + "\n" +
		`{"task":"two","expected":["b"],"metadata":{"types":["csv"]}}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := loadSuite(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Task != "one" || rows[1].Metadata["types"] == nil {
		t.Fatalf("rows = %+v", rows)
	}
	if _, err := loadSuite(filepath.Join(t.TempDir(), "missing.jsonl")); err == nil {
		t.Fatal("missing suite must fail")
	}
}
