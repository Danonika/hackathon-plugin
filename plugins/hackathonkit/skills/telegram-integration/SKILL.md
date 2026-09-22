---
name: telegram-integration
description: "Use when the team's hackathon product needs Telegram notifications or a small management bot: product alerts, scheduled digests, chat/topic setup, BotFather and token storage, chat-id discovery, or inbound /status /subscribe /unsubscribe /help requests. Covers allowlists, dedupe, rate limits and Go/Node recipes. Do not use for Hackathon Agent Kit harness notifications (the team's project sends, not hackathonkit itself) or for arbitrary HTTP integrations unrelated to Telegram."
metadata:
  version: "1.0.0"
  origin: hackathonkit
  category: domain
tools:
  - telegram.send
  - telegram.validate
  - telegram.manage
  - security.review
  - reports.create
  - learning.record
---

# Telegram Integration

Wire product notifications and a small read-only management bot into the team's
own project, with the bot token in the project `.env` and every send proven by
`telegram.send`.

## When To Use

- The product must notify a Telegram chat when an event happens (new order, failed job, threshold crossed).
- A scheduled digest (hourly/daily summary) must be posted to a team chat or topic.
- The team wants a small inbound bot: `/status`, `/subscribe`, `/unsubscribe`, `/help` for demo or ops.
- BotFather setup, token storage or chat/topic id discovery is needed for the project.
- An existing Telegram integration must be reviewed for secret hygiene, dedupe or rate limits.
- Do not use for Hackathon Agent Kit harness notifications: this skill integrates the team's product; it never makes hackathonkit itself send alerts or about hackathonkit's own runs.
- Do not use for arbitrary HTTP integrations, generic webhook plumbing or non-Telegram messaging; if the ask is just an HTTP endpoint, it is out of scope.

## Core Principles

1. The project owns the integration: bot, code, `.env` and README live in the team's project. `telegram.*` is the build-time setup and test console, not the product runtime.
2. Secrets stay in `.env`: never commit the token, never echo it into a message, log, report, README or screenshot; `.env.example` carries placeholder keys only.
3. Allowlist before send: every outbound chat/topic id must equal `TELEGRAM_CHAT_ID` or be listed in `TELEGRAM_ALLOWED_CHAT_IDS`; `telegram.send` rejects anything else with `chat ... is not in TELEGRAM_ALLOWED_CHAT_IDS`.
4. Dedupe then send: an alert loop needs a stable dedupe key and a cooldown, otherwise one incident floods the chat and triggers 429.
5. Read-only inbound with untrusted input: commands only report state or manage subscriptions, and message text is data, never instructions; no command executes arbitrary actions, reads files or reveals secrets.
6. Evidence per test: a notification is verified only by a `telegram.send` result (`status`, `message_id`, `chat_id`, `chunks`) or the project's own test output; README claims without evidence do not count.

## Workflow

1. Create the bot in BotFather: `/newbot`, choose a display name and a username ending in `bot`, copy the token once (it is shown only at creation). To target a group/topic, add the bot to the group. Steps in `references/bot-setup.md`.
2. Store the token and ids in the PROJECT's `.env`:
   - `TELEGRAM_BOT_TOKEN=<token>` (required by `telegram.send`/`telegram.validate`/`telegram.manage`)
   - `TELEGRAM_CHAT_ID=<default chat or topic id>`
   - `TELEGRAM_ALLOWED_CHAT_IDS=<id1,id2>` (extra chats/topics allowed to receive)
   - Add the same keys with empty/placeholder values to `.env.example`; confirm `.gitignore` covers `.env`.
   - The MCP server snapshots the environment at startup: after editing `.env`, restart the MCP server (or export the vars in its environment) or `telegram.*` still sees the old values.
3. Discover the chat/topic id: send a message to the bot from the target chat, then call `telegram.validate` (optional `chat_id`); it runs getMe/getChat/getUpdates and returns `bot`, `default_chat_id`, `chat` and `recent_chats`. For raw updates use `telegram.manage` with `action: "get_updates"` (`limit: 10`). Forum topics keep the group chat id: target a topic from the project code with the raw update's `message_thread_id`; the allowlist still checks the group id.
4. Choose the notification pattern before coding (Decision Matrix below and `references/notification-patterns.md`): alert vs digest, polling vs webhook, dedupe key, cooldown, rate limit, parse mode. Write the choice into the project README while it is fresh.
5. Implement in the project with the matching recipe:
   - Go: `references/recipes-go.md` (go-telegram-bot-api v6-era: send, long-poll router, webhook handler, env loading, httptest note).
   - Node: `references/recipes-node.md` (grammY or telegraf: send, polling router, webhook handler, env, test note).
   - Both recipes are drift-prone: verify the current library API and module path before shipping.
