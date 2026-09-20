package tools

import (
	"context"

	"hackathonkit.local/agentkit/internal/learning"
	"hackathonkit.local/agentkit/internal/mcp"
)

func registerLearning(reg *mcp.Registry, d Deps) {
	reg.Register(tool(
		"learning.record",
		"Record a durable learning from a completed task: a trigger (when it applies) and an insight (what to do). Learnings are project-scoped, confidence-scored, deduplicated and injected into future sessions.",
		map[string]any{
			"trigger":  strProp("When this learning applies, e.g. 'when a Go tool handler returns an error'"),
			"insight":  strProp("What to do or avoid, one or two sentences"),
			"outcome":  strProp("success (default), failure or neutral"),
			"scope":    strProp("project (default) or global"),
			"evidence": arrProp("Evidence references: artifact ids, test output, trace ids", map[string]any{"type": "string"}),
		},
		[]string{"trigger", "insight"},
		func(ctx context.Context, args map[string]any) (any, error) {
			if d.Learning == nil {
				return mcp.Fail(trace(ctx), "learning store is disabled"), nil
			}
			result, err := d.Learning.Record(ctx, learning.RecordInput{
				Trigger:  argString(args, "trigger"),
				Insight:  argString(args, "insight"),
				Outcome:  argString(args, "outcome"),
				Scope:    argString(args, "scope"),
				Evidence: argStringSlice(args, "evidence"),
			})
			if err != nil {
				return mcp.Fail(trace(ctx), err.Error()), nil
			}
			if d.Metrics != nil {
				d.Metrics.Inc("learning_records_total")
			}
			return mcp.OK(trace(ctx), map[string]any{
				"id":           result.Learning.ID,
				"confidence":   result.Learning.Confidence,
				"observations": result.Learning.Observations,
				"scope":        result.Learning.Scope,
				"created":      result.Created,
			}).WithMetrics(map[string]any{
				"confidence": result.Learning.Confidence,
				"created":    result.Created,
			}), nil
		},
	))

	reg.Register(tool(
		"learning.recall",
		"Recall the most relevant stored learnings for a query. Use before planning or repeating a task to reuse what already worked or failed.",
		map[string]any{
			"query":          strProp("What you are about to do, in natural language"),
			"limit":          numProp("Maximum matches (default 5, max 50)"),
			"min_confidence": numProp("Minimum confidence (default from configs/config.yaml)"),
			"scope":          strProp("project (default, includes global), global or all"),
		},
		[]string{"query"},
		func(ctx context.Context, args map[string]any) (any, error) {
			if d.Learning == nil {
				return mcp.Fail(trace(ctx), "learning store is disabled"), nil
			}
			settings := d.Cfg.Config().Learning
			minConfidence := argFloat(args, "min_confidence", settings.MinConfidence)
			matches, err := d.Learning.Recall(ctx, argString(args, "query"), argInt(args, "limit", 5), minConfidence, argString(args, "scope"))
			if err != nil {
				return mcp.Fail(trace(ctx), err.Error()), nil
			}
			if d.Metrics != nil {
				d.Metrics.Inc("learning_recalls_total")
			}
			if len(matches) == 0 {
				return mcp.Partial(trace(ctx), map[string]any{
					"matches": []any{},
					"count":   0,
				}, "no learnings matched; record one with learning.record after the task"), nil
			}
			return mcp.OK(trace(ctx), map[string]any{
				"matches": matches,
				"count":   len(matches),
			}).WithMetrics(map[string]any{"count": len(matches)}), nil
		},
	))

	reg.Register(tool(
		"learning.promote",
		"Promote a project learning to global scope once it has been observed enough times and is confident enough.",
		map[string]any{
			"id": strProp("Learning id returned by learning.record or learning.recall"),
		},
		[]string{"id"},
		func(ctx context.Context, args map[string]any) (any, error) {
			if d.Learning == nil {
				return mcp.Fail(trace(ctx), "learning store is disabled"), nil
			}
			settings := d.Cfg.Config().Learning
			item, promoted, err := d.Learning.Promote(argString(args, "id"), settings.PromoteAfter, settings.InjectMinConfidence)
			if err != nil {
				return mcp.Fail(trace(ctx), err.Error()), nil
			}
			reason := "promoted to global scope"
			if !promoted {
				if item.Scope == "global" {
					reason = "already global"
				} else {
					reason = "not eligible yet: needs more observations or higher confidence"
				}
			}
			return mcp.OK(trace(ctx), map[string]any{
				"promoted": promoted,
				"reason":   reason,
				"learning": item,
			}), nil
		},
	))
}
