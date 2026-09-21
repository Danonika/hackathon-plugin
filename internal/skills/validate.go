// Package skills validates the skill catalog against docs/SKILL-STANDARD.md.
package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	minLines            = 120
	maxLines            = 250
	maxDescriptionChars = 500
	maxShortDescription = 80
)

var requiredSections = []string{
	"when to use",
	"core principles",
	"workflow",
	"decision matrix",
	"output contract",
	"verification",
	"failure modes",
	"anti-patterns",
	"references",
}

// Skills whose workflow consumes external documents must include the
// untrusted-content block.
var untrustedContentSkills = map[string]bool{
	"theme-research":        true,
	"rag-research":          true,
	"document-intelligence": true,
	"government-services":   true,
}

type SkillReport struct {
	Name     string   `json:"name"`
	OK       bool     `json:"ok"`
	Lines    int      `json:"lines"`
	Problems []string `json:"problems,omitempty"`
}

type Summary struct {
	Total  int `json:"total"`
	Passed int `json:"passed"`
	Failed int `json:"failed"`
}

type Report struct {
	OK      bool          `json:"ok"`
	Summary Summary       `json:"summary"`
	Skills  []SkillReport `json:"skills"`
}

type frontmatter struct {
	Name        string         `yaml:"name"`
	Description string         `yaml:"description"`
	Metadata    map[string]any `yaml:"metadata"`
	Tools       []string       `yaml:"tools"`
}

type openaiInterface struct {
	Interface struct {
		DisplayName      string `yaml:"display_name"`
		ShortDescription string `yaml:"short_description"`
		DefaultPrompt    string `yaml:"default_prompt"`
	} `yaml:"interface"`
	Policy struct {
		AllowImplicitInvocation *bool `yaml:"allow_implicit_invocation"`
	} `yaml:"policy"`
}

var (
	headingPattern = regexp.MustCompile(`(?m)^##\s+(.+?)\s*$`)
	refPattern     = regexp.MustCompile("`(references/[^`]+)`")
	mdRefPattern   = regexp.MustCompile(`\]\((references/[^)]+)\)`)
)

func Validate(root string) (Report, error) {
	base := filepath.Join(root, "plugins", "hackathonkit", "skills")
	entries, err := os.ReadDir(base)
	if err != nil {
		return Report{}, fmt.Errorf("skills directory: %w", err)
	}
	report := Report{Skills: []SkillReport{}}
	names := []string{}
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		item := validateSkill(base, name)
		report.Skills = append(report.Skills, item)
		report.Summary.Total++
		if item.OK {
			report.Summary.Passed++
		} else {
			report.Summary.Failed++
		}
	}
	report.OK = report.Summary.Failed == 0
	return report, nil
}

