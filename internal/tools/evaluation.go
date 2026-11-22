package tools

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"hackathonkit.local/agentkit/internal/atomicfile"
	"hackathonkit.local/agentkit/internal/evaluation"
	"hackathonkit.local/agentkit/internal/mcp"
	"hackathonkit.local/agentkit/internal/metadata"
	"hackathonkit.local/agentkit/internal/storage"
)

func registerEvaluation(reg *mcp.Registry, d Deps) {
	reg.Register(tool(
		"eval.aggregate",
		"Aggregate judge reports deterministically: validate schemas, drop conflicted or invalid reports (a missing report is never zero), require the configured minimum valid reports (min_valid_judges, default 3 per regulation 9.5), then compute the unrounded mean, sample variance, high-disagreement criteria, common deductions and likely questions. The judges stay LLM-driven; this tool owns the arithmetic.",
		map[string]any{
			"reports":      arrProp("Judge report file paths relative to the repository root", map[string]any{"type": "string"}),
			"reports_json": arrProp("Inline judge report objects, an alternative to reports", objProp("Judge report")),
			"snapshot_sha": strProp("Expected snapshot sha; reports carrying another sha are dropped"),
			"kind":         strProp("Artifact directory name under evals/ (default judges)"),
			"persist":      boolProp("Write evals/<kind>/<sha>/aggregate.json (default true)"),
		},
		nil,
		func(ctx context.Context, args map[string]any) (any, error) {
			reports, loadInvalids, err := readReports(d.Cfg.Root(), args, "reports", "reports_json")
			if err != nil {
				return mcp.Fail(trace(ctx), err.Error()), nil
			}
			minValid := d.Cfg.Evaluation().MinValidJudges
			agg := aggregateLoaded(reports, loadInvalids, argString(args, "snapshot_sha"), minValid)
			if d.Metrics != nil {
				d.Metrics.Inc("eval_aggregate_total")
				if dropped := len(agg.DroppedInvalid) + len(agg.DroppedConflict); dropped > 0 {
					d.Metrics.Add("eval_reports_dropped_total", int64(dropped))
				}
			}
			data := aggregateData(agg)

			kind := argString(args, "kind")
			if kind == "" {
				kind = "judges"
			}
			if argBool(args, "persist", true) && d.Store != nil {
				rel := filepath.ToSlash(filepath.Join("evals", pathSegment(kind), uniqueSegment(agg.SnapshotSHA), "aggregate.json"))
				if path, werr := writeRepoJSON(d.Cfg.Root(), rel, agg); werr != nil {
					data["persist_error"] = werr.Error()
				} else {
					data["aggregate_path"] = path
				}
			}

			if d.Meta != nil && d.Meta.Enabled() {
				_ = d.Meta.RecordEvalRun(ctx, metadata.EvalRun{
					Kind:        "aggregate:" + kind,
					SnapshotSHA: agg.SnapshotSHA,
					Score:       agg.MeanTotal,
					Payload:     agg,
				})
			}

			var env mcp.Envelope
			if agg.Inconclusive {
				env = mcp.Partial(trace(ctx), data, fmt.Sprintf("fewer than %d valid judge reports; assign a substitute judge on the same frozen bundle and re-aggregate", agg.MinValid))
			} else {
				env = mcp.OK(trace(ctx), data)
			}
			if len(agg.DroppedInvalid) > 0 {
				env = env.WithWarnings(fmt.Sprintf("%d report(s) dropped as invalid", len(agg.DroppedInvalid)))
			}
			if len(agg.DroppedConflict) > 0 {
				env = env.WithWarnings(fmt.Sprintf("%d report(s) dropped for conflict of interest", len(agg.DroppedConflict)))
			}
			if msg, ok := data["persist_error"]; ok {
				env = env.WithWarnings(fmt.Sprintf("aggregate not persisted: %v", msg))
			}
			return env.WithMetrics(map[string]any{
				"valid_judges":     agg.ValidJudges,
				"dropped_invalid":  len(agg.DroppedInvalid),
				"dropped_conflict": len(agg.DroppedConflict),
				"mean_total":       agg.MeanTotal,
				"rubric_version":   evaluation.RubricVersion,
			}), nil
		},
	))

	reg.Register(tool(
		"eval.calibrate",
		"Test-retest calibration: aggregate the same frozen snapshot scored twice, compute retest_delta and noise_floor = max(min_delta, 2 * retest_delta). Every judge-driven stop condition must use this noise floor.",
		map[string]any{
			"run1":         arrProp("First run judge report paths", map[string]any{"type": "string"}),
			"run2":         arrProp("Second run judge report paths", map[string]any{"type": "string"}),
			"run1_json":    arrProp("First run inline judge reports", objProp("Judge report")),
			"run2_json":    arrProp("Second run inline judge reports", objProp("Judge report")),
			"snapshot_sha": strProp("Expected snapshot sha for both runs"),
			"min_delta":    numProp("Configured minimum delta (default from configs/evaluation.yaml)"),
			"persist":      boolProp("Write evals/calibration/<sha>/calibration.json (default true)"),
		},
		nil,
		func(ctx context.Context, args map[string]any) (any, error) {
			root := d.Cfg.Root()
			run1, invalid1, err := readReports(root, args, "run1", "run1_json")
			if err != nil {
				return mcp.Fail(trace(ctx), "run1: "+err.Error()), nil
			}
			run2, invalid2, err := readReports(root, args, "run2", "run2_json")
			if err != nil {
				return mcp.Fail(trace(ctx), "run2: "+err.Error()), nil
			}
			sha := argString(args, "snapshot_sha")
			minValid := d.Cfg.Evaluation().MinValidJudges
			agg1 := aggregateLoaded(run1, invalid1, sha, minValid)
			agg2 := aggregateLoaded(run2, invalid2, sha, minValid)

			settings := d.Cfg.Evaluation()
			minDelta := argFloat(args, "min_delta", -1)
			if minDelta < 0 {
				minDelta = settings.MinDelta
			}
			cal, err := evaluation.Calibrate(agg1, agg2, minDelta, minValid)
			if err != nil {
				return mcp.Fail(trace(ctx), err.Error()), nil
			}
			if d.Metrics != nil {
				d.Metrics.Inc("eval_calibrations_total")
			}
			data := map[string]any{
				"calibration": cal,
				"run1":        aggregateData(agg1),
				"run2":        aggregateData(agg2),
			}
			if argBool(args, "persist", true) && d.Store != nil {
				rel := filepath.ToSlash(filepath.Join("evals", "calibration", uniqueSegment(cal.SnapshotSHA), "calibration.json"))
				if path, werr := writeRepoJSON(root, rel, cal); werr != nil {
					data["persist_error"] = werr.Error()
				} else {
					data["calibration_path"] = path
				}
			}
			if d.Meta != nil && d.Meta.Enabled() {
				_ = d.Meta.RecordEvalRun(ctx, metadata.EvalRun{
					Kind:        "calibration",
					SnapshotSHA: cal.SnapshotSHA,
					Score:       cal.RetestDelta,
					NoiseFloor:  cal.NoiseFloor,
					Decision:    "calibrated",
					Payload:     cal,
				})
			}
			env := mcp.OK(trace(ctx), data)
			if msg, ok := data["persist_error"]; ok {
				env = env.WithWarnings(fmt.Sprintf("calibration not persisted: %v", msg))
			}
			return env.WithMetrics(map[string]any{
				"retest_delta":   cal.RetestDelta,
				"noise_floor":    cal.NoiseFloor,
				"run1_judges":    cal.Run1Judges,
				"run2_judges":    cal.Run2Judges,
				"rubric_version": evaluation.RubricVersion,
			}), nil
		},
	))

	reg.Register(tool(
		"eval.compare",
		"Apply the self-improvement loop rules to one candidate and return a deterministic decision: target_reached, accept, rollback or stop. Codex performs the rollback; this tool owns the stop condition.",
		map[string]any{
			"mode":                      strProp("technical (default) or judge"),
			"candidate":                 numProp("New candidate score or judge mean"),
			"best":                      numProp("Previous accepted score (default 0 for the first candidate)"),
			"target":                    numProp("Override the target from configs/evaluation.yaml"),
			"noise_floor":               numProp("Override the configured min_delta / calibrated noise floor"),
			"mandatory_scenario_failed": boolProp("Set when the mandatory demo scenario regressed"),
			"snapshot_sha":              strProp("Snapshot sha for the decision record"),
			"persist":                   boolProp("Write evals/decisions/<sha>/<mode>-<ts>.json (default false)"),
		},
		[]string{"candidate"},
		func(ctx context.Context, args map[string]any) (any, error) {
			candidate := argFloat(args, "candidate", math.NaN())
			if math.IsNaN(candidate) {
				return mcp.Fail(trace(ctx), "candidate is required"), nil
			}
			settings := d.Cfg.Evaluation()
			mode := evaluation.Mode(strings.ToLower(argString(args, "mode")))
			if mode != evaluation.ModeJudge {
				mode = evaluation.ModeTechnical
			}
			target := argFloat(args, "target", -1)
			if target < 0 {
				if mode == evaluation.ModeJudge {
					target = settings.DemoDayTarget
				} else {
					target = settings.TechnicalTarget
				}
			}
			noiseFloor := argFloat(args, "noise_floor", -1)
			if noiseFloor < 0 {
				noiseFloor = settings.MinDelta
			}
			comparison := evaluation.Compare(evaluation.CompareInput{
				Mode:                    mode,
				Candidate:               candidate,
				Best:                    argFloat(args, "best", 0),
				Target:                  target,
				NoiseFloor:              noiseFloor,
				MandatoryScenarioFailed: argBool(args, "mandatory_scenario_failed", false),
			})
			if d.Metrics != nil {
				d.Metrics.Inc("eval_decisions_total")
				d.Metrics.Inc("eval_decision_" + string(comparison.Decision) + "_total")
			}
			data := map[string]any{
				"decision":                  comparison.Decision,
				"reason":                    comparison.Reason,
				"mode":                      comparison.Mode,
				"candidate":                 comparison.Candidate,
				"best":                      comparison.Best,
				"target":                    comparison.Target,
				"noise_floor":               comparison.NoiseFloor,
				"improvement":               comparison.Improvement,
				"mandatory_scenario_failed": comparison.MandatoryFailed,
			}
			if argBool(args, "persist", false) && d.Store != nil {
				created := time.Now().UTC()
				stamp := historyStamp(created)
				rel := filepath.ToSlash(filepath.Join("evals", "decisions", uniqueSegment(argString(args, "snapshot_sha")),
					fmt.Sprintf("%s-%s.json", mode, stamp)))
				record := struct {
					evaluation.Comparison
					TS time.Time `json:"ts"`
				}{Comparison: comparison, TS: created}
				if path, werr := writeRepoJSON(d.Cfg.Root(), rel, record); werr != nil {
					data["persist_error"] = werr.Error()
				} else {
					data["decision_path"] = path
				}
			}
			if d.Meta != nil && d.Meta.Enabled() {
				_ = d.Meta.RecordEvalRun(ctx, metadata.EvalRun{
					Kind:        "decision:" + string(mode),
					SnapshotSHA: argString(args, "snapshot_sha"),
					Score:       comparison.Candidate,
					Target:      comparison.Target,
					NoiseFloor:  comparison.NoiseFloor,
					Decision:    string(comparison.Decision),
					Payload:     comparison,
				})
			}
			env := mcp.OK(trace(ctx), data)
			switch comparison.Decision {
			case evaluation.DecisionRollback, evaluation.DecisionStop:
				env = env.WithWarnings(comparison.Reason)
			}
			if msg, ok := data["persist_error"]; ok {
				env = env.WithWarnings(fmt.Sprintf("decision not persisted: %v", msg))
			}
			return env.WithMetrics(map[string]any{
				"decision":       string(comparison.Decision),
				"improvement":    comparison.Improvement,
				"noise_floor":    comparison.NoiseFloor,
				"rubric_version": evaluation.RubricVersion,
			}), nil
		},
	))

	reg.Register(tool(
		"eval.freeze_bundle",
		"Freeze a read-only snapshot of the repository for judge runs: copies the tree, writes git-sha.txt, rubric-version.txt, bundle.json and a deterministic bundle hash. Mutable state (logs, data, evals, secrets) is excluded.",
		map[string]any{
			"out":       strProp("Bundle directory relative to the repository root (default data/judge-bundles)"),
			"exclude":   arrProp("Extra exclude patterns: repository-relative paths, base names or globs (for example .env*, secrets/*)", map[string]any{"type": "string"}),
			"read_only": boolProp("Mark copied files read-only (default true)"),
			"model":     strProp("Model id recorded in the bundle manifest"),
			"profile":   strProp("full (default), technical (alias) or demo (presentation evidence only: TASK, README, architecture, demo artifacts, limitations)"),
		},
		nil,
		func(ctx context.Context, args map[string]any) (any, error) {
			root := d.Cfg.Root()
			out := argString(args, "out")
			if out == "" {
				out = "data/judge-bundles"
			}
			outAbs, err := resolveRepoPath(root, out)
			if err != nil {
				return mcp.Fail(trace(ctx), err.Error()), nil
			}
			bundle, err := evaluation.FreezeBundle(evaluation.BundleOptions{
				Root:          root,
				OutDir:        outAbs,
				Exclude:       argStringSlice(args, "exclude"),
				ReadOnly:      argBool(args, "read_only", true),
				Model:         argString(args, "model"),
				RubricVersion: evaluation.RubricVersion,
				Profile:       argString(args, "profile"),
			})
			if err != nil {
				return mcp.Fail(trace(ctx), err.Error()), nil
			}
			if d.Metrics != nil {
				d.Metrics.Inc("eval_bundles_total")
			}
			rel, relErr := filepath.Rel(root, bundle.Dir)
			if relErr != nil {
				rel = bundle.Dir
			}
			data := map[string]any{
				"bundle_path":    filepath.ToSlash(rel),
				"bundle_hash":    bundle.BundleHash,
				"git_sha":        bundle.GitSHA,
				"model":          bundle.Model,
				"rubric_version": bundle.RubricVersion,
				"profile":        bundle.Profile,
				"files":          bundle.Files,
				"bytes":          bundle.Bytes,
				"excluded":       bundle.Excluded,
			}
			if d.Meta != nil && d.Meta.Enabled() {
				_ = d.Meta.RecordEvalRun(ctx, metadata.EvalRun{
					Kind:        "bundle",
					SnapshotSHA: bundle.GitSHA,
					Payload:     bundle,
				})
			}
			env := mcp.OK(trace(ctx), data)
			if bundle.GitSHA == "" {
				env = env.WithWarnings("git sha not resolved; the bundle is not tied to a commit")
			}
			return env.WithMetrics(map[string]any{
				"files":       bundle.Files,
				"bytes":       bundle.Bytes,
				"bundle_hash": bundle.BundleHash,
			}), nil
		},
	))

	reg.Register(tool(
		"eval.rank",
		"Rank two or more judge-run aggregates with the regulation 9.6 rules: unrounded mean total first, then result_quality, value and presentation_demo_qa, then a recorded three-member commission vote. Persists the ranking under evals/rankings/.",
		map[string]any{
			"aggregates":      arrProp("Paths to aggregate.json files relative to the repository root", map[string]any{"type": "string"}),
			"aggregates_json": arrProp("Inline aggregate objects", objProp("Aggregate")),
			"commission":      objProp("Optional tie-break commission: {members:[3+ non-conflicted judges], votes:{sha:count}, grounds}"),
			"persist":         boolProp("Write evals/rankings/<ts>.json (default true)"),
		},
		nil,
		func(ctx context.Context, args map[string]any) (any, error) {
			aggregates, err := readAggregates(d.Cfg.Root(), args)
			if err != nil {
				return mcp.Fail(trace(ctx), err.Error()), nil
			}
			ranking, err := evaluation.Rank(aggregates)
			if err != nil {
				return mcp.Fail(trace(ctx), err.Error()), nil
			}
			if raw := argMap(args, "commission"); raw != nil {
				commission := evaluation.Commission{
					Members: argStringSlice(raw, "members"),
					Grounds: argString(raw, "grounds"),
					Votes:   map[string]int{},
				}
				if votes, ok := raw["votes"].(map[string]any); ok {
					for sha, value := range votes {
						fv, isNum := value.(float64)
						// Bound the value before the int conversion: Go's
						// float->int conversion is undefined out of range.
						if !isNum || math.IsNaN(fv) || math.IsInf(fv, 0) || fv < 0 || fv > math.MaxInt32 || fv != math.Trunc(fv) {
							return mcp.Fail(trace(ctx), fmt.Sprintf("vote for %s must be a non-negative integer", sha)), nil
						}
						commission.Votes[sha] = int(fv)
					}
				}
				ranking, err = evaluation.ApplyCommission(ranking, commission)
				if err != nil {
					return mcp.Fail(trace(ctx), err.Error()), nil
				}
			}
			if d.Metrics != nil {
				d.Metrics.Inc("eval_rankings_total")
			}
			data := map[string]any{
				"ranking":        ranking,
				"rubric_version": evaluation.RubricVersion,
			}
			if argBool(args, "persist", true) && d.Store != nil {
				stamp := historyStamp(time.Now().UTC())
				rel := filepath.ToSlash(filepath.Join("evals", "rankings", "ranking-"+stamp+".json"))
				if path, werr := writeRepoJSON(d.Cfg.Root(), rel, ranking); werr != nil {
					data["persist_error"] = werr.Error()
				} else {
					data["ranking_path"] = path
				}
			}
			env := mcp.OK(trace(ctx), data)
			if len(ranking.Unresolved) > 0 {
				env = env.WithWarnings("unresolved tie: record a three-member non-conflicted commission vote via the commission argument")
			}
			if msg, ok := data["persist_error"]; ok {
				env = env.WithWarnings(fmt.Sprintf("ranking not persisted: %v", msg))
			}
			return env.WithMetrics(map[string]any{
				"entries":        len(ranking.Entries),
				"unresolved":     len(ranking.Unresolved),
				"rubric_version": evaluation.RubricVersion,
			}), nil
		},
	))
}

