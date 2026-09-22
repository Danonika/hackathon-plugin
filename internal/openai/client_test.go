package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hackathonkit.local/agentkit/internal/config"
)

func testSettings() config.OpenAISettings {
	return config.OpenAISettings{
		ChatModel:       "chat-model",
		VisionModel:     "vision-model",
		TranscribeModel: "whisper-model",
		EmbeddingModel:  "embed-model",
	}
}

func newFakeClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("OPENAI_BASE_URL", srv.URL)
	return New(testSettings())
}

func TestNewDefaults(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "secret")
	t.Setenv("OPENAI_BASE_URL", "")
	c := New(testSettings())
	if c.APIKey != "secret" {
		t.Fatalf("APIKey = %q", c.APIKey)
	}
	if c.BaseURL != "https://api.openai.com/v1" {
		t.Fatalf("BaseURL = %q", c.BaseURL)
	}
	if c.ChatModel != "chat-model" || c.VisionModel != "vision-model" ||
		c.TranscribeModel != "whisper-model" || c.EmbeddingModel != "embed-model" {
		t.Fatalf("models not copied: %+v", c)
	}
	if c.SessionID == "" || c.HTTP == nil {
		t.Fatalf("client not initialized: %+v", c)
	}
}

func TestNewTrimsTrailingSlash(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(srv.Close)
	t.Setenv("OPENAI_BASE_URL", srv.URL+"/")
	c := New(testSettings())
	if c.BaseURL != srv.URL {
		t.Fatalf("BaseURL = %q, want %q", c.BaseURL, srv.URL)
	}
}

func TestAvailable(t *testing.T) {
	var nilClient *Client
	if nilClient.Available() {
		t.Fatal("nil client must not be available")
	}
	if (&Client{}).Available() {
		t.Fatal("client without key must not be available")
	}
	if !(&Client{APIKey: "key"}).Available() {
		t.Fatal("client with key must be available")
	}
}

func TestNewSessionID(t *testing.T) {
	first := newSessionID()
	second := newSessionID()
	if first == second {
		t.Fatal("session ids must differ")
	}
	parts := strings.Split(first, "-")
	if len(parts) != 5 {
		t.Fatalf("session id = %q", first)
	}
	if len(parts[0]) != 8 || len(parts[1]) != 4 || len(parts[2]) != 4 || len(parts[3]) != 4 || len(parts[4]) != 12 {
		t.Fatalf("session id shape = %q", first)
	}
}

