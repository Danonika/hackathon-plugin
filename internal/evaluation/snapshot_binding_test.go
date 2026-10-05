package evaluation

import "testing"

// When the caller omits the expected snapshot sha, AggregateReports must infer
// it from the reports themselves so reports from different frozen bundles are
// never merged into one aggregate (which would defeat calibration binding).
func TestAggregateReportsInfersAndBindsSnapshot(t *testing.T) {
	reports := []JudgeReport{
		mkReport("judge-01", "sha-a", uniform(80)),
		mkReport("judge-02", "sha-b", uniform(85)),
	}
	agg := AggregateReports(reports, "", 1)
	if agg.SnapshotSHA != "sha-a" {
		t.Fatalf("inferred snapshot = %q, want sha-a", agg.SnapshotSHA)
	}
	if agg.ValidJudges != 1 {
		t.Fatalf("valid judges = %d, want 1 (mismatched snapshot must be dropped): %+v", agg.ValidJudges, agg.DroppedInvalid)
	}

	other := AggregateReports([]JudgeReport{mkReport("judge-03", "sha-b", uniform(85))}, "", 1)
	if _, err := Calibrate(agg, other, 0.5, 1); err == nil {
		t.Fatal("aggregates inferred from different snapshots must not calibrate together")
	}
}
