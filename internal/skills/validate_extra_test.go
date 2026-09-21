package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateSkillMissingSkillMD(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "plugins", "hackathonkit", "skills", "no-md")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	report, err := Validate(root)
	if err != nil {
		t.Fatal(err)
	}
	if report.OK || report.Summary.Failed != 1 {
		t.Fatalf("report = %+v", report)
	}
	if !strings.Contains(strings.Join(report.Skills[0].Problems, "; "), "missing SKILL.md") {
		t.Fatalf("problems = %v", report.Skills[0].Problems)
	}
}

func TestValidateSkillTooLong(t *testing.T) {
	root := t.TempDir()
	writeSkill(t, root, "long", validSkillBody("long", maxLines+20), validOpenAI, map[string]string{"references/protocol.md": "x"})
	report, err := Validate(root)
	if err != nil {
		t.Fatal(err)
	}
	if report.OK {
		t.Fatal("over-long skill must fail")
	}
	if !strings.Contains(strings.Join(report.Skills[0].Problems, "; "), "maximum") {
		t.Fatalf("problems = %v", report.Skills[0].Problems)
	}
}

func TestParseFrontmatterErrors(t *testing.T) {
	if _, err := parseFrontmatter("# no frontmatter\n"); err == nil {
		t.Fatal("missing frontmatter must fail")
	}
	if _, err := parseFrontmatter("---\nname: x\n"); err == nil {
		t.Fatal("unterminated frontmatter must fail")
	}
	if _, err := parseFrontmatter("---\nname: [broken\n---\nbody\n"); err == nil {
		t.Fatal("invalid yaml must fail")
	}
	fm, err := parseFrontmatter("---\nname: x\ndescription: d\n---\nbody\n")
	if err != nil || fm.Name != "x" || fm.Description != "d" {
		t.Fatalf("frontmatter = %+v err=%v", fm, err)
	}
}

func TestValidateSkillFrontmatterProblems(t *testing.T) {
	root := t.TempDir()
	body := "---\nname: other-name\ndescription: \"" + strings.Repeat("x", maxDescriptionChars+10) + "\"\n---\n\n# Other\n\n"
	writeSkill(t, root, "frontmatter-demo", body, validOpenAI, nil)

	report, err := Validate(root)
	if err != nil {
		t.Fatal(err)
	}
	if report.OK {
		t.Fatal("frontmatter problems must fail validation")
	}
	problems := strings.Join(report.Skills[0].Problems, "; ")
	for _, want := range []string{"does not match directory", "maximum is 500", "trigger phrase", "metadata block is missing", "tools list is missing"} {
		if !strings.Contains(problems, want) {
			t.Fatalf("problems missing %q:\n%s", want, problems)
		}
	}
}

func TestValidateSkillEmptyDescription(t *testing.T) {
	root := t.TempDir()
	body := "---\nname: empty-desc\ndescription: \"  \"\nmetadata:\n  version: \"1\"\ntools:\n  - system.context\n---\n\n# Empty\n\n"
	writeSkill(t, root, "empty-desc", body, validOpenAI, nil)
	report, err := Validate(root)
	if err != nil {
		t.Fatal(err)
	}
	if report.OK || !strings.Contains(strings.Join(report.Skills[0].Problems, "; "), "description is empty") {
		t.Fatalf("problems = %v", report.Skills[0].Problems)
	}
}

func TestValidateOpenAIYAMLErrors(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{"parse", "interface: [broken\n", "does not parse"},
		{"short-description", "interface:\n  display_name: \"X\"\n", "short_description"},
		{"short-description-long", "interface:\n  display_name: \"X\"\n  short_description: \"" + strings.Repeat("y", maxShortDescription+1) + "\"\n", "exceeds"},
		{"default-prompt", "interface:\n  display_name: \"X\"\n  short_description: \"ok\"\n", "default_prompt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.MkdirAll(filepath.Join(dir, "agents"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "agents", "openai.yaml"), []byte(tc.content), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := validateOpenAIYAML(dir); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want substring %q", err, tc.want)
			}
		})
	}

	dir := t.TempDir()
	if _, err := validateOpenAIYAML(dir); err == nil || !strings.Contains(err.Error(), "missing agents/openai.yaml") {
		t.Fatalf("missing file err = %v", err)
	}

	valid := t.TempDir()
	if err := os.MkdirAll(filepath.Join(valid, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(valid, "agents", "openai.yaml"), []byte(validOpenAI), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := validateOpenAIYAML(valid)
	if err != nil || doc.Interface.DisplayName != "Test Skill" {
		t.Fatalf("valid openai.yaml = %+v err=%v", doc, err)
	}
}

func TestReferencedFilesDedupeAndMarkdownLinks(t *testing.T) {
	text := "See `references/a.md` and `references/a.md`.\n" +
		"Also [details](references/b.md) and [more](references/c.md).\n"
	refs := referencedFiles(text)
	want := []string{"references/a.md", "references/b.md", "references/c.md"}
	if len(refs) != len(want) {
		t.Fatalf("refs = %v", refs)
	}
	for i := range want {
		if refs[i] != want[i] {
			t.Fatalf("refs = %v, want %v", refs, want)
		}
	}
	if refs := referencedFiles("no references here"); len(refs) != 0 {
		t.Fatalf("refs = %v", refs)
	}
}
