package tools

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"hackathonkit.local/agentkit/internal/mcp"
	"hackathonkit.local/agentkit/internal/resources"
)

const (
	defaultOpenAIBaseURL = "https://api.openai.com/v1"
	defaultNVIDIABaseURL = "https://integrate.api.nvidia.com/v1"
)

func registerResources(reg *mcp.Registry, d Deps) {
	reg.Register(tool(
		"system.resources",
		"Report which external API resources the user has (OpenAI, NVIDIA, Telegram, Postgres, MinIO, Codex Pro) and the persisted answers. Call this at session start: if the profile is missing, ask the user once which resources they have and which hackathon track they build, then record the answers with system.resources_set. Raw keys never belong here - they stay in .env.",
		map[string]any{
			"task":  strProp("Optional task description to explain which variant it will use"),
			"track": strProp("Optional track reference (01-energy ... 10-retail, number or slug)"),
		},
		nil,
		func(ctx context.Context, args map[string]any) (any, error) {
			return resourcesEnvelope(ctx, d, argString(args, "task"), argString(args, "track")), nil
		},
	))

	reg.Register(tool(
		"system.resources_set",
		"Record which API resources the user has and which hackathon track they build. Pass booleans only - never pass raw keys here; the response returns the exact scripts/env-set.sh commands for keys that are claimed but missing from .env.",
		map[string]any{
			"openai_api": boolProp("User has an OpenAI API key"),
			"nvidia_api": boolProp("User has an NVIDIA API key"),
			"telegram":   boolProp("User has a Telegram bot token and chat"),
			"postgres":   boolProp("User has a Postgres DSN for the control plane"),
			"minio":      boolProp("User has MinIO credentials for artifact mirroring"),
			"codex_pro":  boolProp("User has a ChatGPT/Codex Pro subscription"),
			"track":      strProp("Track reference, for example 08 or 08-innovation"),
			"note":       strProp("Optional free-form note"),
		},
		nil,
		func(ctx context.Context, args map[string]any) (any, error) {
			root := d.Cfg.Root()
			valid := map[string]bool{"track": true, "note": true}
			for _, def := range resources.Defs() {
				valid[def.ID] = true
			}
			for key := range args {
				if !valid[key] {
					return mcp.Fail(trace(ctx), fmt.Sprintf("unknown resource %q", key)), nil
				}
			}
			err := resources.Update(root, func(profile *resources.Profile) error {
				changed := false
				for _, def := range resources.Defs() {
					raw, ok := args[def.ID]
					if !ok {
						continue
					}
					value, isBool := raw.(bool)
					if !isBool {
						return fmt.Errorf("%s must be a boolean", def.ID)
					}
					if err := resources.SetAnswer(profile, def.ID, value); err != nil {
						return err
					}
					changed = true
				}
				if track := argString(args, "track"); track != "" {
					resolved, ok := resources.LookupTrack(track)
					if !ok {
						return fmt.Errorf("unknown track %q", track)
					}
					profile.Track = resolved.ID
					changed = true
				}
				if note := argString(args, "note"); note != "" {
					profile.Note = note
					changed = true
				}
				if !changed {
					return fmt.Errorf("provide at least one provider flag, track or note")
				}
				return nil
			})
			if err != nil {
				return mcp.Fail(trace(ctx), err.Error()), nil
			}
			envelope := resourcesEnvelope(ctx, d, "", "")
			if d.Metrics != nil {
				d.Metrics.Inc("resources_profile_writes_total")
			}
			return envelope, nil
		},
	))

	reg.Register(tool(
		"system.resources_probe",
		"Check whether configured API resources are reachable. Presence-only by default; live=true makes short GET /models calls (OpenAI, NVIDIA) and getMe (Telegram). Keys are never returned or logged.",
		map[string]any{
			"provider": strProp("Optional provider id (openai_api, nvidia_api, telegram); defaults to every provider"),
			"live":     boolProp("Make live network calls with a 5s timeout (default false)"),
		},
		nil,
		func(ctx context.Context, args map[string]any) (any, error) {
			profile, _, err := resources.Load(d.Cfg.Root())
			if err != nil {
				return mcp.Fail(trace(ctx), err.Error()), nil
			}
			statuses := resources.Statuses(profile)
			selected := strings.TrimSpace(argString(args, "provider"))
			if selected != "" && !knownProvider(selected) {
				return mcp.Fail(trace(ctx), fmt.Sprintf("unknown provider %q", selected)), nil
			}
			live := argBool(args, "live", false)
			results := []any{}
			configured, reachable := 0, 0
			probeableConfigured, probeableReachable := 0, 0
			for _, status := range statuses {
				if selected != "" && status.ID != selected {
					continue
				}
				if status.Ready {
					configured++
				}
				result := probeProvider(ctx, d, status, live)
				if result["reachable"] == true {
					reachable++
				}
				// Presence-only providers never set reachable, so only compare
				// providers that actually support a live probe.
				if live && status.Ready && liveProbeSupported(status.ID) {
					probeableConfigured++
					if result["reachable"] == true {
						probeableReachable++
					}
				}
				results = append(results, result)
			}
			data := map[string]any{
				"live":    live,
				"results": results,
			}
			if d.Metrics != nil {
				d.Metrics.Inc("resources_probe_total")
			}
			incomplete := configured < len(results)
			if live && probeableReachable < probeableConfigured {
				incomplete = true
			}
			if incomplete {
				return mcp.Partial(trace(ctx), data, "some providers are not configured or not reachable").WithMetrics(map[string]any{
					"configured": configured,
					"reachable":  reachable,
				}), nil
			}
			return mcp.OK(trace(ctx), data).WithMetrics(map[string]any{
				"configured": configured,
				"reachable":  reachable,
			}), nil
		},
	))
	reg.Register(tool(
		"routing.variant",
		"Choose the optimal execution variant per capability for a hackathon track: deterministic tools when no keys are configured, OpenAI/NVIDIA-backed variants when the resource profile is ready. Degraded capabilities list the deterministic fallbacks and missing[] the keys that would upgrade them.",
		map[string]any{
			"task":         strProp("Optional task description (recorded for context)"),
			"track":        strProp("Track reference; defaults to the profile track"),
			"capabilities": arrProp("Optional capability override list", map[string]any{"type": "string"}),
		},
		nil,
		func(ctx context.Context, args map[string]any) (any, error) {
			profile, present, err := resources.Load(d.Cfg.Root())
			if err != nil {
				return mcp.Fail(trace(ctx), err.Error()), nil
			}
			statuses := resources.Statuses(profile)
			matrix, err := resources.LoadMatrix(d.Cfg.Root())
			if err != nil {
				return mcp.Fail(trace(ctx), err.Error()), nil
			}
			ref := strings.TrimSpace(argString(args, "track"))
			if ref == "" {
				ref = profile.Track
			}
			if ref == "" {
				return mcp.Partial(trace(ctx), map[string]any{
					"tracks": resources.Tracks(),
					"hint":   "pass track or record one with system.resources_set",
				}, "no track selected - pass track or set one with system.resources_set"), nil
			}
			track, decisions, err := resources.Plan(matrix, statuses, ref)
			if err != nil {
				return mcp.Fail(trace(ctx), err.Error()), nil
			}
			if override := argStringSlice(args, "capabilities"); len(override) > 0 {
				for _, capability := range override {
					if len(matrix.Capabilities[capability]) == 0 {
						return mcp.Fail(trace(ctx), fmt.Sprintf("unknown capability %q", capability)), nil
					}
				}
				plan := matrix.Tracks[track.ID]
				plan.Capabilities = override
				matrix.Tracks[track.ID] = plan
				track, decisions, err = resources.Plan(matrix, statuses, track.ID)
				if err != nil {
					return mcp.Fail(trace(ctx), err.Error()), nil
				}
			}
			data := map[string]any{
				"track":           track,
				"decisions":       decisions,
				"summary":         resources.DecisionSummary(decisions),
				"degraded":        resources.DegradedCapabilities(decisions),
				"missing":         resources.Missing(statuses),
				"profile_present": present,
				"tracks":          resources.Tracks(),
			}
			if task := argString(args, "task"); task != "" {
				data["task"] = task
			}
			degraded := resources.DegradedCapabilities(decisions)
			metrics := map[string]any{"capabilities": len(decisions), "degraded": len(degraded)}
			if len(degraded) > 0 {
				return mcp.Partial(trace(ctx), data, "capabilities on deterministic fallbacks: "+strings.Join(degraded, ", ")).WithMetrics(metrics), nil
			}
			return mcp.OK(trace(ctx), data).WithMetrics(metrics), nil
		},
	))
}

