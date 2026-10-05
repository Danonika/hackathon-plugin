package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"hackathonkit.local/agentkit/internal/mcp"
	"hackathonkit.local/agentkit/internal/plans"
)

// normalizePlanSlug validates and normalizes a raw plan slug. The reserved
// fallback name "plan" is only accepted when the caller asked for it
// explicitly, so a bogus slug cannot silently address evals/plans/plan.json.
func normalizePlanSlug(rawSlug string) (string, error) {
	raw := strings.TrimSpace(rawSlug)
	if raw == "" {
		return "", fmt.Errorf("slug is required")
	}
	slug := plans.Slug(raw)
	if slug == "plan" && !strings.EqualFold(raw, "plan") {
		return "", fmt.Errorf("slug %q normalizes to the reserved name %q; use letters or digits", rawSlug, "plan")
	}
	if !containsLetterOrDigit(slug) {
		return "", fmt.Errorf("slug must contain at least one letter or digit")
	}
	return slug, nil
}

func containsLetterOrDigit(value string) bool {
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

func registerPlans(reg *mcp.Registry, d Deps) {
	reg.Register(tool(
		"plan.create",
		"Create a staged build plan (S0 outcome .. S6 freeze) stored under evals/plans/. The plan's mandatory scenario and stage gates drive the improvement loops.",
		map[string]any{
			"slug":               strProp("Short plan name, e.g. telemetry-anomalies"),
			"overwrite":          boolProp("Replace an existing plan with this slug (default false)"),
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
			slug, slugErr := normalizePlanSlug(argString(args, "slug"))
			if slugErr != nil {
				return mcp.Fail(trace(ctx), slugErr.Error()), nil
			}
			if !argBool(args, "overwrite", false) {
				existing, err := plans.Load(d.Cfg.Root(), slug)
				if err == nil && existing.Slug != "" {
					return mcp.Fail(trace(ctx), fmt.Sprintf("plan %q already exists (revision %d); use plan.update or pass overwrite=true", slug, existing.Revision)), nil
				}
				if err != nil && !os.IsNotExist(err) {
					return mcp.Fail(trace(ctx), "cannot read existing plan: "+err.Error()), nil
				}
			}
			warnings := []string{}
			stages := []plans.Stage{}
			seenStage := map[string]bool{}
			if rawValue, present := args["stages"]; present && rawValue != nil {
				if _, isArray := rawValue.([]any); !isArray {
					warnings = append(warnings, "stages must be an array; default stages were used")
				}
			}
			if raw, ok := args["stages"].([]any); ok && len(raw) > 0 {
				skipped := 0
				for _, item := range raw {
					m, ok := item.(map[string]any)
					if !ok {
						skipped++
						continue
					}
					id, _ := m["id"].(string)
					if strings.TrimSpace(id) == "" {
						skipped++
						continue
					}
					goal, _ := m["goal"].(string)
					status, _ := m["status"].(string)
					status = plans.NormalizeStatus(status)
					if status == "" {
						status = "todo"
					}
					if !plans.ValidStatus(status) {
						return mcp.Fail(trace(ctx), fmt.Sprintf("invalid stage status %q", status)), nil
					}
					timebox := argInt(m, "timebox_minutes", 0)
					if timebox < 0 {
						return mcp.Fail(trace(ctx), "timebox_minutes must be >= 0"), nil
					}
					cleanID := strings.TrimSpace(id)
					if seenStage[cleanID] {
						return mcp.Fail(trace(ctx), "duplicate stage id: "+cleanID), nil
					}
					seenStage[cleanID] = true
					stages = append(stages, plans.Stage{ID: cleanID, Goal: goal, Status: status, TimeboxMinute: timebox})
				}
				if skipped > 0 {
					warnings = append(warnings, fmt.Sprintf("skipped %d malformed stage(s)", skipped))
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
			envelope := mcp.OK(trace(ctx), map[string]any{
				"slug":     plan.Slug,
				"revision": plan.Revision,
				"path":     relPath(d.Cfg.Root(), plans.Path(d.Cfg.Root(), plan.Slug)),
				"stages":   plan.Stages,
				"summary":  plans.Summary(plan),
			})
			if len(warnings) > 0 {
				envelope = envelope.WithWarnings(warnings...)
			}
			return envelope, nil
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
			slug, slugErr := normalizePlanSlug(argString(args, "slug"))
			stageID := argString(args, "stage")
			status := plans.NormalizeStatus(argString(args, "status"))
			if slugErr != nil {
				return mcp.Fail(trace(ctx), slugErr.Error()), nil
			}
			if stageID == "" || status == "" {
				return mcp.Fail(trace(ctx), "stage and status are required"), nil
			}
			if !plans.ValidStatus(status) {
				return mcp.Fail(trace(ctx), fmt.Sprintf("invalid status %q", status)), nil
			}
			plan, err := plans.Load(d.Cfg.Root(), slug)
			if err != nil {
				return mcp.Fail(trace(ctx), err.Error()), nil
			}
			// Never let the slug stored inside the file redirect the write to a
			// different plan than the one just read.
			plan.Slug = slug
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
				normalized, slugErr := normalizePlanSlug(slug)
				if slugErr != nil {
					return mcp.Fail(trace(ctx), slugErr.Error()), nil
				}
				plan, err := plans.Load(d.Cfg.Root(), normalized)
				if err != nil {
					return mcp.Fail(trace(ctx), err.Error()), nil
				}
				return mcp.OK(trace(ctx), planData(d.Cfg.Root(), plan)), nil
			}
			var latestErr error
			plan, ok, latestErr = plans.LatestErr(d.Cfg.Root())
			if !ok {
				if latestErr != nil {
					return mcp.Partial(trace(ctx), map[string]any{"plan": nil}, "latest plan unreadable: "+latestErr.Error()), nil
				}
				return mcp.Partial(trace(ctx), map[string]any{"plan": nil}, "no plan found; create one with plan.create"), nil
			}
			env := mcp.OK(trace(ctx), planData(d.Cfg.Root(), plan))
			if latestErr != nil {
				// The latest readable plan is returned, but a sibling file was
				// unreadable/unparseable: surface that instead of hiding it.
				env = env.WithWarnings("some plan files could not be read: " + latestErr.Error())
			}
			return env, nil
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
