package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// BotAPITransport is the production transport. It uses long polling and the
// Telegram Bot API only. The bot token is kept in this process and is never
// included in OutgoingMessage, Pairing or ServerClient payloads.
type BotAPITransport struct {
	BaseURL    string
	BotToken   string
	HTTPClient *http.Client
}

func (t *BotAPITransport) client() *http.Client {
	if t.HTTPClient != nil {
		return t.HTTPClient
	}
	return http.DefaultClient
}

func (t *BotAPITransport) call(ctx context.Context, method string, values url.Values, result any) error {
	if strings.TrimSpace(t.BotToken) == "" {
		return errors.New("telegram: bot token is required")
	}
	base := strings.TrimRight(t.BaseURL, "/")
	if base == "" {
		base = "https://api.telegram.org"
	}
	endpoint := base + "/bot" + t.BotToken + "/" + method
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := t.client().Do(request)
	if err != nil {
		return fmt.Errorf("telegram: Bot API %s request failed", method)
	}
	defer response.Body.Close()
	if response.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("telegram: Bot API %s returned %s: %s", method, response.Status, redactBotSecret(t.BotToken, strings.TrimSpace(string(body))))
	}
	var envelope struct {
		OK          bool            `json:"ok"`
		Description string          `json:"description"`
		Result      json.RawMessage `json:"result"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		return err
	}
	if !envelope.OK {
		return fmt.Errorf("telegram: Bot API %s failed: %s", method, redactBotSecret(t.BotToken, envelope.Description))
	}
	if result != nil {
		if err := json.Unmarshal(envelope.Result, result); err != nil {
			return fmt.Errorf("telegram: decode Bot API %s: %w", method, err)
		}
	}
	return nil
}

type botUpdateDTO struct {
	ID      int64          `json:"update_id"`
	Message *botMessageDTO `json:"message,omitempty"`
}

type botMessageDTO struct {
	From *struct {
		ID int64 `json:"id"`
	} `json:"from,omitempty"`
	Chat *struct {
		ID int64 `json:"id"`
	} `json:"chat,omitempty"`
	ThreadID int64  `json:"message_thread_id,omitempty"`
	Text     string `json:"text,omitempty"`
}

func (dto botUpdateDTO) update() Update {
	update := Update{ID: dto.ID}
	if dto.Message == nil {
		return update
	}
	message := &Message{ThreadID: dto.Message.ThreadID, Text: dto.Message.Text}
	if dto.Message.Chat != nil {
		message.ChatID = dto.Message.Chat.ID
	}
	if dto.Message.From != nil {
		message.FromID = dto.Message.From.ID
	}
	update.Message = message
	return update
}

func redactBotSecret(secret, text string) string {
	if secret == "" {
		return text
	}
	return strings.ReplaceAll(text, secret, "[redacted]")
}

func (t *BotAPITransport) GetUpdates(ctx context.Context, offset int64, timeout time.Duration) ([]Update, error) {
	seconds := int(timeout / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	values := url.Values{"offset": {strconv.FormatInt(offset, 10)}, "timeout": {strconv.Itoa(seconds)}, "allowed_updates": {"[\"message\"]"}}
	var decoded []botUpdateDTO
	if err := t.call(ctx, "getUpdates", values, &decoded); err != nil {
		return nil, err
	}
	updates := make([]Update, 0, len(decoded))
	for _, dto := range decoded {
		updates = append(updates, dto.update())
	}
	return updates, nil
}

func (t *BotAPITransport) SendMessage(ctx context.Context, message OutgoingMessage) error {
	values := url.Values{"chat_id": {strconv.FormatInt(message.ChatID, 10)}, "text": {message.Text}}
	if message.ThreadID != 0 {
		values.Set("message_thread_id", strconv.FormatInt(message.ThreadID, 10))
	}
	return t.call(ctx, "sendMessage", values, nil)
}

func (t *BotAPITransport) SendChatAction(ctx context.Context, chatID, threadID int64, action string) error {
	if chatID == 0 || strings.TrimSpace(action) == "" {
		return errors.New("telegram: chat id and action are required")
	}
	values := url.Values{"chat_id": {strconv.FormatInt(chatID, 10)}, "action": {strings.TrimSpace(action)}}
	if threadID != 0 {
		values.Set("message_thread_id", strconv.FormatInt(threadID, 10))
	}
	return t.call(ctx, "sendChatAction", values, nil)
}

func (t *BotAPITransport) CreateForumTopic(ctx context.Context, chatID int64, name string) (ForumTopic, error) {
	values := url.Values{"chat_id": {strconv.FormatInt(chatID, 10)}, "name": {name}}
	var result struct {
		MessageThreadID int64  `json:"message_thread_id"`
		Name            string `json:"name"`
	}
	if err := t.call(ctx, "createForumTopic", values, &result); err != nil {
		return ForumTopic{}, err
	}
	return ForumTopic{ChatID: chatID, ThreadID: result.MessageThreadID, Name: result.Name}, nil
}

// HTTPServerClient speaks only the public Client API. It cannot call the Node
// protocol and never sends a Node token or native runtime identifier.
type HTTPServerClient struct {
	BaseURL    string
	Credential string
	HTTPClient *http.Client
}

func (c *HTTPServerClient) client() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

func (c *HTTPServerClient) post(ctx context.Context, path string, body any, idempotency string) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+path, strings.NewReader(string(encoded)))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+c.Credential)
	request.Header.Set("Idempotency-Key", idempotency)
	response, err := c.client().Do(request)
	if err != nil {
		return fmt.Errorf("telegram: server request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("telegram: server returned %s: %s", response.Status, redactBotSecret(c.Credential, strings.TrimSpace(string(body))))
	}
	return nil
}

func (c *HTTPServerClient) SendMessage(ctx context.Context, message InboundMessage) error {
	return c.post(ctx, "/v1/messages", map[string]string{"external_message_id": message.ExternalMessageID, "body": message.Body}, "telegram:"+message.ExternalMessageID)
}

func (c *HTTPServerClient) SendWorkerMessage(ctx context.Context, message WorkerMessage) error {
	path := "/v1/workers/" + url.PathEscape(message.WorkerRef) + "/message"
	body := map[string]string{"text": message.Text}
	if message.RequestID != "" {
		body["request_id"] = message.RequestID
	}
	return c.post(ctx, path, body, "telegram:"+message.ExternalMessageID)
}
