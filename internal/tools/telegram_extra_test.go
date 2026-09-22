package tools

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hackathonkit.local/agentkit/internal/mcp"
	"hackathonkit.local/agentkit/internal/telegram"
)

func telegramErrorFake(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":false,"error_code":500,"description":"boom"}`))
	}))
	t.Cleanup(server.Close)
	return server
}

func telegramClientRegistry(t *testing.T, settings telegram.Settings) *mcp.Registry {
	t.Helper()
	reg, _ := testRegistry(t)
	client := telegram.New(settings).WithRateLimit(1000).WithRetryDelay(time.Millisecond)
	registerTelegram(reg, Deps{Telegram: client})
	return reg
}

func TestTelegramConfiguredErrorPaths(t *testing.T) {
	server := telegramErrorFake(t)
	reg := telegramClientRegistry(t, telegram.Settings{
		BotToken: "test-token",
		ChatID:   "42",
		BaseURL:  server.URL,
		Timeout:  5 * time.Second,
	})

	cases := []struct {
		name string
		args map[string]any
	}{
		{"send", map[string]any{"text": "hello"}},
		{"validate", map[string]any{}},
		{"get_updates", map[string]any{"action": "get_updates"}},
		{"set_commands", map[string]any{"action": "set_commands", "commands": []any{map[string]any{"command": "a", "description": "b"}}}},
		{"set_webhook", map[string]any{"action": "set_webhook", "url": "https://example.com/hook"}},
		{"delete_webhook", map[string]any{"action": "delete_webhook", "drop_pending": true}},
	}
	names := map[string]string{
		"send": "telegram.send", "validate": "telegram.validate",
		"get_updates": "telegram.manage", "set_commands": "telegram.manage",
		"set_webhook": "telegram.manage", "delete_webhook": "telegram.manage",
	}
	for _, c := range cases {
		env := call(t, reg, names[c.name], c.args)
		want := "error"
		if c.name == "send" {
			// A partially delivered message reports partial, not error.
			want = "partial"
		}
		if env.Status != want {
			t.Fatalf("%s: status %s, want %s (%+v)", c.name, env.Status, want, env)
		}
		if c.name == "send" {
			if !strings.Contains(strings.Join(env.Warnings, " "), "partially failed") {
				t.Fatalf("send: warnings = %v", env.Warnings)
			}
		}
	}
}

func TestTelegramChatNotConfigured(t *testing.T) {
	reg := telegramClientRegistry(t, telegram.Settings{BotToken: "test-token"})
	env := call(t, reg, "telegram.send", map[string]any{"text": "hello"})
	if env.Status != "error" {
		t.Fatalf("missing chat id must fail: %+v", env)
	}
	if !strings.Contains(env.Data.(map[string]any)["error"].(string), "chat id is not configured") {
		t.Fatalf("error = %+v", env.Data)
	}
}

func TestTelegramNilClient(t *testing.T) {
	reg := mcp.NewRegistry()
	registerTelegram(reg, Deps{})
	for _, name := range []string{"telegram.send", "telegram.validate", "telegram.manage"} {
		env := call(t, reg, name, map[string]any{"text": "hello", "action": "get_updates"})
		if env.Status != "error" {
			t.Fatalf("%s: status %s, want error", name, env.Status)
		}
		if !strings.Contains(env.Data.(map[string]any)["error"].(string), "telegram client unavailable") {
			t.Fatalf("%s: error = %+v", name, env.Data)
		}
	}
}

func TestTelegramValidateChatErrorAndSuccessPaths(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/getMe"):
			_, _ = w.Write([]byte(`{"ok":true,"result":{"id":1,"username":"bot"}}`))
		case strings.HasSuffix(r.URL.Path, "/getChat"):
			_, _ = w.Write([]byte(`{"ok":false,"error_code":400,"description":"chat not found"}`))
		default:
			_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
		}
	}))
	t.Cleanup(server.Close)
	reg := telegramClientRegistry(t, telegram.Settings{
		BotToken: "test-token",
		ChatID:   "42",
		BaseURL:  server.URL,
		Timeout:  5 * time.Second,
	})

	env := call(t, reg, "telegram.validate", map[string]any{})
	if env.Status != "ok" {
		t.Fatalf("status %s data %+v", env.Status, env.Data)
	}
	data := env.Data.(map[string]any)
	if data["chat_error"] == nil {
		t.Fatalf("expected chat_error, got %+v", data)
	}
	if data["default_chat_id"] != "42" {
		t.Fatalf("default_chat_id = %v", data["default_chat_id"])
	}

	manage := call(t, reg, "telegram.manage", map[string]any{"action": "set_webhook", "url": "https://example.com/hook", "secret_token": "s"})
	if manage.Status != "ok" {
		t.Fatalf("set_webhook: %+v", manage)
	}
	deleted := call(t, reg, "telegram.manage", map[string]any{"action": "delete_webhook"})
	if deleted.Status != "ok" || deleted.Data.(map[string]any)["deleted"] != true {
		t.Fatalf("delete_webhook: %+v", deleted)
	}
}

func TestParseBotCommands(t *testing.T) {
	if _, err := parseBotCommands(map[string]any{}); err == nil {
		t.Fatal("missing commands must fail")
	}
	if _, err := parseBotCommands(map[string]any{"commands": []any{}}); err == nil {
		t.Fatal("empty commands must fail")
	}
	if _, err := parseBotCommands(map[string]any{"commands": []any{map[string]any{"command": "a"}}}); err == nil {
		t.Fatal("missing description must fail")
	}
	if _, err := parseBotCommands(map[string]any{"commands": []any{"not-a-map"}}); err == nil {
		t.Fatal("non-object entries must fail")
	}
	if _, err := parseBotCommands(map[string]any{"commands": []any{
		map[string]any{"command": "status", "description": "Show status"},
		"oops",
	}}); err == nil {
		t.Fatal("a non-object entry must fail even when other entries are valid")
	}
	commands, err := parseBotCommands(map[string]any{"commands": []any{
		map[string]any{"command": "status", "description": "Show status"},
	}})
	if err != nil || len(commands) != 1 || commands[0].Command != "status" {
		t.Fatalf("commands = %+v err %v", commands, err)
	}
}

func TestSummarizeChats(t *testing.T) {
	updates := []telegram.UpdateInfo{
		{ChatID: "1", ChatType: "private", ChatName: "a"},
		{ChatID: "1", ChatType: "private", ChatName: "a"},
		{ChatID: "", ChatType: "private"},
		{ChatID: "2"}, {ChatID: "3"}, {ChatID: "4"}, {ChatID: "5"}, {ChatID: "6"}, {ChatID: "7"},
	}
	out := summarizeChats(updates)
	if len(out) != 5 {
		t.Fatalf("expected 5 chats, got %+v", out)
	}
	if out[0]["chat_id"] != "1" || out[1]["chat_id"] != "2" {
		t.Fatalf("dedupe failed: %+v", out)
	}
}
