# Fly Protocol

Fly is an experimental capability router. It recommends; Codex decides. Fly is never an orchestrator, executor or single point of failure.

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

`available_capabilities` is optional; when omitted the server ranks its full capability list. Pass it when the plan is already constrained.

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

`encoder` is `keyword` when embeddings are unavailable and `keyword+embedding` when the OpenAI encoder is active. Embedding centroids are cached server-side; the first call in a session may take longer.

## Decision Rules

1. Treat `recommendations` as a shortlist with scores, not as a plan.
2. Verify each capability against the actual input metadata you observed (not the metadata you guessed).
3. Keep the useful ones, drop irrelevant ones, add missing ones (for example `reports.create` is rarely recommended but often needed).
4. If the recommendation contradicts observed reality (for example `timeseries.detect_anomalies` but no timestamp column), override the route and say why in the final answer only if it changes the result.

## Fallback Matrix

| Situation | Action |
|---|---|
| Tool error or timeout | Continue in normal mode |
| Empty `recommendations` | Continue in normal mode |
| `max_score < threshold` (warning in envelope) | Continue in normal mode |
| Recommendation names an unavailable tool | Drop it, keep the rest |
| `status: partial` | Use what is present, note the warning |

In every fallback case the user must still get a complete result. Never surface internal routing failures as task failures.

## Feedback Loop

After a fly-mode task completes, call `routing.fly_feedback` exactly once:

- `task`: the same task text used for routing.
- `chosen`: capabilities that were actually executed.
- `recommended`: capabilities the router suggested (optional, improves agreement metrics).
- `source`: `accepted` (plan matched), `codex-correction` (Codex overrode), `normal-teacher` (normal-mode plan used as ground truth), `fallback` (fly failed or below threshold).

The router embeds the task and stores the example in `data/index/fly-memory.json` (kNN memory, capped at 500, deduplicated by task). Subsequent recommendations blend keyword, embedding and kNN signals; the `encoder` field shows which fired. Batch rebuild from `logs/fly-feedback.jsonl`: `go run ./cmd/fly-train`.

Lesson rule: only report confirmed outcomes. Never feed Fly its own unverified recommendations as ground truth.

## Logging

Every fly call appends an event to `logs/fly-events.jsonl` (route, encoder, scores, recommendations, task) and, when Postgres is configured, a row in `fly_predictions`. The demo dashboard reads the JSONL file for the ranking panel.
