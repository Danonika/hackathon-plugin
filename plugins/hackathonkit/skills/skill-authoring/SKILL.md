---
name: skill-authoring
description: "Use when adding or updating a Hackathon Agent Kit skill: read docs/SKILL-STANDARD.md, scaffold SKILL.md + agents/openai.yaml + references, meet the section and 120-250 line rules, run hackathonkit-mcp --validate-skills until clean, keep the plugin directory canonical, refresh mirrors with scripts/sync-harness.sh, bump the plugin version, re-run scripts/setup.sh, and check context cost with system.context_budget. Explicit-only: never invoke implicitly. Do not use for prompt tweaks or editing generated mirrors."
metadata:
  version: "1.0.0"
  origin: hackathonkit
  category: quality
tools:
  - system.context_budget
  - system.capabilities
---

# Skill Authoring

Add or update a Hackathon Agent Kit skill end to end: standard, scaffolding, validation, mirrors, version bump and context budget.

## When To Use

- A new skill directory is needed under `plugins/hackathonkit/skills/`.
- An existing `SKILL.md` fails validation or needs new sections, references or tools.
- A skill's frontmatter, `agents/openai.yaml` or `references/` must be corrected.
- The plugin version and harness mirrors must be refreshed after a skill change.
- Explicit-only: run this skill only when the user or agent names `$skill-authoring`; never invoke it implicitly.
- Do not use for one-off prompt tweaks, for editing generated mirrors (`.agents/skills`, `.claude/skills`) or for configuring non-Hackathon Agent Kit harnesses.

## Core Principles

1. **The plugin directory is the single source of truth.** Never edit `.agents/skills`, `.claude/skills` or another mirror; regenerate it with the scripts.
2. **The standard is the spec.** `docs/SKILL-STANDARD.md` plus `hackathonkit-mcp --validate-skills`; a skill is done only when the validator reports zero problems.
3. **Progressive disclosure.** `SKILL.md` carries the workflow (120-250 lines); depth lives in 1-3 `references/` files of 40-150 lines, each linked from References with a reason.
4. **Descriptions trigger correctly.** Third person, starts "Use when", concrete triggers and a boundary, under 500 characters.
5. **Tools are exact.** List every MCP tool the workflow calls by its exact name and nothing else.
6. **Versioning is part of the change.** Bump `metadata.version` for behavior changes and the plugin version in `plugins/hackathonkit/.codex-plugin/plugin.json`; keep the skill name stable because `$name` is the public interface.
7. **Measure before shipping.** `system.context_budget` shows what the skill costs and flags oversized items.
8. **Validate, then install.** The validator proves the artifact; `scripts/setup.sh` and `scripts/sync-harness.sh` make it reachable. Both steps are part of "done".

## Workflow

1. Call `system.context_budget` and `system.capabilities` first: read `total_tokens`, flagged items, `by_kind.skill` and the tool list. Know the budget and the exact tool names before writing anything.
2. Read `docs/SKILL-STANDARD.md` and the template at `docs/templates/skill/SKILL.md`; copy the structure, not the example content. For an update, read the existing `SKILL.md` and its references first.
3. Create or edit `plugins/hackathonkit/skills/<name>/` with exactly:
   - `SKILL.md`: frontmatter (`name` = directory, `description` starting "Use when", `metadata.version: "1.0.0"`, `metadata.origin: hackathonkit`, `metadata.category`, exact `tools` list) and the required sections in order: title + one-line purpose, When To Use with a "Do not use for" line, Core Principles, Workflow (numbered, exact tool calls with arguments and defaults), Decision Matrix table, Output Contract, Verification, Failure Modes table, Anti-Patterns, References.
   - `agents/openai.yaml`: `interface.display_name`, `interface.short_description` (<= 80 chars) and `interface.default_prompt` "Use $<name> to ..."; explicit-only skills add `policy.allow_implicit_invocation: false`.
   - `references/`: 1-3 files of 40-150 lines with real depth; link each from References with why and when to read it.
