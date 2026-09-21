package fly

import (
	"context"
	"reflect"
	"testing"
)

func TestNewHashEncoderDefaults(t *testing.T) {
	if got := NewHashEncoder(0); got.Dims != 512 {
		t.Fatalf("dims = %d, want 512", got.Dims)
	}
	if got := NewHashEncoder(-3); got.Dims != 512 {
		t.Fatalf("dims = %d, want 512", got.Dims)
	}
	if got := NewHashEncoder(64); got.Dims != 64 {
		t.Fatalf("dims = %d, want 64", got.Dims)
	}
}

func TestTokenizeFlyUnicodeAndShortTokens(t *testing.T) {
	got := tokenizeFly("Go ЯЗЫК тест 7 !")
	want := []string{"go", "язык", "тест"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tokens = %v, want %v", got, want)
	}
	if got := tokenizeFly("!"); got != nil {
		t.Fatalf("punctuation-only text = %v, want nil", got)
	}
}

func TestHashEncoderZeroVectorForEmptyText(t *testing.T) {
	encoder := NewHashEncoder(16)
	vectors, err := encoder.Embed(context.Background(), []string{"!", "a"})
	if err != nil || len(vectors) != 2 {
		t.Fatalf("embed failed: %v", err)
	}
	for _, vector := range vectors {
		for i, v := range vector {
			if v != 0 {
				t.Fatalf("a tokenless text must embed to zeros, got [%d]=%v", i, v)
			}
		}
	}
}

func TestChainEmbedderUsesPrimaryOnSuccess(t *testing.T) {
	chain := NewChainEmbedder(NewHashEncoder(32), failingEmbedder{})
	vectors, err := chain.Embed(context.Background(), []string{"hello world"})
	if err != nil || len(vectors) != 1 {
		t.Fatalf("chain = %v err=%v, want the primary result", vectors, err)
	}
}

func TestChainEmbedderFallbackOnWrongCount(t *testing.T) {
	primary := funcEmbedder{fn: func(_ context.Context, texts []string) ([][]float64, error) {
		return [][]float64{}, nil
	}}
	chain := NewChainEmbedder(primary, NewHashEncoder(32))
	vectors, err := chain.Embed(context.Background(), []string{"hello world"})
	if err != nil || len(vectors) != 1 {
		t.Fatalf("chain = %v err=%v, want the fallback result", vectors, err)
	}
}

func TestChainEmbedderNilPrimaryUsesFallback(t *testing.T) {
	chain := NewChainEmbedder(nil, NewHashEncoder(32))
	if _, err := chain.Embed(context.Background(), []string{"hello"}); err != nil {
		t.Fatalf("nil primary must fall back: %v", err)
	}
}

func TestChainEmbedderWithoutEmbedders(t *testing.T) {
	chain := NewChainEmbedder(nil, nil)
	if _, err := chain.Embed(context.Background(), []string{"hello"}); err == nil {
		t.Fatal("a chain without embedders must fail")
	}
}
