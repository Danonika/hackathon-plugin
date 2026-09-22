package tools

import (
	"context"
	"fmt"
	"path/filepath"

	"hackathonkit.local/agentkit/internal/mcp"
	"hackathonkit.local/agentkit/internal/plans"
)

func registerPlans(reg *mcp.Registry, d Deps) {
	reg.Register(tool(
		"plan.create",
		"Create a staged build plan (S0 outcome .. S6 freeze) stored under evals/plans/. The plan's mandatory scenario and stage gates drive the improvement loops.",
		map[string]any{
			"slug":               strProp("Short plan name, e.g. telemetry-anomalies"),
			"task":               strProp("Task statement: input -> expected output"),
			"mandatory_scenario": objProp("Mandatory scenario: {input, command, expected}"),
			"stages": arrProp("Optional stages: [{id, goal, status, timebox_minutes}]", map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":              map[string]any{"type": "string"},
					"goal":            map[string]any{"type": "string"},
					"status":          map[string]any{"type": "string"},
					"timebox_minutes": map[string]any{"type": "number"},
				},
			}),
		},
		[]string{"slug"},
		func(ctx context.Context, args map[string]any) (any, error) {
			slug := plans.Slug(argString(args, "slug"))
			if slug == "" {
				return mcp.Fail(trace(ctx), "slug is required"), nil
			}
			stages := []plans.Stage{}
			if raw, ok := args["stages"].([]any); ok && len(raw) > 0 {
				for _, item := range raw {
					m, ok := item.(map[string]any)
					if !ok {
						continue
					}
					id, _ := m["id"].(string)
					if id == "" {
						continue
					}
					goal, _ := m["goal"].(string)
					status, _ := m["status"].(string)
					if status == "" {
						status = "todo"
					}
					if !plans.ValidStatus(status) {
						return mcp.Fail(trace(ctx), fmt.Sprintf("invalid stage status %q", status)), nil
					}
					timebox := 0
					if v, ok := m["timebox_minutes"].(float64); ok {
						timebox = int(v)
					}
					stages = append(stages, plans.Stage{ID: id, Goal: goal, Status: status, TimeboxMinute: timebox})
				}
			}
			if len(stages) == 0 {
				stages = append(stages, plans.DefaultStages...)
			}
			plan := plans.Plan{
				Slug:              slug,
				Revision:          1,
				Task:              argString(args, "task"),
				MandatoryScenario: argMap(args, "mandatory_scenario"),
				Stages:            stages,
			}
			if err := plans.Save(d.Cfg.Root(), plan); err != nil {
				return mcp.Fail(trace(ctx), err.Error()), nil
			}
			if d.Metrics != nil {
				d.Metrics.Inc("plan_creates_total")
			}
			return mcp.OK(trace(ctx), map[string]any{
				"slug":     plan.Slug,
				"revision": plan.Revision,
				"path":     relPath(d.Cfg.Root(), plans.Path(d.Cfg.Root(), plan.Slug)),
				"stages":   plan.Stages,
				"summary":  plans.Summary(plan),
			}), nil
		},
	))

	reg.Register(tool(
		"plan.update",
		"Update a stage of a build plan: status, evidence references and optionally its goal. Bumps the plan revision.",
		map[string]any{
			"slug":     strProp("Plan name"),
			"stage":    strProp("Stage id, e.g. S3"),
			"status":   strProp("todo | doing | done | failed | cut"),
			"evidence": arrProp("Evidence references: artifact ids, test output, trace ids", map[string]any{"type": "string"}),
			"goal":     strProp("Optional new goal text"),
		},
		[]string{"slug", "stage", "status"},
		func(ctx context.Context, args map[string]any) (any, error) {
			slug := plans.Slug(argString(args, "slug"))
			stageID := argString(args, "stage")
			status := argString(args, "status")
			if slug == "" || stageID == "" || status == "" {
				return mcp.Fail(trace(ctx), "slug, stage and status are required"), nil
			}
			if !plans.ValidStatus(status) {
				return mcp.Fail(trace(ctx), fmt.Sprintf("invalid status %q", status)), nil
			}
			plan, err := plans.Load(d.Cfg.Root(), slug)
			if err != nil {
				return mcp.Fail(trace(ctx), err.Error()), nil
			}
			found := false
			for i := range plan.Stages {
				if plan.Stages[i].ID != stageID {
					continue
				}
				plan.Stages[i].Status = status
				if goal := argString(args, "goal"); goal != "" {
					plan.Stages[i].Goal = goal
				}
				plan.Stages[i].Evidence = append(plan.Stages[i].Evidence, argStringSlice(args, "evidence")...)
				found = true
				break
			}
			if !found {
				return mcp.Fail(trace(ctx), "unknown stage "+stageID), nil
			}
			plan.Revision++
			if err := plans.Save(d.Cfg.Root(), plan); err != nil {
				return mcp.Fail(trace(ctx), err.Error()), nil
			}
			if d.Metrics != nil {
				d.Metrics.Inc("plan_updates_total")
			}
			return mcp.OK(trace(ctx), map[string]any{
				"slug":     plan.Slug,
				"revision": plan.Revision,
				"stage":    stageID,
				"status":   status,
				"summary":  plans.Summary(plan),
			}), nil
		},
	))

	reg.Register(tool(
		"plan.status",
		"Read a build plan and its current stage, or the latest plan when no slug is given.",
		map[string]any{
			"slug": strProp("Optional plan name; defaults to the most recently updated plan"),
		},
		nil,
		func(ctx context.Context, args map[string]any) (any, error) {
			slug := argString(args, "slug")
			var plan plans.Plan
			var ok bool
			if slug != "" {
				plan, err := plans.Load(d.Cfg.Root(), plans.Slug(slug))
				if err != nil {
					return mcp.Fail(trace(ctx), err.Error()), nil
				}
				return mcp.OK(trace(ctx), planData(d.Cfg.Root(), plan)), nil
			}
			plan, ok = plans.Latest(d.Cfg.Root())
			if !ok {
				return mcp.Partial(trace(ctx), map[string]any{"plan": nil}, "no plan found; create one with plan.create"), nil
			}
			return mcp.OK(trace(ctx), planData(d.Cfg.Root(), plan)), nil
		},
	))
}

func planData(root string, plan plans.Plan) map[string]any {
	stage, hasStage := plans.CurrentStage(plan)
	data := map[string]any{
		"slug":     plan.Slug,
		"revision": plan.Revision,
		"task":     plan.Task,
		"stages":   plan.Stages,
		"path":     relPath(root, plans.Path(root, plan.Slug)),
		"summary":  plans.Summary(plan),
	}
	if hasStage {
		data["current_stage"] = stage
	}
	return data
}

func relPath(root, full string) string {
	if rel, err := filepath.Rel(root, full); err == nil {
		return filepath.ToSlash(rel)
	}
	return full
}