func TestChatSuccess(t *testing.T) {
	c := newFakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q", got)
		}
		if r.Header.Get("x-opencode-session") == "" {
			t.Error("missing x-opencode-session header")
		}
		var body struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if body.Model != "chat-model" || len(body.Messages) != 2 ||
			body.Messages[0].Role != "system" || body.Messages[1].Content != "hi" {
			t.Errorf("unexpected payload: %+v", body)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"  hello world  "}}]}`))
	})

	got, err := c.Chat(context.Background(), "be nice", "hi")
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello world" {
		t.Fatalf("Chat = %q", got)
	}
}

func TestChatErrors(t *testing.T) {
	t.Run("api error", func(t *testing.T) {
		c := newFakeClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"boom"}`))
		})
		_, err := c.Chat(context.Background(), "s", "u")
		if err == nil || !strings.Contains(err.Error(), "status 500") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("malformed json", func(t *testing.T) {
		c := newFakeClient(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`not json`))
		})
		if _, err := c.Chat(context.Background(), "s", "u"); err == nil {
			t.Fatal("expected a json error")
		}
	})

	t.Run("empty choices", func(t *testing.T) {
		c := newFakeClient(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"choices":[]}`))
		})
		_, err := c.Chat(context.Background(), "s", "u")
		if err == nil || !strings.Contains(err.Error(), "empty choices") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("transport error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		t.Setenv("OPENAI_API_KEY", "test-key")
		t.Setenv("OPENAI_BASE_URL", srv.URL)
		c := New(testSettings())
		srv.Close()
		if _, err := c.Chat(context.Background(), "s", "u"); err == nil {
			t.Fatal("expected a transport error")
		}
	})
}

func TestChatWithImage(t *testing.T) {
	c := newFakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body := string(raw)
		if !strings.Contains(body, `"model":"vision-model"`) {
			t.Errorf("payload model missing: %s", body)
		}
		if !strings.Contains(body, `"data:image/png;base64,`) {
			t.Errorf("payload data uri missing: %s", body)
		}
		if !strings.Contains(body, `"text":"describe this"`) {
			t.Errorf("payload prompt missing: %s", body)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":" a fly "}}]}`))
	})
	got, err := c.ChatWithImage(context.Background(), "describe this", []byte("png-bytes"), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	if got != "a fly" {
		t.Fatalf("ChatWithImage = %q", got)
	}

	empty := newFakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[]}`))
	})
	if _, err := empty.ChatWithImage(context.Background(), "p", []byte("x"), "image/png"); err == nil || !strings.Contains(err.Error(), "empty choices") {
		t.Fatalf("err = %v", err)
	}
}

func TestEmbed(t *testing.T) {
	c := newFakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			t.Errorf("path = %s", r.URL.Path)
		}
		var body struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode: %v", err)
		}
		if body.Model != "embed-model" || len(body.Input) != 2 {
			t.Errorf("payload = %+v", body)
		}
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1,0.2]},{"embedding":[0.3]}]}`))
	})
	vectors, err := c.Embed(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vectors) != 2 || len(vectors[0]) != 2 || vectors[0][0] != 0.1 {
		t.Fatalf("vectors = %+v", vectors)
	}

	t.Run("api error", func(t *testing.T) {
		c := newFakeClient(t, func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "denied", http.StatusForbidden)
		})
		if _, err := c.Embed(context.Background(), []string{"a"}); err == nil || !strings.Contains(err.Error(), "status 403") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("malformed json", func(t *testing.T) {
		c := newFakeClient(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{`))
		})
		if _, err := c.Embed(context.Background(), []string{"a"}); err == nil {
			t.Fatal("expected a json error")
		}
	})

	t.Run("empty data", func(t *testing.T) {
		c := newFakeClient(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"data":[]}`))
		})
		if _, err := c.Embed(context.Background(), []string{"a"}); err == nil || !strings.Contains(err.Error(), "0 vectors for 1 inputs") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("indexed response is reordered", func(t *testing.T) {
		c := newFakeClient(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"data":[{"index":1,"embedding":[0.3]},{"index":0,"embedding":[0.1,0.2]}]}`))
		})
		vectors, err := c.Embed(context.Background(), []string{"a", "b"})
		if err != nil {
			t.Fatal(err)
		}
		if len(vectors) != 2 || vectors[0][0] != 0.1 || vectors[1][0] != 0.3 {
			t.Fatalf("vectors = %+v", vectors)
		}
	})

	t.Run("missing index entry", func(t *testing.T) {
		c := newFakeClient(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[0.1]}]}`))
		})
		if _, err := c.Embed(context.Background(), []string{"a", "b"}); err == nil || !strings.Contains(err.Error(), "missing a vector for input 1") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestClassifyTask(t *testing.T) {
	scores := func(t *testing.T, content string) *Client {
		t.Helper()
		return newFakeClient(t, func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{"message": map[string]any{"content": content}}},
			})
		})
	}

	client := scores(t, `Here you go: {"build": 1.5, "research": -0.4, "qa": 0.6}`)
	got, err := client.ClassifyTask(context.Background(), "task", []string{"build", "research", "qa", "unknown"})
	if err != nil {
		t.Fatal(err)
	}
	if got["build"] != 1 || got["research"] != 0 || got["qa"] != 0.6 {
		t.Fatalf("scores = %+v", got)
	}
	if _, ok := got["unknown"]; ok {
		t.Fatalf("unknown category leaked: %+v", got)
	}

	t.Run("invalid json", func(t *testing.T) {
		client := scores(t, "I cannot answer that")
		_, err := client.ClassifyTask(context.Background(), "task", []string{"build"})
		if err == nil || !strings.Contains(err.Error(), "llm classifier json") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("no known categories", func(t *testing.T) {
		client := scores(t, `{"other": 0.9}`)
		_, err := client.ClassifyTask(context.Background(), "task", []string{"build"})
		if err == nil || !strings.Contains(err.Error(), "no known categories") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("chat error", func(t *testing.T) {
		client := newFakeClient(t, func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "nope", http.StatusBadGateway)
		})
		if _, err := client.ClassifyTask(context.Background(), "task", []string{"build"}); err == nil {
			t.Fatal("expected an error")
		}
	})
}

func TestTranscribe(t *testing.T) {
	audioPath := filepath.Join(t.TempDir(), "clip.wav")
	if err := os.WriteFile(audioPath, []byte("audio-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := newFakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/audio/transcriptions" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("Content-Type"); !strings.HasPrefix(got, "multipart/form-data") {
			t.Errorf("Content-Type = %q", got)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("multipart parse: %v", err)
			return
		}
		if got := r.FormValue("model"); got != "whisper-model" {
			t.Errorf("model = %q", got)
		}
		if got := r.FormValue("response_format"); got != "json" {
			t.Errorf("response_format = %q", got)
		}
		if got := r.FormValue("language"); got != "en" {
			t.Errorf("language = %q", got)
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			t.Errorf("file part: %v", err)
			return
		}
		defer file.Close()
		if header.Filename != "clip.wav" {
			t.Errorf("filename = %q", header.Filename)
		}
		content, _ := io.ReadAll(file)
		if string(content) != "audio-bytes" {
			t.Errorf("file content = %q", content)
		}
		_, _ = w.Write([]byte(`{"text":"  transcribed text  "}`))
	})

	got, err := c.Transcribe(context.Background(), audioPath, "en")
	if err != nil {
		t.Fatal(err)
	}
	if got != "transcribed text" {
		t.Fatalf("Transcribe = %q", got)
	}
}

func TestTranscribeWithoutLanguage(t *testing.T) {
	audioPath := filepath.Join(t.TempDir(), "clip.mp3")
	if err := os.WriteFile(audioPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := newFakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("multipart parse: %v", err)
			return
		}
		if _, ok := r.MultipartForm.Value["language"]; ok {
			t.Error("language field must be omitted")
		}
		_, _ = w.Write([]byte(`{"text":"ok"}`))
	})
	if _, err := c.Transcribe(context.Background(), audioPath, ""); err != nil {
		t.Fatal(err)
	}
}