// readAggregates loads aggregates from file paths and/or inline JSON.
func readAggregates(root string, args map[string]any) ([]evaluation.Aggregate, error) {
	aggregates := []evaluation.Aggregate{}
	if raw, ok := args["aggregates_json"].([]any); ok {
		for _, item := range raw {
			data, err := json.Marshal(item)
			if err != nil {
				return nil, fmt.Errorf("cannot marshal inline aggregate: %w", err)
			}
			var aggregate evaluation.Aggregate
			if err := json.Unmarshal(data, &aggregate); err != nil {
				return nil, fmt.Errorf("cannot parse inline aggregate: %w", err)
			}
			aggregates = append(aggregates, aggregate)
		}
	}
	for _, rel := range argStringSlice(args, "aggregates") {
		full, err := resolveRepoPath(root, rel)
		if err != nil {
			return nil, err
		}
		data, err := os.ReadFile(full)
		if err != nil {
			return nil, fmt.Errorf("cannot read aggregate %s: %w", rel, err)
		}
		var aggregate evaluation.Aggregate
		if err := json.Unmarshal(data, &aggregate); err != nil {
			return nil, fmt.Errorf("cannot parse aggregate %s: %w", rel, err)
		}
		aggregates = append(aggregates, aggregate)
	}
	if len(aggregates) == 0 {
		return nil, fmt.Errorf("aggregates or aggregates_json is required")
	}
	return aggregates, nil
}

