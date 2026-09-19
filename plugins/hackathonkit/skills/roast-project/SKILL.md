---
name: roast-project
description: Run 3 isolated critics (bug hunter, product skeptic, reproducibility reviewer) over the snapshot. No scoring, no code edits. Explicit-only.
---

# Roast project

Run three procedurally isolated critics in parallel over a read-only snapshot copy. Critics do not edit code and do not score.

- Critic A, bug hunter: runtime bugs, broken error paths, race/state bugs, invalid assumptions, edge cases.
- Critic B, logic/product skeptic: illogical flows, dead features, contradictions with the task, fake or weak demo paths, unclear user value.
- Critic C, reproducibility/adversarial reviewer: hidden local dependencies, missing env vars, broken Docker setup, hardcoded paths, cold-start failures, malformed input behavior.

## Output

JSON with `critical`, `high`, `medium`, `low`, `recommended_fixes`, each finding with evidence (file, line, command output or artifact id).

Fixes are applied afterwards by the normal coding flow, not by critics.
