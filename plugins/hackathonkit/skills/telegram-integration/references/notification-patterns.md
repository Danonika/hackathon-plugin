# Notification Patterns

Decide the pattern before writing code. The right choice is the one the README
can describe in one sentence and a judge can see working in the demo.

## Alert vs Digest

| Dimension | Alert | Digest |
|---|---|---|
| Trigger | One event, immediately relevant | A time window (hour, day) or a batch size |
| Volume | Low: boundaries crossed, failures, payments | High: every record, metric point or log line |
| Message | What happened, when, the entity id, next step | Counts, top items, links to detail |
| Noise risk | High if dedupe is missing | Low; bounded by the schedule |
| User expectation | Interrupts (`silent: false`) | Quotidian (`silent: true` is fine) |
| Demo value | Visible immediately | Needs a manual run or a short window |

Rules of thumb:

- Default high-frequency events to a digest; reserve alerts for pages a human
  must act on.
- A digest should state the window (`last 60 min`), the total, and the worst
  offenders; empty windows can be skipped or reported as `no activity`.
- An alert must be actionable: include the dedupe key so a human can reference
  the same incident after it repeats.

## Dedupe Keys

Build a stable key from the event class and identity, never from the timestamp
alone:

```text
<event_type>:<entity_id>[:<bucket>]
payment_failed:order-1024
webhook_error:service-billing:2026-09-21T14   # hourly bucket
deploy_failed:commit-abc123
```

- Keep a last-sent map `key -> timestamp` (JSON file, SQLite table or Redis key)
  shared by every process that can send.
- Cooldown: suppress the same key for a window (for example 15 min for errors,
  24 h for a daily condition); increment a suppressed counter and fold it into
  the next message as `(+3 similar)`.
- Resolution event: send a closing message when the incident clears and drop the
  key, so the next occurrence alerts again.
- For digests the dedupe key is the window; at most one message per window per
  chat.

## Retry and Backoff

- 429: read `retry_after` and wait exactly that many seconds before one retry.
  The Hackathon Agent Kit client does this automatically for `telegram.send`; the project's
  runtime needs the same logic.
- 5xx and network timeouts: retry with exponential backoff (1s, 2s, 4s, ...)
  plus jitter, capped at a small number of attempts (3-4); then log and drop.
- 4xx (except 429): do not retry; a bad chat id or blocked bot will fail again.
- Make sends idempotent where it matters: a retry after an uncertain failure can
  duplicate a message, so prefer "send once, record the result" with a visible
  counter over blind retries.

## Rate Limits

- Telegram constraints: about 20 messages per minute to one group and about 30
  messages per second overall (broadcast target); the practical pace for a
  hackathon bot is far lower.
- `telegram.send` enforces a local 20 sends/minute limiter and waits when full.
- The project's runtime must rate-limit itself: a token-per-second bucket per
  chat plus a global cap, and a queue so bursts do not become 429s.
- Chunked messages count per chunk, not per logical message: a 10,000-character
  alert is three API calls.

## Formatting

- Plain text by default; request `parse_mode: "HTML"` only when formatting is
  needed. The supported subset is limited (`<b>`, `<i>`, `<u>`, `<s>`,
  `<code>`, `<pre>`, `<a href="...">`).
- Escape dynamic text before interpolation: `&` -> `&amp;`, `<` -> `&lt;`,
  `>` -> `&gt;`. The Hackathon Agent Kit client exposes the same rule; do it in the
  project's code too.
- Never build markup from raw user input or event payloads without escaping; an
  unescaped `<` makes the whole send fail with 400 `can't parse entities`.
- Keep lines short; Telegram clients do not wrap preformatted blocks, so a
  monolithic `<pre>` line is unreadable on mobile.

## Chunking

- Hard limit: 4096 characters per message. `telegram.send` chunks long text
  automatically and reports `chunks`; the project must do the same.
- Prefer newline boundaries within the last half of the window, and prefix
  continuations with `(2/3)` so readers can reassemble the order.
- Keep a send's chunks together in time; interleaving another alert between them
  scrambles the story.

## Choosing a Pattern

| Event shape | Pattern |
|---|---|
| Payment failed, threshold crossed, job crashed | Alert with dedupe key and cooldown |
| Hourly metrics, daily report, new rows | Digest on a schedule |
| Both: volume plus a few critical events | Digest for the stream, alert for the critical subset |
| Repeated identical failure | Dedupe plus `(+N similar)` counter, alert only on state change |
| Demo with no traffic | Trigger the sender manually or shorten the digest window |

Whichever pattern is chosen, record it in the project README with the window,
dedupe key shape and rate-limit behavior.
