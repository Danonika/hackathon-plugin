package fly

import (
	"context"
	"fmt"
	"hash/fnv"
	"math"
	"strings"
	"unicode"
)

type HashEncoder struct {
	Dims int
}

func NewHashEncoder(dims int) *HashEncoder {
	if dims <= 0 {
		dims = 512
	}
	return &HashEncoder{Dims: dims}
}

func (h *HashEncoder) Embed(ctx context.Context, texts []string) ([][]float64, error) {
	out := make([][]float64, 0, len(texts))
	for _, text := range texts {
		vector := make([]float64, h.Dims)
		for _, token := range tokenizeFly(text) {
			hash := fnv.New64a()
			_, _ = hash.Write([]byte(token))
			value := hash.Sum64()
			index := int(value % uint64(h.Dims))
			sign := 1.0
			if (value>>63)&1 == 1 {
				sign = -1
			}
			vector[index] += sign
		}
		norm := 0.0
		for _, v := range vector {
			norm += v * v
		}
		if norm > 0 {
			norm = math.Sqrt(norm)
			for i := range vector {
				vector[i] /= norm
			}
		}
		out = append(out, vector)
	}
	return out, nil
}

func tokenizeFly(text string) []string {
	var out []string
	var current strings.Builder
	flush := func() {
		if current.Len() >= 2 {
			out = append(out, strings.ToLower(current.String()))
		}
		current.Reset()
	}
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return out
}

type ChainEmbedder struct {
	Primary  Embedder
	Fallback Embedder
}

func NewChainEmbedder(primary, fallback Embedder) *ChainEmbedder {
	return &ChainEmbedder{Primary: primary, Fallback: fallback}
}

func (c *ChainEmbedder) Embed(ctx context.Context, texts []string) ([][]float64, error) {
	if c.Primary != nil {
		if vectors, err := c.Primary.Embed(ctx, texts); err == nil && len(vectors) == len(texts) {
			return vectors, nil
		}
	}
	if c.Fallback != nil {
		return c.Fallback.Embed(ctx, texts)
	}
	return nil, fmt.Errorf("no embedder available")
}
