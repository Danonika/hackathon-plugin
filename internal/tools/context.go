package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"hackathonkit.local/agentkit/internal/mcp"
)

const (
	toolTokenWarn     = 500
	skillLinesWarn    = 400
	manifestTokenWarn = 4000
	descriptionWarn   = 500
)

type contextItem struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Bytes  int    `json:"bytes"`
	Lines  int    `json:"lines"`
	Tokens int    `json:"tokens"`
	Flag   string `json:"flag,omitempty"`
}

type contextRecommendation struct {
	Item       string `json:"item"`
	Reason     string `json:"reason"`
	Suggestion string `json:"suggestion"`
}

func registerContextBudget(reg *mcp.Registry, d Deps) {
	reg.Register(tool(
		"system.context_budget",
		"Estimate context-window cost of the loaded tool schemas, skills, references and manifests. Returns per-item token estimates, flags and prioritized savings recommendations.",
		map[string]any{},
		nil,
		func(ctx context.Context, args map[string]any) (any, error) {
			items := []contextItem{}
			items = append(items, toolContextItems(reg)...)
			skillItems, skillErr := skillContextItems(ctx, d.Cfg.Root())
			if skillErr != nil {
				return mcp.Fail(trace(ctx), skillErr.Error()), nil
			}
			items = append(items, skillItems...)
			items = append(items, manifestContextItems(d.Cfg.Root())...)
			sort.SliceStable(items, func(i, j int) bool {
				if items[i].Tokens != items[j].Tokens {
					return items[i].Tokens > items[j].Tokens
				}
				return items[i].Name < items[j].Name
			})

			byKind := map[string]int{}
			total := 0
			flagged := 0
			for _, item := range items {
				byKind[item.Kind] += item.Tokens
				total += item.Tokens
				if item.Flag != "" {
					flagged++
				}
			}
			recommendations, dropped := contextRecommendations(items, byKind)
			truncated := dropped
			data := map[string]any{
				"recommendations_truncated": truncated,
				"total_tokens":              total,
				"by_kind":                   byKind,
				"tool_count":                len(reg.Names()),
				"items":                     items,
				"recommendations":           recommendations,
				"limits": map[string]any{
					"tool_tokens_warn":     toolTokenWarn,
					"skill_lines_warn":     skillLinesWarn,
					"manifest_tokens_warn": manifestTokenWarn,
					"description_chars":    descriptionWarn,
				},
				"estimate": "tokens ~= bytes / 4",
			}
			if d.Metrics != nil {
				d.Metrics.Add("context_budget_total_tokens", int64(total))
			}
			env := mcp.OK(trace(ctx), data)
			if flagged > 0 {
				env = env.WithWarnings(fmt.Sprintf("%d context item(s) exceed the recommended budget", flagged))
			}
			return env.WithMetrics(map[string]any{
				"total_tokens": total,
				"flagged":      flagged,
			}), nil
		},
	))
}

func toolContextItems(reg *mcp.Registry) []contextItem {
	items := []contextItem{}
	for _, t := range reg.List() {
		payload := map[string]any{
			"name":        t.Name,
			"description": t.Description,
			"inputSchema": t.InputSchema,
		}
		data, err := json.Marshal(payload)
		if err != nil {
			items = append(items, contextItem{
				Kind: "tool",
				Name: t.Name,
				Flag: "schema marshal failed: " + err.Error(),
			})
			continue
		}
		item := contextItem{
			Kind:   "tool",
			Name:   t.Name,
			Bytes:  len(data),
			Tokens: estimateTokens(len(data)),
		}
		if item.Tokens > toolTokenWarn {
			item.Flag = fmt.Sprintf("schema+description > %d tokens", toolTokenWarn)
		}
		items = append(items, item)
	}
	return items
}

