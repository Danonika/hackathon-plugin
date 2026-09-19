---
name: judge
description: "Simulate Demo Day scoring of a frozen project snapshot with 10 procedurally isolated judge runs using the official rubric. Explicit-only: never invoke implicitly."
---

# Judge

Evaluate a **frozen snapshot**, never a live working tree. Judges do not edit code.

## When To Use

- A candidate snapshot is frozen and improvement decisions need a score.
- Before final submission, to collect likely jury questions and common deductions.

## Input Bundle

The controller prepares `judge-bundle/` and each judge receives a read-only copy:

`TASK.md`, `README.md`, `architecture.md`, `selected-source/`, `test-results.json`, `smoke-results.json`, `docker-build.json`, `demo-artifacts/`, `known-limitations.md`, `git-sha.txt`, `rubric-version.txt`.

Judges must not read `evals/judges/*`, previous scores, the target score, other judge reports or the mutable repository.

## Procedure

1. Launch 10 isolated judge sessions in parallel, each in its own temp directory with the read-only bundle.
2. Same rubric and weights for everyone; a different perspective only changes the questions asked:
   01 product value, 02 result quality, 03 innovation skeptic, 04 scaling, 05 demo and presentation,
   06 partner/business, 07 technical specialist, 08 critical buyer, 09 adversarial Q&A, 10 strict generalist.
3. Each judge: scores all five criteria, cites evidence for every deduction, separates observed facts from assumptions, produces likely jury questions, declares `conflict_of_interest`.
4. Aggregate deterministically: schema validation -> drop invalid and conflicted reports -> require >= 3 valid -> mean, criterion means, sample variance, common deductions, question frequency.

## Evidence Rules

- No evidence, no points. Evidence is a file path, test result, artifact id or clean-room report.
- Presentation and demo criteria require the demo evidence bundle; without it the judge must mark the score explicitly incomplete rather than guess.
- High variance means disagreement; report it, do not adjust scores.

## Output Contract

Per judge: `judge_id`, `snapshot_sha`, `conflict_of_interest`, `scores{value, result_quality, innovation, scalability, presentation_demo_qa}`, `total`, `evidence[]`, `deductions[]`, `questions[]`, `top_improvements[]`.

Aggregate: `valid_judges`, `mean_total`, `criterion_means`, `score_variance`, `high_disagreement[]`, `common_deductions[]`, `likely_questions[]`.

Full rubric, aggregation rules and calibration procedure: `references/rubric.md`.

## Rules

- A candidate is never compared to judge scores that do not exist yet.
- Judges never see the target score; the controller compares and decides.
- Reproducibility: record model id, runtime version, bundle hash and rubric version with every run.
