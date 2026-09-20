package metadata

import (
	"context"
	"testing"
)

func TestDisabledRecorder(t *testing.T) {
	var recorder Recorder = Disabled{}
	if recorder.Enabled() {
		t.Fatal("disabled recorder must report Enabled() == false")
	}
	if err := recorder.RecordArtifact(context.Background(), ArtifactRecord{ArtifactID: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.RecordRoutingRun(context.Background(), RoutingRun{Mode: "fly", Version: 2}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.RecordFlyPrediction(context.Background(), FlyPrediction{Route: "data"}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.RecordEvalRun(context.Background(), EvalRun{Kind: "aggregate", Score: 84}); err != nil {
		t.Fatal(err)
	}
	recorder.Close()
}

func TestOpenPostgresEmptyDSN(t *testing.T) {
	if _, err := OpenPostgres(context.Background(), "   "); err == nil {
		t.Fatal("expected error for an empty dsn")
	}
}
