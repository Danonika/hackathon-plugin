# Plan Tooling Cookbook

`plan.*` stores staged plans under `evals/plans/<slug>.json`. The same files are
read by the SessionStart hook, so the plan is the handoff contract across sessions.
Never edit them by hand; `plan.update` owns revisions.

## plan.create

| Argument | Required | Notes |
|---|---|---|
| `slug` | yes | kebab-case, max 60 chars; Cyrillic is accepted and transliterated by the server |
| `task` | no | input -> expected output, one paragraph |
| `mandatory_scenario` | no | `{input, command, expected}` object |
| `stages` | no | `[{id, goal, status, timebox_minutes}]`; omit for the default S0-S6 |

Defaults when `stages` is omitted: S0 20 min, S1 30, S2 45, S3 90, S4 60, S5 60, S6 30.
Invalid stage statuses fail the call.

```json
{
  "slug": "telemetry-anomalies",
  "task": "Input: hourly telemetry CSV. Output: anomaly list, chart and report.",
  "mandatory_scenario": {
    "input": "data/input/sample-telemetry.csv",
    "command": "Find anomalies in the telemetry and produce a chart and report",
    "expected": "anomaly list with timestamps + chart artifact + report artifact"
  }
}
```

Response: `{slug, revision: 1, path: "evals/plans/telemetry-anomalies.json", stages, summary}`.

## plan.update

| Argument | Required | Notes |
|---|---|---|
| `slug` | yes | plan name |
| `stage` | yes | existing stage id, e.g. `S3` |
| `status` | yes | `todo` / `doing` / `done` / `failed` / `cut` |
| `evidence` | no | list; entries are appended, never replaced |
| `goal` | no | rewrites the stage goal (re-planning) |

Every call bumps `revision` by one. Unknown stages and invalid statuses return `error`.

```json
{
  "slug": "telemetry-anomalies",
  "stage": "S3",
  "status": "done",
  "evidence": ["artifact:telemetry-anomalies-report", "trace:kit-abc123", "test:go test ./... ok"]
}
```

Response: `{slug, revision: 7, stage: "S3", status: "done", summary: "plan: ... stage S4 (todo)"}`.

## plan.status

- With `slug`: loads that plan and returns `current_stage` (first stage not `done`/`cut`).
- Without `slug`: returns the most recently updated plan.
- No plan at all: `status: partial`, `data.plan: null`, warning `no plan found; create one with plan.create`.
- Response includes `slug`, `revision`, `task`, `stages`, `path`, `summary` and, when a stage is open, `current_stage`.

## Cookbook

1. Create:
   `plan.create {"slug":"telemetry-anomalies","task":"...","mandatory_scenario":{...}}`
2. Start a stage:
   `plan.update {"slug":"telemetry-anomalies","stage":"S2","status":"doing"}`
3. Finish a stage:
   `plan.update {"slug":"telemetry-anomalies","stage":"S2","status":"done","evidence":["smoke: initialize+tools/list+system.context ok"]}`
4. Resume next session:
   `plan.status {"slug":"telemetry-anomalies"}` -> read `current_stage` and `summary`.
5. Re-plan:
   `plan.update {"slug":"telemetry-anomalies","stage":"S4","goal":"hardening: only error paths, tests deferred","status":"doing"}`
6. Cut scope:
   `plan.update {"slug":"telemetry-anomalies","stage":"S4","status":"cut","evidence":["time: reserve reached"]}`

## Artifact Handoff

- `reports.create` produces `build-plan-<slug>` for humans; the JSON plan remains authoritative.
- The judge evidence bundle should reference both: the plan `path` and the report `artifact_id`.
- `$project-cost` and `$judge` read the mandatory scenario from the plan; changing it invalidates prior scores.

## Failure Handling

| Symptom | Action |
|---|---|
| `error: slug is required` | Pass a non-empty `slug` |
| `error: unknown stage S9` | `plan.status` and use an existing id |
| `error: invalid status` | Use `todo`, `doing`, `done`, `failed` or `cut` |
| `plan.status` partial | Create the plan; do not fabricate stages |
| `persist` of the human report fails | Continue; the JSON plan is still saved |
| Plan file missing after a checkout | `evals/plans/` may be git-ignored in a clean clone; re-create from the session handoff |
