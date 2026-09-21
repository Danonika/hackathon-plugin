# Go Recipe (go-telegram-bot-api, Bot API 6.x-era)

Drift-prone sketch, not a pinned reference: verify the current library API before
shipping. The module path is still
`github.com/go-telegram-bot-api/telegram-bot-api/v5` (latest tag `v5.5.1`,
December 2021) while some Bot API 6.x-era fields live on `master`. Pin a
pseudo-version if you need them, read the vendored source and compile early.
Snippets below stay on the stable v5 API surface.

## Environment Loading

```go
func env(key string) string {
    v := os.Getenv(key)
    if v == "" { panic("missing required env: " + key) }
    return v
}
```

Read `TELEGRAM_BOT_TOKEN`, `TELEGRAM_CHAT_ID`, `TELEGRAM_ALLOWED_CHAT_IDS`; keep
`.env.example` in sync and never log the token.

## Send

```go
import (
    "strconv"
    "strings"

    tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func escapeHTML(s string) string {
    return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

func chunk(text string, limit int) []string {
    if limit <= 0 { limit = 4096 }
    runes := []rune(text)
    var out []string
    for len(runes) > limit {
        cut := limit
        for i := limit; i > limit/2; i-- {
            if runes[i-1] == '\n' { cut = i; break }
        }
        out, runes = append(out, string(runes[:cut])), runes[cut:]
    }
    if len(runes) > 0 { out = append(out, string(runes)) }
    return out
}

func Send(bot *tgbotapi.BotAPI, chatID int64, html string) error {
    parts := chunk(html, 4096)
    for i, part := range parts {
        if len(parts) > 1 {
            part = "(" + strconv.Itoa(i+1) + "/" + strconv.Itoa(len(parts)) + ") " + part
        }
        msg := tgbotapi.NewMessage(chatID, part)
        msg.ParseMode = tgbotapi.ModeHTML
        msg.DisableNotification = true
        if _, err := bot.Send(msg); err != nil { return err }
    }
    return nil
}
```

Escape every dynamic field before it enters `html`.

## Long-Poll Router with Commands

```go
func runPolling(bot *tgbotapi.BotAPI, allowed map[int64]bool) {
    u := tgbotapi.NewUpdate(0)
    u.Timeout = 60
    for update := range bot.GetUpdatesChan(u) {
        if update.Message == nil {
            continue
        }
        chatID := update.Message.Chat.ID
        if !allowed[chatID] && !isChatAllowed(chatID) {
            reply(bot, update.Message, "not authorized")
            continue
        }
        switch update.Message.Command() {
        case "status":
            reply(bot, update.Message, "queue: 0\nlast event: none")
        case "subscribe":
            _ = store.Subscribe(chatID, update.Message.From.ID)
            reply(bot, update.Message, "subscribed")
        case "unsubscribe":
            _ = store.Unsubscribe(chatID)
            reply(bot, update.Message, "unsubscribed")
        case "help":
            reply(bot, update.Message, "/status /subscribe /unsubscribe /help")
        default:
            reply(bot, update.Message, "unknown command")
        }
    }
}
```

`reply` mirrors the chat, and on `master` also sets
`msg.MessageThreadID = update.Message.MessageThreadID`, so answers land in the
right forum topic.

## Webhook Handler

Set the webhook once, preferably with Hackathon Agent Kit:
`telegram.manage action: "set_webhook" url: "https://host/telegram/webhook"
secret_token: "<random>"`. The library's `ListenForWebhook` does not check the
secret header, so verify it in your own handler:

```go
http.HandleFunc("/telegram/webhook", func(w http.ResponseWriter, r *http.Request) {
    if r.Header.Get("X-Telegram-Bot-Api-Secret-Token") != secret {
        http.Error(w, "unauthorized", http.StatusUnauthorized); return
    }
    var update tgbotapi.Update
    if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&update); err != nil {
        http.Error(w, "bad request", http.StatusBadRequest); return
    }
    go dispatch(context.Background(), bot, update) // same dispatch as polling
    w.WriteHeader(http.StatusOK)
})
http.ListenAndServeTLS(":8443", "cert.pem", "key.pem", nil)
```

## httptest Note

`tgbotapi.NewBotAPIWithClient(token, apiEndpoint, client)` calls `getMe`, so the
test server must answer that method first; pass `server.URL+"/bot%s/%s"` as the
endpoint. Then assert outgoing `sendMessage` payloads without real network:

```go
server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    if strings.HasSuffix(r.URL.Path, "/getMe") {
        _, _ = w.Write([]byte(`{"ok":true,"result":{"id":1,"username":"testbot","first_name":"T"}}`))
        return
    }
    sends++
    _, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
}))
defer server.Close()
bot, err := tgbotapi.NewBotAPIWithClient("token", server.URL+"/bot%s/%s", server.Client())
if err != nil { t.Fatal(err) }
if err := Send(bot, 42, strings.Repeat("x", 9000)); err != nil { t.Fatal(err) }
if sends != 3 { t.Fatalf("expected 3 chunks, got %d", sends) }
```