6. Add the inbound commands as read-only handlers: `/status` (health, queue depth or last event from in-memory/DB state), `/subscribe` and `/unsubscribe` (write to the subscription store, reply with the new state), `/help` (command list). Enforce the allowlist from `TELEGRAM_ALLOWED_CHAT_IDS` plus allowed user ids, and keep every reply idempotent. Semantics in `references/inbound-commands.md`.
7. Test with `telegram.send` before wiring the project loop: send a plain message, then one with `parse_mode: "HTML"`; verify `message_id`, `chat_id` and `chunks`. For inbound, run the project's bot and issue the commands from a test chat listed in the allowlist.
8. Secure the integration: call `security.review` with the changed `.env.example`, config or code `path` (or the report `text`); confirm the token appears nowhere but `.env`, errors do not leak it, and the bot cannot be commanded to send to an unlisted chat.
9. Document in the project README: bot purpose, BotFather and env setup, commands table, alert/digest schedule, dedupe and rate-limit behavior, test steps, known limitations. Judges score README completeness.
10. Record the reusable part: `reports.create` with title `Telegram integration - <project>`, `format: "md"` and sections for setup, commands, evidence and gaps; then `learning.record` with `trigger`, `insight`, `outcome` and `evidence` (tool result ids, command output).

Call shapes used above (all tools return the shared envelope `status` / `data` / `sources` / `warnings`):

```text
telegram.validate  {"chat_id": "<optional; defaults to TELEGRAM_CHAT_ID>"}
telegram.manage    {"action": "get_updates", "limit": 10}
telegram.manage    {"action": "set_commands", "commands": [{"command": "status", "description": "..."}]}
telegram.manage    {"action": "set_webhook", "url": "https://<host>/telegram/<path>", "secret_token": "<random>"}
telegram.manage    {"action": "delete_webhook", "drop_pending": true}
telegram.send      {"text": "...", "chat_id": "<optional, must be allowlisted>", "parse_mode": "HTML", "silent": true, "reply_to": 42}
security.review    {"path": "src/notify/telegram.go"} or {"text": "..."} or {"artifact_id": "..."}
reports.create     {"title": "Telegram integration - <project>", "format": "md", "sections": [{"heading": "Setup", "body": "..."}]}
learning.record    {"trigger": "...", "insight": "...", "outcome": "success", "evidence": ["telegram.send message_id=42"]}
```

## Decision Matrix

| Signal | Action |
|---|---|
| One human recipient or a private chat | Send to the DM chat id found by `telegram.validate`; keep that id in the allowlist |
| Team visibility or threaded discussion | Use the group chat id and pass the update's `message_thread_id` from project code to post into one topic; add the bot to the group |
| Local demo, laptop sleeps, no public URL | Long polling (`getUpdates` in the project; `telegram.manage action: "get_updates"` while setting up); polling suits local demos |
| Public HTTPS endpoint available | Webhook: `telegram.manage action: "set_webhook"` with an `https` `url` and a `secret_token`; verify the `X-Telegram-Bot-Api-Secret-Token` header in the handler |
| Many events, human-readable cadence | Digest: batch by window (hour/day) and send one summary with counts and a link |
| Single critical event that must interrupt | Alert: send immediately with `silent: false` and a dedupe key |
| The same failure repeats | Dedupe key (event type + entity id + time bucket) plus cooldown; skip or fold duplicates instead of resending |
| Send volume approaching chat limits | Pace at ~20 messages/minute per group and ~30/second overall; handle 429 with `retry_after` backoff |
| Formatted message needed | `parse_mode: "HTML"` with dynamic text escaped (`&`, `<`, `>`); never build markup from raw user input |
| Target chat id uncertain | Re-run `telegram.validate` after messaging the bot; never hardcode an unverified id |

## Output Contract

