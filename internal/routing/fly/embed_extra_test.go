package fly

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"hackathonkit.local/agentkit/internal/config"
)

type funcEmbedder struct {
	fn func(ctx context.Context, texts []string) ([][]float64, error)
}

func (f funcEmbedder) Embed(ctx context.Context, texts []string) ([][]float64, error) {
	return f.fn(ctx, texts)
}

func TestEmbeddingCacheReuseSkipsBatchEmbed(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "emb.json")
	cached := embeddingCache{
		Model:     "fake-model",
		Hash:      exemplarHash(),
		Centroids: map[string][]float64{"data": {1, 0}},
	}
	data, err := json.Marshal(cached)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	var batchCalls int32
	embedder := funcEmbedder{fn: func(_ context.Context, texts []string) ([][]float64, error) {
		if len(texts) > 1 {
			atomic.AddInt32(&batchCalls, 1)
		}
		out := make([][]float64, len(texts))
		for i := range out {
			out[i] = []float64{1, 0}
		}
		return out, nil
	}}
	r := NewRouter(config.RoutingSettings{FlyThreshold: 0.55, TopK: 5}).
		WithEmbeddings(embedder, cachePath, "fake-model")
	res := r.Recommend(context.Background(), "analyze the csv table", nil, nil)
	if got := atomic.LoadInt32(&batchCalls); got != 0 {
		t.Fatalf("a valid cache must skip the exemplar batch embed, got %d batch calls", got)
	}
	if !strings.Contains(res.Encoder, "embedding") {
		t.Fatalf("cached centroids must still produce embedding scores, encoder=%q", res.Encoder)
	}
	if res.Route != "data" {
		t.Fatalf("route = %q, want data (scores %v)", res.Route, res.CategoryScores)
	}
}

func TestEmbeddingCacheInvalidEntriesAreRebuilt(t *testing.T) {
	cases := []struct {
		name  string
		write func(path string) error
	}{
		{"garbage", func(path string) error {
			return os.WriteFile(path, []byte("not-json"), 0o644)
		}},
		{"stale hash", func(path string) error {
			return os.WriteFile(path, []byte(`{"model":"fake-model","hash":"stale","centroids":{"data":[1,0]}}`), 0o644)
		}},
		{"wrong model", func(path string) error {
			cache := embeddingCache{Model: "other-model", Hash: exemplarHash(), Centroids: map[string][]float64{"data": {1, 0}}}
			data, err := json.Marshal(cache)
			if err != nil {
				return err
			}
			return os.WriteFile(path, data, 0o644)
		}},
		{"empty centroids", func(path string) error {
			cache := embeddingCache{Model: "fake-model", Hash: exemplarHash(), Centroids: map[string][]float64{}}
			data, err := json.Marshal(cache)
			if err != nil {
				return err
			}
			return os.WriteFile(path, data, 0o644)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			cachePath := filepath.Join(dir, "emb.json")
			if err := tc.write(cachePath); err != nil {
				t.Fatal(err)
			}
			r := NewRouter(config.RoutingSettings{}).WithEmbeddings(fakeEmbedder{}, cachePath, "fake-model")
			res := r.Recommend(context.Background(), "analyze the csv table", nil, nil)
			if !strings.Contains(res.Encoder, "embedding") {
				t.Fatalf("an invalid cache must be rebuilt, encoder=%q", res.Encoder)
			}
			data, err := os.ReadFile(cachePath)
			if err != nil {
				t.Fatal(err)
			}
			var cache embeddingCache
			if err := json.Unmarshal(data, &cache); err != nil {
				t.Fatalf("rebuilt cache is not valid JSON: %v", err)
			}
			if cache.Hash != exemplarHash() || cache.Model != "fake-model" || len(cache.Centroids) != len(categoryExemplars) {
				t.Fatalf("cache not rebuilt correctly: model=%q hash=%q centroids=%d", cache.Model, cache.Hash, len(cache.Centroids))
			}
		})
	}
}

func TestEmbeddingBuildFailuresFallBackToKeywords(t *testing.T) {
	cases := []struct {
		name string
		fn   func(ctx context.Context, texts []string) ([][]float64, error)
	}{
		{"error", func(context.Context, []string) ([][]float64, error) {
			return nil, errors.New("boom")
		}},
		{"wrong count", func(_ context.Context, texts []string) ([][]float64, error) {
			return make([][]float64, len(texts)-1), nil
		}},
		{"zero dimension", func(_ context.Context, texts []string) ([][]float64, error) {
			out := make([][]float64, len(texts))
			for i := range out {
				out[i] = []float64{}
			}
			return out, nil
		}},
		{"dimension mismatch", func(_ context.Context, texts []string) ([][]float64, error) {
			out := make([][]float64, len(texts))
			for i := range out {
				out[i] = []float64{1, 0}
				if i == 1 {
					out[i] = []float64{1, 0, 0}
				}
			}
			return out, nil
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRouter(config.RoutingSettings{}).WithEmbeddings(funcEmbedder{fn: tc.fn}, "", "fake-model")
			res := r.Recommend(context.Background(), "find anomalies in telemetry", nil, nil)
			if res.Encoder != "keyword" {
				t.Fatalf("encoder = %q, want keyword fallback", res.Encoder)
			}
		})
	}
}