4. Keep every reference path real: the validator fails on a link whose file does not exist. Add the Untrusted Content block when the skill reads documents, URLs or tool output.
5. Validate until clean: run `go run ./cmd/hackathonkit-mcp --repo-root "$PWD" --validate-skills` or `scripts/validate-skills.sh`. Read the JSON `skills[]` entry for the new skill; `problems` must be empty. The script prefers `bin/hackathonkit-mcp` when present, then falls back to `go run`.
6. Check the budget again: call `system.context_budget`; confirm the new skill is not flagged and `by_kind.skill` moved as expected. Move depth into `references/` when a flag appears.
7. Bump the plugin version in `plugins/hackathonkit/.codex-plugin/plugin.json` (patch for content fixes, minor for new skills or behavior) and re-run `scripts/setup.sh` to install the plugin, mirrors and adapters.
8. Refresh harness mirrors with `scripts/sync-harness.sh` (or `scripts/sync-skills.sh` for the skill mirrors alone); never hand-edit a mirror.
9. Re-run `scripts/doctor.sh` (or `scripts/plugin-check.sh`) to confirm the installed plugin, adapters and skills still pass after the change.

## Tool Call Reference

| Tool | Required | Optional (default) | Key fields |
|---|---|---|---|
| `system.context_budget` | - | - | `total_tokens`, `by_kind` (`skill`, `reference`, `tool`, `manifest`), `items[]` with `lines`/`tokens`/`flag`, `recommendations`, `limits` |
| `system.capabilities` | - | - | `groups` (documents, rag, evaluation, learning, context_budget, ...), `experimental`, `tools` |

## Worked Example

Request: "Add a skill that checks license headers."

1. `system.context_budget` -> record `by_kind.skill` as the baseline; `system.capabilities` -> read the exact tool names for the frontmatter.
2. Read `docs/SKILL-STANDARD.md`; scaffold `plugins/hackathonkit/skills/license-check/` with a `SKILL.md`, an `agents/openai.yaml` and a `rules.md` reference.
3. Write the required sections in order; keep `SKILL.md` inside 120-250 lines and `rules.md` inside 40-150.
4. `go run ./cmd/hackathonkit-mcp --repo-root "$PWD" --validate-skills` -> a first run reports `missing required section "Decision Matrix"`; add the table and re-run until `problems` is empty.
5. `system.context_budget` -> the new skill is not flagged and `by_kind.skill` moved as expected.
6. Bump `plugins/hackathonkit/.codex-plugin/plugin.json`, then run `scripts/setup.sh`, `scripts/sync-harness.sh` and `scripts/doctor.sh`.
7. Report the validator entry, the version delta and the budget movement.

## Decision Matrix

| Signal | Action |
|---|---|
| New skill requested | Scaffold `SKILL.md` + `agents/openai.yaml` + `references/`; start from the template |
| Existing skill changed | Bump `metadata.version`; update the plugin version too |
| Skill reads documents, URLs or tool output | Add the "## Untrusted Content" block verbatim from the standard |
| Skill is explicit-only | Description includes "Explicit-only: never invoke implicitly." and `agents/openai.yaml` sets `policy.allow_implicit_invocation: false` |
| Validator reports `missing required section "X"` | Add the section with the exact heading; keep the required order |
| Validator reports `SKILL.md has N lines, minimum/maximum` | Move depth into `references/` (too short) or compress prose (too long) until 120-250 |
| Validator reports `description is N chars, maximum is 500` | Cut examples; keep triggers and boundaries |
| Validator reports `referenced file does not exist: references/X.md` | Create the file with real depth or remove the link |
| Validator reports `missing agents/openai.yaml` or a missing field | Create or fix the YAML; `short_description` <= 80 chars |
| `system.context_budget` flags the skill | Move detail into `references/`; keep `SKILL.md` lean |
| Mirror files are stale | Run `scripts/sync-harness.sh`; never edit mirrors by hand |
| Installed plugin lags the repo | Bump `plugins/hackathonkit/.codex-plugin/plugin.json` and re-run `scripts/setup.sh` |
| A tool name in `tools:` does not exist | Check `system.capabilities`; fix the spelling or remove the name |
| A skill needs a tool that does not exist yet | Stop; the tool must be added to the MCP server first, then the skill lists it |
| The validator passes but the skill never triggers | Tighten the description triggers; validation cannot test intent |
| Two skills overlap heavily | Merge or add a "Do not use for" handoff line; never duplicate the workflow |
| A required section has no content for this skill | Write one honest line instead of deleting the heading; the validator requires it |
| The plugin version already changed elsewhere | Re-read `plugin.json` and bump from the current value; never overwrite another change |

