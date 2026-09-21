package fly

import (
	"context"
	"path/filepath"
	"testing"

	"hackathonkit.local/agentkit/internal/config"
)

func TestActivationSnapshotDefaultsWithoutBrain(t *testing.T) {
	r := NewRouter(config.RoutingSettings{})
	ctx := context.Background()
	for _, topN := range []int{0, -1, 4096} {
		got := r.ActivationSnapshot(ctx, "analyze telemetry", topN)
		if got["kc_total"] != MBKenyonCells {
			t.Fatalf("topN=%d kc_total = %v, want %d", topN, got["kc_total"], MBKenyonCells)
		}
		samples, ok := got["active_kc"].([]KCSample)
		if !ok || len(samples) != 0 {
			t.Fatalf("topN=%d active_kc = %#v, want empty", topN, got["active_kc"])
		}
		mbon, ok := got["mbon"].(map[string]float64)
		if !ok || len(mbon) != 0 {
			t.Fatalf("topN=%d mbon = %#v, want empty", topN, got["mbon"])
		}
	}
}

func TestActivationSnapshotWithSavedBrain(t *testing.T) {
	dir := t.TempDir()
	brainPath := filepath.Join(dir, "brain.json")
	brain := NewMBrain(128, 0.2, []string{"data", "reports"}, 13)
	if err := brain.SaveTo(brainPath); err != nil {
		t.Fatal(err)
	}
	r := NewRouter(config.RoutingSettings{}).
		WithEmbeddings(fakeEmbedder{}, filepath.Join(dir, "emb.json"), "fake-model").
		WithBrain(brainPath)
	got := r.ActivationSnapshot(context.Background(), "analyze the csv table", 4)
	if got["kc_total"] != 128 {
		t.Fatalf("kc_total = %v, want 128", got["kc_total"])
	}
	samples, ok := got["active_kc"].([]KCSample)
	if !ok || len(samples) == 0 || len(samples) > 4 {
		t.Fatalf("active_kc = %#v, want 1..4 samples", got["active_kc"])
	}
	for i, sample := range samples {
		if sample.Activation <= 0 {
			t.Fatalf("zero activations must be omitted: %+v", sample)
		}
		if sample.Index < 0 || sample.Index >= 128 {
			t.Fatalf("sample index out of range: %+v", sample)
		}
		if i > 0 && samples[i-1].Activation < sample.Activation {
			t.Fatalf("samples must be sorted descending: %+v", samples)
		}
	}
	mbon, ok := got["mbon"].(map[string]float64)
	if !ok || len(mbon) != 2 {
		t.Fatalf("mbon = %#v, want both categories", got["mbon"])
	}
}

func TestActivationSnapshotFailureModes(t *testing.T) {
	ctx := context.Background()
	brainPath := saveTestBrain(t, NewMBrain(32, 0.2, []string{"data"}, 3))

	r := NewRouter(config.RoutingSettings{}).WithBrain(brainPath)
	if got := r.ActivationSnapshot(ctx, "x", 10); got["kc_total"] != MBKenyonCells {
		t.Fatalf("without an embedder the snapshot must be empty, got %+v", got)
	}

	r = NewRouter(config.RoutingSettings{}).WithEmbeddings(failingEmbedder{}, "", "fake").WithBrain(brainPath)
	if got := r.ActivationSnapshot(ctx, "x", 10); got["kc_total"] != MBKenyonCells {
		t.Fatalf("a failing embedder must return the empty snapshot, got %+v", got)
	}

	r = NewRouter(config.RoutingSettings{}).WithEmbeddings(fakeEmbedder{}, "", "fake")
	r.brain = &MBrain{KC: 0}
	if got := r.ActivationSnapshot(ctx, "x", 10); got["kc_total"] != MBKenyonCells {
		t.Fatalf("a brain that cannot score must return the empty snapshot, got %+v", got)
	}
}
