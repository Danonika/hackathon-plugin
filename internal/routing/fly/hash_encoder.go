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
	if h == nil || h.Dims <= 0 {
		return nil, fmt.Errorf("hash encoder: invalid dimensions %d", dimsOf(h))
	}
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
	var primaryErr error
	if c.Primary != nil {
		vectors, err := c.Primary.Embed(ctx, texts)
		if err == nil && len(vectors) == len(texts) {
			return vectors, nil
		}
		if err != nil {
			primaryErr = err
		} else {
			primaryErr = fmt.Errorf("primary returned %d vectors for %d texts", len(vectors), len(texts))
		}
	}
	if c.Fallback != nil {
		vectors, err := c.Fallback.Embed(ctx, texts)
		if err != nil {
			if primaryErr != nil {
				return nil, fmt.Errorf("primary embedder failed: %w; fallback failed: %v", primaryErr, err)
			}
			return nil, err
		}
		return vectors, nil
	}
	if primaryErr != nil {
		return nil, fmt.Errorf("primary embedder failed: %w", primaryErr)
	}
	return nil, fmt.Errorf("no embedder available")
}

func dimsOf(h *HashEncoder) int {
	if h == nil {
		return 0
	}
	return h.Dims
}
