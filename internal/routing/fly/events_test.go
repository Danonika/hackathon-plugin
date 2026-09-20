package fly

import (
	"context"
	"testing"
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
