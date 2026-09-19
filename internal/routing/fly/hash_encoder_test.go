package fly

import (
	"context"
	"math"
	"testing"
)

func TestHashEncoderDeterministicAndNormalized(t *testing.T) {
	encoder := NewHashEncoder(256)
	ctx := context.Background()
	first, err := encoder.Embed(ctx, []string{"найди аномалии в телеметрии"})
	if err != nil || len(first) != 1 {
		t.Fatalf("embed failed: %v", err)
	}
	second, _ := encoder.Embed(ctx, []string{"найди аномалии в телеметрии"})
	if len(second) != 1 {
		t.Fatal("second embed failed")
	}
	for i := range first[0] {
		if first[0][i] != second[0][i] {
			t.Fatalf("hash encoder must be deterministic at dim %d", i)
		}
	}
	norm := 0.0
	for _, v := range first[0] {
		norm += v * v
	}
	if math.Abs(math.Sqrt(norm)-1) > 1e-9 {
		t.Fatalf("vector must be L2-normalized, got norm %v", math.Sqrt(norm))
	}
}

func TestChainEmbedderFallsBack(t *testing.T) {
	chain := NewChainEmbedder(failingEmbedder{}, NewHashEncoder(64))
	vectors, err := chain.Embed(context.Background(), []string{"тест"})
	if err != nil || len(vectors) != 1 {
		t.Fatalf("chain must fall back to the hash encoder: %v", err)
	}
}
