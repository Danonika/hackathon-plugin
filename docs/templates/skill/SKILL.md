---
name: example-skill
description: "Use when <concrete trigger>. <What the skill produces>. Do not use for <boundary>."
metadata:
  version: "1.0.0"
  origin: hackathonkit
  category: domain
tools:
  - system.context
---

# Example Skill

One-line purpose of this skill.

## When To Use

- <Concrete trigger 1>
- <Concrete trigger 2>
- Do not use for <near-miss task>; use `$other-skill` instead.

## Core Principles

1. <Non-negotiable 1 and why it matters>
2. <Non-negotiable 2>
3. Deterministic tools compute; you interpret.

## Workflow

1. Call `system.context` to read the live routing mode.
2. Call `<mcp.tool>` with `<arguments and defaults>`; keep the returned `sources`.
3. Branch on the result:
   - `<condition>` -> `<action>`
   - `<condition>` -> `<action>`
4. Persist the result with `<tool>` and keep the `artifact_id`.

## Decision Matrix

| Signal | Action |
|---|---|
| <signal> | <action> |
| <signal> | <action> |
| <signal> | <action> |

## Output Contract

- Result with evidence (`sources`, `artifact_id`).
- Parameters used (tool, filter, limit).
- Warnings from every envelope.
- Explicit gaps and limitations.

## Verification

- Confirm every claim traces to a tool result.
- Confirm the artifact exists and `status != "error"`.
- Report warnings instead of hiding them.

## Failure Modes

| Symptom | Action |
|---|---|
| `<tool>` returns `partial` | Report the warning and continue with the covered part |
| `<tool>` returns `error` | Fix the arguments once; on repeat, report the exact failure |
| <external dependency missing> | Use the documented fallback; state it in the answer |

## Anti-Patterns

- <Specific mistake 1>
- <Specific mistake 2>
- Inventing numbers, timestamps or quotes.

## References

- `references/<file>.md` - <why and when to read it>