func resourcesEnvelope(ctx context.Context, d Deps, task, trackRef string) mcp.Envelope {
	profile, present, err := resources.Load(d.Cfg.Root())
	if err != nil {
		return mcp.Fail(trace(ctx), err.Error())
	}
	statuses := resources.Statuses(profile)
	data := resources.View(profile, present)
	warnings := []string{}
	if !present {
		warnings = append(warnings, "resource profile is not set - ask the user which API keys they have and record the answers with system.resources_set")
	}
	if task != "" {
		data["task"] = task
	}
	ref := strings.TrimSpace(trackRef)
	if ref == "" {
		ref = profile.Track
	}
	if ref != "" {
		if track, ok := resources.LookupTrack(ref); ok {
			data["track"] = track
			if matrix, err := resources.LoadMatrix(d.Cfg.Root()); err != nil {
				warnings = append(warnings, "resource matrix could not be loaded: "+err.Error())
			} else if plan, decisions, err := resources.Plan(matrix, statuses, track.ID); err != nil {
				warnings = append(warnings, "variant plan unavailable: "+err.Error())
			} else {
				data["track_plan"] = plan
				data["variants"] = decisions
				data["variant_summary"] = resources.DecisionSummary(decisions)
			}
		} else {
			warnings = append(warnings, fmt.Sprintf("unknown track %q", ref))
		}
	}
	metrics := map[string]any{"providers_ready": countReady(statuses)}
	if !present || len(resources.Missing(statuses)) > 0 {
		if len(warnings) == 0 {
			warnings = append(warnings, "resource profile is incomplete; some providers are not configured")
		}
		return mcp.Partial(trace(ctx), data, warnings...).WithMetrics(metrics)
	}
	envelope := mcp.OK(trace(ctx), data).WithMetrics(metrics)
	if len(warnings) > 0 {
		envelope = envelope.WithWarnings(warnings...)
	}
	return envelope
}

