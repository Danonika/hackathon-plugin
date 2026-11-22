package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
	"hackathonkit.local/agentkit/internal/mcp"
)

const (
	toolTokenWarn     = 500
	allToolsTokenWarn = 8000
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
					"tool_tokens_warn":      toolTokenWarn,
					"all_tools_tokens_warn": allToolsTokenWarn,
					"skill_lines_warn":      skillLinesWarn,
					"manifest_tokens_warn":  manifestTokenWarn,
					"description_chars":     descriptionWarn,
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
		if err != nil {
			items = append(items, contextItem{Kind: "skill", Name: path, Flag: "walk failed: " + err.Error()})
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		rel, relErr := filepath.Rel(base, path)
		if relErr != nil {
			items = append(items, contextItem{Kind: "skill", Name: path, Flag: "relative path failed: " + relErr.Error()})
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
		f, err := os.Open(path)
		if err != nil {
			items = append(items, contextItem{Kind: kind, Name: name, Flag: "read failed: " + err.Error()})
			return nil
		}
		item := contextItem{Kind: kind, Name: name, Lines: 1}
		// Only frontmatter needs parsing. Keep a bounded prefix and stream all line counts.
		const maxPrefix = 64 << 10
		var prefix bytes.Buffer
		var buf [32 << 10]byte
		for {
			if err := ctx.Err(); err != nil {
				f.Close()
				return err
			}
			n, err := f.Read(buf[:])
			item.Bytes += n
			item.Lines += bytes.Count(buf[:n], []byte("\n"))
			if kind == "skill" && prefix.Len() < maxPrefix {
				prefix.Write(buf[:min(n, maxPrefix-prefix.Len())])
			}
			if err != nil {
				if err != io.EOF {
					item.Flag = "read failed: " + err.Error()
				}
				break
			}
		}
		f.Close()
		item.Tokens = estimateTokens(item.Bytes)
		if kind == "skill" && item.Flag == "" {
			flags := []string{}
			if item.Lines > skillLinesWarn {
				flags = append(flags, fmt.Sprintf("SKILL.md > %d lines", skillLinesWarn))
			}
			if desc, err := parseFrontmatterDescription(prefix.String()); err != nil {
				flags = append(flags, "invalid skill frontmatter: "+err.Error())
			} else if len(desc) > descriptionWarn {
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
	for _, rel := range candidates {
		path := filepath.Join(root, filepath.FromSlash(rel))
		info, err := os.Stat(path)
		if err != nil {
			if !os.IsNotExist(err) {
				items = append(items, contextItem{Kind: "manifest", Name: rel, Flag: "stat failed: " + err.Error()})
			}
			continue
		}
		if info.IsDir() {
			continue
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			items = append(items, contextItem{Kind: "manifest", Name: rel, Flag: "read failed: " + readErr.Error()})
			continue
		}
		item := contextItem{
			Kind:   "manifest",
			Name:   rel,
			Bytes:  int(info.Size()),
			Lines:  bytes.Count(data, []byte("\n")) + 1,
			Tokens: estimateTokens(int(info.Size())),
		}
		if item.Tokens > manifestTokenWarn {
			item.Flag = fmt.Sprintf("manifest > %d tokens", manifestTokenWarn)
		}
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
	if byKind["tool"] > allToolsTokenWarn {
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

func parseFrontmatterDescription(text string) (string, error) {
	block, ok := frontmatterBlock(text)
	if !ok {
		return "", fmt.Errorf("missing or unterminated frontmatter")
	}
	// A real YAML parser handles block scalars, quoted values and inline
	// comments without hand-rolled edge cases.
	var meta struct {
		Description string `yaml:"description"`
	}
	if err := yaml.Unmarshal([]byte(block), &meta); err != nil {
		return "", err
	}
	return strings.TrimSpace(meta.Description), nil
}

// frontmatterBlock returns the text between the leading "---" fence and the
// next "---" fence, if the document starts with frontmatter.
func frontmatterBlock(text string) (string, bool) {
	lines := strings.Split(text, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return "", false
	}
	var b strings.Builder
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return b.String(), true
		}
		b.WriteString(lines[i])
		b.WriteString("\n")
	}
	return "", false
}
