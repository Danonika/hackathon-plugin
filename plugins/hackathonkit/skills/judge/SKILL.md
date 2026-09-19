---
name: judge
description: Evaluate a frozen project snapshot with the Demo Day rubric using 10 procedurally isolated judge runs. Explicit-only: never invoke implicitly.
---

# Judge

Evaluate the current frozen snapshot. Judges never edit code.

## Input

A curated immutable bundle: `TASK.md`, `README.md`, `architecture.md`, `selected-source/`, `test-results.json`, `smoke-results.json`, `docker-build.json`, `demo-artifacts/`, `known-limitations.md`, `git-sha.txt`, `rubric-version.txt`. Do not read `evals/judges/*`, previous scores, target scores or other judge reports.

## Rubric (weights are fixed)

| Criterion | Max |
|---|---:|
| Value of the solution | 25 |
| Result and quality of the solution | 20 |
| Innovation | 15 |
| Development and scaling potential | 20 |
| Presentation, demo and Q&A | 20 |
| **Total** | **100** |

## Procedure

1. Run 10 isolated judge sessions in parallel, each in its own temp directory with a read-only copy of the bundle.
2. Each judge uses the same rubric but a different perspective: product value, result quality, innovation skeptic, scaling, demo/presentation, partner/business, technical specialist, critical buyer, adversarial Q&A, strict generalist.
3. Perspective changes questions asked, never the rubric weights.
4. Each judge scores every criterion, cites evidence for deductions, separates observed facts from assumptions, generates likely jury questions and declares conflict of interest.
5. Aggregate deterministically: schema validation, exclude invalid/conflicted reports, require at least 3 valid reports, compute mean, criterion means and sample variance.

## Rules

- No evidence -> no points. Presentation scores require the demo evidence bundle.
- High variance is a disagreement signal, not a score change.
- Never compare a candidate with judge scores that do not exist yet.
