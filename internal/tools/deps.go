package tools

import (
	"context"
	"strconv"
	"strings"

	"hackathonkit.local/agentkit/internal/config"
	"hackathonkit.local/agentkit/internal/mcp"
	"hackathonkit.local/agentkit/internal/metadata"
	"hackathonkit.local/agentkit/internal/observability"
	"hackathonkit.local/agentkit/internal/openai"
	"hackathonkit.local/agentkit/internal/routing/fly"
	"hackathonkit.local/agentkit/internal/storage"
)

type Deps struct {
	Cfg     *config.Manager
	Store   *storage.Store
	OpenAI  *openai.Client
	Metrics *observability.Metrics
	Router  *fly.Router
	Meta    metadata.Recorder
	Version string
}

func RegisterAll(reg *mcp.Registry, d Deps) {
	registerSystem(reg, d)
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
	switch v := args[key].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case string:
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

func argInt(args map[string]any, key string, def int) int {
	return int(argFloat(args, key, float64(def)))
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
		return v
	case []any:
		out := []string{}
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case string:
		if v == "" {
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
		if strings.HasPrefix(name, "system.") || strings.HasPrefix(name, "routing.") || strings.HasPrefix(name, "files.") {
			continue
		}
		out = append(out, name)
	}
	return out
}
