# Bot Setup

Everything here happens in the team's project. Hackathon Agent Kit's `telegram.*` tools are
the setup and test console; they are not the product runtime.

## 1. Create the Bot in BotFather

1. Open Telegram, start a chat with `@BotFather`, send `/newbot`.
2. Provide a display name and a username that ends in `bot` (must be unique).
3. Copy the token exactly once; BotFather can re-show it with `/token` or reset
   it with `/revoke` (a revoke invalidates the old token immediately).
4. Optional housekeeping in BotFather:
   - `/setdescription` and `/setabouttext` for the profile;
   - `/setuserpic` for the avatar;
   - `/setcommands` to seed the command menu (or do it with
     `telegram.manage action: "set_commands"`, see below);
   - `/setprivacy` controls whether the bot in a group sees all messages
     (`Disable`) or only commands and mentions (`Enable`, the default).
   - `/deletebot` removes the bot and its token.

Multiple environments: create one bot for local/demo and, if needed, a second
for production. Never share one token across teams.

## 2. Store the Token Safely

Write the values into the project `.env` (never into source, README or chat):

```dotenv
TELEGRAM_BOT_TOKEN=123456:replace-with-botfather-token
TELEGRAM_CHAT_ID=-1001234567890
TELEGRAM_ALLOWED_CHAT_IDS=-1001234567890,123456789
```

- `TELEGRAM_CHAT_ID` is the default destination for `telegram.send`.
- `TELEGRAM_ALLOWED_CHAT_IDS` is a comma-separated list of extra chats/topics
  that may receive; the default chat is always allowed.
- Add the same keys with empty values to `.env.example` and confirm `.env` is in
  `.gitignore`. Commit `.env.example`, never `.env`.
- The Hackathon Agent Kit MCP server loads `.env` and snapshots the environment at startup.
  After editing, restart the MCP server (or export the variables before launch),
  otherwise `telegram.*` still sees the old token.

## 3. Discover Chat and Topic Ids

1. Start a chat with the bot (or add it to the group) and send any message, for
   example `hello`. A bot cannot message a user who never started it.
2. Call `telegram.validate` (optional `chat_id`). It runs getMe, getChat and
   getUpdates and returns:
   - `bot`: id, username, first name;
   - `default_chat_id`: the configured `TELEGRAM_CHAT_ID`;
   - `chat`: resolved metadata for the requested/default id;
   - `recent_chats`: up to five `{chat_id, type, name}` entries from recent
     updates.
3. For the raw update list call `telegram.manage` with
   `action: "get_updates"`, `limit: 10`; each update carries `update_id`,
   `chat_id`, `chat_type`, `chat_name`, `from_id` and `text`.
4. Put the desired id into `TELEGRAM_CHAT_ID` (or the allowlist) and re-run
   `telegram.validate` to confirm `chat_error` is absent.

Group vs DM vs topic:

- Private chat ids are positive (`123456789`); groups, supergroups and channels
  are negative (`-1001234567890`).
- A forum topic is not a separate chat. The group keeps one chat id and each
  topic message carries a `message_thread_id` in the raw Bot API update. Pass
  that value from the project's code when replying into a topic; the allowlist
  still applies to the group chat id.

## 4. Group Admin and Privacy Mode

- Promote the bot to admin only when it must delete/pin messages, or when the
  group restricts member posting. Otherwise a normal member with privacy mode
  disabled is enough.
- Privacy mode: with `Enable`, the bot in a group only receives commands
  addressed to it (`/status@BotName`), mentions and replies; with `Disable` it
  receives all group messages. Keep the default unless the product needs to
  observe everything, and say so in the README.
- After changing privacy mode, remove and re-add the bot (or restart the group
  session) for the setting to take effect.
- If the bot is removed from the group, sends fail with 400 `chat not found` or
  403; re-add it and re-run `telegram.validate`.

## 5. Seed the Command Menu

Call `telegram.manage` with `action: "set_commands"` and a list:

```json
{"action": "set_commands", "commands": [
  {"command": "status", "description": "Show service health and queue depth"},
  {"command": "subscribe", "description": "Enable notifications for this chat"},
  {"command": "unsubscribe", "description": "Disable notifications for this chat"},
  {"command": "help", "description": "List available commands"}
]}
```

The result echoes `commands` and `count`. Telegram limits commands to 100, and
every entry needs both `command` and `description`. Keep command names lowercase
and free of spaces.

## 6. Setup Checklist

- [ ] Bot created and token stored in `.env` only.
- [ ] `.env.example` has placeholder keys; `.env` is gitignored.
- [ ] `telegram.validate` returns `status: "ok"` with the right bot username.
- [ ] `telegram.send` delivers to the intended chat.
- [ ] Command menu set with `telegram.manage set_commands`.
- [ ] Bot added to the group; privacy mode and admin rights recorded in the README.
- [ ] MCP server restarted after the last `.env` edit, so `telegram.*` sees it.