func skillContextItems(ctx context.Context, root string) ([]contextItem, error) {
	base := filepath.Join(root, "plugins", "hackathonkit", "skills")
	items := []contextItem{}
	walkErr := filepath.WalkDir(base, func(path string, entry os.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil || entry.IsDir() {
			return nil
		}
		info, statErr := entry.Info()
		if statErr != nil {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		rel, relErr := filepath.Rel(base, path)
		if relErr != nil {
			return nil
		}
		name := filepath.ToSlash(rel)
		kind := "asset"
		switch {
		case entry.Name() == "SKILL.md":
			kind = "skill"
		case strings.Contains(name, "/references/"):
			kind = "reference"
		case strings.Contains(name, "/templates/"):
			kind = "template"
		}
		item := contextItem{
			Kind:   kind,
			Name:   name,
			Bytes:  int(info.Size()),
			Lines:  strings.Count(string(data), "\n") + 1,
			Tokens: estimateTokens(int(info.Size())),
		}
		if kind == "skill" {
			flags := []string{}
			if item.Lines > skillLinesWarn {
				flags = append(flags, fmt.Sprintf("SKILL.md > %d lines", skillLinesWarn))
			}
			if desc := frontmatterDescription(string(data)); len(desc) > descriptionWarn {
				flags = append(flags, fmt.Sprintf("description > %d chars", descriptionWarn))
			}
			item.Flag = strings.Join(flags, "; ")
		}
		items = append(items, item)
		return nil
	})
	if walkErr != nil {
		if errors.Is(walkErr, context.Canceled) || errors.Is(walkErr, context.DeadlineExceeded) {
			return items, walkErr
		}
		items = append(items, contextItem{Kind: "skill", Name: "skills scan", Flag: "walk failed: " + walkErr.Error()})
	}
	return items, nil
}

func manifestContextItems(root string) []contextItem {
	candidates := []string{
		"AGENTS.md",
		".codex/AGENTS.md",
		".codex/config.toml",
		"plugins/hackathonkit/.codex-plugin/plugin.json",
		"plugins/hackathonkit/.mcp.json",
		"configs/config.yaml",
		"configs/evaluation.yaml",
		"configs/routing.yaml",
	}
	items := []contextItem{}
	total := 0
	for _, rel := range candidates {
		path := filepath.Join(root, filepath.FromSlash(rel))
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			continue
		}
		item := contextItem{
			Kind:   "manifest",
			Name:   rel,
			Bytes:  int(info.Size()),
			Lines:  strings.Count(string(data), "\n") + 1,
			Tokens: estimateTokens(int(info.Size())),
		}
		if item.Tokens > manifestTokenWarn {
			item.Flag = fmt.Sprintf("manifest > %d tokens", manifestTokenWarn)
		}
		total += item.Tokens
		items = append(items, item)
	}
	return items
}

func contextRecommendations(items []contextItem, byKind map[string]int) ([]contextRecommendation, bool) {
	out := []contextRecommendation{}
	for _, item := range items {
		if item.Flag == "" {
			continue
		}
		suggestion := "trim the description and move detail into the tool result or a skill"
		if item.Kind == "skill" {
			suggestion = "move depth into references/ so SKILL.md stays under the budget"
		}
		out = append(out, contextRecommendation{
			Item:       item.Name,
			Reason:     item.Flag,
			Suggestion: suggestion,
		})
	}
	if byKind["tool"] > 8000 {
		out = append(out, contextRecommendation{
			Item:       "tools",
			Reason:     fmt.Sprintf("tool schemas cost ~%d tokens", byKind["tool"]),
			Suggestion: "merge overlapping tools or shorten descriptions and schemas",
		})
	}
	dropped := false
	if len(out) > 10 {
		out = out[:10]
		dropped = true
	}
	return out, dropped
}

func estimateTokens(bytes int) int {
	if bytes <= 0 {
		return 0
	}
	return int(math.Ceil(float64(bytes) / 4.0))
}

func frontmatterDescription(text string) string {
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return ""
	}
	for i := 1; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "---" {
			break
		}
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "description:") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(trimmed, "description:"))
		if len(value) > 0 && value[0] != '"' && value[0] != '\'' {
			if idx := strings.Index(value, " #"); idx >= 0 {
				value = strings.TrimSpace(value[:idx])
			}
		}
		if value == "|" || value == ">" || value == "|-" || value == ">-" {
			// Block scalar: collect the indented lines that follow.
			var b strings.Builder
			for j := i + 1; j < len(lines); j++ {
				next := lines[j]
				if strings.TrimSpace(next) == "---" || (strings.TrimSpace(next) != "" && !strings.HasPrefix(next, " ") && !strings.HasPrefix(next, "\t")) {
					break
				}
				if b.Len() > 0 {
					b.WriteString(" ")
				}
				b.WriteString(strings.TrimSpace(next))
			}
			return strings.TrimSpace(b.String())
		}
		return strings.Trim(value, `"'`)
	}
	return ""
}
