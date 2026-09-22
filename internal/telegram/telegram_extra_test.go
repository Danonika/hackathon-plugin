package telegram

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSettingsFromEnv(t *testing.T) {
	t.Setenv("TELEGRAM_API_BASE", " http://localhost:8081 ")
	t.Setenv("TELEGRAM_TIMEOUT_SEC", "30")
	t.Setenv("TELEGRAM_BOT_TOKEN", " token-1 ")
	t.Setenv("TELEGRAM_CHAT_ID", " 42 ")
	t.Setenv("TELEGRAM_ALLOWED_CHAT_IDS", " 7, 8 ,,9 ")

	settings := SettingsFromEnv()
	if settings.BaseURL != "http://localhost:8081" || settings.Timeout != 30*time.Second {
		t.Fatalf("settings = %+v", settings)
	}
	if settings.BotToken != "token-1" || settings.ChatID != "42" {
		t.Fatalf("credentials = %+v", settings)
	}
	if len(settings.AllowedChatIDs) != 3 || settings.AllowedChatIDs[0] != "7" || settings.AllowedChatIDs[2] != "9" {
		t.Fatalf("allowed = %v", settings.AllowedChatIDs)
	}
	if !settings.Configured() {
		t.Fatal("settings with token must be configured")
	}
}

func TestSettingsFromEnvDefaultsAndInvalidTimeout(t *testing.T) {
	t.Setenv("TELEGRAM_API_BASE", "")
	t.Setenv("TELEGRAM_TIMEOUT_SEC", "not-a-number")
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	t.Setenv("TELEGRAM_CHAT_ID", "")
	t.Setenv("TELEGRAM_ALLOWED_CHAT_IDS", "")

	settings := SettingsFromEnv()
	if settings.BaseURL != DefaultBaseURL || settings.Timeout != 15*time.Second {
		t.Fatalf("defaults = %+v", settings)
	}
	if len(settings.AllowedChatIDs) != 0 {
		t.Fatalf("allowed = %v", settings.AllowedChatIDs)
	}
	if settings.Configured() {
		t.Fatal("empty token must not be configured")
	}

	t.Setenv("TELEGRAM_TIMEOUT_SEC", "-5")
	if got := SettingsFromEnv().Timeout; got != 15*time.Second {
		t.Fatalf("negative timeout = %v", got)
	}
}

func TestClientAccessors(t *testing.T) {
	settings := Settings{BotToken: "t", ChatID: "42", BaseURL: "http://example.test"}
	client := New(settings)
	if client.Settings().ChatID != "42" || client.DefaultChatID() != "42" {
		t.Fatalf("accessors = %+v", client.Settings())
	}
	if !client.Configured() {
		t.Fatal("configured client")
	}

	custom := &http.Client{Timeout: time.Second}
	if got := client.WithHTTPClient(custom); got.http != custom {
		t.Fatal("WithHTTPClient must install the client")
	}
	if got := client.WithHTTPClient(nil); got.http != custom {
		t.Fatal("WithHTTPClient(nil) must keep the existing client")
	}
	if got := New(Settings{}).WithRateLimit(0); got.rateLimitPerMin != 20 {
		t.Fatalf("rate limit = %d", got.rateLimitPerMin)
	}
	if got := New(Settings{}).WithRetryDelay(0); got.retryDelay != time.Second {
		t.Fatalf("retry delay = %v", got.retryDelay)
	}
}

func TestWaitRateLimitCanceled(t *testing.T) {
	client := New(Settings{BotToken: "t", BaseURL: "http://example.test"}).WithRateLimit(1)
	if err := client.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := client.wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}

	client.sent = []time.Time{time.Now().Add(-2 * time.Minute)}
	if err := client.wait(context.Background()); err != nil {
		t.Fatalf("stale timestamps must be pruned: %v", err)
	}
}

func TestChatAllowedEmpty(t *testing.T) {
	client := New(Settings{ChatID: "42", AllowedChatIDs: []string{"7"}})
	if client.chatAllowed("") {
		t.Fatal("empty chat id must not be allowed")
	}
	if !client.chatAllowed("42") || !client.chatAllowed("7") {
		t.Fatal("configured chats must be allowed")
	}
}

