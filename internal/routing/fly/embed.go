package fly

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"sort"

	"hackathonkit.local/agentkit/internal/atomicfile"
)

type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float64, error)
}

var categoryExemplars = map[string][]string{
	"documents": {
		"extract text from a pdf contract", "parse a scanned document", "достань данные из договора pdf",
		"document field extraction", "compare two docx documents", "извлеки текст из документа",
	},
	"knowledge": {
		"find the rule in the regulation corpus", "answer a question with citations", "найди ответ в регламенте",
		"search internal policy documents", "verify a claim against sources", "найди информацию в базе знаний",
	},
	"speech": {
		"transcribe this audio recording", "make subtitles from the interview", "расшифруй аудиозапись встречи",
		"speech to text for a meeting", "транскрипция голоса", "запись разговора в текст",
	},
	"vision": {
		"analyze this image", "read a screenshot", "опиши что на фото", "recognize objects on the picture",
		"analyze a photo of the equipment", "что видно на скриншоте",
	},
	"media": {
		"cut video segments with ffmpeg", "make a clip from the video", "нарежь видео на фрагменты",
		"media montage", "выбери лучшие фрагменты записи",
	},
	"data": {
		"analyze the csv table", "compute statistics for the dataset", "посчитай статистику по таблице",
		"correlations between columns", "query rows in the spreadsheet", "проанализируй данные xlsx",
	},
	"timeseries": {
		"find anomalies in sensor telemetry", "detect spikes in the time series", "найди аномалии в телеметрии датчиков",
		"trend of the metric over time", "analyze hourly readings", "тренд показателя во времени",
	},
	"classification": {
		"classify these logs", "categorize the incoming messages", "определи тип инцидента",
		"spam or fraud detection", "классифицируй обращения", "risk category of the text",
	},
	"geo": {
		"plot the field on the map", "process coordinates and geometry", "карта участка и координаты",
		"geospatial analysis", "нанеси точки на карту",
	},
	"reports": {
		"build a report with findings", "prepare the final summary", "собери отчёт с выводами",
		"make a presentation summary", "оформи результаты анализа",
	},
}

type embeddingCache struct {
	Model     string               `json:"model"`
	Hash      string               `json:"hash"`
	Centroids map[string][]float64 `json:"centroids"`
}

func exemplarHash() string {
	cats := make([]string, 0, len(categoryExemplars))
	for cat := range categoryExemplars {
		cats = append(cats, cat)
	}
	sort.Strings(cats)
	h := sha1.New()
	for _, cat := range cats {
		fmt.Fprintf(h, "%s|", cat)
		for _, ex := range categoryExemplars[cat] {
			fmt.Fprintf(h, "%s|", ex)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (r *Router) embedTask(ctx context.Context, task string) ([]float64, bool) {
	// One-entry cache: Recommend embeds the same task for the embedding, kNN
	// and mushroom-body signals, which would otherwise be three provider calls.
	r.embedCacheMu.Lock()
	if r.embedCacheTask == task && (r.embedCacheOK || r.embedCacheVec == nil) {
		vec, ok := r.embedCacheVec, r.embedCacheOK
		r.embedCacheMu.Unlock()
		return vec, ok
	}
	r.embedCacheMu.Unlock()
	if r.embedder == nil {
		return nil, false
	}
	vectors, err := r.embedder.Embed(ctx, []string{task})
	ok := err == nil && len(vectors) == 1
	var vec []float64
	if ok {
		vec = normalizeVector(vectors[0])
	}
	r.embedCacheMu.Lock()
	r.embedCacheTask, r.embedCacheVec, r.embedCacheOK = task, vec, ok
	r.embedCacheMu.Unlock()
	return vec, ok
}

func (r *Router) embeddingCategoryScores(ctx context.Context, task string) (map[string]float64, bool) {
	centroids, ok := r.ensureCentroids(ctx)
	if !ok {
		return nil, false
	}
	taskVec, ok := r.embedTask(ctx, task)
	if !ok {
		return nil, false
	}
	sims := map[string]float64{}
	for cat, centroid := range centroids {
		if len(centroid) != len(taskVec) {
			// Stale cache from another embedding model: skip instead of
			// silently comparing truncated vectors.
			continue
		}
		sims[cat] = cosine(taskVec, centroid)
	}
	if len(sims) == 0 {
		return nil, false
	}
	// Keep the relative ranking of the categories, but cap each score by how
	// similar the category is in absolute terms, so a field of mediocre
	// cosine matches does not get normalized into a confident recommendation.
	const embFloor = 0.2
	const embCeil = 0.8
	minSim, maxSim := math.Inf(1), math.Inf(-1)
	for _, s := range sims {
		if s < minSim {
			minSim = s
		}
		if s > maxSim {
			maxSim = s
		}
	}
	if len(sims) == 1 {
		for cat, s := range sims {
			absolute := (s - embFloor) / (embCeil - embFloor)
			if absolute < 0 {
				absolute = 0
			}
			if absolute > 1 {
				absolute = 1
			}
			return map[string]float64{cat: absolute}, true
		}
	}
	if maxSim-minSim < 1e-9 {
		// All similarities are (nearly) equal: no usable ranking signal.
		return nil, false
	}
	out := map[string]float64{}
	for cat, s := range sims {
		rank := (s - minSim) / (maxSim - minSim)
		absolute := (s - embFloor) / (embCeil - embFloor)
		if absolute < 0 {
			absolute = 0
		}
		if absolute > 1 {
			absolute = 1
		}
		if rank > absolute {
			rank = absolute
		}
		out[cat] = rank
	}
	return out, true
}

func (r *Router) ensureCentroids(ctx context.Context) (map[string][]float64, bool) {
	if r.embedder == nil {
		return nil, false
	}
	r.mu.Lock()
	if r.centroids != nil {
		cached := r.centroids
		r.mu.Unlock()
		return cached, true
	}
	if r.building != nil {
		wait := r.building
		r.mu.Unlock()
		select {
		case <-wait:
		case <-ctx.Done():
			return nil, false
		}
		r.mu.Lock()
		cached := r.centroids
		r.mu.Unlock()
		return cached, cached != nil
	}
	r.building = make(chan struct{})
	r.mu.Unlock()

	var centroids map[string][]float64
	var ok bool
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("fly: embedding centroid build panicked: %v", rec)
				centroids, ok = nil, false
			}
		}()
		centroids, ok = r.buildCentroids(ctx)
	}()

	r.mu.Lock()
	if ok {
		r.centroids = centroids
	}
	close(r.building)
	r.building = nil
	r.mu.Unlock()
	if !ok {
		return nil, false
	}
	return centroids, true
}

