package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"hackathonkit.local/agentkit/internal/mcp"
	"hackathonkit.local/agentkit/internal/metadata"
	"hackathonkit.local/agentkit/internal/observability"
	"hackathonkit.local/agentkit/internal/routing/fly"
)

func registerSystem(reg *mcp.Registry, d Deps) {
	reg.Register(tool(
		"system.context",
		"Read live routing mode, routing config version and the available capabilities. Call this before planning. Use it to detect normal vs fly mode.",
		map[string]any{},
		nil,
		func(ctx context.Context, args map[string]any) (any, error) {
			state := d.Cfg.Routing()
			caps := domainCapabilities(reg)
			env := mcp.OK(trace(ctx), map[string]any{
				"routing_mode":           state.Mode,
				"routing_config_version": state.Version,
				"capabilities":           caps,
			})
			return env.WithMetrics(map[string]any{
				"repo_root":  d.Cfg.Root(),
				"tool_count": len(reg.Names()),
			}), nil
		},
	))

	reg.Register(tool(
		"system.capabilities",
		"Report capability groups and experimental features of the Hackathon Agent Kit MCP server.",
		map[string]any{},
		nil,
		func(ctx context.Context, args map[string]any) (any, error) {
			available := map[string]bool{}
			for _, n := range reg.Names() {
				available[n] = true
			}
			groups := map[string]bool{
				"documents":      available["documents.extract"],
				"rag":            available["knowledge.search"],
				"speech":         available["audio.transcribe"],
				"vision":         available["vision.analyze"],
				"data_analysis":  available["data.inspect"],
				"timeseries":     available["timeseries.analyze"],
				"classification": available["classification.classify"],
				"charts":         available["charts.create"],
				"reports":        available["reports.create"],
				"evaluation":     available["eval.aggregate"],
				"learning":       available["learning.recall"],
				"context_budget": available["system.context_budget"],
				"telegram":       available["telegram.send"],
			}
			experimental := map[string]any{"fly_router": available["routing.fly_recommend"]}
			groups["fly_router_available"] = available["routing.fly_recommend"]
			return mcp.OK(trace(ctx), map[string]any{
				"groups":       groups,
				"experimental": experimental,
				"tools":        reg.Names(),
			}), nil
		},
	))

	reg.Register(tool(
		"routing.fly_recommend",
		"Experimental Fly capability router. Returns a ranked capability shortlist for a task. This is a recommendation only: Codex validates it and keeps the final decision.",
		map[string]any{
			"task":                   strProp("Natural-language task description"),
			"input_metadata":         objProp("Input metadata such as types, rows, columns, has_timestamp"),
			"available_capabilities": arrProp("Capability list to rank", map[string]any{"type": "string"}),
			"emit_activation":        boolProp("Re-embed the task to export the KC activation snapshot (default true; set false to skip the extra embed pass)"),
		},
		[]string{"task"},
		func(ctx context.Context, args map[string]any) (any, error) {
			task := argString(args, "task")
			if task == "" {
				return mcp.Fail(trace(ctx), "task is required"), nil
			}
			available := argStringSlice(args, "available_capabilities")
			if len(available) == 0 {
				available = domainCapabilities(reg)
			}
			if d.Metrics != nil {
				d.Metrics.Inc("fly_route_requests_total")
			}
			// emit_activation defaults to true to preserve the demo event stream;
			// callers that want to avoid the extra embed pass can disable it.
			emitActivation := argBool(args, "emit_activation", true)
			telemetryWarnings := []string{}
			logJSONL := func(path string, record map[string]any) {
				if err := observability.AppendJSONL(path, record); err != nil {
					telemetryWarnings = append(telemetryWarnings, "telemetry append failed: "+redactBackendError(err))
				}
			}
			attachTelemetryWarnings := func(env mcp.Envelope) mcp.Envelope {
				if len(telemetryWarnings) > 0 {
					env = env.WithWarnings(telemetryWarnings...)
				}
				return env
			}
			res := d.Router.Recommend(ctx, task, argMap(args, "input_metadata"), available)
			if len(res.Recommendations) == 0 {
				if d.Store != nil {
					logJSONL(filepath.Join(d.Store.Root(), "logs", "fly-events.jsonl"), map[string]any{
						"ts":       time.Now().UTC().Format(time.RFC3339Nano),
						"trace_id": trace(ctx),
						"route":    "unknown",
						"encoder":  res.Encoder,
						"task":     snippet(task, 200),
					})
				}
				return attachTelemetryWarnings(mcp.Partial(trace(ctx), map[string]any{
					"route":           "unknown",
					"recommendations": []any{},
				}, "fly router produced no recommendations; continue in normal mode")), nil
			}
			env := mcp.OK(trace(ctx), map[string]any{
				"route":           res.Route,
				"recommendations": res.Recommendations,
			})
			env = env.WithMetrics(map[string]any{
				"max_score":       res.MaxScore,
				"threshold":       res.Threshold,
				"encoder":         res.Encoder,
				"category_scores": res.CategoryScores,
			})
			if res.MaxScore < res.Threshold {
				env = env.WithWarnings("fly max score below threshold; ignore recommendation and continue in normal mode")
			}
			if d.Store != nil {
				logJSONL(filepath.Join(d.Store.Root(), "logs", "fly-events.jsonl"), map[string]any{
					"ts":              time.Now().UTC().Format(time.RFC3339Nano),
					"trace_id":        trace(ctx),
					"route":           res.Route,
					"encoder":         res.Encoder,
					"max_score":       res.MaxScore,
					"threshold":       res.Threshold,
					"category_scores": res.CategoryScores,
					"recommendations": res.Recommendations,
					"task":            snippet(task, 200),
				})
				var snapshot map[string]any
				if emitActivation {
					snapshot = d.Router.ActivationSnapshot(ctx, task, 64)
				}
				// Only emit the neuron event when the snapshot was actually
				// captured: with emit_activation disabled the snapshot is nil,
				// and an event full of nulls is indistinguishable from a failed
				// capture (and would break the boolean activation_available
				// contract).
				if emitActivation && snapshot != nil {
					logJSONL(filepath.Join(d.Store.Root(), "logs", "fly-neuron-events.jsonl"), map[string]any{
						"ts":                   time.Now().UTC().Format(time.RFC3339Nano),
						"type":                 "recommend",
						"trace_id":             trace(ctx),
						"task":                 snippet(task, 200),
						"route":                res.Route,
						"encoder":              res.Encoder,
						"max_score":            res.MaxScore,
						"active_kc":            snapshot["active_kc"],
						"mbon":                 snapshot["mbon"],
						"kc_total":             snapshot["kc_total"],
						"activation_available": snapshot["activation_available"],
						"activation_complete":  snapshot["activation_complete"],
						"active_kc_count":      snapshot["active_kc_count"],
					})
				}
			}
			if d.Meta != nil && d.Meta.Enabled() {
				if err := d.Meta.RecordFlyPrediction(ctx, metadata.FlyPrediction{
					TraceID:         trace(ctx),
					Route:           res.Route,
					Encoder:         res.Encoder,
					MaxScore:        res.MaxScore,
					Threshold:       res.Threshold,
					CategoryScores:  res.CategoryScores,
					Recommendations: res.Recommendations,
				}); err != nil {
					telemetryWarnings = append(telemetryWarnings, "fly prediction not recorded: "+redactBackendError(err))
				}
			}
			return attachTelemetryWarnings(env), nil
		},
	))
	reg.Register(tool(
		"routing.fly_feedback",
		"Report the final decision for a fly-mode task so the router can learn: what it recommended, what Codex actually executed and the outcome. Call once per completed task.",
		map[string]any{
			"task":           strProp("Task description used for routing"),
			"chosen":         arrProp("Capabilities that Codex actually executed", map[string]any{"type": "string"}),
			"recommended":    arrProp("Capabilities the router recommended", map[string]any{"type": "string"}),
			"source":         strProp("accepted | codex-correction | normal-teacher | fallback"),
			"input_metadata": objProp("Input metadata used for routing"),
		},
		[]string{"task", "chosen"},
		func(ctx context.Context, args map[string]any) (any, error) {
			task := argString(args, "task")
			chosen := argStringSlice(args, "chosen")
			if task == "" || len(chosen) == 0 {
				return mcp.Fail(trace(ctx), "task and chosen are required"), nil
			}
			source := argString(args, "source")
			if source == "" {
				source = "accepted"
			}
			switch source {
			case "accepted", "codex-correction", "normal-teacher", "fallback":
			default:
				return mcp.Fail(trace(ctx), fmt.Sprintf("invalid source %q (expected accepted, codex-correction, normal-teacher or fallback)", source)), nil
			}
			recommended := []fly.Recommendation{}
			for _, name := range argStringSlice(args, "recommended") {
				recommended = append(recommended, fly.Recommendation{Capability: name})
			}
			size, learned, err := d.Router.AddFeedback(ctx, fly.FeedbackEvent{
				Task:          task,
				Recommended:   recommended,
				Chosen:        chosen,
				Source:        source,
				InputMetadata: argMap(args, "input_metadata"),
			})
			if err != nil {
				return mcp.Fail(trace(ctx), redactBackendError(err)), nil
			}
			if d.Metrics != nil {
				d.Metrics.Inc("fly_feedback_total")
			}
			telemetryWarnings := []string{}
			logJSONL := func(path string, record map[string]any) {
				if err := observability.AppendJSONL(path, record); err != nil {
					telemetryWarnings = append(telemetryWarnings, "telemetry append failed: "+redactBackendError(err))
				}
			}
			if d.Store != nil {
				logJSONL(filepath.Join(d.Store.Root(), "logs", "fly-feedback.jsonl"), map[string]any{
					"ts":          time.Now().UTC().Format(time.RFC3339Nano),
					"trace_id":    trace(ctx),
					"task":        snippet(task, 200),
					"chosen":      chosen,
					"recommended": recommended,
					"source":      source,
					"memory_size": size,
				})
				stats := d.Router.Dopamine()
				logJSONL(filepath.Join(d.Store.Root(), "logs", "fly-neuron-events.jsonl"), map[string]any{
					"ts":       time.Now().UTC().Format(time.RFC3339Nano),
					"type":     "feedback",
					"trace_id": trace(ctx),
					"source":   source,
					"reward":   source == "accepted" || source == "normal-teacher",
					"pam":      stats.PAM,
					"ppl1":     stats.PPL1,
					"chosen":   chosen,
				})
			}
			env := mcp.OK(trace(ctx), map[string]any{
				"stored":  size,
				"source":  source,
				"learned": learned,
			}).WithMetrics(map[string]any{"memory_size": size})
			if len(telemetryWarnings) > 0 {
				env = env.WithWarnings(telemetryWarnings...)
			}
			return env, nil
		},
	))
}