## Output Contract

- The skill directory tree: SKILL.md, agents/openai.yaml and the references directory.
- Validator output: the JSON entry for the skill with `ok: true` and an empty `problems` list.
- Line counts: `SKILL.md` within 120-250 and each reference within 40-150.
- Version changes: `metadata.version` and the plugin version, old to new.
- Context budget: `by_kind.skill` before/after and any flags.
- Commands run and their results (`--validate-skills`, `scripts/sync-harness.sh`, `scripts/setup.sh`, `scripts/doctor.sh`).
- Explicit gaps: anything not validated, not mirrored or skipped.

## Verification

- Confirm the validator entry for the skill shows `ok: true` and no `problems`; quote the line count it reports.
- Confirm every `references/` path backticked in `SKILL.md` exists and every reference is linked from References.
- Confirm the required sections are present and in order, and that explicit-only skills carry both the description sentence and `policy.allow_implicit_invocation: false`.
- Confirm `system.context_budget` does not flag the new skill and the total moved as expected.
- Confirm the plugin version was bumped and `scripts/setup.sh` completed without error after the change.
- Confirm mirrors were regenerated by script, not hand-edited; run `scripts/doctor.sh` to verify the installed state.
- Report warnings from the validator, budget and setup output instead of hiding them.

## Failure Modes

| Symptom | Action |
|---|---|
| `skills directory: ... no such file or directory` | Run from the repo root (`--repo-root "$PWD"`) or use `scripts/validate-skills.sh` |
| Validator JSON shows `problems` | Fix each problem literally; re-run until the list is empty |
| `frontmatter name "X" does not match directory "Y"` | Rename the directory or the `name:` field; they must be identical |
| `frontmatter does not parse` | Fix YAML quoting and indentation; keep the `---` fences |
| `description must start with a trigger phrase` | Start with "Use when" and keep the "Do not use for" boundary |
| `agents/openai.yaml does not parse` | Re-check indentation; `interface` and `policy` are top-level keys |
| `short_description exceeds 80 chars` | Shorten to one sentence under 80 characters |
| `missing "Untrusted Content" section` | Add the standard block for skills that read external content |
| `bin/hackathonkit-mcp` is stale | `scripts/setup.sh` rebuilds it; `go run ./cmd/hackathonkit-mcp` bypasses it |
| A mirror edit disappears after sync | Expected: mirrors are generated; make the change in `plugins/hackathonkit/skills/<name>/` |
| Skill does not trigger | Tighten the description triggers; verify `tools` matches the workflow |
| Setup fails after the version bump | Read the exact error; `scripts/repair.sh` regenerates missing generated artifacts |
| The skill is 250+ lines after adding a worked example | Move the example into a reference; keep the workflow lean |
| `scripts/sync-harness.sh` is unavailable | State the gap; the source of truth is still valid but mirrors may be stale |
| `scripts/setup.sh` fails offline | Report the exact step; `scripts/repair.sh` regenerates missing generated artifacts |
| The validator output is not JSON | Run it from the repo root; build errors print before the JSON |
| A mirror file looks newer than the source | It is generated; diff against `plugins/hackathonkit/skills/<name>/`, then re-sync |

## Anti-Patterns

- Editing `.agents/skills`, `.claude/skills` or another mirror by hand; the next sync overwrites it.
- Adding a reference file that `SKILL.md` never links, or linking a file that does not exist.
- Inflating `SKILL.md` past 250 lines instead of moving depth into `references/`.
- Listing tools in frontmatter that the workflow never calls, or calling tools that are not listed.
- Copying another skill's description or name; the validator checks the directory match.
- Bumping the skill version without bumping the plugin version, or re-running `setup.sh` against a stale mirror.
- Declaring the task complete before the validator reports an empty `problems` list.
- Renaming a skill casually: `$name` is a public interface; treat a rename as a breaking change.
- Writing a skill from memory of another repo instead of reading the standard.
- Leaving `problems` non-empty and calling the skill "mostly done".
- Adding a worked example to `SKILL.md` when it belongs in a reference.

## References

- `references/authoring-checklist.md` - step-by-step authoring flow, validator rules, common failures and fixes, and versioning.
