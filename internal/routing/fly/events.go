package fly

import (
	"context"
	"sort"
)

type KCSample struct {
	Index      int     `json:"index"`
	Activation float64 `json:"activation"`
}

func (r *Router) ActivationSnapshot(ctx context.Context, task string, topN int) map[string]any {
	if topN <= 0 || topN > 512 {
		topN = 64
	}
	empty := map[string]any{"active_kc": []KCSample{}, "mbon": map[string]float64{}, "kc_total": MBKenyonCells}
	brain := r.loadBrain()
	if brain != nil && brain.KC > 0 {
		empty["kc_total"] = brain.KC
	}
	if brain == nil || r.embedder == nil {
		return empty
	}
	vec, ok := r.embedTask(ctx, task)
	if !ok {
		return empty
	}
	brainMu.Lock()
	scores, code, ok := brain.scores(vec)
	brainMu.Unlock()
	if !ok {
		return empty
	}
	samples := make([]KCSample, 0, topN)
	for i, v := range code {
		if v > 0 {
			samples = append(samples, KCSample{Index: i, Activation: v})
		}
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i].Activation > samples[j].Activation })
	if len(samples) > topN {
		samples = samples[:topN]
	}
	return map[string]any{"active_kc": samples, "mbon": scores, "kc_total": brain.KC}
}
