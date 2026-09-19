package fly

import (
	"context"
	"math"
	"sort"
	"strings"
	"sync"

	"hackathonkit.local/agentkit/internal/config"
)

type Recommendation struct {
	Capability string  `json:"capability"`
	Score      float64 `json:"score"`
}

type Result struct {
	Route           string             `json:"route"`
	Recommendations []Recommendation   `json:"recommendations"`
	MaxScore        float64            `json:"max_score"`
	Threshold       float64            `json:"threshold"`
	Encoder         string             `json:"encoder"`
	CategoryScores  map[string]float64 `json:"category_scores"`
}

type Router struct {
	threshold float64
	topK      int

	embedder  Embedder
	cachePath string
	modelTag  string

	mu        sync.Mutex
	centroids map[string][]float64
	building  chan struct{}
}

func NewRouter(settings config.RoutingSettings) *Router {
	threshold := settings.FlyThreshold
	if threshold <= 0 {
		threshold = 0.55
	}
	topK := settings.TopK
	if topK <= 0 {
		topK = 5
	}
	return &Router{threshold: threshold, topK: topK}
}

func (r *Router) WithEmbeddings(embedder Embedder, cachePath string, modelTag string) *Router {
	r.embedder = embedder
	r.cachePath = cachePath
	r.modelTag = modelTag
	return r
}

var categoryKeywords = map[string][]string{
	"documents": {
		"pdf", "docx", "документ", "договор", "акт", "скан", "scan", "извлеч", "текст", "смет", "инструкц",
	},
	"knowledge": {
		"найди", "найти", "поиск", "search", "регламент", "правил", "policy", "справк", "источник", "ответь", "вопрос",
	},
	"speech": {
		"аудио", "audio", "запис", "интервью", "interview", "транскриб", "субтитр", "subtitle", "голос", "voice", "речь",
	},
	"vision": {
		"изображен", "фото", "картинк", "скриншот", "screenshot", "image", "vision", "схем", "чертеж",
	},
	"media": {
		"видео", "video", "монтаж", "нарезк", "ffmpeg", "клип", "фрагмент видео",
	},
	"data": {
		"csv", "xlsx", "excel", "таблиц", "датасет", "dataset", "строк", "колон", "статистик", "данн", "показател", "метрик",
	},
	"timeseries": {
		"телеметри", "telemetry", "датчик", "sensor", "аномал", "anomal", "врем", "time series", "тренд", "динамик", "период",
	},
	"classification": {
		"классифиц", "classify", "категор", "spam", "спам", "фрод", "fraud", "логи", "logs", "типизац", "риск",
	},
	"geo": {
		"карт", "гео", "geo", "координат", "местност", "поля", "участк",
	},
	"reports": {
		"отчёт", "отчет", "report", "презентац", "резюме", "итог", "вывод",
	},
}

var categoryCapabilities = map[string][]string{
	"documents":      {"documents.ingest", "documents.extract", "documents.get"},
	"knowledge":      {"knowledge.search", "documents.get"},
	"speech":         {"audio.transcribe"},
	"vision":         {"vision.analyze"},
	"media":          {"vision.analyze", "audio.transcribe"},
	"data":           {"data.inspect", "data.query", "data.analyze"},
	"timeseries":     {"timeseries.analyze", "timeseries.detect_anomalies", "charts.create"},
	"classification": {"classification.classify"},
	"geo":            {"vision.analyze"},
	"reports":        {"reports.create", "charts.create"},
}

