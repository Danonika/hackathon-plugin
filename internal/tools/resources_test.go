package tools

import (
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"hackathonkit.local/agentkit/internal/resources"
	"hackathonkit.local/agentkit/internal/telegram"
)

func clearResourceEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"OPENAI_API_KEY", "NVIDIA_API_KEY", "TELEGRAM_BOT_TOKEN", "TELEGRAM_CHAT_ID",
		"POSTGRES_DSN", "MINIO_ENDPOINT", "MINIO_ACCESS_KEY", "MINIO_SECRET_KEY",
		"OPENAI_BASE_URL", "NVIDIA_BASE_URL",
	} {
		t.Setenv(key, "")
	}
}

func TestSystemResourcesUnconfigured(t *testing.T) {
	clearResourceEnv(t)
	reg, _ := testRegistry(t)

	env := call(t, reg, "system.resources", map[string]any{})
	if env.Status != "partial" {
		t.Fatalf("status = %s, want partial", env.Status)
	}
	data := env.Data.(map[string]any)
	profile := data["profile"].(map[string]any)
	if profile["present"] != false {
		t.Fatalf("profile = %+v, want absent", profile)
	}
	providers := data["providers"].([]resources.Status)
	if len(providers) != 6 {
		t.Fatalf("providers = %d, want 6", len(providers))
	}
	if tracks := data["tracks"].([]resources.Track); len(tracks) != 10 {
		t.Fatalf("tracks = %d, want 10", len(tracks))
	}
	if len(env.Warnings) == 0 || !strings.Contains(env.Warnings[0], "ask the user") {
		t.Fatalf("warnings = %v", env.Warnings)
	}
	if steps := data["next_steps"].([]string); len(steps) == 0 {
		t.Fatal("next_steps missing")
	}
	if summary, _ := data["summary"].(string); !strings.Contains(summary, "openai_api=no") {
		t.Fatalf("summary = %q", summary)
	}

	env = call(t, reg, "system.resources", map[string]any{"track": "energy"})
	data = env.Data.(map[string]any)
	track, _ := data["track"].(resources.Track)
	if track.ID != "01-energy" {
		t.Fatalf("track = %+v", data["track"])
	}
	env = call(t, reg, "system.resources", map[string]any{"track": "99-nope"})
	if !strings.Contains(strings.Join(env.Warnings, " "), "unknown track") {
		t.Fatalf("warnings = %v", env.Warnings)
	}
}

func TestSystemResourcesSet(t *testing.T) {
	clearResourceEnv(t)
	reg, root := testRegistry(t)

	env := call(t, reg, "system.resources_set", map[string]any{"codex_pro": true, "track": "08"})
	if env.Status != "ok" {
		t.Fatalf("status = %s data=%+v", env.Status, env.Data)
	}
	data := env.Data.(map[string]any)
	profile := data["profile"].(map[string]any)
	if profile["track"] != "08-innovation" || profile["codex_pro"] != true {
		t.Fatalf("profile = %+v", profile)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(resources.Path(root))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("profile mode = %v, want 0600", info.Mode().Perm())
		}
	}

	env = call(t, reg, "system.resources_set", map[string]any{"nvidia_api": true})
	if env.Status != "partial" {
		t.Fatalf("claimed nvidia without key: status = %s, want partial", env.Status)
	}
	data = env.Data.(map[string]any)
	missing := data["missing"].([]string)
	if len(missing) != 1 || missing[0] != "NVIDIA_API_KEY" {
		t.Fatalf("missing = %v", missing)
	}
	if steps := strings.Join(data["next_steps"].([]string), " "); !strings.Contains(steps, "env-set.sh .env NVIDIA_API_KEY") {
		t.Fatalf("next_steps = %q", steps)
	}

	if env := call(t, reg, "system.resources_set", map[string]any{}); env.Status != "error" {
		t.Fatalf("empty set: status = %s, want error", env.Status)
	}
	if env := call(t, reg, "system.resources_set", map[string]any{"track": "99-nope"}); env.Status != "error" {
		t.Fatalf("bad track: status = %s, want error", env.Status)
	}
}

func TestSystemResourcesProbePresenceOnly(t *testing.T) {
	clearResourceEnv(t)
	t.Setenv("OPENAI_API_KEY", "sk-test")
	reg, _ := testRegistry(t)

	env := call(t, reg, "system.resources_probe", map[string]any{"provider": "openai_api"})
	if env.Status != "ok" {
		t.Fatalf("status = %s data=%+v", env.Status, env.Data)
	}
	results := env.Data.(map[string]any)["results"].([]any)
	first := results[0].(map[string]any)
	if first["configured"] != true || first["reachable"] != false || first["live"] != false {
		t.Fatalf("result = %+v", first)
	}

	if env := call(t, reg, "system.resources_probe", map[string]any{"provider": "gemini"}); env.Status != "error" {
		t.Fatalf("unknown provider: status = %s, want error", env.Status)
	}
}

func TestSystemResourcesProbeLive(t *testing.T) {
	clearResourceEnv(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-live" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{},{},{}]}`))
	}))
	t.Cleanup(server.Close)
	t.Setenv("OPENAI_API_KEY", "sk-live")
	t.Setenv("OPENAI_BASE_URL", server.URL)
	reg, _ := testRegistry(t)

	env := call(t, reg, "system.resources_probe", map[string]any{"provider": "openai_api", "live": true})
	if env.Status != "ok" {
		t.Fatalf("status = %s data=%+v", env.Status, env.Data)
	}
	first := env.Data.(map[string]any)["results"].([]any)[0].(map[string]any)
	if first["reachable"] != true || first["models"] != 3 {
		t.Fatalf("result = %+v", first)
	}

	t.Setenv("OPENAI_API_KEY", "wrong-key")
	env = call(t, reg, "system.resources_probe", map[string]any{"provider": "openai_api", "live": true})
	if env.Status != "partial" {
		t.Fatalf("bad key: status = %s, want partial", env.Status)
	}
	first = env.Data.(map[string]any)["results"].([]any)[0].(map[string]any)
	if first["error"] != "http 401" {
		t.Fatalf("result = %+v", first)
	}
	if strings.Contains(env.Data.(map[string]any)["results"].([]any)[0].(map[string]any)["error"].(string), "wrong-key") {
		t.Fatal("probe must not leak the key")
	}
}

func TestSystemResourcesProbeTelegram(t *testing.T) {
	clearResourceEnv(t)
	t.Setenv("TELEGRAM_BOT_TOKEN", "test-token")
	t.Setenv("TELEGRAM_CHAT_ID", "42")
	server := telegramFake(t)
	client := telegram.New(telegram.Settings{
		BotToken: "test-token",
		ChatID:   "42",
		BaseURL:  server.URL,
		Timeout:  5 * time.Second,
	}).WithRateLimit(1000)
	reg, _ := testRegistryWith(t, client)

	env := call(t, reg, "system.resources_probe", map[string]any{"provider": "telegram", "live": true})
	if env.Status != "ok" {
		t.Fatalf("status = %s data=%+v", env.Status, env.Data)
	}
	first := env.Data.(map[string]any)["results"].([]any)[0].(map[string]any)
	if first["reachable"] != true {
		t.Fatalf("result = %+v", first)
	}
}
