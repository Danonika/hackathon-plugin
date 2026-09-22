package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSkill(t *testing.T, root, name, content, openai string, refs map[string]string) {
	t.Helper()
	dir := filepath.Join(root, "plugins", "hackathonkit", "skills", name)
	if err := os.MkdirAll(filepath.Join(dir, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agents", "openai.yaml"), []byte(openai), 0o644); err != nil {
		t.Fatal(err)
	}
	for rel, body := range refs {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func validSkillBody(name string, lines int) string {
	var b strings.Builder
	b.WriteString("---\nname: " + name + "\ndescription: \"Use when testing the validator. Produces a report.\"\nmetadata:\n  version: \"1.0.0\"\n  origin: hackathonkit\ntools:\n  - system.context\n---\n\n")
	b.WriteString("# " + name + "\n\nPurpose line.\n\n")
	b.WriteString("## When To Use\n\n- Trigger\n\n")
	b.WriteString("## Core Principles\n\n1. Principle\n\n")
	b.WriteString("## Workflow\n\n1. Call `system.context`.\n\n")
	b.WriteString("## Decision Matrix\n\n| Signal | Action |\n|---|---|\n| a | b |\n\n")
	b.WriteString("## Output Contract\n\n- Result\n\n")
	b.WriteString("## Verification\n\n- Check the artifact.\n\n")
	b.WriteString("## Failure Modes\n\n| Symptom | Action |\n|---|---|\n| a | b |\n\n")
	b.WriteString("## Anti-Patterns\n\n- Mistake\n\n")
	b.WriteString("## References\n\n- `references/protocol.md` - details\n")
	for strings.Count(b.String(), "\n")+1 < lines {
		b.WriteString("- filler line for length\n")
	}
	return b.String()
}

const validOpenAI = "interface:\n  display_name: \"Test Skill\"\n  short_description: \"Short description.\"\n  default_prompt: \"Use $test to work.\"\n"

func TestValidateAcceptsStandardSkill(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "demo", validSkillBody("demo", 140), validOpenAI, map[string]string{"references/protocol.md": "details\n"})
	report, err := Validate(root)
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK || report.Summary.Passed != 1 {
		t.Fatalf("expected a passing report, got %+v", report)
	}
}

func TestValidateRejectsProblems(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "short", validSkillBody("short", 40), validOpenAI, nil)
	writeSkill(t, root, "renamed", strings.Replace(validSkillBody("renamed", 140), "## Verification", "## Checks", 1), validOpenAI, map[string]string{"references/protocol.md": "x"})
	writeSkill(t, root, "missing-ref", validSkillBody("missing-ref", 140), validOpenAI, nil)
	writeSkill(t, root, "bad-openai", validSkillBody("bad-openai", 140), "interface:\n  display_name: \"\"\n", map[string]string{"references/protocol.md": "x"})

	report, err := Validate(root)
	if err != nil {
		t.Fatal(err)
	}
	if report.OK || report.Summary.Failed != 4 {
		t.Fatalf("expected 4 failures, got %+v", report.Summary)
	}
	problems := map[string]string{}
	for _, item := range report.Skills {
		problems[item.Name] = strings.Join(item.Problems, "; ")
	}
	if !strings.Contains(problems["short"], "minimum") {
		t.Fatalf("short skill problem = %q", problems["short"])
	}
	if !strings.Contains(problems["renamed"], "Verification") {
		t.Fatalf("renamed skill problem = %q", problems["renamed"])
	}
	if !strings.Contains(problems["missing-ref"], "referenced file does not exist") {
		t.Fatalf("missing-ref problem = %q", problems["missing-ref"])
	}
	if !strings.Contains(problems["bad-openai"], "display_name") {
		t.Fatalf("bad-openai problem = %q", problems["bad-openai"])
	}
}

func TestValidateRequiresUntrustedContentForDocumentSkills(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "rag-research", validSkillBody("rag-research", 140), validOpenAI, map[string]string{"references/protocol.md": "x"})
	report, err := Validate(root)
	if err != nil {
		t.Fatal(err)
	}
	if report.OK {
		t.Fatal("rag-research without an Untrusted Content section must fail")
	}
}

func TestValidateMissingSkillsDir(t *testing.T) {
	if _, err := Validate(t.TempDir()); err == nil {
		t.Fatal("missing skills directory must fail")
	}
}

func TestValidateRequiresPolicyForExplicitOnly(t *testing.T) {
	root := t.TempDir()
	explicitBody := strings.Replace(validSkillBody("explicit-demo", 140),
		"description: \"Use when testing the validator. Produces a report.\"",
		"description: \"Use when testing. Explicit-only: never invoke implicitly.\"", 1)
	writeSkill(t, root, "explicit-demo", explicitBody, validOpenAI, map[string]string{"references/protocol.md": "x"})
	report, err := Validate(root)
	if err != nil {
		t.Fatal(err)
	}
	if report.OK {
		t.Fatal("explicit-only skill without policy must fail")
	}
	problems := strings.Join(report.Skills[0].Problems, "; ")
	if !strings.Contains(problems, "allow_implicit_invocation") {
		t.Fatalf("problem = %q", problems)
	}

	root2 := t.TempDir()
	policyYAML := validOpenAI + "policy:\n  allow_implicit_invocation: false\n"
	writeSkill(t, root2, "explicit-demo", explicitBody, policyYAML, map[string]string{"references/protocol.md": "x"})
	report2, err := Validate(root2)
	if err != nil {
		t.Fatal(err)
	}
	if !report2.OK {
		t.Fatalf("explicit-only skill with policy must pass: %+v", report2.Skills)
	}
}
