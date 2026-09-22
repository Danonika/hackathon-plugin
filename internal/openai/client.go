package openai

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hackathonkit.local/agentkit/internal/config"
)

type Client struct {
	APIKey          string
	BaseURL         string
	SessionID       string
	HTTP            *http.Client
	ChatModel       string
	VisionModel     string
	TranscribeModel string
	EmbeddingModel  string
}

func newSessionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fall back to time+pid rather than a constant shared by every client.
		return fmt.Sprintf("hackathonkit-session-%d-%d", time.Now().UnixNano(), os.Getpid())
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func New(cfg config.OpenAISettings) *Client {
	base := os.Getenv("OPENAI_BASE_URL")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	return &Client{
		APIKey:          os.Getenv("OPENAI_API_KEY"),
		BaseURL:         strings.TrimRight(base, "/"),
		SessionID:       newSessionID(),
		HTTP:            &http.Client{Timeout: 180 * time.Second},
		ChatModel:       cfg.ChatModel,
		VisionModel:     cfg.VisionModel,
		TranscribeModel: cfg.TranscribeModel,
		EmbeddingModel:  cfg.EmbeddingModel,
	}
}

func (c *Client) Available() bool {
	return c != nil && c.APIKey != ""
}

func (c *Client) doJSON(ctx context.Context, path string, payload any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-opencode-session", c.SessionID)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, rerr := io.ReadAll(io.LimitReader(resp.Body, 8<<20+1))
	if rerr != nil {
		return fmt.Errorf("openai %s: reading response: %w", path, rerr)
	}
	if len(data) > 8<<20 {
		return fmt.Errorf("openai %s: response exceeds 8MB", path)
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("openai %s: status %d: %s", path, resp.StatusCode, truncate(string(data), 400))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(data, out)
}

func (c *Client) Chat(ctx context.Context, system, user string) (string, error) {
	payload := map[string]any{
		"model": c.ChatModel,
		"messages": []map[string]any{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := c.doJSON(ctx, "/chat/completions", payload, &out); err != nil {
		return "", err
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("openai chat: empty choices")
	}
	return strings.TrimSpace(out.Choices[0].Message.Content), nil
}

func (c *Client) ChatWithImage(ctx context.Context, prompt string, image []byte, mimeType string) (string, error) {
	dataURI := "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(image)
	payload := map[string]any{
		"model": c.VisionModel,
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{"type": "text", "text": prompt},
					{"type": "image_url", "image_url": map[string]any{"url": dataURI}},
				},
			},
		},
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := c.doJSON(ctx, "/chat/completions", payload, &out); err != nil {
		return "", err
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("openai vision: empty choices")
	}
	return strings.TrimSpace(out.Choices[0].Message.Content), nil
}

func (c *Client) Embed(ctx context.Context, texts []string) ([][]float64, error) {
	payload := map[string]any{"model": c.EmbeddingModel, "input": texts}
	var out struct {
		Data []struct {
			Index     *int      `json:"index"`
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := c.doJSON(ctx, "/embeddings", payload, &out); err != nil {
		return nil, err
	}
	indexed := false
	for _, d := range out.Data {
		if d.Index != nil {
			indexed = true
			break
		}
	}
	if !indexed {
		// Providers that omit the index field return vectors in input order.
		if len(out.Data) != len(texts) {
			return nil, fmt.Errorf("embeddings response returned %d vectors for %d inputs", len(out.Data), len(texts))
		}
		result := make([][]float64, len(texts))
		for i, d := range out.Data {
			if d.Embedding == nil {
				return nil, fmt.Errorf("embeddings response missing a vector for input %d", i)
			}
			result[i] = d.Embedding
		}
		return result, nil
	}
	result := make([][]float64, len(texts))
	for _, d := range out.Data {
		if d.Index == nil {
			return nil, fmt.Errorf("embeddings response mixes indexed and unindexed entries")
		}
		index := *d.Index
		if index < 0 || index >= len(texts) {
			return nil, fmt.Errorf("embeddings response index %d out of range for %d inputs", index, len(texts))
		}
		result[index] = d.Embedding
	}
	for i, vector := range result {
		if vector == nil {
			return nil, fmt.Errorf("embeddings response missing a vector for input %d", i)
		}
	}
	return result, nil
}

const maxAudioBytes = 25 << 20

func (c *Client) Transcribe(ctx context.Context, path string, language string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	st, serr := f.Stat()
	if serr != nil {
		return "", serr
	}
	if st.Size() > maxAudioBytes {
		return "", fmt.Errorf("audio file exceeds %d bytes", maxAudioBytes)
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := w.WriteField("model", c.TranscribeModel); err != nil {
		return "", err
	}
	if err := w.WriteField("response_format", "json"); err != nil {
		return "", err
	}
	if language != "" {
		if err := w.WriteField("language", language); err != nil {
			return "", err
		}
	}
	part, err := w.CreateFormFile("file", filepath.Base(path))
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, f); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/audio/transcriptions", &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("x-opencode-session", c.SessionID)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, rerr := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if rerr != nil {
		return "", fmt.Errorf("openai transcribe: reading response: %w", rerr)
	}
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("openai transcribe: status %d: %s", resp.StatusCode, truncate(string(data), 400))
	}
	var out struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", err
	}
	return strings.TrimSpace(out.Text), nil
}

func (c *Client) ClassifyTask(ctx context.Context, task string, categories []string) (map[string]float64, error) {
	system := "You are a routing classifier for an agent capability layer. Score how relevant each capability category is to the task. Reply with a strict JSON object mapping every category to a number between 0 and 1. No prose, no code fences."
	user := "Categories: " + strings.Join(categories, ", ") + "\nTask: " + task
	raw, err := c.Chat(ctx, system, user)
	if err != nil {
		return nil, err
	}
	raw = strings.TrimSpace(raw)
	if start := strings.Index(raw, "{"); start >= 0 {
		if end := strings.LastIndex(raw, "}"); end > start {
			raw = raw[start : end+1]
		}
	}
	var parsed map[string]float64
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, fmt.Errorf("llm classifier json: %w", err)
	}
	out := map[string]float64{}
	for _, category := range categories {
		value, ok := parsed[category]
		if !ok {
			continue
		}
		if value < 0 {
			value = 0
		}
		if value > 1 {
			value = 1
		}
		out[category] = value
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("llm classifier returned no known categories")
	}
	return out, nil
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}
