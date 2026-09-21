package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeBot struct {
	mu       sync.Mutex
	methods  []string
	payloads []map[string]any
	handler  func(call int, method string, payload map[string]any) (int, string)
}

func (f *fakeBot) server(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		method := parts[len(parts)-1]
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		f.mu.Lock()
		f.methods = append(f.methods, method)
		f.payloads = append(f.payloads, payload)
		call := len(f.methods)
		f.mu.Unlock()
		status, body := 200, `{"ok":true,"result":{}}`
		if f.handler != nil {
			status, body = f.handler(call, method, payload)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

func (f *fakeBot) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.methods)
}

func (f *fakeBot) lastPayload() map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.payloads) == 0 {
		return nil
	}
	return f.payloads[len(f.payloads)-1]
}

func testClient(t *testing.T, fake *fakeBot, settings Settings) *Client {
	t.Helper()
	server := fake.server(t)
	if settings.BotToken == "" {
		settings.BotToken = "test-token"
	}
	settings.BaseURL = server.URL
	return New(settings).WithRateLimit(1000).WithRetryDelay(time.Millisecond)
}

func TestSendMessageChunksLongText(t *testing.T) {
	fake := &fakeBot{handler: func(call int, method string, payload map[string]any) (int, string) {
		return 200, fmt.Sprintf(`{"ok":true,"result":{"message_id":%d}}`, call)
	}}
	client := testClient(t, fake, Settings{ChatID: "42"})
	result, err := client.SendMessage(context.Background(), strings.Repeat("a", MaxMessageChars+10), SendOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if fake.count() != 2 || result.Chunks != 2 {
		t.Fatalf("calls=%d chunks=%d, want 2/2", fake.count(), result.Chunks)
	}
	if result.MessageID != 2 {
		t.Fatalf("message_id = %d, want 2 (last chunk)", result.MessageID)
	}
	if payload := fake.lastPayload(); payload["chat_id"] != "42" {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestSendMessageAllowlist(t *testing.T) {
	fake := &fakeBot{}
	client := testClient(t, fake, Settings{ChatID: "42", AllowedChatIDs: []string{"99"}})

	if _, err := client.SendMessage(context.Background(), "hi", SendOptions{ChatID: "77"}); err == nil || !strings.Contains(err.Error(), "TELEGRAM_ALLOWED_CHAT_IDS") {
		t.Fatalf("unlisted chat must be rejected, got %v", err)
	}
	if _, err := client.SendMessage(context.Background(), "hi", SendOptions{ChatID: "99"}); err != nil {
		t.Fatalf("allowlisted chat rejected: %v", err)
	}
	if _, err := client.SendMessage(context.Background(), "hi", SendOptions{}); err != nil {
		t.Fatalf("default chat rejected: %v", err)
	}
	if fake.count() != 2 {
		t.Fatalf("calls = %d, want 2", fake.count())
	}
}

func TestSendMessageRetriesOn429(t *testing.T) {
	fake := &fakeBot{handler: func(call int, method string, payload map[string]any) (int, string) {
		if call == 1 {
			return 429, `{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":0}}`
		}
		return 200, `{"ok":true,"result":{"message_id":7}}`
	}}
	client := testClient(t, fake, Settings{ChatID: "42"})
	result, err := client.SendMessage(context.Background(), "hello", SendOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if fake.count() != 2 || result.MessageID != 7 {
		t.Fatalf("calls=%d message_id=%d", fake.count(), result.MessageID)
	}
}

func TestErrorsNeverContainToken(t *testing.T) {
	fake := &fakeBot{handler: func(call int, method string, payload map[string]any) (int, string) {
		return 401, `{"ok":false,"error_code":401,"description":"Unauthorized"}`
	}}
	client := testClient(t, fake, Settings{})
	_, err := client.GetMe(context.Background())
	if err == nil {
		t.Fatal("expected an API error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != 401 {
		t.Fatalf("expected an APIError with code 401, got %v", err)
	}
	if strings.Contains(err.Error(), "test-token") {
		t.Fatalf("token leaked into error: %v", err)
	}
}

func TestRequestErrorDoesNotLeakToken(t *testing.T) {
	client := New(Settings{BotToken: "secret-token", BaseURL: "http://127.0.0.1:1", Timeout: time.Second})
	if _, err := client.GetMe(context.Background()); err == nil {
		t.Fatal("expected a connection error")
	} else if strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("token leaked into error: %v", err)
	}
}

func TestValidateParsesBotChatAndUpdates(t *testing.T) {
	fake := &fakeBot{handler: func(call int, method string, payload map[string]any) (int, string) {
		switch method {
		case "getMe":
			return 200, `{"ok":true,"result":{"id":1,"username":"hackathonkit_bot","first_name":"Hackathon Agent Kit"}}`
		case "getChat":
			return 200, `{"ok":true,"result":{"id":-100123,"type":"supergroup","title":"Demo Team"}}`
		case "getUpdates":
			return 200, `{"ok":true,"result":[{"update_id":11,"message":{"chat":{"id":-100123,"type":"supergroup","title":"Demo Team"},"from":{"id":555},"text":"/status"}}]}`
		}
		return 200, `{"ok":true,"result":{}}`
	}}
	client := testClient(t, fake, Settings{})
	bot, err := client.GetMe(context.Background())
	if err != nil || bot.Username != "hackathonkit_bot" {
		t.Fatalf("getMe = %+v err=%v", bot, err)
	}
	chat, err := client.GetChat(context.Background(), "-100123")
	if err != nil || chat.Title != "Demo Team" || chat.Type != "supergroup" {
		t.Fatalf("getChat = %+v err=%v", chat, err)
	}
	updates, err := client.GetUpdates(context.Background(), 0, 5, 0)
	if err != nil || len(updates) != 1 {
		t.Fatalf("getUpdates = %+v err=%v", updates, err)
	}
	if updates[0].ChatID != "-100123" || updates[0].FromID != "555" || updates[0].Text != "/status" {
		t.Fatalf("update = %+v", updates[0])
	}
}

func TestSetMyCommandsAndWebhook(t *testing.T) {
	fake := &fakeBot{}
	client := testClient(t, fake, Settings{})
	if err := client.SetMyCommands(context.Background(), nil); err == nil {
		t.Fatal("empty command list must fail")
	}
	if err := client.SetMyCommands(context.Background(), []BotCommand{{Command: "status", Description: "Show product status"}}); err != nil {
		t.Fatal(err)
	}
	if payload := fake.lastPayload(); payload["commands"] == nil {
		t.Fatalf("payload = %+v", payload)
	}

	if err := client.SetWebhook(context.Background(), "http://insecure.example/hook", ""); err == nil {
		t.Fatal("http webhook must be rejected")
	}
	if err := client.SetWebhook(context.Background(), "https://demo.example/telegram", "s3cret"); err != nil {
		t.Fatal(err)
	}
	if payload := fake.lastPayload(); payload["url"] != "https://demo.example/telegram" || payload["secret_token"] != "s3cret" {
		t.Fatalf("webhook payload = %+v", payload)
	}
	if err := client.DeleteWebhook(context.Background(), true); err != nil {
		t.Fatal(err)
	}
}

func TestUnconfiguredClient(t *testing.T) {
	client := New(Settings{})
	if client.Configured() {
		t.Fatal("empty settings must not be configured")
	}
	if _, err := client.SendMessage(context.Background(), "hi", SendOptions{}); err == nil || !strings.Contains(err.Error(), "TELEGRAM_BOT_TOKEN") {
		t.Fatalf("err = %v", err)
	}
}

func TestChunkTextAndEscapeHTML(t *testing.T) {
	chunks := ChunkText(strings.Repeat("x", 10)+"\n"+strings.Repeat("y", 10), 12)
	if len(chunks) != 2 || chunks[0] != strings.Repeat("x", 10)+"\n" {
		t.Fatalf("chunks = %#v", chunks)
	}
	if got := EscapeHTML("<b>&</b>"); got != "&lt;b&gt;&amp;&lt;/b&gt;" {
		t.Fatalf("EscapeHTML = %q", got)
	}
}
