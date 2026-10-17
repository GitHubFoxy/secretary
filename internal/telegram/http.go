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

var ErrFormatting = errors.New("telegram: entity parsing rejected")

func botAPIError(method string, code int, description string) error {
	lower := strings.ToLower(description)
	if method == "sendMessage" && code == 400 && (strings.Contains(lower, "can't parse entities") || strings.Contains(lower, "cannot parse entities")) {
		return ErrFormatting
	}
	return fmt.Errorf("telegram: Bot API %s failed (%d): %s", method, code, description)
}

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
		var failure struct{ Description string }
		if json.Unmarshal(body, &failure) == nil && failure.Description != "" {
			return botAPIError(method, response.StatusCode, redactBotSecret(t.BotToken, failure.Description))
		}
		return botAPIError(method, response.StatusCode, redactBotSecret(t.BotToken, strings.TrimSpace(string(body))))
	}
	var envelope struct {
		OK          bool            `json:"ok"`
		ErrorCode   int             `json:"error_code"`
		Description string          `json:"description"`
		Result      json.RawMessage `json:"result"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		return err
	}
	if !envelope.OK {
		return botAPIError(method, envelope.ErrorCode, redactBotSecret(t.BotToken, envelope.Description))
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

type botVoiceDTO struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	Duration     int    `json:"duration"`
	MimeType     string `json:"mime_type,omitempty"`
	FileSize     int64  `json:"file_size,omitempty"`
}

type TelegramFile struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	FileSize     int64  `json:"file_size"`
	FilePath     string `json:"file_path"`
}

type botMessageDTO struct {
	MessageID int64 `json:"message_id,omitempty"`
	From      *struct {
		ID int64 `json:"id"`
	} `json:"from,omitempty"`
	Chat *struct {
		ID      int64  `json:"id"`
		Type    string `json:"type"`
		IsForum bool   `json:"is_forum"`
	} `json:"chat,omitempty"`
	ThreadID int64        `json:"message_thread_id,omitempty"`
	Text     string       `json:"text,omitempty"`
	Voice    *botVoiceDTO `json:"voice,omitempty"`
}

func (dto botUpdateDTO) update() Update {
	update := Update{ID: dto.ID}
	if dto.Message == nil {
		return update
	}
	message := &Message{MessageID: dto.Message.MessageID, ThreadID: dto.Message.ThreadID, Text: dto.Message.Text}
	if dto.Message.Chat != nil {
		message.ChatID = dto.Message.Chat.ID
		message.ChatType = dto.Message.Chat.Type
		message.IsForum = dto.Message.Chat.IsForum
	}
	if dto.Message.From != nil {
		message.FromID = dto.Message.From.ID
	}
	if dto.Message.Voice != nil {
		message.Voice = &Voice{
			FileID:       dto.Message.Voice.FileID,
			FileUniqueID: dto.Message.Voice.FileUniqueID,
			Duration:     dto.Message.Voice.Duration,
			MimeType:     dto.Message.Voice.MimeType,
			FileSize:     dto.Message.Voice.FileSize,
		}
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
	if message.ReplyToMessageID != 0 {
		values.Set("reply_parameters", fmt.Sprintf(`{"message_id":%d,"allow_sending_without_reply":true}`, message.ReplyToMessageID))
	}
	if message.ParseMode != "" {
		values.Set("parse_mode", message.ParseMode)
	}
	if message.DisableLinkPreview {
		values.Set("link_preview_options", `{"is_disabled":true}`)
	}
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

func (t *BotAPITransport) GetFile(ctx context.Context, fileID string) (TelegramFile, error) {
	if strings.TrimSpace(fileID) == "" {
		return TelegramFile{}, errors.New("telegram: file_id is required")
	}
	values := url.Values{"file_id": {fileID}}
	var file TelegramFile
	if err := t.call(ctx, "getFile", values, &file); err != nil {
		return TelegramFile{}, err
	}
	return file, nil
}

func (t *BotAPITransport) DownloadFile(ctx context.Context, filePath string, maxBytes int64) ([]byte, error) {
	if strings.TrimSpace(t.BotToken) == "" {
		return nil, errors.New("telegram: bot token is required")
	}
	if strings.TrimSpace(filePath) == "" {
		return nil, errors.New("telegram: file_path is required")
	}
	base := strings.TrimRight(t.BaseURL, "/")
	if base == "" {
		base = "https://api.telegram.org"
	}
	endpoint := base + "/file/bot" + t.BotToken + "/" + strings.TrimLeft(filePath, "/")
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	response, err := t.client().Do(request)
	if err != nil {
		return nil, errors.New("telegram: file download request failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("telegram: file download failed with status %d", response.StatusCode)
	}
	limit := maxBytes
	if limit <= 0 {
		limit = 10 * 1024 * 1024
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("telegram: read downloaded file: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("telegram: file exceeds maximum size (%d bytes)", limit)
	}
	return data, nil
}

type HTTPServerClient struct {
	BaseURL    string
	Credential string
	HTTPClient *http.Client
}

const maxServerErrorBody = 16 << 10

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
		body, readErr := io.ReadAll(io.LimitReader(response.Body, maxServerErrorBody+1))
		if readErr != nil {
			return fmt.Errorf("telegram: read server error response: %w", readErr)
		}
		if len(body) > maxServerErrorBody {
			return fmt.Errorf("telegram: server returned %s: error response exceeded %d bytes", response.Status, maxServerErrorBody)
		}
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

func (c *HTTPServerClient) ResumeWorker(ctx context.Context, workerRef string) error {
	path := "/v1/workers/" + url.PathEscape(workerRef) + "/resume"
	return c.post(ctx, path, map[string]string{}, "")
}
