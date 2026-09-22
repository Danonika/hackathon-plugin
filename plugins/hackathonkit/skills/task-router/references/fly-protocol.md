# Fly Protocol

Fly is an experimental capability router (`routing.fly_recommend`). It recommends; Codex decides.
Fly is never an orchestrator, executor or single point of failure.

## When It Runs

Only when `system.context` reports `routing_mode: fly`. In `normal` mode skip Fly entirely.
Toggle with `hackathonkit-mcp --set-routing-mode fly` or `POST /routing {"mode":"fly"}` on the demo API.
`system.context` re-reads `configs/routing.yaml` on every call, so no restart is needed.

## Request

```json
{
  "task": "Find anomalies in equipment telemetry",
  "input_metadata": {
    "types": ["csv"],
    "rows": 120000,
    "columns": 14,
    "has_timestamp": true
  }
}
```

`available_capabilities` is optional; when omitted the server ranks its full capability list.
Pass it only when the plan is already constrained (for example a RAG-only task).

## Response

```json
{
  "status": "ok",
  "data": {
    "route": "timeseries",
    "recommendations": [
      {"capability": "data.inspect", "score": 0.97},
      {"capability": "timeseries.analyze", "score": 0.94},
      {"capability": "timeseries.detect_anomalies", "score": 0.91},
      {"capability": "charts.create", "score": 0.73}
    ]
  },
  "metrics": {
    "max_score": 0.97,
    "threshold": 0.55,
    "encoder": "keyword+embedding",
    "category_scores": {"data": 0.95, "timeseries": 1.0}
  }
}
```

## Signals

Results are top-K (`routing.top_k`, default 5) and blend five signals, each attenuating earlier ones:

1. **keyword** - deterministic category keywords, `min(1, 0.3 * hits)`.
2. **embedding** - cosine similarity against category centroids; the encoder is OpenAI embeddings or a deterministic 512-dim hash encoder when no key is set.
3. **knn** - similarity-weighted votes from `data/index/fly-memory.json` (cap 500, deduplicated by task hash).
4. **mb** - sparse mushroom-body network trained by `cmd/fly-train` into `data/index/fly-brain.json`.
5. **llm** - optional chat-model classification when a key is configured.

`encoder` shows which signal produced the ranking; `keyword` means embeddings were unavailable (intentional fallback).

## Decision Rules

1. Treat `recommendations` as a scored shortlist, not as a plan.
2. Verify each capability against the actual input metadata you observed (not the metadata you guessed).
3. Keep the useful ones, drop irrelevant ones, add missing ones - `reports.create` is rarely recommended but often needed.
4. If a recommendation contradicts observed reality (for example `timeseries.detect_anomalies` with no timestamp column), override the route and state why only if it changes the result.
5. Never execute a tool only because Fly listed it; every call must be justified by the task.

## Fallback Matrix

| Situation | Action |
|---|---|
| Tool error or timeout | Continue in normal mode |
| Empty `recommendations` (`status: partial`) | Continue in normal mode |
| `max_score < threshold` (warning in the envelope) | Continue in normal mode |
| Recommendation names an unavailable tool | Drop it, keep the rest |
| `status: partial` | Use what is present, note the warning |
| Encoder is `keyword` only | Accept the ranking; embeddings are optional |

In every fallback case the user still gets a complete result. Never surface internal routing failures as task failures.

## Feedback Loop

After a fly-mode task completes, call `routing.fly_feedback` exactly once:

- `task`: the same task text used for routing.
- `chosen`: capabilities that were actually executed.
- `recommended`: capabilities the router suggested (optional; improves agreement metrics).
- `source`: `accepted` (plan matched), `codex-correction` (Codex overrode), `normal-teacher` (normal-mode plan used as ground truth), `fallback` (fly failed or below threshold).

The router embeds the task and stores the example in `data/index/fly-memory.json`.
Subsequent recommendations blend keyword, embedding and kNN signals.
Batch rebuild from `logs/fly-feedback.jsonl`: `go run ./cmd/fly-train -model knn`.

Lesson rule: only report confirmed outcomes. Never feed Fly its own unverified recommendations as ground truth.

## Validation Checklist

- [ ] `routing_mode` was read live and is `fly`.
- [ ] `input_metadata` matches `files.describe` output, not assumptions.
- [ ] Every accepted capability exists in `references/tools.md`.
- [ ] The accepted order respects dependencies (describe before analyse, index before search).
- [ ] `reports.create` and `learning.record` were added even if Fly omitted them.
- [ ] Exactly one `routing.fly_feedback` call was made with the executed list.

## Logging

Every fly call appends an event to `logs/fly-events.jsonl` (route, encoder, scores, recommendations, task)
and, when Postgres is configured, a row in `fly_predictions`. The demo dashboard reads the JSONL file for the ranking panel.
