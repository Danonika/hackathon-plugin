// Package telegram is a minimal Telegram Bot API client for the Hackathon Agent Kit MCP
// tools. It is used at build time to set up, test and manage the Telegram
// integration of the project being built. The bot token is read from the
// environment and never appears in returned errors or logs.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	DefaultBaseURL  = "https://api.telegram.org"
	MaxMessageChars = 4096
	maxResponseSize = 1 << 20
)

type Settings struct {
	BotToken       string
	ChatID         string
	AllowedChatIDs []string
	BaseURL        string
	Timeout        time.Duration
}

func SettingsFromEnv() Settings {
	base := strings.TrimSpace(os.Getenv("TELEGRAM_API_BASE"))
	if base == "" {
		base = DefaultBaseURL
	}
	timeout := 15 * time.Second
	if value := strings.TrimSpace(os.Getenv("TELEGRAM_TIMEOUT_SEC")); value != "" {
		if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
			timeout = time.Duration(seconds) * time.Second
		}
	}
	allowed := []string{}
	for _, part := range strings.Split(os.Getenv("TELEGRAM_ALLOWED_CHAT_IDS"), ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			allowed = append(allowed, trimmed)
		}
	}
	return Settings{
		BotToken:       strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN")),
		ChatID:         strings.TrimSpace(os.Getenv("TELEGRAM_CHAT_ID")),
		AllowedChatIDs: allowed,
		BaseURL:        base,
		Timeout:        timeout,
	}
}

func (s Settings) Configured() bool {
	return s.BotToken != ""
}

// APIError is a Telegram API-level error. The endpoint and token are never
// included.
type APIError struct {
	Method      string
	Code        int
	Description string
	RetryAfter  int
}

func (e *APIError) Error() string {
	return fmt.Sprintf("telegram %s failed: %d %s", e.Method, e.Code, e.Description)
}

type Client struct {
	settings        Settings
	http            *http.Client
	rateLimitPerMin int
	retryDelay      time.Duration

	mu   sync.Mutex
	sent []time.Time
}

func New(settings Settings) *Client {
	timeout := settings.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &Client{
		settings:        settings,
		http:            &http.Client{Timeout: timeout},
		rateLimitPerMin: 20,
		retryDelay:      time.Second,
	}
}

func (c *Client) WithHTTPClient(client *http.Client) *Client {
	if client != nil {
		c.http = client
	}
	return c
}

func (c *Client) WithRateLimit(perMinute int) *Client {
	if perMinute > 0 {
		c.rateLimitPerMin = perMinute
	}
	return c
}

// WithRetryDelay overrides the 429 backoff; tests use a short delay.
func (c *Client) WithRetryDelay(delay time.Duration) *Client {
	if delay > 0 {
		c.retryDelay = delay
	}
	return c
}

func (c *Client) Settings() Settings {
	return c.settings
}

func (c *Client) Configured() bool {
	return c.settings.Configured()
}

func (c *Client) DefaultChatID() string {
	return c.settings.ChatID
}

type SendOptions struct {
	ChatID    string
	ParseMode string
	Silent    bool
	ReplyTo   int
}

type SendResult struct {
	MessageID int64  `json:"message_id"`
	ChatID    string `json:"chat_id"`
	Chunks    int    `json:"chunks"`
}

type BotInfo struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
}

type ChatInfo struct {
	ID       int64  `json:"id"`
	Type     string `json:"type"`
	Title    string `json:"title,omitempty"`
	Username string `json:"username,omitempty"`
}

type UpdateInfo struct {
	UpdateID int64  `json:"update_id"`
	ChatID   string `json:"chat_id"`
	ChatType string `json:"chat_type,omitempty"`
	ChatName string `json:"chat_name,omitempty"`
	FromID   string `json:"from_id,omitempty"`
	Text     string `json:"text,omitempty"`
}

type BotCommand struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

type apiResponse struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	Description string          `json:"description"`
	ErrorCode   int             `json:"error_code"`
	Parameters  struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

func (c *Client) chatAllowed(chatID string) bool {
	if chatID == "" {
		return false
	}
	if c.settings.ChatID != "" && chatID == c.settings.ChatID {
		return true
	}
	for _, allowed := range c.settings.AllowedChatIDs {
		if allowed == chatID {
			return true
		}
	}
	return false
}

