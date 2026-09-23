# Build Evidence

What counts as proof that a stage is done, by stage type.

| Stage type | Exit proof |
|---|---|
| Data/report/document | Artifact id from `reports.create`, or the extracted text re-read with `documents.get` |
| Analysis | The tool result (table, ranking, interval list) quoted with its parameters |
| Code | Test output or quality-gate result; review-pair findings and the applied changes |
| Integration (Telegram/compose) | A real send result or a successful compose health check |
| Fly routing | `routing.fly_feedback` recorded with the accepted/rejected signal |

## Rules

- Quote the smallest runnable proof, not a whole log.
- A stage cannot be `done` while any of its exit tests is unproven.
- Envelope warnings that change interpretation block a clean claim.
- Re-open or re-run the artifact before pasting it as evidence.
