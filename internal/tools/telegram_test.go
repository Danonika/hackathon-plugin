package tools

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hackathonkit.local/agentkit/internal/mcp"
	"hackathonkit.local/agentkit/internal/telegram"
)

func telegramFake(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		method := parts[len(parts)-1]
		w.Header().Set("Content-Type", "application/json")
		switch method {
		case "getMe":
			_, _ = w.Write([]byte(`{"ok":true,"result":{"id":1,"username":"hackathonkit_bot"}}`))
		case "getChat":
			_, _ = w.Write([]byte(`{"ok":true,"result":{"id":-100123,"type":"supergroup","title":"Demo Team"}}`))
		case "getUpdates":
			_, _ = w.Write([]byte(`{"ok":true,"result":[{"update_id":3,"message":{"chat":{"id":-100123,"type":"supergroup","title":"Demo Team"},"from":{"id":7},"text":"/status"}}]}`))
		case "sendMessage":
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":5}}`))
		default:
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func telegramRegistry(t *testing.T, baseURL string) *mcp.Registry {
	t.Helper()
	reg, _ := testRegistry(t)
	client := telegram.New(telegram.Settings{
		BotToken: "test-token",
		ChatID:   "42",
		BaseURL:  baseURL,
		Timeout:  5 * time.Second,
	}).WithRateLimit(1000)
	registerTelegram(reg, Deps{Telegram: client})
	return reg
}

func TestTelegramSendTool(t *testing.T) {
	server := telegramFake(t)
	reg := telegramRegistry(t, server.URL)
	env := call(t, reg, "telegram.send", map[string]any{"text": "demo is ready"})
	if env.Status != "ok" {
		t.Fatalf("status %s data %+v warnings %v", env.Status, env.Data, env.Warnings)
	}
	data := env.Data.(map[string]any)
	if data["message_id"].(int64) != 5 || data["chat_id"] != "42" {
		t.Fatalf("data = %+v", data)
	}
}

func TestTelegramSendUnconfiguredIsPartial(t *testing.T) {
	reg, _ := testRegistry(t)
	env := call(t, reg, "telegram.send", map[string]any{"text": "hello"})
	if env.Status != "partial" {
		t.Fatalf("expected partial, got %s", env.Status)
	}
	if len(env.Warnings) == 0 {
		t.Fatal("unconfigured send must carry a warning")
	}
	data := env.Data.(map[string]any)
	if data["configured"] != false || data["hint"] == nil {
		t.Fatalf("data = %+v", data)
	}
}

func TestTelegramValidateTool(t *testing.T) {
	server := telegramFake(t)
	reg := telegramRegistry(t, server.URL)
	env := call(t, reg, "telegram.validate", map[string]any{})
	if env.Status != "ok" {
		t.Fatalf("status %s data %+v", env.Status, env.Data)
	}
	data := env.Data.(map[string]any)
	bot := data["bot"].(telegram.BotInfo)
	if bot.Username != "hackathonkit_bot" {
		t.Fatalf("bot = %+v", bot)
	}
	chats := data["recent_chats"].([]map[string]any)
	if len(chats) != 1 || chats[0]["chat_id"] != "-100123" {
		t.Fatalf("recent_chats = %+v", chats)
	}
}

func TestTelegramManageTool(t *testing.T) {
	server := telegramFake(t)
	reg := telegramRegistry(t, server.URL)

	commands := call(t, reg, "telegram.manage", map[string]any{
		"action": "set_commands",
		"commands": []any{
			map[string]any{"command": "status", "description": "Show product status"},
			map[string]any{"command": "help", "description": "List commands"},
		},
	})
	if commands.Status != "ok" || commands.Data.(map[string]any)["count"].(int) != 2 {
		t.Fatalf("set_commands = %+v", commands.Data)
	}

	updates := call(t, reg, "telegram.manage", map[string]any{"action": "get_updates", "limit": float64(5)})
	if updates.Status != "ok" || updates.Data.(map[string]any)["count"].(int) != 1 {
		t.Fatalf("get_updates = %+v", updates.Data)
	}

	if bad := call(t, reg, "telegram.manage", map[string]any{"action": "set_webhook", "url": "http://insecure.example/hook"}); bad.Status != "error" {
		t.Fatalf("http webhook must fail: %+v", bad)
	}
	if unknown := call(t, reg, "telegram.manage", map[string]any{"action": "shell"}); unknown.Status != "error" {
		t.Fatalf("unknown action must fail: %+v", unknown)
	}
}

func TestTelegramManageRequiresCommands(t *testing.T) {
	server := telegramFake(t)
	reg := telegramRegistry(t, server.URL)
	env := call(t, reg, "telegram.manage", map[string]any{"action": "set_commands"})
	if env.Status != "error" {
		t.Fatalf("missing commands must fail: %+v", env)
	}
	raw, _ := json.Marshal(env.Data)
	if !strings.Contains(string(raw), "commands is required") {
		t.Fatalf("data = %s", raw)
	}
}
