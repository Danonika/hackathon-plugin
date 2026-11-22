package fly

import (
	"context"
	"sort"
)

type KCSample struct {
	Index      int     `json:"index"`
	Activation float64 `json:"activation"`
}

// ActivationSnapshot returns the actual sparse KC code. A zero limit includes
// every active cell; omitted cells are zero only when activation_complete is true.
func (r *Router) ActivationSnapshot(ctx context.Context, task string, topN int) map[string]any {
	if topN < 0 {
		topN = 64
	}
	empty := map[string]any{
		"active_kc": []KCSample{}, "mbon": map[string]float64{}, "kc_total": MBKenyonCells,
		"activation_available": false, "activation_complete": false, "active_kc_count": 0,
	}
	brain := r.loadBrain()
	if brain != nil && brain.KC > 0 {
		empty["kc_total"] = brain.KC
	}
	if brain == nil {
		empty["reason"] = "brain not loaded"
		return empty
	}
	if r.embedder == nil {
		empty["reason"] = "no embedder configured"
		return empty
	}
	vec, ok := r.embedTask(ctx, task)
	if !ok {
		empty["reason"] = "task embedding unavailable"
		return empty
	}
	scores, code, ok := brain.scores(vec)
	if !ok {
		empty["reason"] = "brain readout unavailable"
		return empty
	}
	samples := make([]KCSample, 0)
	for i, v := range code {
		if v > 0 {
			samples = append(samples, KCSample{Index: i, Activation: v})
		}
	}
	sort.Slice(samples, func(i, j int) bool {
		if samples[i].Activation == samples[j].Activation {
			return samples[i].Index < samples[j].Index
		}
		return samples[i].Activation > samples[j].Activation
	})
	activeCount := len(samples)
	if topN > 0 && len(samples) > topN {
		samples = samples[:topN]
	}
	return map[string]any{
		"active_kc": samples, "mbon": scores, "kc_total": brain.KC,
		"activation_available": true, "activation_complete": len(samples) == activeCount,
		"active_kc_count": activeCount,
	}
}
