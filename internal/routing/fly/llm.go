package fly

import (
	"context"
	"math"
	"reflect"
	"sort"
)

type LLMClassifier interface {
	ClassifyTask(ctx context.Context, task string, categories []string) (map[string]float64, error)
}

func (r *Router) WithLLM(classifier LLMClassifier) *Router {
	r.llm = classifier
	return r
}

func (r *Router) llmCategoryScores(ctx context.Context, task string) (map[string]float64, bool) {
	if r.llm == nil || reflect.ValueOf(r.llm).Kind() == reflect.Ptr && reflect.ValueOf(r.llm).IsNil() {
		return nil, false
	}
	categories := make([]string, 0, len(categoryCapabilities))
	for category := range categoryCapabilities {
		categories = append(categories, category)
	}
	sort.Strings(categories)
	scores, err := r.llm.ClassifyTask(ctx, task, categories)
	if err != nil || len(scores) == 0 {
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
	// signal. A single category is a legitimate signal.
	first, maxScore := 0.0, 0.0
	for _, score := range clean {
		first = score
		break
	}
	for _, score := range clean {
		if score > maxScore {
			maxScore = score
		}
	}
	if maxScore <= 0 {
		return nil, false
	}
	if len(clean) > 1 {
		varying := false
		for _, score := range clean {
			if math.Abs(score-first) > 1e-9 {
				varying = true
				break
			}
		}
		if !varying {
			return nil, false
		}
	}
	return clean, true
}
