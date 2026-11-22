package fly

import (
	"context"
	"log"
	"math"
	"reflect"
	"sort"
	"time"
)

type LLMClassifier interface {
	ClassifyTask(ctx context.Context, task string, categories []string) (map[string]float64, error)
}

func (r *Router) WithLLM(classifier LLMClassifier) *Router {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.llmRevision++
	r.llm = classifier
	r.llmRetryAt = time.Time{}
	return r
}

func (r *Router) llmCategoryScores(ctx context.Context, task string) (map[string]float64, bool) {
	r.mu.Lock()
	revision := r.llmRevision
	classifier := r.llm
	coolingDown := time.Now().Before(r.llmRetryAt)
	r.mu.Unlock()
	if coolingDown || classifier == nil || nilInterface(classifier) {
		return nil, false
	}
	categories := make([]string, 0, len(categoryCapabilities))
	for category := range categoryCapabilities {
		categories = append(categories, category)
	}
	sort.Strings(categories)
	scores, err := classifier.ClassifyTask(ctx, task, categories)
	if err != nil {
		// A persistently failing classifier must be distinguishable from "no
		// classifier configured", so surface it in the logs.
		if ctx.Err() == nil {
			r.mu.Lock()
			if r.llmRevision == revision {
				r.llmRetryAt = time.Now().Add(30 * time.Second)
			}
			r.mu.Unlock()
			log.Print("fly: llm classifier unavailable; retrying after 30s")
		}
		return nil, false
	}
	if len(scores) == 0 {
		return nil, false
	}
	known := map[string]bool{}
	for _, category := range categories {
		known[category] = true
	}
	clean := map[string]float64{}
	for category, score := range scores {
		if !known[category] || math.IsNaN(score) || math.IsInf(score, 0) {
			continue
		}
		if score < 0 {
			score = 0
		}
		if score > 1 {
			score = 1
		}
		clean[category] = score
	}
	if len(clean) == 0 {
		return nil, false
	}
	// A classifier that returns all-zero or (with several categories) all-equal
	// scores carries no information; treat it as unavailable instead of a flat
	// signal. A single category is a legitimate signal. Deriving a
	// deterministic min/max baseline avoids the random map-iteration order.
	minScore, maxScore := math.Inf(1), 0.0
	for _, score := range clean {
		if score < minScore {
			minScore = score
		}
		if score > maxScore {
			maxScore = score
		}
	}
	if maxScore <= 0 {
		return nil, false
	}
	if len(clean) > 1 && maxScore-minScore <= 1e-9 {
		return nil, false
	}
	return clean, true
}

// nilInterface reports whether v is a nil value of a nil-capable kind, so a
// typed-nil map/slice/func/chan classifier is treated like a nil pointer.
func nilInterface(v any) bool {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return rv.IsNil()
	}
	return false
}