// aggregateLoaded maps validation indexes from the loaded reports back to
// their original positions among inline and file inputs.
func aggregateLoaded(reports []evaluation.JudgeReport, invalid []evaluation.InvalidReport, sha string, minValid int) evaluation.Aggregate {
	agg := evaluation.AggregateReports(reports, sha, minValid)
	if len(invalid) == 0 {
		return agg
	}
	positions := make([]int, 0, len(reports))
	for i, skip := 0, 0; i < len(reports)+len(invalid); i++ {
		if skip < len(invalid) && invalid[skip].Index == i {
			skip++
			continue
		}
		positions = append(positions, i)
	}
	for i, entry := range agg.DroppedInvalid {
		agg.DroppedInvalid[i].Index = positions[entry.Index]
	}
	agg.DroppedInvalid = append(invalid, agg.DroppedInvalid...)
	return agg
}

func aggregateData(agg evaluation.Aggregate) map[string]any {
	return map[string]any{
		"snapshot_sha":            agg.SnapshotSHA,
		"rubric_version":          agg.RubricVersion,
		"min_valid_judges":        agg.MinValid,
		"valid_judges":            agg.ValidJudges,
		"inconclusive":            agg.Inconclusive,
		"mean_total":              agg.MeanTotal,
		"mean_total_rounded":      agg.MeanTotalRounded,
		"criterion_means":         agg.CriterionMeans,
		"criterion_means_rounded": agg.CriterionMeansRounded,
		"score_variance":          agg.ScoreVariance,
		"high_disagreement":       agg.HighDisagreement,
		"common_deductions":       agg.CommonDeductions,
		"likely_questions":        agg.LikelyQuestions,
		"top_improvements":        agg.TopImprovements,
		"dropped_invalid":         agg.DroppedInvalid,
		"dropped_conflict":        agg.DroppedConflict,
	}
}