func TestTranscribeErrors(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		c := newFakeClient(t, func(http.ResponseWriter, *http.Request) {})
		if _, err := c.Transcribe(context.Background(), filepath.Join(t.TempDir(), "nope.wav"), ""); err == nil {
			t.Fatal("expected an open error")
		}
	})

	t.Run("api error", func(t *testing.T) {
		audioPath := filepath.Join(t.TempDir(), "clip.wav")
		if err := os.WriteFile(audioPath, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		c := newFakeClient(t, func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "bad audio", http.StatusUnprocessableEntity)
		})
		if _, err := c.Transcribe(context.Background(), audioPath, ""); err == nil || !strings.Contains(err.Error(), "status 422") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("malformed json", func(t *testing.T) {
		audioPath := filepath.Join(t.TempDir(), "clip.wav")
		if err := os.WriteFile(audioPath, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		c := newFakeClient(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{`))
		})
		if _, err := c.Transcribe(context.Background(), audioPath, ""); err == nil {
			t.Fatal("expected a json error")
		}
	})

	t.Run("transport error", func(t *testing.T) {
		audioPath := filepath.Join(t.TempDir(), "clip.wav")
		if err := os.WriteFile(audioPath, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		t.Setenv("OPENAI_API_KEY", "test-key")
		t.Setenv("OPENAI_BASE_URL", srv.URL)
		c := New(testSettings())
		srv.Close()
		if _, err := c.Transcribe(context.Background(), audioPath, ""); err == nil {
			t.Fatal("expected a transport error")
		}
	})
}

func TestDoJSONMarshalError(t *testing.T) {
	c := &Client{BaseURL: "http://127.0.0.1:1", HTTP: http.DefaultClient}
	if err := c.doJSON(context.Background(), "/x", make(chan int), nil); err == nil {
		t.Fatal("expected a marshal error")
	}
}

func TestDoJSONNilOut(t *testing.T) {
	c := newFakeClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ignored":true}`))
	})
	if err := c.doJSON(context.Background(), "/anything", map[string]string{"a": "b"}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 10); got != "short" {
		t.Fatalf("truncate short = %q", got)
	}
	got := truncate(strings.Repeat("x", 10), 4)
	if got != "xxxx..." {
		t.Fatalf("truncate long = %q", got)
	}
}
