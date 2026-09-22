---
description: Create or update the staged build plan (S0-S6) with a mandatory scenario for the current task.
argument-hint: "[task description]"
---

# /plan

Run the `$build-planner` skill for: $ARGUMENTS

The agent does the work; do not ask the user to name tools.

1. `system.context`, then `system.resources` (ask once if the profile is missing).
2. If no plan exists, call `plan.create` with `slug`, `task`, `mandatory_scenario` and the
   default stages S0-S6 (outcome, discovery, skeleton, vertical slice, hardening, evaluation, freeze).
3. If a plan exists, call `plan.status`, then `plan.update` the current stage as work lands.
4. Keep `mandatory_scenario` explicit: it is the same scenario `$verification-loop` and `$judge` verify.

Never invent evidence; every `plan.update` carries an artifact id or command output.
