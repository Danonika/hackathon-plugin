---
name: plan
description: Create or update the staged build plan (S0-S6) with a mandatory scenario. Use when asked for a plan, roadmap, stages, or to update plan progress.
---

# Plan

Run the `$build-planner` skill for the requested task.

1. `system.context`, then `system.resources` (ask once if the profile is missing).
2. If no plan exists, call `plan.create` with `slug`, `task`, `mandatory_scenario` and the
   default stages S0-S6 (outcome, discovery, skeleton, vertical slice, hardening, evaluation, freeze).
3. If a plan exists, call `plan.status`, then `plan.update` the current stage as work lands.
4. Keep `mandatory_scenario` explicit: it is the same scenario `$verification-loop` and `$judge` verify.

Never invent evidence; every `plan.update` carries an artifact id or command output.
