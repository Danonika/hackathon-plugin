package tools

import (
	"context"
	"fmt"
	"os"
	"strings"

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
			profile, _, err := resources.Load(root)
			if err != nil {
				return mcp.Fail(trace(ctx), err.Error()), nil
			}
			changed := false
			for _, def := range resources.Defs() {
				raw, ok := args[def.ID]
				if !ok {
					continue
				}
				value, _ := raw.(bool)
				if err := resources.SetAnswer(&profile, def.ID, value); err != nil {
					return mcp.Fail(trace(ctx), err.Error()), nil
				}
				changed = true
			}
			if track := argString(args, "track"); track != "" {
				resolved, ok := resources.LookupTrack(track)
				if !ok {
					return mcp.Fail(trace(ctx), fmt.Sprintf("unknown track %q", track)), nil
				}
				profile.Track = resolved.ID
				changed = true
			}
			if note := argString(args, "note"); note != "" {
				profile.Note = note
				changed = true
			}
			if !changed {
				return mcp.Fail(trace(ctx), "provide at least one provider flag, track or note"), nil
			}
			if err := resources.Save(root, profile); err != nil {
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
			if live {
				incomplete = reachable < configured
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
		} else {
			warnings = append(warnings, fmt.Sprintf("unknown track %q", ref))
		}
	}
	metrics := map[string]any{"providers_ready": countReady(statuses)}
	if !present || len(resources.Missing(statuses)) > 0 {
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
		probe := resources.ProbeOpenAICompatible(ctx, status.ID, base, os.Getenv(status.EnvKeys[0]))
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
		bot, err := d.Telegram.GetMe(ctx)
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