// buildCentroids performs the cache read, embedding round-trip and atomic
// cache write without holding the router lock, so concurrent callers can wait
// on the single in-flight build and observe their own context deadline.
func (r *Router) buildCentroids(ctx context.Context) (map[string][]float64, bool) {
	hash := exemplarHash()
	if r.cachePath != "" {
		if data, err := os.ReadFile(r.cachePath); err == nil {
			var cache embeddingCache
			if json.Unmarshal(data, &cache) == nil &&
				cache.Hash == hash &&
				cache.Model == r.modelTag &&
				len(cache.Centroids) > 0 {
				return cache.Centroids, true
			}
		}
	}

	cats := make([]string, 0, len(categoryExemplars))
	for cat := range categoryExemplars {
		cats = append(cats, cat)
	}
	sort.Strings(cats)
	texts := []string{}
	index := []int{}
	for ci, cat := range cats {
		for _, ex := range categoryExemplars[cat] {
			texts = append(texts, ex)
			index = append(index, ci)
		}
	}
	vectors, err := r.embedder.Embed(ctx, texts)
	if err != nil || len(vectors) != len(texts) {
		return nil, false
	}
	dims := -1
	for _, vec := range vectors {
		if dims < 0 {
			dims = len(vec)
			if dims == 0 {
				return nil, false
			}
			continue
		}
		if len(vec) != dims {
			return nil, false
		}
	}
	sums := make([][]float64, len(cats))
	counts := make([]int, len(cats))
	for i, vec := range vectors {
		ci := index[i]
		if sums[ci] == nil {
			sums[ci] = make([]float64, dims)
		}
		for d := range vec {
			sums[ci][d] += vec[d]
		}
		counts[ci]++
	}
	centroids := map[string][]float64{}
	for ci, cat := range cats {
		if counts[ci] == 0 {
			continue
		}
		for d := range sums[ci] {
			sums[ci][d] /= float64(counts[ci])
		}
		centroids[cat] = normalizeVector(sums[ci])
	}
	if len(centroids) == 0 {
		return nil, false
	}

	if r.cachePath != "" {
		cache := embeddingCache{Model: r.modelTag, Hash: hash, Centroids: centroids}
		if data, err := json.Marshal(cache); err == nil {
			writeFileAtomic(r.cachePath, data)
		}
	}
	return centroids, true
}

// writeFileAtomic writes data via a unique temp file + rename so concurrent
// writers cannot leave a truncated or interleaved cache behind. Cache writes
// are best-effort: a failed write is retried on the next save.
func writeFileAtomic(path string, data []byte) {
	_ = atomicfile.Write(path, data, atomicfile.ModeContent)
}

func normalizeVector(v []float64) []float64 {
	norm := 0.0
	for _, x := range v {
		norm += x * x
	}
	norm = math.Sqrt(norm)
	if norm == 0 {
		return v
	}
	out := make([]float64, len(v))
	for i, x := range v {
		out[i] = x / norm
	}
	return out
}

func cosine(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		// Mismatched dimensions are not comparable; never truncate silently.
		return 0
	}
	n := len(a)
	sum := 0.0
	for i := 0; i < n; i++ {
		sum += a[i] * b[i]
	}
	return sum
}