func TestSendMessageOptionPayloads(t *testing.T) {
	fake := &fakeBot{handler: func(call int, method string, payload map[string]any) (int, string) {
		return 200, `{"ok":true,"result":"not-an-object"}`
	}}
	client := testClient(t, fake, Settings{ChatID: "42"})
	result, err := client.SendMessage(context.Background(), " hello ", SendOptions{
		ParseMode: "HTML", Silent: true, ReplyTo: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.MessageID != 0 {
		t.Fatalf("unparseable result must leave message id zero: %+v", result)
	}
	payload := fake.lastPayload()
	if payload["parse_mode"] != "HTML" || payload["disable_notification"] != true || payload["reply_to_message_id"] != float64(5) {
		t.Fatalf("payload = %+v", payload)
	}
	if payload["text"] != "hello" {
		t.Fatalf("text = %q", payload["text"])
	}
}

func TestSendMessageValidation(t *testing.T) {
	client := New(Settings{BotToken: "t", BaseURL: "http://example.test"})
	if _, err := client.SendMessage(context.Background(), "hi", SendOptions{}); err == nil || !strings.Contains(err.Error(), "TELEGRAM_CHAT_ID") {
		t.Fatalf("missing chat id err = %v", err)
	}
	client = New(Settings{BotToken: "t", ChatID: "42", BaseURL: "http://example.test"})
	if _, err := client.SendMessage(context.Background(), "   ", SendOptions{}); err == nil || !strings.Contains(err.Error(), "text is required") {
		t.Fatalf("empty text err = %v", err)
	}
}

func TestGetChatValidation(t *testing.T) {
	client := New(Settings{BotToken: "t", BaseURL: "http://example.test"})
	if _, err := client.GetChat(context.Background(), "  "); err == nil {
		t.Fatal("empty chat_id must fail")
	}
}

func TestGetUpdatesLimitsAndFallbacks(t *testing.T) {
	fake := &fakeBot{handler: func(call int, method string, payload map[string]any) (int, string) {
		return 200, `{"ok":true,"result":[
			{"update_id":1,"message":{"chat":{"id":5,"type":"private","username":"alice"},"from":{"id":9},"text":"hi"}},
			{"update_id":2}
		]}`
	}}
	client := testClient(t, fake, Settings{})
	updates, err := client.GetUpdates(context.Background(), 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if payload := fake.lastPayload(); payload["limit"] != float64(10) {
		t.Fatalf("limit payload = %+v", payload)
	}
	if len(updates) != 2 {
		t.Fatalf("updates = %+v", updates)
	}
	if updates[0].ChatName != "alice" || updates[0].ChatID != "5" || updates[0].FromID != "9" {
		t.Fatalf("first update = %+v", updates[0])
	}
	if updates[1].ChatID != "" || updates[1].Text != "" {
		t.Fatalf("messageless update = %+v", updates[1])
	}
	if _, err := client.GetUpdates(context.Background(), 0, 500, 0); err != nil {
		t.Fatal(err)
	}
	if payload := fake.lastPayload(); payload["limit"] != float64(10) {
		t.Fatalf("limit payload = %+v", payload)
	}
}

func TestGetUpdatesInvalidResult(t *testing.T) {
	fake := &fakeBot{handler: func(call int, method string, payload map[string]any) (int, string) {
		return 200, `{"ok":true,"result":"nope"}`
	}}
	client := testClient(t, fake, Settings{})
	if _, err := client.GetUpdates(context.Background(), 0, 5, 0); err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestSetMyCommandsLimits(t *testing.T) {
	client := New(Settings{BotToken: "t", BaseURL: "http://example.test"})
	if err := client.SetMyCommands(context.Background(), []BotCommand{{Command: "a", Description: " "}}); err == nil {
		t.Fatal("empty description must fail")
	}
	tooMany := make([]BotCommand, 101)
	for i := range tooMany {
		tooMany[i] = BotCommand{Command: "c", Description: "d"}
	}
	if err := client.SetMyCommands(context.Background(), tooMany); err == nil {
		t.Fatal("more than 100 commands must fail")
	}
}

func TestCallInvalidPayloadAndResponse(t *testing.T) {
	client := New(Settings{BotToken: "t", BaseURL: "http://example.test"})
	if _, err := client.call(context.Background(), "sendMessage", make(chan int)); err == nil || !strings.Contains(err.Error(), "invalid payload") {
		t.Fatalf("payload err = %v", err)
	}

	fake := &fakeBot{handler: func(call int, method string, payload map[string]any) (int, string) {
		return 200, "not json at all"
	}}
	client = testClient(t, fake, Settings{})
	if _, err := client.GetMe(context.Background()); err == nil || !strings.Contains(err.Error(), "invalid response") {
		t.Fatalf("response err = %v", err)
	}
}

func TestCallRejectedWithoutDescription(t *testing.T) {
	fake := &fakeBot{handler: func(call int, method string, payload map[string]any) (int, string) {
		return 500, `{"ok":false}`
	}}
	client := testClient(t, fake, Settings{})
	_, err := client.GetMe(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v", err)
	}
	if apiErr.Code != 500 || apiErr.Description != "request rejected" {
		t.Fatalf("apiErr = %+v", apiErr)
	}
	if got := apiErr.Error(); !strings.Contains(got, "500") {
		t.Fatalf("APIError.Error = %q", got)
	}
}

func TestCallWithRetryCanceled(t *testing.T) {
	fake := &fakeBot{handler: func(call int, method string, payload map[string]any) (int, string) {
		return 429, `{"ok":false,"error_code":429,"description":"slow down","parameters":{"retry_after":1}}`
	}}
	client := testClient(t, fake, Settings{})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := client.GetMe(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
}

func TestChunkTextBoundaries(t *testing.T) {
	if got := ChunkText("short", 0); len(got) != 1 || got[0] != "short" {
		t.Fatalf("default limit chunks = %#v", got)
	}
	exact := strings.Repeat("a", 24)
	chunks := ChunkText(exact, 12)
	if len(chunks) != 2 || chunks[0] != strings.Repeat("a", 12) || chunks[1] != strings.Repeat("a", 12) {
		t.Fatalf("exact chunks = %#v", chunks)
	}
	noNewline := ChunkText(strings.Repeat("b", 25), 10)
	if len(noNewline) != 3 || noNewline[0] != strings.Repeat("b", 10) {
		t.Fatalf("hard cut chunks = %#v", noNewline)
	}
	newlineAtLimit := strings.Repeat("c", 9) + "\n" + strings.Repeat("d", 5)
	chunks = ChunkText(newlineAtLimit, 10)
	if len(chunks) != 2 || chunks[0] != strings.Repeat("c", 9)+"\n" {
		t.Fatalf("newline-at-limit chunks = %#v", chunks)
	}
}
