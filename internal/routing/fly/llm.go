package fly

import (
	"context"
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
	if r.llm == nil {
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
	return scores, true
}
