package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ProbeResult is one provider check. It never contains the API key.
type ProbeResult struct {
	Provider   string `json:"provider"`
	Configured bool   `json:"configured"`
	Reachable  bool   `json:"reachable"`
	Live       bool   `json:"live"`
	StatusCode int    `json:"status_code,omitempty"`
	LatencyMS  int64  `json:"latency_ms,omitempty"`
	Models     int    `json:"models,omitempty"`
	Error      string `json:"error,omitempty"`
}

// ProbeOpenAICompatible checks GET {base}/models with a bearer key. OpenAI and
// NVIDIA expose the same endpoint shape, so both providers share this probe.
func ProbeOpenAICompatible(ctx context.Context, providerID, baseURL, apiKey string) ProbeResult {
	result := ProbeResult{Provider: providerID, Configured: strings.TrimSpace(apiKey) != "", Live: true}
	if !result.Configured {
		result.Error = "not configured"
		return result
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		result.Error = "no base url configured"
		return result
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/models", nil)
	if err != nil {
		result.Error = redact(err.Error(), apiKey)
		return result
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	result.LatencyMS = time.Since(start).Milliseconds()
	if err != nil {
		result.Error = redact(err.Error(), apiKey)
		return result
	}
	defer resp.Body.Close()
	result.StatusCode = resp.StatusCode
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		result.Error = fmt.Sprintf("http %d", resp.StatusCode)
		return result
	}
	var parsed struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil {
		result.Models = len(parsed.Data)
	}
	result.Reachable = true
	return result
}

func redact(text, secret string) string {
	if secret == "" {
		return text
	}
	return strings.ReplaceAll(text, secret, "***")
}
