package tools

import (
	"context"

	"hackathonkit.local/agentkit/internal/mcp"
	"hackathonkit.local/agentkit/internal/quality"
)

func registerQuality(reg *mcp.Registry, d Deps) {
	reg.Register(tool(
		"quality.gate",
		"Read the latest build/verify gate evidence (evals/quality/<sha>/gate.json): gofmt, build, vet, tests, MCP selftest and skill validation. Produce evidence with scripts/quality-gate.sh or `docker compose run --rm quality`.",
		map[string]any{},
		nil,
		func(ctx context.Context, args map[string]any) (any, error) {
			report, ok := quality.Latest(d.Cfg.Root())
			if !ok {
				return mcp.Partial(trace(ctx), map[string]any{
					"available": false,
				}, "no gate evidence found; run scripts/quality-gate.sh (or docker compose run --rm quality)"), nil
			}
			failed := quality.FailedChecks(report)
			data := map[string]any{
				"available":   true,
				"sha":         report.SHA,
				"ok":          report.OK,
				"started_at":  report.StartedAt,
				"finished_at": report.FinishedAt,
				"summary":     report.Summary,
				"failed":      failed,
				"checks":      report.Checks,
			}
			env := mcp.OK(trace(ctx), data)
			if !report.OK {
				env = env.WithWarnings("quality gate has failing checks")
			}
			return env.WithMetrics(map[string]any{
				"ok":            report.OK,
				"failed_checks": len(failed),
				"duration_hint": "see checks[].duration_ms",
			}), nil
		},
	))
}
