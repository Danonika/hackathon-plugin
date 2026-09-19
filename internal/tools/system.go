package tools

import (
	"context"
	"strings"

	"hackathonkit.local/agentkit/internal/mcp"
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
			res := d.Router.Recommend(task, argMap(args, "input_metadata"), available)
			if len(res.Recommendations) == 0 {
				return mcp.Partial(trace(ctx), map[string]any{
					"route":           "unknown",
					"recommendations": []any{},
				}, "fly router produced no recommendations; continue in normal mode"), nil
			}
			env := mcp.OK(trace(ctx), map[string]any{
				"route":           res.Route,
				"recommendations": res.Recommendations,
			})
			env = env.WithMetrics(map[string]any{
				"max_score":       res.MaxScore,
				"threshold":       res.Threshold,
				"category_scores": res.CategoryScores,
			})
			if res.MaxScore < res.Threshold {
				env = env.WithWarnings("fly max score below threshold; ignore recommendation and continue in normal mode")
			}
			return env, nil
		},
	))
}

func toolNameIsExperimental(name string) bool {
	return strings.HasPrefix(name, "routing.")
}