// readReports loads judge reports from file paths and/or inline JSON. Failed
// files become dropped-invalid entries instead of aborting the run.
func readReports(root string, args map[string]any, pathsKey, jsonKey string) ([]evaluation.JudgeReport, []evaluation.InvalidReport, error) {
	reports := []evaluation.JudgeReport{}
	invalids := []evaluation.InvalidReport{}
	// Use one monotonically increasing index across both input kinds so an
	// inline entry and a file entry can never share the same Index.
	index := 0
	if raw, ok := args[jsonKey].([]any); ok {
		for _, item := range raw {
			i := index
			index++
			data, err := json.Marshal(item)
			if err != nil {
				invalids = append(invalids, evaluation.InvalidReport{Index: i, Reasons: []string{"cannot marshal inline report: " + err.Error()}})
				continue
			}
			var report evaluation.JudgeReport
			if err := json.Unmarshal(data, &report); err != nil {
				invalids = append(invalids, evaluation.InvalidReport{Index: i, Reasons: []string{"cannot parse inline report: " + err.Error()}})
				continue
			}
			reports = append(reports, report)
		}
	}
	for _, rel := range argStringSlice(args, pathsKey) {
		i := index
		index++
		full, err := resolveRepoPath(root, rel)
		if err != nil {
			// Kept as a dropped-invalid entry (the specific reason, e.g.
			// "path escapes repository root", is preserved in reasons).
			invalids = append(invalids, evaluation.InvalidReport{Index: i, Path: rel, Reasons: []string{err.Error()}})
			continue
		}
		data, err := os.ReadFile(full)
		if err != nil {
			invalids = append(invalids, evaluation.InvalidReport{Index: i, Path: rel, Reasons: []string{"cannot read report: " + err.Error()}})
			continue
		}
		var report evaluation.JudgeReport
		if err := json.Unmarshal(data, &report); err != nil {
			invalids = append(invalids, evaluation.InvalidReport{Index: i, Path: rel, Reasons: []string{"cannot parse report: " + err.Error()}})
			continue
		}
		reports = append(reports, report)
	}
	if len(reports) == 0 && len(invalids) == 0 {
		return nil, nil, fmt.Errorf("%s or %s is required", pathsKey, jsonKey)
	}
	return reports, invalids, nil
}

