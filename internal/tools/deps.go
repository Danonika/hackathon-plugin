package tools

import (
	"context"
	"math"
	"strconv"
	"strings"

	"hackathonkit.local/agentkit/internal/config"
	"hackathonkit.local/agentkit/internal/learning"
	"hackathonkit.local/agentkit/internal/mcp"
	"hackathonkit.local/agentkit/internal/metadata"
	"hackathonkit.local/agentkit/internal/observability"
	"hackathonkit.local/agentkit/internal/openai"
	"hackathonkit.local/agentkit/internal/routing/fly"
	"hackathonkit.local/agentkit/internal/storage"
	"hackathonkit.local/agentkit/internal/telegram"
)

type Deps struct {
	Cfg      *config.Manager
	Store    *storage.Store
	OpenAI   *openai.Client
	Metrics  *observability.Metrics
	Router   *fly.Router
	Learning *learning.Store
	Telegram *telegram.Client
	Meta     metadata.Recorder
}

func RegisterAll(reg *mcp.Registry, d Deps) {
	registerSystem(reg, d)
	registerResources(reg, d)
	registerEvaluation(reg, d)
	registerLearning(reg, d)
	registerPlans(reg, d)
	registerQuality(reg, d)
	registerSecurity(reg, d)
	registerTelegram(reg, d)
	registerContextBudget(reg, d)
	registerFiles(reg, d)
	registerDocuments(reg, d)
	registerKnowledge(reg, d)
	registerData(reg, d)
	registerTimeSeries(reg, d)
	registerExtras(reg, d)
}

func trace(ctx context.Context) string {
	return observability.TraceFrom(ctx)
}

func tool(name, description string, props map[string]any, required []string, handler mcp.Handler) mcp.Tool {
	schema := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		schema["required"] = required
	}
	return mcp.Tool{Name: name, Description: description, InputSchema: schema, Handler: handler}
}

func strProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

func numProp(desc string) map[string]any {
	return map[string]any{"type": "number", "description": desc}
}

func boolProp(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}

func objProp(desc string) map[string]any {
	return map[string]any{"type": "object", "description": desc}
}

func arrProp(desc string, items map[string]any) map[string]any {
	return map[string]any{"type": "array", "description": desc, "items": items}
}

func argString(args map[string]any, key string) string {
	if v, ok := args[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func argFloat(args map[string]any, key string, def float64) float64 {
	var value float64
	switch v := args[key].(type) {
	case float64:
		value = v
	case int:
		value = float64(v)
	case string:
		parsed, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return def
		}
		value = parsed
	default:
		return def
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return def
	}
	return value
}

func argInt(args map[string]any, key string, def int) int {
	value := argFloat(args, key, float64(def))
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return def
	}
	// Out-of-range values fall back to the default so callers can tell that
	// the supplied value was not usable.
	if value > float64(math.MaxInt32) || value < float64(math.MinInt32) {
		return def
	}
	return int(value)
}

func argBool(args map[string]any, key string, def bool) bool {
	if v, ok := args[key].(bool); ok {
		return v
	}
	return def
}

func argStringSlice(args map[string]any, key string) []string {
	switch v := args[key].(type) {
	case []string:
		out := []string{}
		for _, item := range v {
			if item = strings.TrimSpace(item); item != "" {
				out = append(out, item)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case []any:
		out := []string{}
		for _, item := range v {
			if s, ok := item.(string); ok {
				if s = strings.TrimSpace(s); s != "" {
					out = append(out, s)
				}
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case string:
		if v = strings.TrimSpace(v); v == "" {
			return nil
		}
		return []string{v}
	}
	return nil
}

func argMap(args map[string]any, key string) map[string]any {
	if v, ok := args[key].(map[string]any); ok {
		return v
	}
	return nil
}

func domainCapabilities(reg *mcp.Registry) []string {
	out := []string{}
	for _, name := range reg.Names() {
		if strings.HasPrefix(name, "system.") || strings.HasPrefix(name, "routing.") || strings.HasPrefix(name, "files.") ||
			strings.HasPrefix(name, "eval.") || strings.HasPrefix(name, "learning.") || strings.HasPrefix(name, "plan.") ||
			strings.HasPrefix(name, "quality.") || strings.HasPrefix(name, "security.") || strings.HasPrefix(name, "telegram.") {
			continue
		}
		out = append(out, name)
	}
	return out
}
