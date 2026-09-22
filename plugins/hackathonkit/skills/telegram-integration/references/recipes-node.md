# Node Recipe (grammY or telegraf)

Drift-prone sketch: both libraries rename things between majors; verify the installed API before shipping.

## Environment Loading

```js
import "dotenv/config"; // or your own loader

const token = process.env.TELEGRAM_BOT_TOKEN;
if (!token) throw new Error("TELEGRAM_BOT_TOKEN is required");

const allowedChats = new Set(
  [process.env.TELEGRAM_CHAT_ID, ...(process.env.TELEGRAM_ALLOWED_CHAT_IDS ?? "").split(",")]
    .map((v) => v?.trim()).filter(Boolean),
);
```

Packages: `grammy`, optional `dotenv`; telegraf uses `import { Telegraf } from "telegraf"`.
Never log the token; keep `.env.example` updated.

## Send with Escaping and Chunking (grammY)

```js
function escapeHTML(s) {
  return s.replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;");
}

function chunk(text, limit = 4096) {
  const out = [];
  let rest = text;
  while (rest.length > limit) {
    let cut = limit;
    for (let i = limit; i > limit / 2; i--) {
      if (rest[i - 1] === "\n") { cut = i; break; }
    }
    out.push(rest.slice(0, cut));
    rest = rest.slice(cut);
  }
  return rest.length ? [...out, rest] : out;
}

export async function send(chatId, text, { threadId, silent = true } = {}) {
  const parts = chunk(escapeHTML(text));
  for (const [i, part] of parts.entries()) {
    const body = parts.length > 1 ? `(${i + 1}/${parts.length}) ${part}` : part;
    await bot.api.sendMessage(chatId, body, {
      parse_mode: "HTML",
      disable_notification: silent,
      ...(threadId ? { message_thread_id: threadId } : {}),
    });
  }
}
```

Keep to a small HTML subset: `<b>`, `<i>`, `<code>`, `<pre>`, `<a href>`.
Escape every dynamic value, including error messages built from user input.

## Polling Router with Commands (grammY)

```js
import { Bot } from "grammy";

export const bot = new Bot(token);

bot.on("message:text", async (ctx, next) => {
  if (!allowedChats.has(String(ctx.chat.id))) {
    await ctx.reply("not authorized");
    return;
  }
  await next();
});

bot.command("status", (ctx) => ctx.reply("queue: 0\nlast event: none"));
bot.command("subscribe", async (ctx) => {
  await store.subscribe(ctx.chat.id, ctx.from?.id); // idempotent upsert
  await ctx.reply(`subscribed ${ctx.chat.id}`);
});
bot.command("unsubscribe", async (ctx) => {
  await store.unsubscribe(ctx.chat.id);
  await ctx.reply(`unsubscribed ${ctx.chat.id}`);
});
bot.command("help", (ctx) => ctx.reply("/status /subscribe /unsubscribe /help"));
bot.on("message:text", (ctx) => ctx.reply("unknown command"));

await bot.start(); // long polling; no public URL needed
```

`ctx.chat.id` is the allowlist key; `ctx.message_thread_id` (when present) is the
topic for forum groups. Keep the store shared with the notifier.

## Webhook Handler (grammY)

```js
import express from "express";
import { webhookCallback } from "grammy";

const app = express();
app.use(express.json());
app.use(
  "/telegram/webhook",
  webhookCallback(bot, "express", { secretToken: process.env.TELEGRAM_WEBHOOK_SECRET }),
);
app.listen(3000, async () => {
  await bot.api.setWebhook("https://your-host/telegram/webhook", {
    secret_token: process.env.TELEGRAM_WEBHOOK_SECRET,
    allowed_updates: ["message"],
  });
});
```

You can set the webhook with Hackathon Agent Kit instead (`telegram.manage action:
"set_webhook"`); the `secret_token` must match between Telegram, `.env` and the
handler, and switching back to polling needs `telegram.manage action:
"delete_webhook"`. grammY checks the `X-Telegram-Bot-Api-Secret-Token` header for
you; telegraf's `webhookCallback` does not, so add a 401 middleware.

## telegraf Variant

```js
import { Telegraf } from "telegraf";

const bot = new Telegraf(process.env.TELEGRAM_BOT_TOKEN);
bot.command("status", (ctx) => ctx.reply("queue: 0"));

await bot.telegram.sendMessage(chatId, text, { parse_mode: "HTML" });
await bot.telegram.setWebhook("https://your-host/telegram/webhook", {
  secret_token: process.env.TELEGRAM_WEBHOOK_SECRET,
});
await bot.launch({
  webhook: {
    domain: "https://your-host",
    hookPath: "/telegram/webhook",
    secretToken: process.env.TELEGRAM_WEBHOOK_SECRET,
  },
});
```

Polling with telegraf is `bot.launch()` with no webhook options; never call
`launch()` twice in one process.

## Test Note

- Extract command logic into plain functions (`onStatus(state)`,
  `onSubscribe(store, chatId)`) and unit-test those without Telegram.
- grammY does not hit the network until `start()`/`init()`: craft an update and
  call `bot.handleUpdate(update)` with `bot.botInfo` set, or intercept calls
  with `bot.api.config.use(...)` to assert the payload.
- With telegraf, use `bot.handleUpdate(update)` and stub `bot.telegram.callApi`;
  `node --test` (or vitest/jest) keeps tests independent of network egress.
