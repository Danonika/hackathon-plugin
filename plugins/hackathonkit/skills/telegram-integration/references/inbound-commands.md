# Inbound Commands

Inbound means chat -> project. Keep the surface tiny, read-only and
idempotent; the demo only needs four commands.

## Command Semantics

| Command | Purpose | Reply | Side effects |
|---|---|---|---|
| `/status` | Prove the service is alive and show load | Service name, uptime, queue depth, last event time | None (read-only query) |
| `/subscribe` | Opt this chat into notifications | `subscribed` plus the current chat id and categories | Adds a row to the subscription store; answering twice must not create two rows |
| `/unsubscribe` | Opt this chat out | `unsubscribed` plus the chat id | Removes (or marks inactive) the row; answering twice is `already unsubscribed` |
| `/help` | List commands and the privacy note | The command list with one-line descriptions | None |

Guidelines:

- A bot can only reply to a chat that started it; unknown chats cannot be
  messaged, so `/subscribe` is always initiated by the user, never by the
  service.
- Include the resolved `chat_id` in the reply; it lets the operator verify the
  allowlist quickly.
- Optional `/start` behaves like `/help` for first contact. Keep every other
  command rejected with a short `unknown command` reply.

## Allowlist

Two layers, both deny-by-default:

1. Chat allowlist: the incoming `chat.id` must be the configured chat or one of
   `TELEGRAM_ALLOWED_CHAT_IDS`. Parsing rule: split on commas, trim spaces,
   ignore empty entries.
2. User allowlist (private chats and admin actions): store allowed
   `from.id` values in the project config (for example
   `TELEGRAM_ALLOWED_USER_IDS` in `.env` or a `config/allowed_users.json`).
   A group message is checked by chat id; a DM is checked by user id.

Refusal behavior: reply `not authorized` and log the chat id; never include
internal state, file paths or secrets. If the allowlist is empty in a group, the
bot refuses everyone, which is safe but makes the demo look broken; seed it
before the demo and say so in the README. Do not accept every chat just to make
demoing easier.

## Subscription Storage

Pick the smallest store that survives a restart; the demo only needs one.

| Option | When | Shape |
|---|---|---|
| JSON file | Single process, hackathon default | `{"chats": {"-100123": {"subscribed": true, "categories": ["alerts"], "updated_at": "..."}}}` |
| SQLite | Multiple processes or queries | table `subscriptions(chat_id TEXT PRIMARY KEY, thread_id INTEGER, subscribed INTEGER, categories TEXT, updated_at TEXT)` |
| Postgres (compose service) | Product already uses it | same columns as SQLite, with `ON CONFLICT (chat_id) DO UPDATE` |
| Redis | Ephemeral demo | set `subs:<chat_id>` or a hash per chat |

Rules:

- Key by `chat_id` (and `thread_id` only if topics are supported); subscribing
  overwrites, unsubscribing updates the row - never duplicate.
- The notifier reads the same store to decide recipients; the digest sender and
  the webhook handler must not keep separate copies.
- Record `updated_at` and the originating user id for auditing.
- Never store message text or personal data you do not need.

## Idempotency

- Telegram may redeliver an update if the bot does not acknowledge it; process
  each `update_id` at most once. Keep the last processed `update_id` (or a small
  seen-set) in the subscription store and skip repeats.
- Commands that change state must be idempotent: `/subscribe` twice yields one
  row, `/unsubscribe` twice yields `already unsubscribed`.
- With long polling, advance the offset after a successful handle
  (`offset = update_id + 1`); with webhooks, return HTTP 200 quickly and let
  Telegram stop retrying.

## Polling vs Webhook

| Aspect | Long polling | Webhook |
|---|---|---|
| Requirement | Outbound HTTPS to `api.telegram.org` | Public HTTPS endpoint with a valid certificate |
| Setup | None in the project beyond the token | `telegram.manage action: "set_webhook"` `url: "https://..."`, `secret_token: "<random>"` |
| Local demo | Works behind NAT and on a laptop | Needs a tunnel or deployed host |
| Secret check | Not applicable | Compare `X-Telegram-Bot-Api-Secret-Token` with the configured `secret_token`; reject mismatches with 401 |
| Failure mode | Two pollers -> 409 `terminated by other getUpdates` | URL/secret mismatch -> updates never arrive; wrong response code -> retries |
| Cleanup | Stop the poller | `telegram.manage action: "delete_webhook"`, `drop_pending: true` when switching back |

Never run both at once: a webhook makes `getUpdates` (and the project's poller)
return nothing useful, and two pollers fight over the stream. Record the chosen
transport in the README.

## Inbound Checklist

- [ ] Four commands implemented; unknown commands get a short refusal.
- [ ] Allowlist checked before any reply or state change.
- [ ] Subscription store chosen and shared with the notifier.
- [ ] `/subscribe` and `/unsubscribe` are idempotent.
- [ ] Duplicate `update_id`s skipped.
- [ ] Polling or webhook chosen; webhook has a `secret_token` and header check.
- [ ] Refusals and state changes logged without secrets.