// resolveRepoPath resolves a repository-relative path and rejects absolute
// paths and traversal outside the root.
func resolveRepoPath(root, rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", fmt.Errorf("empty path")
	}
	rel = strings.ReplaceAll(rel, "\\", "/")
	if strings.HasPrefix(rel, "/") || (len(rel) > 1 && rel[1] == ':') {
		return "", fmt.Errorf("absolute paths are not allowed: %s", rel)
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes repository root: %s", rel)
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	// Fail closed: if the root cannot be resolved the containment check is
	// unverifiable, so reject the path instead of accepting it unchecked.
	resolvedRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", fmt.Errorf("cannot resolve repository root: %w", err)
	}
	resolved, err := storage.ResolveWithin(resolvedRoot, clean)
	if err != nil {
		return "", fmt.Errorf("cannot resolve repository path %s: %w", rel, err)
	}
	return resolved, nil
}

// Preserve chronological filename ordering while keeping simultaneous records distinct.
func historyStamp(created time.Time) string {
	return created.Format("20060102-150405.000000000") + "-" + rand.Text()
}

func writeRepoJSON(root, rel string, value any) (string, error) {
	full, err := resolveRepoPath(root, rel)
	if err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", err
	}
	if err := atomicfile.Write(full, append(data, '\n'), atomicfile.ModeState); err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

var pathSegmentUnsafe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// uniqueSegment avoids collapsing every sha-less artifact onto "unknown".
func uniqueSegment(value string) string {
	if strings.TrimSpace(value) != "" {
		return pathSegment(value)
	}
	return "unknown-" + time.Now().UTC().Format("20060102T150405.000000000")
}

func pathSegment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	value = pathSegmentUnsafe.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-.")
	if value == "" {
		return "unknown"
	}
	if len(value) > 64 {
		// Keep a short hash of the full value so distinct inputs that share a
		// long prefix do not collapse onto the same path segment.
		suffix := "-" + shortHash(value)
		keep := 64 - len(suffix)
		if keep < 1 {
			keep = 1
		}
		value = value[:keep] + suffix
	}
	return value
}