// SendMessage sends text, chunking messages longer than 4096 characters. The
// chat must be the configured chat or be present in TELEGRAM_ALLOWED_CHAT_IDS.
func (c *Client) SendMessage(ctx context.Context, text string, opts SendOptions) (SendResult, error) {
	if !c.Configured() {
		return SendResult{}, fmt.Errorf("telegram bot token is not configured (set TELEGRAM_BOT_TOKEN)")
	}
	chatID := strings.TrimSpace(opts.ChatID)
	if chatID == "" {
		chatID = c.settings.ChatID
	}
	if chatID == "" {
		return SendResult{}, fmt.Errorf("telegram chat id is not configured (set TELEGRAM_CHAT_ID)")
	}
	if !c.chatAllowed(chatID) {
		return SendResult{}, fmt.Errorf("chat %s is not the configured chat and is not in TELEGRAM_ALLOWED_CHAT_IDS", chatID)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return SendResult{}, fmt.Errorf("text is required")
	}
	chunks := ChunkText(text, MaxMessageChars)
	result := SendResult{ChatID: chatID, Chunks: len(chunks)}
	for i, chunk := range chunks {
		payload := map[string]any{"chat_id": chatID, "text": chunk}
		if opts.ParseMode != "" {
			payload["parse_mode"] = opts.ParseMode
		}
		if opts.Silent {
			payload["disable_notification"] = true
		}
		if opts.ReplyTo != 0 && i == 0 {
			payload["reply_to_message_id"] = opts.ReplyTo
		}
		if err := c.wait(ctx); err != nil {
			return result, err
		}
		raw, err := c.callWithRetry(ctx, "sendMessage", payload)
		if err != nil {
			return result, err
		}
		var message struct {
			MessageID int64 `json:"message_id"`
		}
		if err := json.Unmarshal(raw, &message); err == nil {
			result.MessageID = message.MessageID
		}
	}
	return result, nil
}

func (c *Client) GetMe(ctx context.Context) (BotInfo, error) {
	var info BotInfo
	raw, err := c.callWithRetry(ctx, "getMe", map[string]any{})
	if err != nil {
		return info, err
	}
	err = json.Unmarshal(raw, &info)
	return info, err
}

func (c *Client) GetChat(ctx context.Context, chatID string) (ChatInfo, error) {
	var info ChatInfo
	if strings.TrimSpace(chatID) == "" {
		return info, fmt.Errorf("chat_id is required")
	}
	raw, err := c.callWithRetry(ctx, "getChat", map[string]any{"chat_id": chatID})
	if err != nil {
		return info, err
	}
	err = json.Unmarshal(raw, &info)
	return info, err
}

// GetUpdates reads recent updates; timeoutSec > 0 enables long polling, which
// the built project's bot owns at runtime.
func (c *Client) GetUpdates(ctx context.Context, offset int64, limit, timeoutSec int) ([]UpdateInfo, error) {
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	raw, err := c.callWithRetry(ctx, "getUpdates", map[string]any{
		"offset":          offset,
		"limit":           limit,
		"timeout":         timeoutSec,
		"allowed_updates": []string{"message"},
	})
	if err != nil {
		return nil, err
	}
	var updates []struct {
		UpdateID int64 `json:"update_id"`
		Message  *struct {
			Chat struct {
				ID       int64  `json:"id"`
				Type     string `json:"type"`
				Title    string `json:"title"`
				Username string `json:"username"`
			} `json:"chat"`
			From struct {
				ID int64 `json:"id"`
			} `json:"from"`
			Text string `json:"text"`
		} `json:"message"`
	}
	if err := json.Unmarshal(raw, &updates); err != nil {
		return nil, err
	}
	out := []UpdateInfo{}
	for _, update := range updates {
		info := UpdateInfo{UpdateID: update.UpdateID}
		if update.Message != nil {
			info.ChatID = strconv.FormatInt(update.Message.Chat.ID, 10)
			info.ChatType = update.Message.Chat.Type
			info.ChatName = update.Message.Chat.Title
			if info.ChatName == "" {
				info.ChatName = update.Message.Chat.Username
			}
			info.FromID = strconv.FormatInt(update.Message.From.ID, 10)
			info.Text = update.Message.Text
		}
		out = append(out, info)
	}
	return out, nil
}

