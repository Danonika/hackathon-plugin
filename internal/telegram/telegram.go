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
	"regexp"
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
	return &Client{
		settings: settings,
		http: &http.Client{
			// The bot token is part of the request URL: following a redirect
			// would send it to another host.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		rateLimitPerMin: 20,
		retryDelay:      time.Second,
	}
}

func (c *Client) WithHTTPClient(client *http.Client) *Client {
	if client != nil {
		// Preserve the security guard: the bot token is part of the request URL,
		// so a redirect must never be followed. This overrides any caller
		// CheckRedirect that would allow one.
		client.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
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

// requestTimeout is the default per-request deadline when the caller's context
// has none.
func (c *Client) requestTimeout() time.Duration {
	if c.settings.Timeout > 0 {
		return c.settings.Timeout
	}
	return 15 * time.Second
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
	// Chunks counts chunks actually delivered; Planned is the total the message
	// was split into. A partial failure reports Chunks < Planned.
	Chunks  int `json:"chunks"`
	Planned int `json:"planned"`
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
	var chunks []string
	switch {
	case strings.EqualFold(opts.ParseMode, "HTML"):
		chunks = ChunkHTML(text, MaxMessageChars)
	case opts.ParseMode == "":
		chunks = ChunkText(text, MaxMessageChars)
	case len([]rune(text)) > MaxMessageChars:
		// Other entity-bearing parse modes (MarkdownV2, Markdown) cannot be
		// split without risking a boundary inside an entity/escape that makes
		// every chunk unparsable. Fail closed with a clear message instead of
		// emitting chunks the API will reject.
		return SendResult{}, fmt.Errorf("cannot split a %s message longer than %d characters; send it in shorter pieces", opts.ParseMode, MaxMessageChars)
	default:
		chunks = []string{text}
	}
	result := SendResult{ChatID: chatID, Planned: len(chunks)}
	sent := 0
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
			result.Chunks = sent
			return result, err
		}
		raw, err := c.callWithRetry(ctx, "sendMessage", payload)
		if err != nil {
			result.Chunks = sent
			return result, err
		}
		sent++
		var message struct {
			MessageID int64 `json:"message_id"`
		}
		if err := json.Unmarshal(raw, &message); err == nil {
			result.MessageID = message.MessageID
		}
	}
	result.Chunks = sent
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
// the built project's bot owns at runtime. The HTTP deadline is extended past
// the requested polling duration so a quiet poll can finish.
func (c *Client) GetUpdates(ctx context.Context, offset int64, limit, timeoutSec int) ([]UpdateInfo, error) {
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	if timeoutSec > 300 {
		timeoutSec = 300
	}
	if timeoutSec > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second+c.requestTimeout())
		defer cancel()
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
			SenderChat struct {
				ID int64 `json:"id"`
			} `json:"sender_chat"`
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
			if update.Message.From.ID != 0 {
				info.FromID = strconv.FormatInt(update.Message.From.ID, 10)
			} else if update.Message.SenderChat.ID != 0 {
				// Channel posts / anonymous admins have no `from`.
				info.FromID = strconv.FormatInt(update.Message.SenderChat.ID, 10)
			}
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
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.requestTimeout())
		defer cancel()
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
			return nil, fmt.Errorf("telegram %s: request failed: %w", method, urlErr.Err)
		}
		return nil, fmt.Errorf("telegram %s: request failed", method)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseSize+1))
	if err != nil {
		return nil, fmt.Errorf("telegram %s: response read failed", method)
	}
	if int64(len(data)) > maxResponseSize {
		return nil, fmt.Errorf("telegram %s: response exceeds %d bytes", method, maxResponseSize)
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

var htmlTagPattern = regexp.MustCompile(`</?([a-zA-Z][a-zA-Z0-9-]*)(?:\s[^>]*)?>`)

type openTag struct {
	name string
	full string
}

// ChunkHTML splits HTML-formatted text into chunks of at most limit runes.
// Tags open at a boundary are closed at the end of the chunk and reopened in
// the next one, so every chunk stays valid Telegram HTML. Entities such as
// &lt; are not tags.
func ChunkHTML(text string, limit int) []string {
	if limit <= 0 {
		limit = MaxMessageChars
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return []string{text}
	}
	chunks := []string{}
	stack := []openTag{}
	pos := 0
	for pos < len(runes) {
		prefix := ""
		for _, tag := range stack {
			prefix += tag.full
		}
		// Reserve room for the closing tags the chunk will need.
		closersBudget := 0
		for _, tag := range stack {
			closersBudget += len([]rune("</" + tag.name + ">"))
		}
		budget := limit - len([]rune(prefix)) - closersBudget
		if budget < 1 {
			// The open tags need more than `limit` runes to reopen and close,
			// so no valid chunk can be produced with this prefix. Close the
			// pending tags in their own chunk and restart with a clean stack
			// instead of emitting an oversized chunk Telegram rejects.
			closers := ""
			for i := len(stack) - 1; i >= 0; i-- {
				closers += "</" + stack[i].name + ">"
			}
			if closers != "" {
				chunks = append(chunks, closers)
			}
			stack = nil
			continue
		}
		cut := pos + budget
		if cut > len(runes) {
			cut = len(runes)
		}
		cut = cutOutsideTag(runes, pos, cut)
		for i := cut; i > pos+1; i-- {
			if runes[i-1] == '\n' {
				cut = i
				break
			}
		}
		cut = cutOutsideTag(runes, pos, cut)
		if cut <= pos {
			cut = pos + 1
		}
		body := string(runes[pos:cut])
		next, closers := scanTags(body, stack)
		for len([]rune(prefix+body+closers)) > limit && cut > pos+1 {
			cut--
			if cut <= pos {
				cut = pos + 1
			}
			cut = cutOutsideTag(runes, pos, cut)
			if cut <= pos {
				cut = pos + 1
			}
			body = string(runes[pos:cut])
			next, closers = scanTags(body, stack)
		}
		chunks = append(chunks, prefix+body+closers)
		stack = next
		pos = cut
	}
	return chunks
}

// scanTags replays a fragment over the open-tag stack and returns the resulting
// stack plus the closing tags needed to balance the fragment.
func scanTags(fragment string, stack []openTag) ([]openTag, string) {
	open := append([]openTag{}, stack...)
	for _, match := range htmlTagPattern.FindAllStringSubmatch(fragment, -1) {
		full := match[0]
		name := strings.ToLower(match[1])
		if strings.HasPrefix(full, "</") {
			for i := len(open) - 1; i >= 0; i-- {
				if open[i].name == name {
					open = append(open[:i], open[i+1:]...)
					break
				}
			}
			continue
		}
		if strings.HasSuffix(full, "/>") {
			continue
		}
		open = append(open, openTag{name: name, full: full})
	}
	closers := ""
	for i := len(open) - 1; i >= 0; i-- {
		closers += "</" + open[i].name + ">"
	}
	return open, closers
}

// cutOutsideTag moves a cut back when it would land inside a tag or split an
// HTML entity such as &lt; or &#39;.
func cutOutsideTag(runes []rune, start, cut int) int {
	open := -1
	for i := start; i < cut; i++ {
		if runes[i] == '<' {
			open = i
		}
		if runes[i] == '>' {
			open = -1
		}
	}
	if open >= 0 {
		return open
	}
	if amp := unterminatedEntityStart(runes, start, cut); amp >= 0 {
		return amp
	}
	return cut
}

// unterminatedEntityStart returns the index of an `&` whose entity is not
// terminated by `;` before the cut, or -1 when the window contains no such
// entity start.
func unterminatedEntityStart(runes []rune, start, cut int) int {
	for i := cut - 1; i >= start; i-- {
		switch runes[i] {
		case ';':
			return -1
		case '&':
			return i
		}
		// Entity names are short; an older ampersand is not part of an entity
		// straddling the cut.
		if cut-i > 32 {
			return -1
		}
	}
	return -1
}