func TestEmbeddingBuildPanicIsRecovered(t *testing.T) {
	r := NewRouter(config.RoutingSettings{}).WithEmbeddings(funcEmbedder{fn: func(context.Context, []string) ([][]float64, error) {
		panic("embedder exploded")
	}}, "", "fake-model")
	res := r.Recommend(context.Background(), "find anomalies in telemetry", nil, nil)
	if res.Encoder != "keyword" {
		t.Fatalf("encoder = %q, want keyword fallback after the panic", res.Encoder)
	}
	if _, ok := r.ensureCentroids(context.Background()); ok {
		t.Fatal("the failed build must not be cached")
	}
}

func TestEnsureCentroidsConcurrentBuildSharesResult(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	embedder := funcEmbedder{fn: func(_ context.Context, texts []string) ([][]float64, error) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
		out := make([][]float64, len(texts))
		for i := range out {
			out[i] = []float64{float64(i%5) + 1, 1}
		}
		return out, nil
	}}
	r := NewRouter(config.RoutingSettings{}).WithEmbeddings(embedder, "", "fake-model")
	done := make(chan Result, 1)
	go func() { done <- r.Recommend(context.Background(), "telemetry csv", nil, nil) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("embedder was never called")
	}
	second := make(chan Result, 1)
	go func() { second <- r.Recommend(context.Background(), "telemetry csv", nil, nil) }()
	close(release)
	for _, res := range []Result{<-done, <-second} {
		if !strings.Contains(res.Encoder, "embedding") {
			t.Fatalf("concurrent callers must both use the shared build, encoder=%q", res.Encoder)
		}
	}
}

func TestEnsureCentroidsHonorsContextWhileWaiting(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	embedder := funcEmbedder{fn: func(_ context.Context, texts []string) ([][]float64, error) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
		out := make([][]float64, len(texts))
		for i := range out {
			out[i] = []float64{1, 1}
		}
		return out, nil
	}}
	r := NewRouter(config.RoutingSettings{}).WithEmbeddings(embedder, "", "fake-model")
	done := make(chan struct{})
	go func() {
		r.Recommend(context.Background(), "telemetry", nil, nil)
		close(done)
	}()
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, ok := r.embeddingCategoryScores(ctx, "telemetry"); ok {
		t.Fatal("a cancelled waiter must not receive centroids")
	}
	close(release)
	<-done
}

func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "file.json")
	writeFileAtomic(path, []byte("hello"))
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "hello" {
		t.Fatalf("writeFileAtomic did not write the file: %q err=%v", data, err)
	}
	writeFileAtomic(path, []byte("second"))
	if data, err = os.ReadFile(path); err != nil || string(data) != "second" {
		t.Fatalf("writeFileAtomic must replace existing content: %q err=%v", data, err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatalf("atomic write left temp files behind: %v err=%v", entries, err)
	}

	blocked := filepath.Join(dir, "blocked")
	if err := os.MkdirAll(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFileAtomic(blocked, []byte("x"))
	info, err := os.Stat(blocked)
	if err != nil || !info.IsDir() {
		t.Fatalf("a failed rename must not clobber the directory: %v err=%v", info, err)
	}

	fileParent := filepath.Join(dir, "file-parent")
	if err := os.WriteFile(fileParent, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeFileAtomic(filepath.Join(fileParent, "child.json"), []byte("x"))
}

func TestEmbedTaskRejectsBadVectorCounts(t *testing.T) {
	ctx := context.Background()
	if _, ok := NewRouter(config.RoutingSettings{}).embedTask(ctx, "task"); ok {
		t.Fatal("a router without an embedder must not embed")
	}
	r := NewRouter(config.RoutingSettings{}).WithEmbeddings(funcEmbedder{fn: func(_ context.Context, texts []string) ([][]float64, error) {
		return [][]float64{}, nil
	}}, "", "fake")
	if _, ok := r.embedTask(ctx, "task"); ok {
		t.Fatal("an empty vector list must be rejected")
	}
	r = r.WithEmbeddings(funcEmbedder{fn: func(_ context.Context, texts []string) ([][]float64, error) {
		return [][]float64{{1}, {2}}, nil
	}}, "", "fake")
	if _, ok := r.embedTask(ctx, "task"); ok {
		t.Fatal("more vectors than texts must be rejected")
	}
}

func TestCosineAndNormalizeEdgeCases(t *testing.T) {
	if got := cosine(nil, nil); got != 0 {
		t.Fatalf("cosine of empty vectors = %v, want 0", got)
	}
	if got := cosine([]float64{1, 2}, []float64{3}); got != 3 {
		t.Fatalf("cosine must use the shorter length, got %v", got)
	}
	if got := cosine([]float64{}, []float64{1}); got != 0 {
		t.Fatalf("cosine with an empty first vector = %v, want 0", got)
	}
	zero := normalizeVector([]float64{0, 0})
	if len(zero) != 2 || zero[0] != 0 || zero[1] != 0 {
		t.Fatalf("a zero vector must be returned unchanged, got %v", zero)
	}
}
