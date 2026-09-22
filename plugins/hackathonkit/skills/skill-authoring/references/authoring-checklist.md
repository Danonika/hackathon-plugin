# Authoring Checklist

Step-by-step flow for adding or updating a skill under `plugins/hackathonkit/skills/<name>/`. Run the validator after every edit; it is the source of truth, not this file.

## Step By Step

1. **Name it.** Pick a kebab-case name that matches the directory exactly; it becomes the public `$name`. Keep it stable after release.
2. **Read the spec.** `docs/SKILL-STANDARD.md` and the template `docs/templates/skill/SKILL.md`. For updates, read the current `SKILL.md` and every reference before changing anything.
3. **Create the tree.** `SKILL.md` (required), `agents/openai.yaml` (required), `references/` (1-3 files, optional only when the workflow needs no depth).
4. **Write the frontmatter.** `name` = directory; `description` third person starting "Use when", with concrete triggers, a boundary and under 500 characters; `metadata.version: "1.0.0"`, `metadata.origin: hackathonkit`, `metadata.category` (`core`/`domain`/`evaluation`/`quality`); `tools` with exact MCP tool names only.
5. **Write the sections in order.** Title + one-line purpose; `## When To Use` with a "Do not use for" line; `## Core Principles` (3-6 non-negotiables); `## Workflow` (numbered, exact tool calls with arguments and defaults); `## Decision Matrix` (table); `## Output Contract`; `## Verification`; `## Failure Modes` (table); `## Anti-Patterns`; `## References`.
6. **Add Untrusted Content** when the skill reads documents, URLs or tool output: the standard block, verbatim.
7. **Write the references.** 40-150 lines each, real depth (rubrics, protocols, worked examples, checklists). Link each one from `## References` with why and when to read it; delete files nothing links.
8. **Write `agents/openai.yaml`.** `interface.display_name`, `interface.short_description` under 80 characters, `interface.default_prompt` "Use $<name> to ...". Explicit-only skills add `policy.allow_implicit_invocation: false` and the description sentence "Explicit-only: never invoke implicitly."
9. **Validate.** `scripts/validate-skills.sh` or `go run ./cmd/hackathonkit-mcp --repo-root "$PWD" --validate-skills`. Iterate until the skill's `problems` list is empty.
10. **Budget.** `system.context_budget`; confirm the skill is unflagged and `by_kind.skill` is as expected.
11. **Version and install.** Bump `plugins/hackathonkit/.codex-plugin/plugin.json`, re-run `scripts/setup.sh`, refresh mirrors with `scripts/sync-harness.sh`, then `scripts/doctor.sh`.

## Validator Rules

The validator (`internal/skills/validate.go`) checks, per skill:

| Rule | Detail |
|---|---|
| Lines | `SKILL.md` must be 120-250 lines (newline count + 1) |
| Name | Frontmatter `name` must equal the directory name |
| Description | Non-empty, <= 500 runes, contains "use when" or "use for" (case-insensitive) |
| Metadata | `metadata` block present; `tools` list non-empty |
| Sections | `when to use`, `core principles`, `workflow`, `decision matrix`, `output contract`, `verification`, `failure modes`, `anti-patterns`, `references` all present as `##` headings |
| Untrusted | Hard-coded skills that read external content must have `## Untrusted Content` |
| References | Every backticked `references/...` and markdown-linked path must exist on disk |
| OpenAI YAML | `agents/openai.yaml` parses; `display_name`, `short_description` (<= 80 chars) and `default_prompt` non-empty |

Validator output is JSON: `{"ok", "summary": {"total","passed","failed"}, "skills": [{"name","ok","lines","problems"}]}`.

## Common Failures And Fixes

| Failure | Fix |
|---|---|
| `SKILL.md has N lines, minimum is 120` | Add depth: a worked example, a stronger decision matrix, one more principle |
| `SKILL.md has N lines, maximum is 250` | Move prose into `references/`; trim examples, not required sections |
| `missing required section "X"` | Add the exact `## X` heading; keep the required order |
| `description is N chars, maximum is 500` | Cut trigger examples; keep the trigger class and the boundary |
| `description must start with a trigger phrase` | Rewrite to start "Use when ..." |
| `frontmatter name "a" does not match directory "b"` | Make them identical; do not rename after release without treating it as breaking |
| `referenced file does not exist: references/x.md` | Create the file with real depth or remove the link |
| `missing agents/openai.yaml` | Create it with the three interface fields |
| `short_description exceeds 80 chars` | One short sentence; move nuance into the description |
| `frontmatter does not parse` | Check YAML quoting; descriptions with `:` must be quoted |
| `agents/openai.yaml does not parse` | Check indentation; `policy:` is top-level, not nested under `interface` |

## Versioning

- `metadata.version`: patch for wording/fix, minor for new behavior, major for a breaking change (renamed tool, changed output contract).
- Plugin version `plugins/hackathonkit/.codex-plugin/plugin.json`: bump on any shipped skill change; `scripts/setup.sh` installs the new version.
- Skill name: never change silently; `$name` is the public interface and appears in prompts and mirrors.
- Commit note: state the version delta and any breaking behavior in the change description.

## Definition Of Done

- [ ] Validator entry for the skill: `ok: true`, empty `problems`.
- [ ] `SKILL.md` 120-250 lines; each reference 40-150 lines.
- [ ] Required sections present and in order; explicit-only markers complete.
- [ ] Every referenced file exists; every reference is linked with a reason.
- [ ] `system.context_budget` shows no flag for the skill.
- [ ] Plugin version bumped; `scripts/setup.sh`, `scripts/sync-harness.sh` and `scripts/doctor.sh` ran clean.
- [ ] No mirror file was edited by hand.
