# Learning Quality

A learning is one trigger (when it applies) plus one insight (what to do), recorded with an outcome and evidence. This file shows what passes, what fails, and how confidence and promotion work.

## Trigger Examples

| Good trigger | Bad trigger | Why |
|---|---|---|
| "When a `data.query` `where` filter returns zero rows but the column exists" | "When working with data" | Names the exact condition that should re-trigger the memory |
| "When `documents.extract` reports `truncated: true` for a PDF" | "When documents are long" | Tool-specific and checkable |
| "When a Go test fails only under `-race`" | "When tests fail" | Distinguishes the class from ordinary failures |
| "When the MCP server rejects a path with `..`" | "When paths are wrong" | Points at the observed envelope |

Rules: start with "When", name a tool, error or observable state, and keep it to one sentence. If the trigger describes a whole domain, it will fire on everything and match nothing usefully.

## Insight Examples

| Good insight | Bad insight | Why |
|---|---|---|
| "Call `documents.ingest` and page with `documents.get` instead of summarizing the fragment" | "Handle large documents carefully" | One concrete action |
| "Add `-race` to the test command before declaring a concurrency fix done" | "Write better tests" | Names the behavior and the moment |
| "Use `filepath.Join(root, rel)` after `InputPath` validation; never clean the raw input" | "Validate paths and also sanitize output and check symlinks" | Single behavior; the bundled version cannot be confirmed cleanly |
| "Record the bundle hash when `git_sha` is empty and state the substitution" | "Watch out for missing shas" | Actionable and testable |

Rules: start with a verb, name the tool or command, and fit in one sentence. An insight that cannot fail is not a learning.

## Confidence Math

Confidence is computed from successes and failures, never asserted. Neutral outcomes increment `observations` but neither counter, so confidence is unchanged.

`confidence = round2(0.3 + 0.6 * successes / (successes + failures + 1))`, clamped to `[0.3, 0.9]`.

| Successes | Failures | Confidence | Reading |
|---:|---:|---:|---|
| 0 | 0 | 0.30 | Neutral-only pair; floor |
| 1 | 0 | 0.60 | First confirmed success |
| 2 | 0 | 0.70 | At the injection threshold |
| 3 | 0 | 0.75 | Promotion candidate (observations 3) |
| 5 | 0 | 0.80 | Strong rule |
| 1 | 1 | 0.50 | Mixed evidence |
| 3 | 1 | 0.66 | Below the injection threshold |
| 9+ | 0 | 0.90 | Ceiling; never higher |

A failure never deletes a learning; it lowers the computed value and is part of the record. Recall filters by `min_confidence` (config default 0.5), so a failure-heavy pair naturally stops surfacing.

## Recall Scoring

`learning.recall` ranks matches by `score = 0.6 * similarity + 0.4 * confidence`. Similarity is embedding cosine when `OPENAI_API_KEY` is set and keyword overlap otherwise; the ranking is local either way. `scope: "project"` (default) includes global learnings; `global` returns only promoted ones; `all` returns everything. An empty result returns `partial` with a warning suggesting you record one after the task.

## Promotion Rules

`learning.promote` moves a project learning to global only when:

1. `observations >= promote_after` (config `configs/config.yaml`, default 3), and
2. `confidence >= inject_min_confidence` (default 0.7).

Outcomes: `promoted: true` with `reason: "promoted to global scope"`; otherwise `promoted: false` with `already global` or `not eligible yet: needs more observations or higher confidence`. Never call it in a retry loop and never promote advice observed once. Global learnings keep their id and lose the project key.

## Session Handoff

- `SessionStart` injects the top `max_inject` (default 6) learnings with `confidence >= inject_min_confidence` (default 0.7).
- `hackathonkit-ctl sessions resume <id>` prints a handoff block that includes top learnings, the latest plan stage and recent events; read it before continuing an interrupted session.
- `learning.recall` with the upcoming task text is the reliable way to re-surface lessons the injection did not include.
- Learnings never leave the local repository: recall is local and embeddings are optional.

## Recording Checklist

- [ ] Trigger names one observable condition; insight names one behavior.
- [ ] Outcome matches what actually happened (`success` / `failure` / `neutral`).
- [ ] Scope is `project` unless the rule is proven across projects.
- [ ] Evidence cites a trace id, artifact id, `file:line` or test output - no secrets, no dumps.
- [ ] The record envelope was read: `created`, `confidence`, `observations`.
- [ ] Promotion considered only at 3+ observations and 0.7+ confidence.