func probeProvider(ctx context.Context, d Deps, status resources.Status, live bool) map[string]any {
	result := map[string]any{
		"provider":   status.ID,
		"configured": status.Ready,
		"live":       live,
		"reachable":  false,
	}
	if !live {
		return result
	}
	switch status.ID {
	case "openai_api", "nvidia_api":
		base := os.Getenv("OPENAI_BASE_URL")
		if status.ID == "nvidia_api" {
			base = os.Getenv("NVIDIA_BASE_URL")
		}
		if strings.TrimSpace(base) == "" {
			if status.ID == "nvidia_api" {
				base = defaultNVIDIABaseURL
			} else {
				base = defaultOpenAIBaseURL
			}
		}
		apiKey := ""
		if len(status.EnvKeys) > 0 {
			apiKey = os.Getenv(status.EnvKeys[0])
		}
		probe := resources.ProbeOpenAICompatible(ctx, status.ID, base, apiKey)
		return map[string]any{
			"provider":    probe.Provider,
			"configured":  probe.Configured,
			"live":        probe.Live,
			"reachable":   probe.Reachable,
			"status_code": probe.StatusCode,
			"latency_ms":  probe.LatencyMS,
			"models":      probe.Models,
			"error":       probe.Error,
		}
	case "telegram":
		if d.Telegram == nil || !d.Telegram.Configured() {
			result["error"] = "not configured"
			return result
		}
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		bot, err := d.Telegram.GetMe(probeCtx)
		if err != nil {
			result["error"] = err.Error()
			return result
		}
		result["reachable"] = true
		result["bot"] = bot
		return result
	default:
		result["note"] = "presence-only provider; no live probe"
		return result
	}
}

// liveProbeSupported reports whether probeProvider performs a live network
// check for the provider; presence-only providers must not count as
// unreachable.
func liveProbeSupported(id string) bool {
	switch id {
	case "openai_api", "nvidia_api", "telegram":
		return true
	}
	return false
}

func knownProvider(id string) bool {
	for _, def := range resources.Defs() {
		if def.ID == id {
			return true
		}
	}
	return false
}

func countReady(statuses []resources.Status) int {
	count := 0
	for _, status := range statuses {
		if status.Ready {
			count++
		}
	}
	return count
}