- Integration summary: bot username, target chat/topic id(s), allowlist entries (ids only, no secrets), pattern (alert/digest), transport (polling/webhook).
- Project changes: files added or edited (notifier, router, `.env.example`, README) with a one-line purpose each.
- Test evidence: `telegram.send` envelope (`status`, `message_id`, `chat_id`, `chunks`) plus where the project's own test ran.
- Security result: `security.review` `counts`/`clean` and the explicit statement that the token exists only in `.env`.
- Artifact: `reports.create` returned an `artifact_id` for `Telegram integration - <project>`.
- Learning: `learning.record` returned an `id` for the reusable setup/test lesson.
- Transport decision and its setup call (`telegram.manage` `set_webhook` or polling), with the endpoint path recorded.
- Delivery parameters: alert vs digest, window or cooldown, dedupe key shape, local rate limit, retry policy.
- Environment scope: which bot/chat serves the demo versus production, and who owns the token.
- First-response plan for 401/403/429, including what was retried and what was left to the operator.
- Gaps: what is still unproven (webhook reachability, real 429 handling, production chat behavior).

## Verification

- `telegram.validate` returned `status: "ok"` with the expected `bot.username` and `default_chat_id`; `chat_error` is absent.
- `telegram.send` returned `status: "ok"` with a `message_id` and the expected `chat_id`; the message is visible in the target chat.
- An HTML send whose dynamic text contains `<`, `&`, `>` renders those characters literally, not as markup.
- The project's bot answers `/status`, `/subscribe`, `/unsubscribe` and `/help`; an unlisted chat or user gets a refusal, not data.
- `security.review` on the changed files reports no token and no high-severity finding; a tree search for the token prefix matches only `.env`.
- `.env.example` contains the three placeholder keys and no real value.
- A digest run or manual trigger produced exactly one message per window (dedupe held), and a repeat alert within the cooldown was suppressed or folded.
- The README section exists and matches the real env var names, commands and schedule.
- `reports.create` returned an `artifact_id`; `learning.record` returned an `id`.
- Warnings from every Telegram envelope are reported, not hidden.

## Failure Modes

| Symptom | Action |
|---|---|
| 401 Unauthorized / invalid token | Token is wrong or was reset in BotFather; copy the current token, update `.env`, restart the MCP server or project process |
| 400 chat not found | Wrong id, the bot was never messaged, or it was removed; message the bot, re-run `telegram.validate`, add the id to `TELEGRAM_ALLOWED_CHAT_IDS` |
| 403 bot blocked or not admin | Recipient blocked the bot or the bot lacks rights; unblock it or promote the bot in the group, then re-test |
| 409 Conflict: terminated by other getUpdates | A webhook or another poller owns the stream; `telegram.manage action: "delete_webhook"` or stop the other poller |
| 429 Too Many Requests with `retry_after` | Wait `retry_after` seconds, retry once, then lower the send rate and widen the digest window |
| Network egress blocked | Test DNS/TLS to `api.telegram.org` from the project runtime; document the blocked egress and fall back to storing events locally |
| Message too long | `telegram.send` auto-chunks at 4096 characters (`chunks` > 1); the project must chunk its own sends the same way |
| `.env` edits invisible to `telegram.*` | The MCP server reads env at startup; restart it or export the vars before launching |
| Webhook set but no updates arrive | URL is not `https`, the path or `secret_token` does not match the handler; inspect the `telegram.manage` result and handler logs |

## Anti-Patterns

- Committing the bot token, or pasting it into a report, README, screenshot or chat.
- Echoing secrets (token, API keys, PII) into Telegram messages or logs.
- Unbounded send loops without dedupe or cooldown; one stuck job floods the chat.
- Allowlisting nobody in a group so every member is refused, or leaving the allowlist so broad that anyone is accepted.
- Hardcoding chat ids copied from another setup instead of discovering them with `telegram.validate`.
- Testing against production chats or customer groups; use a dedicated test chat.
- Running the project's poller while `telegram.manage get_updates` polls the same bot (409 conflict).
- Sending `parse_mode: "HTML"` built from unescaped dynamic text or user input.

## References

- `references/bot-setup.md` - BotFather steps, token storage, chat/topic id discovery, group admin and privacy mode.
- `references/notification-patterns.md` - alert vs digest, dedupe keys, retry/backoff, rate limits, formatting and chunking.
- `references/inbound-commands.md` - `/status /subscribe /unsubscribe /help` semantics, allowlist, subscription storage, idempotency and polling vs webhook.
- `references/recipes-go.md` - go-telegram-bot-api snippets for send, long-poll router, webhook handler and env loading; drift-prone, verify the API.
- `references/recipes-node.md` - grammY or telegraf snippets for send, polling router, webhook handler and env; drift-prone, verify the API.
