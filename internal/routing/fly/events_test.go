package fly

import (
	"context"
	"path/filepath"
	"testing"

	"hackathonkit.local/agentkit/internal/config"
)

func TestActivationSnapshotIncludesEveryActiveNeuron(t *testing.T) {
	brain := &MBrain{
		KC: MBKenyonCells, Seed: 7, Sparsity: MBDefaultSparse,
		Categories: []string{"speech"},
		Readout:    map[string][]float64{"speech": make([]float64, MBKenyonCells)},
	}
	r := &Router{brain: brain, embedder: fakeEmbedder{}}
	snapshot := r.ActivationSnapshot(context.Background(), "audio", 0)
	if snapshot["activation_available"] != true || snapshot["activation_complete"] != true {
		t.Fatalf("expected complete available snapshot: %v", snapshot)
	}
	expected := brain.activate([]float64{0, 1})
	cells := snapshot["active_kc"].([]KCSample)
	seen := make(map[int]bool, len(cells))
	for _, cell := range cells {
		if seen[cell.Index] || cell.Index < 0 || cell.Index >= len(expected) {
			t.Fatalf("duplicate/out-of-range index: %d", cell.Index)
		}
		seen[cell.Index] = true
		if cell.Activation != expected[cell.Index] {
			t.Fatalf("cell %d changed activation: got %v want %v", cell.Index, cell.Activation, expected[cell.Index])
		}
	}
	active := 0
	for i, value := range expected {
		if value > 0 {
			active++
			if !seen[i] {
				t.Fatalf("active neuron %d absent from complete snapshot", i)
			}
		}
	}
	if active <= 64 || len(cells) != active || snapshot["active_kc_count"] != active || snapshot["kc_total"] != MBKenyonCells {
		t.Fatalf("lost active neurons: got %d, expected %d of %d", len(cells), active, MBKenyonCells)
	}
	limited := r.ActivationSnapshot(context.Background(), "audio", 64)
	if len(limited["active_kc"].([]KCSample)) != 64 || limited["activation_complete"] != false || limited["active_kc_count"] != active {
		t.Fatalf("limited snapshot must report truncation and actual active count: %v", limited)
	}
}

func TestActivationSnapshotUnavailableIsNotZeroActivity(t *testing.T) {
	r := &Router{}
	snapshot := r.ActivationSnapshot(context.Background(), "audio", 0)
	if snapshot["activation_available"] != false || snapshot["activation_complete"] != false {
		t.Fatalf("missing model incorrectly reported as complete zero activity: %v", snapshot)
	}
}

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
	if got := r.ActivationSnapshot(ctx, "x", 10); got["kc_total"] != 32 || len(got["active_kc"].([]KCSample)) != 0 {
		t.Fatalf("without an embedder the snapshot must have no active KC, got %+v", got)
	}

	r = NewRouter(config.RoutingSettings{}).WithEmbeddings(failingEmbedder{}, "", "fake").WithBrain(brainPath)
	if got := r.ActivationSnapshot(ctx, "x", 10); got["kc_total"] != 32 || len(got["active_kc"].([]KCSample)) != 0 {
		t.Fatalf("a failing embedder must return no active KC, got %+v", got)
	}

	r = NewRouter(config.RoutingSettings{}).WithEmbeddings(fakeEmbedder{}, "", "fake")
	r.brain = &MBrain{KC: 0}
	if got := r.ActivationSnapshot(ctx, "x", 10); got["kc_total"] != MBKenyonCells {
		t.Fatalf("a brain that cannot score must return the empty snapshot, got %+v", got)
	}
}