func validateSkill(base, name string) SkillReport {
	item := SkillReport{Name: name, OK: true}
	dir := filepath.Join(base, name)

	data, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		item.Problems = append(item.Problems, "missing SKILL.md")
		item.OK = false
		return item
	}
	text := string(data)
	item.Lines = strings.Count(text, "\n") + 1

	if item.Lines < minLines {
		item.Problems = append(item.Problems, fmt.Sprintf("SKILL.md has %d lines, minimum is %d", item.Lines, minLines))
	}
	if item.Lines > maxLines {
		item.Problems = append(item.Problems, fmt.Sprintf("SKILL.md has %d lines, maximum is %d", item.Lines, maxLines))
	}

	fm, fmErr := parseFrontmatter(text)
	if fmErr != nil {
		item.Problems = append(item.Problems, fmErr.Error())
	} else {
		if fm.Name != name {
			item.Problems = append(item.Problems, fmt.Sprintf("frontmatter name %q does not match directory %q", fm.Name, name))
		}
		if strings.TrimSpace(fm.Description) == "" {
			item.Problems = append(item.Problems, "frontmatter description is empty")
		} else {
			if len([]rune(fm.Description)) > maxDescriptionChars {
				item.Problems = append(item.Problems, fmt.Sprintf("description is %d chars, maximum is %d", len([]rune(fm.Description)), maxDescriptionChars))
			}
			if !strings.Contains(strings.ToLower(fm.Description), "use when") && !strings.Contains(strings.ToLower(fm.Description), "use for") {
				item.Problems = append(item.Problems, "description must start with a trigger phrase such as \"Use when\"")
			}
		}
		if len(fm.Metadata) == 0 {
			item.Problems = append(item.Problems, "frontmatter metadata block is missing")
		}
		if len(fm.Tools) == 0 {
			item.Problems = append(item.Problems, "frontmatter tools list is missing")
		}
	}

	sections := map[string]bool{}
	for _, match := range headingPattern.FindAllStringSubmatch(text, -1) {
		sections[normalizeHeading(match[1])] = true
	}
	for _, required := range requiredSections {
		if !sections[required] {
			item.Problems = append(item.Problems, fmt.Sprintf("missing required section %q", titleCase(required)))
		}
	}
	if untrustedContentSkills[name] && !sections["untrusted content"] {
		item.Problems = append(item.Problems, "missing \"Untrusted Content\" section")
	}

	for _, ref := range referencedFiles(text) {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(ref))); err != nil {
			item.Problems = append(item.Problems, fmt.Sprintf("referenced file does not exist: %s", ref))
		}
	}

	doc, err := validateOpenAIYAML(dir)
	if err != nil {
		item.Problems = append(item.Problems, err.Error())
	} else if strings.Contains(fm.Description, "Explicit-only:") {
		if doc.Policy.AllowImplicitInvocation == nil || *doc.Policy.AllowImplicitInvocation {
			item.Problems = append(item.Problems, "explicit-only skill must set policy.allow_implicit_invocation: false in agents/openai.yaml")
		}
	}

	if len(item.Problems) > 0 {
		item.OK = false
		sort.Strings(item.Problems)
	}
	return item
}

func parseFrontmatter(text string) (frontmatter, error) {
	if !strings.HasPrefix(text, "---\n") {
		return frontmatter{}, fmt.Errorf("SKILL.md must start with YAML frontmatter")
	}
	rest := text[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return frontmatter{}, fmt.Errorf("frontmatter is not terminated with ---")
	}
	var fm frontmatter
	if err := yaml.Unmarshal([]byte(rest[:end]), &fm); err != nil {
		return frontmatter{}, fmt.Errorf("frontmatter does not parse: %v", err)
	}
	return fm, nil
}

func validateOpenAIYAML(dir string) (openaiInterface, error) {
	data, err := os.ReadFile(filepath.Join(dir, "agents", "openai.yaml"))
	if err != nil {
		return openaiInterface{}, fmt.Errorf("missing agents/openai.yaml")
	}
	var doc openaiInterface
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return openaiInterface{}, fmt.Errorf("agents/openai.yaml does not parse: %v", err)
	}
	if strings.TrimSpace(doc.Interface.DisplayName) == "" {
		return doc, fmt.Errorf("agents/openai.yaml is missing interface.display_name")
	}
	if strings.TrimSpace(doc.Interface.ShortDescription) == "" {
		return doc, fmt.Errorf("agents/openai.yaml is missing interface.short_description")
	}
	if len([]rune(doc.Interface.ShortDescription)) > maxShortDescription {
		return doc, fmt.Errorf("agents/openai.yaml short_description exceeds %d chars", maxShortDescription)
	}
	if strings.TrimSpace(doc.Interface.DefaultPrompt) == "" {
		return doc, fmt.Errorf("agents/openai.yaml is missing interface.default_prompt")
	}
	return doc, nil
}

func referencedFiles(text string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, match := range refPattern.FindAllStringSubmatch(text, -1) {
		if !seen[match[1]] {
			seen[match[1]] = true
			out = append(out, match[1])
		}
	}
	for _, match := range mdRefPattern.FindAllStringSubmatch(text, -1) {
		if !seen[match[1]] {
			seen[match[1]] = true
			out = append(out, match[1])
		}
	}
	sort.Strings(out)
	return out
}

func normalizeHeading(value string) string {
	return strings.Trim(strings.ToLower(strings.TrimSpace(value)), ":")
}

func titleCase(value string) string {
	words := strings.Fields(value)
	for i, word := range words {
		words[i] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " ")
}
