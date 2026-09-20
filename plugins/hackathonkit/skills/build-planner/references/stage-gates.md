# Stage Gates and Plan Format

## Gate Checklist

Run this before declaring any stage done:

- [ ] Exit evidence exists as a tool result, test output or `artifact_id` (not a claim).
- [ ] The mandatory scenario still passes, or the stage did not touch it.
- [ ] No secret, token or absolute machine path leaked into an artifact.
- [ ] New warnings from tool envelopes are recorded in the plan, not ignored.
- [ ] The next stage's entry criteria are satisfied.
- [ ] Time spent fits the stage timebox; if not, re-plan the remaining budget.

## Plan Artifact Format

`reports.create` with `title: "Build plan - <task>"` and one section per stage:

```json
{
  "task": "one paragraph: input -> expected output",
  "mandatory_scenario": {
    "input": "data/input/sample.csv",
    "command": "the exact user request or script",
    "expected": "artifact + numbers that prove success"
  },
  "time_budget": {"total_minutes": 360, "finalization_reserve_minutes": 45},
  "stages": [
    {
      "id": "S0",
      "goal": "...",
      "entry": ["..."],
      "exit_evidence": ["..."],
      "timebox_minutes": 20,
      "status": "todo | doing | done | failed | cut",
      "evidence": ["artifact_id or tool result reference"],
      "rollback_point": "artifact_id or git sha"
    }
  ],
  "risks": [{"risk": "...", "mitigation": "..."}]
}
```

## Re-planning Triggers

- Two consecutive gate failures in one stage.
- Mandatory scenario changed by the user.
- Research invalidates a core assumption.
- Remaining time <= `finalization_reserve_minutes`.

Re-plan by creating revision `-r2`/`-r3`; keep the previous revision id for the judge bundle.
