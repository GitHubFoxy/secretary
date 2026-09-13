// Package telegram implements the polling-first Telegram Channel adapter.
//
// The adapter owns only Telegram delivery state. Personal Conversation,
// Worker lifecycle and authorization remain server-owned through ServerClient.
package telegram

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrUnauthorized = errors.New("telegram: chat is not allowlisted")
	ErrPairingUsed  = errors.New("telegram: pairing code is expired or already used")
)

type Update struct {
	ID      int64    `json:"update_id"`
	Message *Message `json:"message,omitempty"`
}

type Message struct {
	ChatID   int64  `json:"chat_id"`
	FromID   int64  `json:"from_id"`
	ThreadID int64  `json:"message_thread_id,omitempty"`
	Text     string `json:"text,omitempty"`
}

type OutgoingMessage struct {
	ChatID   int64
	ThreadID int64
	Text     string
}

type SentMessage struct{ OutgoingMessage }

type ForumTopic struct {
	ChatID   int64
	ThreadID int64
	Name     string
}

type Transport interface {
	GetUpdates(context.Context, int64, time.Duration) ([]Update, error)
	SendMessage(context.Context, OutgoingMessage) error
	CreateForumTopic(context.Context, int64, string) (ForumTopic, error)
}

type InboundMessage struct {
	ExternalMessageID string
	Body              string
}

type WorkerMessage struct {
	WorkerRef         string
	ExternalMessageID string
	Text              string
}

type ServerClient interface {
	SendMessage(context.Context, InboundMessage) error
	SendWorkerMessage(context.Context, WorkerMessage) error
}

type Config struct {
	StatePath     string
	OwnerChatID   int64
	PollInterval  time.Duration
	FlushInterval time.Duration
	BotUsername   string
}

type Pairing struct {
	Code      string    `json:"code"`
	DeepLink  string    `json:"deep_link"`
	ExpiresAt time.Time `json:"expires_at"`
	// Credential is intentionally not part of the pairing flow. Telegram
	// receives a one-time code, never a server or Client credential.
	Credential string `json:"-"`
}

type Event struct {
	Kind      string
	WorkerRef string
	Title     string
	Text      string
	Tool      string
	Payload   json.RawMessage
}

type TopicMapping struct {
	WorkerRef string    `json:"worker_ref"`
	ChatID    int64     `json:"chat_id"`
	ThreadID  int64     `json:"thread_id"`
	CreatedAt time.Time `json:"created_at"`
}

type persistedState struct {
	Version   int                      `json:"version"`
	OwnerChat int64                    `json:"owner_chat_id"`
	Offset    int64                    `json:"offset"`
	Processed map[string]time.Time     `json:"processed_updates"`
	Topics    map[string]TopicMapping  `json:"topics"`
	Pairings  map[string]pairingRecord `json:"pairings"`
	Outbox    []OutgoingMessage        `json:"outbox,omitempty"`
}

type pairingRecord struct {
	Hash      string    `json:"hash"`
	ExpiresAt time.Time `json:"expires_at"`
}

type pendingBatch struct {
	SecretaryText  strings.Builder
	SecretaryTools []string
	WorkerLines    map[string][]string
}

type Adapter struct {
	transport Transport
	server    ServerClient
	config    Config

	mu      sync.Mutex
	state   persistedState
	pending pendingBatch
	now     func() time.Time
}

func New(config Config, transport Transport, server ServerClient) (*Adapter, error) {
	if transport == nil || server == nil {
		return nil, errors.New("telegram: transport and server client are required")
	}
	if strings.TrimSpace(config.StatePath) == "" {
		return nil, errors.New("telegram: state path is required")
	}
	if config.PollInterval <= 0 {
		config.PollInterval = 30 * time.Second
	}
	if config.FlushInterval <= 0 {
		config.FlushInterval = 2 * time.Second
	}
	adapter := &Adapter{transport: transport, server: server, config: config, now: func() time.Time { return time.Now().UTC() }}
	adapter.state = persistedState{Version: 1, OwnerChat: config.OwnerChatID, Processed: map[string]time.Time{}, Topics: map[string]TopicMapping{}, Pairings: map[string]pairingRecord{}, Outbox: []OutgoingMessage{}}
	if err := adapter.load(); err != nil {
		return nil, err
	}
	if config.OwnerChatID != 0 && adapter.state.OwnerChat != 0 && config.OwnerChatID != adapter.state.OwnerChat {
		return nil, errors.New("telegram: configured owner does not match durable allowlist")
	}
	if adapter.state.OwnerChat == 0 {
		adapter.state.OwnerChat = config.OwnerChatID
	}
	adapter.pending.WorkerLines = make(map[string][]string)
	return adapter, nil
}

