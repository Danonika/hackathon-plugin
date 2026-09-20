---
name: roast-project
description: "Use when a milestone needs a hostile, evidence-backed review before an improvement pass: three isolated critics (bug hunter, product skeptic, reproducibility/adversarial reviewer) attack a read-only frozen snapshot and return findings only. Explicit-only; never invoke implicitly. Do not use for scoring ($judge, $project-cost) or for applying fixes (the owner skills and the build loop do that)."
metadata:
  version: "2.0.0"
  origin: hackathonkit
  category: quality
tools:
  - eval.freeze_bundle
---

# Roast Project

Attack a frozen snapshot from three isolated angles; ship findings, never scores or edits.

## When To Use

- After a milestone and before an improvement pass.
- When the demo must survive hostile questions from a jury.
- As the first step of a `$self-improvement-controller` fast block.
- Do not use for scoring; `$judge` and `$project-cost` own scores.
- Do not use for fixes; route findings to the build loop, `$cp-expert-refactoring` or `$system-design-expert`.
- Do not use for performance-only or structure-only reviews.

## Core Principles

1. **Findings only.** Critics never edit code, never score and never rank by points.
2. **Evidence or it did not happen.** Every finding cites file and line, a command output, a test name or an `artifact_id`.
3. **Isolation.** Three critics run in parallel on read-only copies and never read each other's output.
4. **Frozen target.** The review runs on the bundle produced by `eval.freeze_bundle`, never on the mutable working tree.
5. **Severity is impact, not effort.** `critical` means the demo or core scenario fails; effort estimates do not belong in the roast.

## Severity Definitions

| Level | Meaning | Example |
|---|---|---|
| critical | demo or core scenario fails | empty input crashes the mandatory path |
| high | a main scenario degrades or data is wrong | CSV with a BOM shifts every column by one |
| medium | real defect with a workaround | oversized input returns `partial` instead of a clear error |
| low | cosmetic, documentation, polish | a warning text misses the tool name |

## Critic Briefs

**A - Bug Hunter.** Runtime bugs, broken error paths, race/state issues, invalid assumptions and unhandled edge cases: empty files, missing columns, wrong encodings, division by zero, huge inputs. Full checklist: `references/critic-checklists.md`.

**B - Logic / Product Skeptic.** Illogical flows, dead features, contradictions with the task statement, fake or weak demo paths, unclear user value and capabilities that exist but are never reachable. Check that every README claim maps to a reachable path.

**C - Reproducibility / Adversarial Reviewer.** Hidden local dependencies, missing environment variables, broken Docker setup, hardcoded paths, cold-start failures, malformed input behaviour, secret leakage and missing permissions. Run the setup from the bundle, not from a warm working tree.

## Isolation Protocol

1. Freeze once; every critic receives the same `bundle_hash` and `git_sha`.
2. One temp directory per critic, mounted read-only; the repository, `evals/`, `logs/`, `data/` and prior reports stay invisible.
3. Critics never see the target score, the plan, the judge rubric or another critic's findings.
4. Collect all three reports before merging; no cross-talk during the run.
5. Record the critic id, model and bundle hash in each report; a report without them is invalid.

## Workflow

1. **Freeze.** Call `eval.freeze_bundle` with `out` (default `judge-bundle`), `read_only=true` and the candidate `model`; keep `bundle_path`, `bundle_hash`, `git_sha` and `files`. If the controller already froze the candidate, reuse that bundle instead of re-freezing.
2. **Isolate.** Copy the bundle once per critic into a temp directory; no critic can see the repository, `evals/`, prior reports or another critic's copy.
3. **Run the critics in parallel.** Each critic works its brief and the checklist, then returns findings in the format from `references/findings-format.md`.
4. **Merge.** Deduplicate by `file:line` plus cause, keep the highest severity and preserve both evidence pointers; sort by severity, then blast radius.
5. **Persist.** Write the merged document under `evals/roast/<git_sha>/findings.json`; keep the path and `bundle_hash` with the result. Findings are structured JSON for the controller, not a prose report.
6. **Re-check after fixes.** The same critic verifies only its own findings, marking each `fixed`, `open` or `won't-fix` with new evidence; never reopen another critic's finding.