func (c *Client) SetMyCommands(ctx context.Context, commands []BotCommand) error {
	if len(commands) == 0 {
		return fmt.Errorf("at least one command is required")
	}
	if len(commands) > 100 {
		return fmt.Errorf("telegram allows at most 100 commands")
	}
	for _, command := range commands {
		if strings.TrimSpace(command.Command) == "" || strings.TrimSpace(command.Description) == "" {
			return fmt.Errorf("every command needs a name and a description")
		}
	}
	_, err := c.callWithRetry(ctx, "setMyCommands", map[string]any{"commands": commands})
	return err
}

func (c *Client) SetWebhook(ctx context.Context, webhookURL, secretToken string) error {
	webhookURL = strings.TrimSpace(webhookURL)
	parsed, err := url.Parse(webhookURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return fmt.Errorf("webhook url must be a valid https url")
	}
	payload := map[string]any{"url": webhookURL, "allowed_updates": []string{"message"}}
	if strings.TrimSpace(secretToken) != "" {
		payload["secret_token"] = secretToken
	}
	_, err = c.callWithRetry(ctx, "setWebhook", payload)
	return err
}

func (c *Client) DeleteWebhook(ctx context.Context, dropPending bool) error {
	_, err := c.callWithRetry(ctx, "deleteWebhook", map[string]any{"drop_pending_updates": dropPending})
	return err
}

// wait enforces a simple per-minute local rate limit.
func (c *Client) wait(ctx context.Context) error {
	for {
		c.mu.Lock()
		now := time.Now()
		cutoff := now.Add(-time.Minute)
		kept := c.sent[:0]
		for _, sent := range c.sent {
			if sent.After(cutoff) {
				kept = append(kept, sent)
			}
		}
		c.sent = kept
		if len(c.sent) < c.rateLimitPerMin {
			c.sent = append(c.sent, now)
			c.mu.Unlock()
			return nil
		}
		delay := c.sent[0].Add(time.Minute).Sub(now)
		c.mu.Unlock()
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (c *Client) callWithRetry(ctx context.Context, method string, payload any) (json.RawMessage, error) {
	raw, err := c.call(ctx, method, payload)
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.Code == http.StatusTooManyRequests {
		delay := c.retryDelay
		if apiErr.RetryAfter > 0 {
			delay = time.Duration(apiErr.RetryAfter) * time.Second
		}
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return c.call(ctx, method, payload)
	}
	return raw, err
}

func (c *Client) call(ctx context.Context, method string, payload any) (json.RawMessage, error) {
	if !c.Configured() {
		return nil, fmt.Errorf("telegram bot token is not configured (set TELEGRAM_BOT_TOKEN)")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("telegram %s: invalid payload", method)
	}
	endpoint := strings.TrimRight(c.settings.BaseURL, "/") + "/bot" + c.settings.BotToken + "/" + method
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("telegram %s: invalid request", method)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.http.Do(request)
	if err != nil {
		// url.Error carries the endpoint (with the token); return only the
		// underlying cause so credentials can never leak into logs.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			return nil, fmt.Errorf("telegram %s: request failed: %v", method, urlErr.Err)
		}
		return nil, fmt.Errorf("telegram %s: request failed", method)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseSize))
	if err != nil {
		return nil, fmt.Errorf("telegram %s: response read failed", method)
	}
	var parsed apiResponse
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("telegram %s: invalid response (status %d)", method, response.StatusCode)
	}
	if !parsed.OK {
		code := parsed.ErrorCode
		if code == 0 {
			code = response.StatusCode
		}
		description := strings.TrimSpace(parsed.Description)
		if description == "" {
			description = "request rejected"
		}
		return nil, &APIError{Method: method, Code: code, Description: description, RetryAfter: parsed.Parameters.RetryAfter}
	}
	return parsed.Result, nil
}

// ChunkText splits long text into <=limit rune chunks, preferring newline
// boundaries.
func ChunkText(text string, limit int) []string {
	if limit <= 0 {
		limit = MaxMessageChars
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return []string{text}
	}
	chunks := []string{}
	for len(runes) > limit {
		cut := limit
		for i := limit; i > limit/2; i-- {
			if runes[i-1] == '\n' {
				cut = i
				break
			}
		}
		chunks = append(chunks, string(runes[:cut]))
		runes = runes[cut:]
	}
	if len(runes) > 0 {
		chunks = append(chunks, string(runes))
	}
	return chunks
}

// EscapeHTML escapes text for parse_mode=HTML.
func EscapeHTML(text string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return replacer.Replace(text)
}