func (r *Router) Recommend(ctx context.Context, task string, meta map[string]any, available []string) Result {
	scores := map[string]float64{}
	lower := strings.ToLower(task)
	for category, keywords := range categoryKeywords {
		hits := 0
		for _, kw := range keywords {
			if strings.Contains(lower, kw) {
				hits++
			}
		}
		if hits > 0 {
			s := 0.3 * float64(hits)
			if s > 1.0 {
				s = 1.0
			}
			scores[category] = s
		}
	}
	keywordSignal := len(scores) > 0
	encoder := "keyword"
	if embeddingScores, ok := r.embeddingCategoryScores(ctx, task); ok {
		encoder = "keyword+embedding"
		for category, emb := range embeddingScores {
			combined := 0.45*scores[category] + 0.55*emb
			if combined > 1.0 {
				combined = 1.0
			}
			scores[category] = combined
		}
	}
	metadataSignal := applyMetadata(scores, meta)
	if keywordSignal || metadataSignal {
		boost(scores, "reports", 0.15)
	}

	avail := map[string]bool{}
	for _, c := range available {
		avail[c] = true
	}
	best := map[string]float64{}
	route := ""
	routeScore := 0.0
	for category, s := range scores {
		if s > routeScore || (s == routeScore && (route == "" || category < route)) {
			route = category
			routeScore = s
		}
		for _, cap := range categoryCapabilities[category] {
			if len(avail) > 0 && !avail[cap] {
				continue
			}
			if s > best[cap] {
				best[cap] = s
			}
		}
	}
	recs := make([]Recommendation, 0, len(best))
	maxScore := 0.0
	for cap, s := range best {
		recs = append(recs, Recommendation{Capability: cap, Score: round2(s)})
		if s > maxScore {
			maxScore = s
		}
	}
	sort.Slice(recs, func(i, j int) bool {
		if recs[i].Score == recs[j].Score {
			return recs[i].Capability < recs[j].Capability
		}
		return recs[i].Score > recs[j].Score
	})
	if len(recs) > r.topK {
		recs = recs[:r.topK]
	}
	if route == "" {
		route = "unknown"
	}
	return Result{
		Route:           route,
		Recommendations: recs,
		MaxScore:        round2(maxScore),
		Threshold:       r.threshold,
		Encoder:         encoder,
		CategoryScores:  roundScores(scores),
	}
}

func applyMetadata(scores map[string]float64, meta map[string]any) bool {
	if meta == nil {
		return false
	}
	applied := false
	boostChecked := func(category string, delta float64) {
		boost(scores, category, delta)
		applied = true
	}
	types := stringSlice(meta["types"])
	for _, t := range types {
		switch t {
		case "csv", "xlsx", "xls", "tsv":
			boostChecked("data", 0.5)
			boostChecked("timeseries", 0.3)
		case "pdf", "docx", "doc":
			boostChecked("documents", 0.7)
			boostChecked("knowledge", 0.3)
		case "audio", "mp3", "wav", "m4a", "ogg":
			boostChecked("speech", 0.7)
			boostChecked("media", 0.2)
		case "image", "png", "jpg", "jpeg":
			boostChecked("vision", 0.7)
		case "video", "mp4", "mov", "mkv":
			boostChecked("media", 0.5)
			boostChecked("vision", 0.3)
			boostChecked("speech", 0.3)
		case "log", "logs", "txt":
			boostChecked("classification", 0.3)
			boostChecked("data", 0.15)
		}
	}
	if v, ok := meta["has_timestamp"].(bool); ok && v {
		boostChecked("timeseries", 0.4)
		boostChecked("data", 0.1)
	}
	if v, ok := meta["rows"].(float64); ok && v >= 500 {
		boostChecked("timeseries", 0.1)
		boostChecked("data", 0.15)
	}
	return applied
}

func boost(scores map[string]float64, category string, delta float64) {
	s := scores[category] + delta
	if s > 1.0 {
		s = 1.0
	}
	if s > 0 {
		scores[category] = s
	}
}

func stringSlice(v any) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok {
				out = append(out, strings.ToLower(s))
			}
		}
		return out
	case string:
		return []string{strings.ToLower(t)}
	}
	return nil
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

func roundScores(in map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(in))
	for k, v := range in {
		out[k] = round2(v)
	}
	return out
}