func (a *Adapter) load() error {
	data, err := os.ReadFile(a.config.StatePath)
	if errors.Is(err, os.ErrNotExist) {
		return a.saveLocked()
	}
	if err != nil {
		return fmt.Errorf("telegram: read state: %w", err)
	}
	if err := json.Unmarshal(data, &a.state); err != nil {
		return fmt.Errorf("telegram: decode state: %w", err)
	}
	if a.state.Version == 0 {
		a.state.Version = 1
	}
	if a.state.Processed == nil {
		a.state.Processed = map[string]time.Time{}
	}
	if a.state.Topics == nil {
		a.state.Topics = map[string]TopicMapping{}
	}
	if a.state.Pairings == nil {
		a.state.Pairings = map[string]pairingRecord{}
	}
	return nil
}

func (a *Adapter) saveLocked() error {
	data, err := json.MarshalIndent(a.state, "", "  ")
	if err != nil {
		return fmt.Errorf("telegram: encode state: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(a.config.StatePath), 0o700); err != nil {
		return fmt.Errorf("telegram: create state directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(a.config.StatePath), ".telegram-state-*")
	if err != nil {
		return fmt.Errorf("telegram: create temporary state: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("telegram: write state: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("telegram: sync state: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, a.config.StatePath); err != nil {
		return fmt.Errorf("telegram: install state: %w", err)
	}
	return nil
}

func (a *Adapter) SetOwnerChat(chatID int64) error {
	if chatID == 0 {
		return errors.New("telegram: owner chat id is required")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.state.OwnerChat != 0 && a.state.OwnerChat != chatID {
		return ErrUnauthorized
	}
	a.state.OwnerChat = chatID
	return a.saveLocked()
}

func (a *Adapter) CreatePairing(botUsername string) (Pairing, error) {
	secret := make([]byte, 18)
	if _, err := rand.Read(secret); err != nil {
		return Pairing{}, fmt.Errorf("telegram: generate pairing code: %w", err)
	}
	code := base64.RawURLEncoding.EncodeToString(secret)
	expires := a.now().Add(10 * time.Minute)
	hash := sha256.Sum256([]byte(code))
	a.mu.Lock()
	a.state.Pairings[hex.EncodeToString(hash[:])] = pairingRecord{Hash: hex.EncodeToString(hash[:]), ExpiresAt: expires}
	err := a.saveLocked()
	a.mu.Unlock()
	if err != nil {
		return Pairing{}, err
	}
	botUsername = strings.TrimPrefix(strings.TrimSpace(botUsername), "@")
	if botUsername == "" {
		botUsername = strings.TrimPrefix(strings.TrimSpace(a.config.BotUsername), "@")
	}
	if botUsername == "" {
		return Pairing{Code: code, ExpiresAt: expires}, nil
	}
	return Pairing{Code: code, DeepLink: "https://t.me/" + botUsername + "?start=" + code, ExpiresAt: expires}, nil
}

func (a *Adapter) RedeemPairing(code string, chatID int64) error {
	if strings.TrimSpace(code) == "" || chatID == 0 {
		return ErrPairingUsed
	}
	hash := sha256.Sum256([]byte(strings.TrimSpace(code)))
	key := hex.EncodeToString(hash[:])
	a.mu.Lock()
	defer a.mu.Unlock()
	record, ok := a.state.Pairings[key]
	if !ok || !a.now().Before(record.ExpiresAt) {
		return ErrPairingUsed
	}
	if a.state.OwnerChat != 0 && a.state.OwnerChat != chatID {
		return ErrUnauthorized
	}
	delete(a.state.Pairings, key)
	a.state.OwnerChat = chatID
	return a.saveLocked()
}

func (a *Adapter) ownerAllowed(chatID int64) bool {
	return chatID != 0 && a.state.OwnerChat != 0 && chatID == a.state.OwnerChat
}

func (a *Adapter) HandleUpdate(ctx context.Context, update Update) error {
	if update.ID <= 0 || update.Message == nil {
		return nil
	}
	a.mu.Lock()
	if _, exists := a.state.Processed[fmt.Sprint(update.ID)]; exists {
		a.mu.Unlock()
		return nil
	}
	// Mark before the side effect. The server receives the update id as its
	// idempotency key, so a crash cannot turn replay into a duplicate turn.
	a.state.Processed[fmt.Sprint(update.ID)] = a.now()
	if err := a.saveLocked(); err != nil {
		a.mu.Unlock()
		return err
	}
	allowed := a.ownerAllowed(update.Message.ChatID) && (update.Message.FromID == 0 || update.Message.FromID == update.Message.ChatID)
	a.mu.Unlock()
	text := strings.TrimSpace(update.Message.Text)
	if strings.HasPrefix(text, "/start ") {
		// Possession of the short-lived deep-link code is the explicit owner
		// pairing proof. It is the only update accepted before allowlisting.
		return a.RedeemPairing(strings.TrimSpace(strings.TrimPrefix(text, "/start ")), update.Message.ChatID)
	}
	if !allowed {
		return nil
	}
	if text == "/start" {
		return nil
	}
	externalID := fmt.Sprint(update.ID)
	if update.Message.ThreadID != 0 {
		a.mu.Lock()
		mapping, ok := a.mappingForThreadLocked(update.Message.ChatID, update.Message.ThreadID)
		a.mu.Unlock()
		if !ok {
			return nil
		}
		err := a.server.SendWorkerMessage(ctx, WorkerMessage{WorkerRef: mapping.WorkerRef, ExternalMessageID: externalID, Text: text})
		if err != nil {
			a.unmarkProcessed(update.ID)
		}
		return err
	}
	if text == "" {
		return nil
	}
	err := a.server.SendMessage(ctx, InboundMessage{ExternalMessageID: externalID, Body: text})
	if err != nil {
		a.unmarkProcessed(update.ID)
	}
	return err
}

func (a *Adapter) unmarkProcessed(updateID int64) {
	a.mu.Lock()
	delete(a.state.Processed, fmt.Sprint(updateID))
	_ = a.saveLocked()
	a.mu.Unlock()
}

func (a *Adapter) mappingForThreadLocked(chatID, threadID int64) (TopicMapping, bool) {
	for _, mapping := range a.state.Topics {
		if mapping.ChatID == chatID && mapping.ThreadID == threadID {
			return mapping, true
		}
	}
	return TopicMapping{}, false
}

func (a *Adapter) PollOnce(ctx context.Context) error {
	a.mu.Lock()
	offset := a.state.Offset + 1
	a.mu.Unlock()
	updates, err := a.transport.GetUpdates(ctx, offset, a.config.PollInterval)
	if err != nil {
		return err
	}
	for _, update := range updates {
		if err := a.HandleUpdate(ctx, update); err != nil && !errors.Is(err, ErrUnauthorized) && !errors.Is(err, ErrPairingUsed) {
			return err
		}
		a.mu.Lock()
		if update.ID > a.state.Offset {
			a.state.Offset = update.ID
		}
		err = a.saveLocked()
		a.mu.Unlock()
		if err != nil {
			return err
		}
	}
	return a.Flush(ctx)
}

func (a *Adapter) Run(ctx context.Context) error {
	ticker := time.NewTicker(a.config.PollInterval)
	defer ticker.Stop()
	for {
		if err := a.PollOnce(ctx); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (a *Adapter) HandleEvent(ctx context.Context, event Event) error {
	if strings.HasPrefix(event.Kind, "secretary.") {
		a.queueSecretary(event)
		return nil
	}
	if event.WorkerRef == "" {
		return nil
	}
	if event.Kind == "worker.created" {
		mapping, err := a.ensureTopic(ctx, event.WorkerRef, event.Title)
		if err != nil {
			return err
		}
		return a.sendMessage(ctx, OutgoingMessage{ChatID: mapping.ChatID, ThreadID: mapping.ThreadID, Text: "Worker принят: " + safeText(event.Title)})
	}
	mapping, err := a.ensureTopic(ctx, event.WorkerRef, event.Title)
	if err != nil {
		return err
	}
	if importantWorkerEvent(event.Kind) {
		text := renderWorkerEvent(event)
		if text == "" {
			return nil
		}
		return a.sendMessage(ctx, OutgoingMessage{ChatID: mapping.ChatID, ThreadID: mapping.ThreadID, Text: text})
	}
	line := renderWorkerActivity(event)
	if line == "" {
		return nil
	}
	a.mu.Lock()
	if a.pending.WorkerLines == nil {
		a.pending.WorkerLines = make(map[string][]string)
	}
	a.pending.WorkerLines[event.WorkerRef] = append(a.pending.WorkerLines[event.WorkerRef], line)
	a.mu.Unlock()
	return nil
}

func (a *Adapter) ensureTopic(ctx context.Context, workerRef, title string) (TopicMapping, error) {
	a.mu.Lock()
	if mapping, ok := a.state.Topics[workerRef]; ok {
		a.mu.Unlock()
		return mapping, nil
	}
	owner := a.state.OwnerChat
	a.mu.Unlock()
	if owner == 0 {
		return TopicMapping{}, ErrUnauthorized
	}
	topic, err := a.transport.CreateForumTopic(ctx, owner, topicName(workerRef, title))
	if err != nil {
		return TopicMapping{}, err
	}
	mapping := TopicMapping{WorkerRef: workerRef, ChatID: topic.ChatID, ThreadID: topic.ThreadID, CreatedAt: a.now()}
	a.mu.Lock()
	if existing, ok := a.state.Topics[workerRef]; ok {
		a.mu.Unlock()
		return existing, nil
	}
	a.state.Topics[workerRef] = mapping
	err = a.saveLocked()
	a.mu.Unlock()
	return mapping, err
}

func (a *Adapter) queueSecretary(event Event) {
	a.mu.Lock()
	defer a.mu.Unlock()
	switch event.Kind {
	case "secretary.text_delta":
		a.pending.SecretaryText.WriteString(safeDelta(event.Text))
	case "secretary.tool_call", "secretary.tool_result":
		tool := safeText(event.Tool)
		if tool == "" {
			tool = safeText(event.Text)
		}
		if tool != "" {
			a.pending.SecretaryTools = append(a.pending.SecretaryTools, tool)
		}
	case "secretary.turn.finished":
		if text := safeText(event.Text); text != "" {
			a.pending.SecretaryText.WriteString(text)
		}
	}
}

func (a *Adapter) Flush(ctx context.Context) error {
	if err := a.drainOutbox(ctx); err != nil {
		return err
	}
	a.mu.Lock()
	text := strings.TrimSpace(a.pending.SecretaryText.String())
	tools := append([]string(nil), a.pending.SecretaryTools...)
	workerLines := make(map[string][]string, len(a.pending.WorkerLines))
	for ref, lines := range a.pending.WorkerLines {
		workerLines[ref] = append([]string(nil), lines...)
	}
	a.pending.SecretaryText.Reset()
	a.pending.SecretaryTools = nil
	a.pending.WorkerLines = make(map[string][]string)
	owner := a.state.OwnerChat
	mappings := make(map[string]TopicMapping, len(a.state.Topics))
	for ref, mapping := range a.state.Topics {
		mappings[ref] = mapping
	}
	a.mu.Unlock()
	if text != "" || len(tools) != 0 {
		parts := make([]string, 0, 2)
		if text != "" {
			parts = append(parts, text)
		}
		if len(tools) != 0 {
			unique := uniqueStrings(tools)
			parts = append(parts, "Шаги: "+strings.Join(unique, ", "))
		}
		if err := a.sendMessage(ctx, OutgoingMessage{ChatID: owner, Text: strings.Join(parts, "\n")}); err != nil {
			return err
		}
	}
	refs := make([]string, 0, len(workerLines))
	for ref := range workerLines {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	for _, ref := range refs {
		mapping, ok := mappings[ref]
		if !ok {
			continue
		}
		if err := a.sendMessage(ctx, OutgoingMessage{ChatID: mapping.ChatID, ThreadID: mapping.ThreadID, Text: "Активность:\n" + strings.Join(uniqueStrings(workerLines[ref]), "\n")}); err != nil {
			return err
		}
	}
	return nil
}

func (a *Adapter) sendMessage(ctx context.Context, message OutgoingMessage) error {
	a.mu.Lock()
	a.state.Outbox = append(a.state.Outbox, message)
	if err := a.saveLocked(); err != nil {
		a.mu.Unlock()
		return err
	}
	a.mu.Unlock()
	if err := a.transport.SendMessage(ctx, message); err != nil {
		return err
	}
	a.mu.Lock()
	for index, pending := range a.state.Outbox {
		if pending == message {
			a.state.Outbox = append(a.state.Outbox[:index], a.state.Outbox[index+1:]...)
			break
		}
	}
	err := a.saveLocked()
	a.mu.Unlock()
	return err
}

func (a *Adapter) drainOutbox(ctx context.Context) error {
	a.mu.Lock()
	outbox := append([]OutgoingMessage(nil), a.state.Outbox...)
	a.mu.Unlock()
	for _, message := range outbox {
		if err := a.transport.SendMessage(ctx, message); err != nil {
			return err
		}
		a.mu.Lock()
		for index, pending := range a.state.Outbox {
			if pending == message {
				a.state.Outbox = append(a.state.Outbox[:index], a.state.Outbox[index+1:]...)
				break
			}
		}
		err := a.saveLocked()
		a.mu.Unlock()
		if err != nil {
			return err
		}
	}
	return nil
}

func importantWorkerEvent(kind string) bool {
	return strings.Contains(kind, "approval") || strings.Contains(kind, "needs_input") || strings.HasSuffix(kind, ".failed") || strings.HasSuffix(kind, ".completed") || strings.HasSuffix(kind, ".succeeded") || strings.HasSuffix(kind, ".canceled") || strings.HasSuffix(kind, ".offline") || strings.HasSuffix(kind, ".completion")
}

func renderWorkerEvent(event Event) string {
	label := map[string]string{"worker.approval_requested": "Нужно разрешение", "worker.needs_input": "Нужен ответ", "worker.failed": "Ошибка Worker", "worker.completed": "Worker завершён", "worker.succeeded": "Worker завершён", "worker.canceled": "Worker отменён", "worker.offline": "Node offline"}[event.Kind]
	if label == "" {
		label = "Статус Worker"
	}
	text := safeText(event.Text)
	if text == "" {
		return label
	}
	return label + ": " + text
}

func renderWorkerActivity(event Event) string {
	tool := safeText(event.Tool)
	if tool == "" {
		tool = "activity"
	}
	return "• " + tool + ": выполнено"
}

func topicName(workerRef, title string) string {
	name := safeText(title)
	if name == "" {
		name = "Worker " + workerRef
	}
	if len(name) > 60 {
		name = name[:60]
	}
	return name
}

var sensitiveText = regexp.MustCompile(`(?i)(node[_ -]?token|channel[_ -]?secret|callback[_ -]?capability|runtime[_ -]?(session|id)|chain[_ -]?of[_ -]?thought|api[_ -]?key|credential|token)\s*[:=]?[[:space:]]*[^,; ]+`)

func safeText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if strings.Contains(strings.ToLower(text), "analysis:") || strings.Contains(strings.ToLower(text), "chain of thought") {
		return "Worker activity update"
	}
	text = sensitiveText.ReplaceAllString(text, "[redacted]")
	text = strings.Join(strings.Fields(text), " ")
	return text
}

func safeDelta(text string) string {
	if strings.Contains(strings.ToLower(text), "analysis:") || strings.Contains(strings.ToLower(text), "chain of thought") {
		return ""
	}
	return sensitiveText.ReplaceAllString(text, "[redacted]")
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok || value == "" {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
