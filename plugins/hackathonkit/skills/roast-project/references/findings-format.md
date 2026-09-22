# Findings Format

One JSON document per roast, stored under `evals/roast/<git_sha>/findings.json`. Findings are structured data for the controller; they are not a report and never contain patches.

## Document

```json
{
  "bundle_hash": "sha256:...",
  "git_sha": "abc1234",
  "critics_completed": 3,
  "findings": {"critical": [], "high": [], "medium": [], "low": []},
  "recommended_fixes": [],
  "ideas": [],
  "artifacts": ["evals/roast/abc1234/findings.json"]
}
```

## Finding Fields

| Field | Required | Rule |
|---|---|---|
| `id` | yes | stable and unique per roast, e.g. `R-014` |
| `severity` | yes | `critical` / `high` / `medium` / `low` |
| `critic` | yes | `bug-hunter` / `product-skeptic` / `reproducibility` |
| `title` | yes | one line, states the defect, not the fix |
| `evidence` | yes | at least one: `file:line`, command output, test name or `artifact_id` |
| `impact` | yes | what breaks for the user or the demo |
| `reproduction` | yes | exact input and steps; `n/a` only for documented omissions |
| `suggested_fix` | no | one line, direction only, never a patch |
| `recheck` | after fixes | `fixed` / `open` / `won't-fix` plus new evidence |

## Evidence Types

- `internal/tools/data.go:214` - source location.
- `cmd: go test ./internal/tools -run TestReadCSVEmpty` - command and observed result.
- `artifact: data/artifacts/<id>` - artifact behind the claim.
- `bundle: data/judge-bundles/README.md:41` - location inside the frozen bundle.

Never cite "the code" without a path, and never cite a chat message.

## Worked Example

```json
{
  "id": "R-014",
  "severity": "high",
  "critic": "bug-hunter",
  "title": "empty CSV aborts the mandatory path instead of returning an empty table",
  "evidence": ["internal/tools/data.go:214", "cmd: data.inspect path=empty.csv -> status=error"],
  "impact": "the mandatory scenario fails on an empty input file; judges see a crash",
  "reproduction": "data/input/empty.csv with the header only; call data.inspect",
  "suggested_fix": "return an explicit empty-table envelope with a warning",
  "recheck": {"status": "fixed", "evidence": "cmd: data.inspect empty.csv -> status=ok, warnings=[empty table]"}
}
```

## Merge Rules

- Same `file:line` and same cause -> one finding, keep both evidence pointers.
- Same cause at different locations -> one finding with a location list.
- Different causes -> separate findings even if the symptom matches.
- Keep the highest severity when the critics disagree.

## Re-check Rules

The original critic re-checks only its own findings after the fix round:

- `fixed` needs the new command output or test result.
- `open` needs the remaining failure and why the fix did not cover it.
- `won't-fix` needs a reason the controller can accept, for example "out of scope for the mandatory scenario".