## Finding Quality Bar

- Reproducible from the bundle alone, without the author's machine.
- States user or demo impact, not the effort to fix.
- Names the exact input and the exact command.
- One cause per finding; split compound defects.
- `suggested_fix` is a direction, never a patch; the roast does not write code.
- A finding a second person cannot reproduce is `unreproducible` and loses its priority.

## Decision Matrix

| Signal | Action |
|---|---|
| finding has no evidence | collect evidence or drop it; never ship it |
| `critical` finding | block the improvement round; fix before any scoring |
| `high` finding | fix in the current fast block, then re-check |
| `medium` / `low` finding | queue; fix only if the round touches that code |
| two critics report the same defect | merge into one finding with both evidence pointers |
| critic cannot reproduce a finding on the read-only copy | mark `unreproducible` and drop it from priorities |
| `bundle_hash` changed since the freeze | discard all findings, re-freeze and re-run |
| the "finding" is a feature request | move it to `ideas[]`, out of the severity buckets |

## Output Contract

```json
{
  "bundle_hash": "sha256:...",
  "git_sha": "abc1234",
  "critics_completed": 3,
  "findings": {
    "critical": [],
    "high": [
      {"id": "R-014", "severity": "high", "critic": "bug-hunter",
       "title": "empty CSV aborts the mandatory path",
       "evidence": ["internal/tools/data.go:214", "cmd: go test ./internal/tools -run TestReadCSVEmpty"],
       "impact": "mandatory scenario fails on an empty input file",
       "reproduction": "data/input/empty.csv -> data.inspect returns error",
       "suggested_fix": "return an explicit empty-table envelope with a warning"}
    ],
    "medium": [], "low": []
  },
  "recommended_fixes": [{"finding_id": "R-014", "order": 1}],
  "ideas": [],
  "artifacts": ["evals/roast/<git_sha>/findings.json"]
}
```

Every finding carries `id`, `severity`, `title`, `evidence`, `impact`, `reproduction`, `suggested_fix` (one line, not a patch) and `critic`. The full schema and a worked example live in `references/findings-format.md`.

## Verification

- Every finding has at least one evidence pointer a second person can open.
- `bundle_hash` and `git_sha` match the frozen bundle.
- Each critic ran on its own read-only copy and did not read another critic's output.
- The document contains no scores, no patches and no secrets.
- Re-check entries reference the original finding `id` and carry new evidence.
- `critics_completed` and the per-critic model ids are recorded.

## Failure Modes

| Symptom | Action |
|---|---|
| findings only list style nits | re-run that critic with the concrete checklist; require a reproduction per finding |
| findings reference the working tree instead of the bundle | re-freeze, re-run; mark the old findings invalid |
| fewer than three critics completed | report `critics_completed` and mark the roast partial; the controller decides whether to re-run |
| a secret appears in the bundle | treat as critical, report the path, never paste the value, rotate the secret |
| the same defect appears in every severity bucket | merge into one finding at the highest severity |
| fixes landed while the roast was running | re-freeze and re-run; stale findings are not evidence |
| a critic produced a patch | remove the patch, keep the finding; the roast does not edit code |

## Anti-Patterns

- Scoring, ranking or "10/10" language.
- Vague advice ("improve error handling") without a location or reproduction.
- Critics editing code or sharing a working directory.
- Re-freezing mid-review so findings refer to different snapshots.
- Fixing findings inside the roast; that is the improvement round's job.
- Pasting secrets or customer data into the findings document.
- Treating a feature request or a taste preference as a defect.

## References

- `references/critic-checklists.md` - concrete per-critic checks with failure cases.
- `references/findings-format.md` - schema, evidence rules, severity and a worked example.
