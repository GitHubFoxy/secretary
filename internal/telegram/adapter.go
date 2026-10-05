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
	"unicode"
	"unicode/utf8"

	"github.com/rivo/uniseg"
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
	ChatID    int64  `json:"chat_id"`
	FromID    int64  `json:"from_id"`
	MessageID int64  `json:"message_id"`
	ThreadID  int64  `json:"message_thread_id,omitempty"`
	Text      string `json:"text,omitempty"`
}

type OutgoingMessage struct {
	ChatID         int64
	ThreadID       int64
	Text           string
	Identity       string `json:"identity,omitempty"`
	DeliveredBytes int    `json:"delivered_bytes,omitempty"`
	ParseMode      string `json:"parse_mode,omitempty"`
	PlainFallback  bool   `json:"plain_fallback,omitempty"`
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
	SendChatAction(context.Context, int64, int64, string) error
	CreateForumTopic(context.Context, int64, string) (ForumTopic, error)
}

type MessageReactionTransport interface {
	SetMessageReaction(context.Context, int64, int64, string) error
}

type InboundMessage struct {
	ExternalMessageID string
	Body              string
}

type WorkerMessage struct {
	WorkerRef         string
	ExternalMessageID string
	RequestID         string
	Text              string
}

type ServerClient interface {
	SendMessage(context.Context, InboundMessage) error
	SendWorkerMessage(context.Context, WorkerMessage) error
}

type Config struct {
	StatePath      string
	OwnerChatID    int64
	PollInterval   time.Duration
	FlushInterval  time.Duration
	BotUsername    string
	TitleGenerator func(context.Context, string) (string, error)
	TitleTimeout   time.Duration
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
	EventID          string
	Sequence         int64
	Kind             string
	Source           string
	WorkerRef        string
	Title            string
	TaskPrompt       string
	Text             string
	Tool             string
	ToolPreview      string
	TerminalIdentity string
	Payload          json.RawMessage
}

type TopicMapping struct {
	WorkerRef        string    `json:"worker_ref"`
	ChatID           int64     `json:"chat_id"`
	ThreadID         int64     `json:"thread_id"`
	Title            string    `json:"title,omitempty"`
	PendingRequestID string    `json:"pending_request_id,omitempty"`
	PromptDelivered  bool      `json:"prompt_delivered,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

type persistedState struct {
	Version               int                      `json:"version"`
	OwnerChat             int64                    `json:"owner_chat_id"`
	OwnerUser             int64                    `json:"owner_user_id,omitempty"`
	Offset                int64                    `json:"offset"`
	LastEventSeq          int64                    `json:"last_event_seq"`
	Processed             map[string]time.Time     `json:"processed_updates"`
	Topics                map[string]TopicMapping  `json:"topics"`
	Pairings              map[string]pairingRecord `json:"pairings"`
	Outbox                []OutgoingMessage        `json:"outbox,omitempty"`
	PendingSecretaryText  string                   `json:"pending_secretary_text,omitempty"`
	PendingSecretaryTools []string                 `json:"pending_secretary_tools,omitempty"`
	PendingEventSeqs      []int64                  `json:"pending_event_seqs,omitempty"`
	SecretaryReady        bool                     `json:"secretary_ready,omitempty"`
	SecretaryDelegated    bool                     `json:"secretary_delegated,omitempty"`
	TerminalNotified      map[string]time.Time     `json:"terminal_notified,omitempty"`
	RequestNotified       map[string]time.Time     `json:"request_notified,omitempty"`
	InboundNotified       map[string]time.Time     `json:"inbound_notified,omitempty"`
	PreviousOwnerBound    bool                     `json:"previous_owner_bound,omitempty"`
	ReboundAt             time.Time                `json:"rebound_at,omitempty"`
	RebindReason          string                   `json:"rebind_reason,omitempty"`
	RebindWatermark       int64                    `json:"rebind_watermark,omitempty"`
}

type pairingRecord struct {
	Hash      string    `json:"hash"`
	ExpiresAt time.Time `json:"expires_at"`
}

type pendingBatch struct {
	SecretaryText  strings.Builder
	SecretaryTools []string
	EventSeqs      []int64
	TurnOpen       bool
	Delegated      bool
	Ready          bool
}

type updateClaim struct {
	done chan struct{}
	err  error
}

type Adapter struct {
	transport Transport
	server    ServerClient
	config    Config

	mu         sync.Mutex
	topicMu    sync.Mutex
	flushMu    sync.Mutex
	typingMu   sync.Mutex
	state      persistedState
	pending    pendingBatch
	lastTyping map[string]time.Time
	claims     map[int64]*updateClaim
	flushTimer *time.Timer
	now        func() time.Time
}

func New(config Config, transport Transport, server ServerClient) (*Adapter, error) {
	if transport == nil || server == nil {
		return nil, errors.New("telegram: transport and server client are required")
	}
	if strings.TrimSpace(config.StatePath) == "" {
		return nil, errors.New("telegram: state path is required")
	}
	if config.PollInterval <= 0 {
		config.PollInterval = 2 * time.Second
	}
	if config.FlushInterval <= 0 {
		config.FlushInterval = 2 * time.Second
	}
	if config.TitleTimeout <= 0 {
		config.TitleTimeout = 5 * time.Second
	}
	adapter := &Adapter{transport: transport, server: server, config: config, claims: make(map[int64]*updateClaim), now: func() time.Time { return time.Now().UTC() }}
	adapter.state = persistedState{Version: 1, OwnerChat: config.OwnerChatID, Processed: map[string]time.Time{}, Topics: map[string]TopicMapping{}, Pairings: map[string]pairingRecord{}, TerminalNotified: map[string]time.Time{}, RequestNotified: map[string]time.Time{}, InboundNotified: map[string]time.Time{}, Outbox: []OutgoingMessage{}}
	if err := adapter.load(); err != nil {
		return nil, err
	}
	if config.OwnerChatID != 0 && adapter.state.OwnerChat != 0 && config.OwnerChatID != adapter.state.OwnerChat {
		return nil, errors.New("telegram: configured owner does not match durable allowlist")
	}
	if adapter.state.OwnerChat == 0 {
		adapter.state.OwnerChat = config.OwnerChatID
	}
	adapter.pending.SecretaryText.WriteString(adapter.state.PendingSecretaryText)
	adapter.pending.SecretaryTools = append([]string(nil), adapter.state.PendingSecretaryTools...)
	adapter.pending.EventSeqs = append([]int64(nil), adapter.state.PendingEventSeqs...)
	adapter.pending.Ready = adapter.state.SecretaryReady
	adapter.pending.Delegated = adapter.state.SecretaryDelegated
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
	if a.state.TerminalNotified == nil {
		a.state.TerminalNotified = map[string]time.Time{}
	}
	if a.state.RequestNotified == nil {
		a.state.RequestNotified = map[string]time.Time{}
	}
	if a.state.InboundNotified == nil {
		a.state.InboundNotified = map[string]time.Time{}
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
	if a.state.OwnerChat != 0 && a.state.OwnerChat != chatID {
		a.mu.Unlock()
		return ErrUnauthorized
	}
	a.state.OwnerChat = chatID
	err := a.saveLocked()
	a.mu.Unlock()
	if err == nil {
		a.scheduleFlush()
	}
	return err
}

func Rebind(statePath string, configOwner int64, reason string, watermark int64) error {
	if configOwner == 0 {
		return errors.New("telegram: new owner chat id is required")
	}
	if reason != "private-to-forum" {
		return errors.New("telegram: reason must be private-to-forum")
	}
	if watermark < 0 {
		return errors.New("telegram: watermark must be non-negative")
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("telegram: state not found at %s", statePath)
		}
		return fmt.Errorf("telegram: read state: %w", err)
	}
	var state persistedState
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("telegram: decode state: %w", err)
	}
	// Preserve offset, validate watermark does not go backwards.
	if watermark < state.LastEventSeq {
		watermark = state.LastEventSeq
	}
	previousBound := state.OwnerChat != 0
	state.PreviousOwnerBound = previousBound || state.PreviousOwnerBound
	state.ReboundAt = time.Now().UTC()
	state.RebindReason = reason
	state.RebindWatermark = watermark
	state.LastEventSeq = watermark
	state.OwnerChat = configOwner
	state.OwnerUser = 0
	state.Topics = map[string]TopicMapping{}
	state.Pairings = map[string]pairingRecord{}
	state.Outbox = nil
	state.PendingSecretaryText = ""
	state.PendingSecretaryTools = nil
	state.PendingEventSeqs = nil
	state.SecretaryReady = false
	state.SecretaryDelegated = false
	// Keep Offset, Processed, TerminalNotified, RequestNotified, Version, OwnerChat/OwnerUser updated.
	encoded, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("telegram: encode state: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
		return fmt.Errorf("telegram: create state directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(statePath), ".telegram-state-*")
	if err != nil {
		return fmt.Errorf("telegram: create temporary state: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(encoded); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("telegram: write state: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("telegram: sync state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, statePath); err != nil {
		return fmt.Errorf("telegram: install state: %w", err)
	}
	return nil
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
	return a.RedeemPairingWithSender(code, chatID, 0)
}

func (a *Adapter) RedeemPairingWithSender(code string, chatID int64, fromID int64) error {
	if strings.TrimSpace(code) == "" || chatID == 0 {
		return ErrPairingUsed
	}
	hash := sha256.Sum256([]byte(strings.TrimSpace(code)))
	key := hex.EncodeToString(hash[:])
	a.mu.Lock()
	record, ok := a.state.Pairings[key]
	if !ok || !a.now().Before(record.ExpiresAt) {
		a.mu.Unlock()
		return ErrPairingUsed
	}
	if a.state.OwnerChat != 0 && a.state.OwnerChat != chatID {
		a.mu.Unlock()
		return ErrUnauthorized
	}
	if a.state.OwnerUser != 0 && fromID != 0 && a.state.OwnerUser != fromID {
		a.mu.Unlock()
		return ErrUnauthorized
	}
	delete(a.state.Pairings, key)
	a.state.OwnerChat = chatID
	if fromID != 0 {
		a.state.OwnerUser = fromID
	} else if a.state.OwnerUser == 0 {
		a.state.OwnerUser = chatID
	}
	err := a.saveLocked()
	a.mu.Unlock()
	if err == nil {
		a.scheduleFlush()
	}
	return err
}

func (a *Adapter) ownerAllowed(chatID int64) bool {
	return chatID != 0 && a.state.OwnerChat != 0 && chatID == a.state.OwnerChat
}

func (a *Adapter) senderAllowed(fromID int64) bool {
	if fromID == 0 {
		return true
	}
	if a.state.OwnerUser != 0 {
		return fromID == a.state.OwnerUser
	}
	return fromID == a.state.OwnerChat
}

func (a *Adapter) HandleUpdate(ctx context.Context, update Update) error {
	if update.ID <= 0 || update.Message == nil {
		return nil
	}
	for {
		a.mu.Lock()
		if _, exists := a.state.Processed[fmt.Sprint(update.ID)]; exists {
			a.mu.Unlock()
			return nil
		}
		if claim, exists := a.claims[update.ID]; exists {
			done := claim.done
			a.mu.Unlock()
			select {
			case <-done:
				if claim.err != nil {
					continue
				}
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		claim := &updateClaim{done: make(chan struct{})}
		a.claims[update.ID] = claim
		allowed := a.ownerAllowed(update.Message.ChatID) && a.senderAllowed(update.Message.FromID)
		a.mu.Unlock()

		err := a.deliverUpdate(ctx, update, allowed)
		a.mu.Lock()
		claim.err = err
		if err == nil {
			a.state.Processed[fmt.Sprint(update.ID)] = a.now()
			err = a.saveLocked()
			claim.err = err
		}
		delete(a.claims, update.ID)
		close(claim.done)
		a.mu.Unlock()
		return err
	}
}

func (a *Adapter) deliverUpdate(ctx context.Context, update Update, allowed bool) error {
	text := strings.TrimSpace(update.Message.Text)
	if strings.HasPrefix(text, "/start ") {
		// Possession of the short-lived deep-link code is the explicit owner
		// pairing proof. It is the only update accepted before allowlisting.
		return a.RedeemPairingWithSender(strings.TrimSpace(strings.TrimPrefix(text, "/start ")), update.Message.ChatID, update.Message.FromID)
	}
	if !allowed || text == "/start" || text == "" {
		return nil
	}
	externalID := fmt.Sprint(update.ID)
	if update.Message.ThreadID != 0 {
		a.mu.Lock()
		mapping, ok := a.mappingForThreadLocked(update.Message.ChatID, update.Message.ThreadID)
		a.mu.Unlock()
		if !ok {
			// Forum General carries thread 1 on some clients; route it to
			// Secretary like a thread-less General message. Any other
			// unknown thread is dropped, never auto-promoted to Secretary.
			if update.Message.ThreadID != 1 {
				return nil
			}
		} else {
			err := a.server.SendWorkerMessage(ctx, WorkerMessage{WorkerRef: mapping.WorkerRef, ExternalMessageID: externalID, RequestID: mapping.PendingRequestID, Text: text})
			if err == nil {
				a.reactToMessage(ctx, update.Message)
				a.sendTyping(update.Message.ChatID, update.Message.ThreadID)
			}
			if err == nil && mapping.PendingRequestID != "" {
				a.mu.Lock()
				if current, exists := a.state.Topics[mapping.WorkerRef]; exists && current.ThreadID == mapping.ThreadID {
					current.PendingRequestID = ""
					a.state.Topics[mapping.WorkerRef] = current
					_ = a.saveLocked()
				}
				a.mu.Unlock()
			}
			return err
		}
	}
	if err := a.server.SendMessage(ctx, InboundMessage{ExternalMessageID: externalID, Body: text}); err != nil {
		return err
	}
	a.reactToMessage(ctx, update.Message)
	thread := update.Message.ThreadID
	if thread == 0 {
		thread = 1
	}
	a.sendTyping(update.Message.ChatID, thread)
	return nil
}

func (a *Adapter) mappingForThreadLocked(chatID, threadID int64) (TopicMapping, bool) {
	for _, mapping := range a.state.Topics {
		if mapping.ChatID == chatID && mapping.ThreadID == threadID {
			return mapping, true
		}
	}
	return TopicMapping{}, false
}

// reactToMessage is a best-effort visual acknowledgement after the server accepts the update.
func (a *Adapter) reactToMessage(ctx context.Context, message *Message) {
	if message == nil || message.MessageID <= 0 {
		return
	}
	transport, ok := a.transport.(MessageReactionTransport)
	if !ok {
		return
	}
	_ = transport.SetMessageReaction(ctx, message.ChatID, message.MessageID, "👀")
}

// sendTyping is best-effort liveness: failures never block delivery.
func (a *Adapter) sendTyping(chatID, threadID int64) {
	if chatID == 0 {
		return
	}
	key := fmt.Sprintf("%d:%d", chatID, threadID)
	a.typingMu.Lock()
	if a.lastTyping == nil {
		a.lastTyping = make(map[string]time.Time)
	}
	if last, ok := a.lastTyping[key]; ok && a.now().Sub(last) < 4*time.Second {
		a.typingMu.Unlock()
		return
	}
	a.lastTyping[key] = a.now()
	a.typingMu.Unlock()
	_ = a.transport.SendChatAction(context.Background(), chatID, threadID, "typing")
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
	backoff := 100 * time.Millisecond
	maxBackoff := a.config.PollInterval
	if maxBackoff < backoff {
		maxBackoff = backoff
	}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err := a.PollOnce(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return ctx.Err()
			case <-timer.C:
			}
			if backoff < maxBackoff {
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
			}
			continue
		}
		backoff = 100 * time.Millisecond
	}
}

func (a *Adapter) LastEventSeq() int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.state.LastEventSeq
}

func (a *Adapter) HandleDurableEvent(ctx context.Context, event Event) error {
	if event.Sequence <= 0 {
		return a.HandleEvent(ctx, event)
	}
	a.flushMu.Lock()
	defer a.flushMu.Unlock()

	a.mu.Lock()
	if event.Sequence <= a.state.LastEventSeq {
		a.mu.Unlock()
		return nil
	}
	if containsEventSeq(a.pending.EventSeqs, event.Sequence) {
		a.state.LastEventSeq = event.Sequence
		err := a.saveLocked()
		a.mu.Unlock()
		return err
	}
	a.mu.Unlock()

	if strings.HasPrefix(event.Kind, "secretary.") {
		return a.handleSecretaryEvent(event, event.Sequence)
	}
	if err := a.handleEvent(ctx, event); err != nil {
		return err
	}
	a.mu.Lock()
	if event.Sequence > a.state.LastEventSeq {
		a.state.LastEventSeq = event.Sequence
	}
	err := a.saveLocked()
	a.mu.Unlock()
	return err
}

func (a *Adapter) HandleEvent(ctx context.Context, event Event) error {
	a.flushMu.Lock()
	defer a.flushMu.Unlock()
	return a.handleEvent(ctx, event)
}

func (a *Adapter) handleEvent(ctx context.Context, event Event) error {
	if strings.HasPrefix(event.Kind, "secretary.") {
		return a.handleSecretaryEvent(event, 0)
	}
	if event.Kind == "message.saved" {
		if !strings.HasPrefix(event.Source, "client:") || event.Source == "client:telegram-adapter" || strings.TrimPrefix(event.Source, "client:") == "" {
			return nil
		}
		text := safeText(event.Text)
		if text == "" {
			return nil
		}
		a.mu.Lock()
		owner := a.state.OwnerChat
		a.mu.Unlock()
		if owner == 0 {
			return nil
		}
		identity := event.EventID
		if identity == "" {
			identity = fmt.Sprintf("seq:%d", event.Sequence)
		}
		return a.sendMessage(ctx, OutgoingMessage{ChatID: owner, Text: text, Identity: "inbound:" + identity})
	}
	if event.WorkerRef == "" {
		return nil
	}
	if event.Kind == "worker.created" {
		mapping, err := a.ensureTopic(ctx, event.WorkerRef, event.Title, event.TaskPrompt)
		if err != nil {
			return err
		}
		if prompt := safeDelta(event.TaskPrompt); strings.TrimSpace(prompt) != "" {
			return a.sendMessage(ctx, OutgoingMessage{
				ChatID: mapping.ChatID, ThreadID: mapping.ThreadID,
				Text: "Задача от Secretary:\n\n" + prompt, Identity: "task-prompt:" + event.WorkerRef,
			})
		}
		return nil
	}
	mapping, err := a.ensureTopic(ctx, event.WorkerRef, event.Title)
	if err != nil {
		return err
	}
	if isApprovalResolution(event.Kind) {
		a.mu.Lock()
		mapping.PendingRequestID = ""
		a.state.Topics[event.WorkerRef] = mapping
		if err := a.saveLocked(); err != nil {
			a.mu.Unlock()
			return err
		}
		a.mu.Unlock()
	} else if requestID := requestIDFromPayload(event.Payload); requestID != "" && isRequestEvent(event.Kind) {
		a.mu.Lock()
		mapping.PendingRequestID = requestID
		a.state.Topics[event.WorkerRef] = mapping
		if err := a.saveLocked(); err != nil {
			a.mu.Unlock()
			return err
		}
		a.mu.Unlock()
	}
	if importantWorkerEvent(event.Kind) {
		var body string
		if event.TerminalIdentity != "" {
			body = safeText(event.Text)
			if body == "" {
				body = workerEventLabel(event.Kind)
			}
		} else {
			body = renderWorkerEvent(event)
		}
		if body == "" {
			return nil
		}
		identity := ""
		alreadyNotified := false
		switch {
		case event.TerminalIdentity != "":
			identity = "terminal:" + event.TerminalIdentity
			a.mu.Lock()
			_, alreadyNotified = a.state.TerminalNotified[event.TerminalIdentity]
			a.mu.Unlock()
		case requestNotificationIdentity(event) != "":
			identity = "request:" + requestNotificationIdentity(event)
			a.mu.Lock()
			_, alreadyNotified = a.state.RequestNotified[requestNotificationIdentity(event)]
			a.mu.Unlock()
		}
		// Each destination is deduplicated by sendMessage. A delivered Topic
		// must not prevent retrying its still-pending General mirror.
		if alreadyNotified && event.TerminalIdentity == "" {
			return nil
		}
		a.sendTyping(mapping.ChatID, mapping.ThreadID)
		if err := a.sendMessage(ctx, OutgoingMessage{ChatID: mapping.ChatID, ThreadID: mapping.ThreadID, Text: body, Identity: identity}); err != nil {
			return err
		}
		if event.TerminalIdentity != "" {
			a.mu.Lock()
			owner := a.state.OwnerChat
			a.mu.Unlock()
			if owner != 0 {
				mirror := OutgoingMessage{ChatID: owner, Text: topicName("", mapping.Title) + ":\n" + body, Identity: identity + ":general"}
				if err := a.sendMessage(ctx, mirror); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return nil
}

func (a *Adapter) ensureTopic(ctx context.Context, workerRef, title string, prompts ...string) (TopicMapping, error) {
	a.topicMu.Lock()
	defer a.topicMu.Unlock()
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
	name := topicName(workerRef, title)
	if len(prompts) > 0 && strings.TrimSpace(prompts[0]) != "" {
		name = a.taskTopicTitle(ctx, prompts[0])
	}
	topic, err := a.transport.CreateForumTopic(ctx, owner, name)
	if err != nil {
		return TopicMapping{}, err
	}
	mapping := TopicMapping{WorkerRef: workerRef, ChatID: topic.ChatID, ThreadID: topic.ThreadID, Title: name, CreatedAt: a.now()}
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

func (a *Adapter) persistPendingLocked() error {
	a.state.PendingSecretaryText = a.pending.SecretaryText.String()
	a.state.PendingSecretaryTools = append([]string(nil), a.pending.SecretaryTools...)
	a.state.PendingEventSeqs = append([]int64(nil), a.pending.EventSeqs...)
	a.state.SecretaryReady = a.pending.Ready
	a.state.SecretaryDelegated = a.pending.Delegated
	return nil
}

func containsEventSeq(seqs []int64, wanted int64) bool {
	for _, seq := range seqs {
		if seq == wanted {
			return true
		}
	}
	return false
}

func (a *Adapter) handleSecretaryEvent(event Event, sequence int64) error {
	queued := a.queueSecretary(event)
	a.mu.Lock()
	switch event.Kind {
	case "secretary.turn.queued", "secretary.turn.started":
		a.pending.TurnOpen = true
		a.pending.Delegated = false
		a.pending.Ready = false
	case "secretary.tool_call":
		if strings.Contains(event.Tool, "spawn_worker") {
			a.pending.Delegated = true
		}
	case "secretary.turn.finished":
		a.pending.TurnOpen = false
		a.pending.Delegated = false
		a.pending.Ready = true
	}
	if sequence > 0 {
		if queued && !containsEventSeq(a.pending.EventSeqs, sequence) {
			a.pending.EventSeqs = append(a.pending.EventSeqs, sequence)
		}
		if sequence > a.state.LastEventSeq {
			a.state.LastEventSeq = sequence
		}
	}
	err := a.persistPendingLocked()
	if err == nil {
		err = a.saveLocked()
	}
	a.mu.Unlock()
	if err != nil {
		return err
	}
	a.scheduleFlush()
	return nil
}

func (a *Adapter) queueSecretary(event Event) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	switch event.Kind {
	case "secretary.text_delta":
		if delta := safeDelta(event.Text); delta != "" {
			a.pending.SecretaryText.WriteString(delta)
			return true
		}
	case "secretary.turn.finished":
		if text := safeText(event.Text); text != "" {
			a.pending.SecretaryText.WriteString(text)
			return true
		}
	}
	return false
}

func (a *Adapter) scheduleFlush() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.flushTimer != nil {
		return
	}
	a.flushTimer = time.AfterFunc(a.config.FlushInterval, func() {
		_ = a.Flush(context.Background())
	})
}

func (a *Adapter) Flush(ctx context.Context) error {
	a.flushMu.Lock()
	defer a.flushMu.Unlock()
	a.mu.Lock()
	if a.flushTimer != nil {
		a.flushTimer.Stop()
		a.flushTimer = nil
	}
	owner := a.state.OwnerChat
	text := strings.TrimSpace(a.pending.SecretaryText.String())
	tools := append([]string(nil), a.pending.SecretaryTools...)
	eventSeqs := append([]int64(nil), a.pending.EventSeqs...)
	turnOpen := a.pending.TurnOpen
	ready := a.pending.Ready && !turnOpen
	secretaryMessage, hasSecretary := secretaryBatchMessage(owner, text, tools)
	outboxIdentities := make(map[string]struct{}, len(a.state.Outbox))
	for _, pending := range a.state.Outbox {
		outboxIdentities[outgoingMessageIdentity(pending)] = struct{}{}
	}
	a.mu.Unlock()
	if err := a.drainOutbox(ctx); err != nil {
		return err
	}
	if owner != 0 && (hasSecretary || turnOpen) {
		a.sendTyping(owner, 1)
	}
	if hasSecretary && ready {
		if _, queued := outboxIdentities[outgoingMessageIdentity(secretaryMessage)]; !queued {
			if err := a.sendMessage(ctx, secretaryMessage); err != nil {
				return err
			}
		}
		if err := a.clearSecretaryPending(text, tools, eventSeqs); err != nil {
			return err
		}
	} else if len(eventSeqs) > 0 && !turnOpen {
		if err := a.clearSecretaryPending(text, tools, eventSeqs); err != nil {
			return err
		}
	}
	return nil
}

func secretaryBatchMessage(owner int64, text string, tools []string) (OutgoingMessage, bool) {
	text = safeText(text)
	if owner == 0 || (text == "" && len(tools) == 0) {
		return OutgoingMessage{}, false
	}
	parts := make([]string, 0, 2)
	if text != "" {
		parts = append(parts, text)
	}
	if len(tools) != 0 {
		parts = append(parts, "Шаги: "+strings.Join(uniqueStrings(tools), ", "))
	}
	return OutgoingMessage{ChatID: owner, Text: strings.Join(parts, "\n")}, true
}

func (a *Adapter) clearSecretaryPending(text string, tools []string, seqs []int64) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if strings.TrimSpace(a.pending.SecretaryText.String()) != text || !sameStrings(a.pending.SecretaryTools, tools) {
		return nil
	}
	a.pending.SecretaryText.Reset()
	a.pending.SecretaryTools = nil
	a.pending.EventSeqs = removeEventSeqs(a.pending.EventSeqs, seqs)
	a.pending.Ready = false
	_ = a.persistPendingLocked()
	return a.saveLocked()
}

func removeEventSeqs(all, removed []int64) []int64 {
	if len(removed) == 0 {
		return all
	}
	result := all[:0]
	for _, seq := range all {
		if !containsEventSeq(removed, seq) {
			result = append(result, seq)
		}
	}
	return result
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func outgoingMessageIdentity(message OutgoingMessage) string {
	if message.Identity != "" {
		return message.Identity
	}
	hash := sha256.Sum256([]byte(fmt.Sprintf("%d:%d:%s", message.ChatID, message.ThreadID, message.Text)))
	return "message:" + hex.EncodeToString(hash[:])
}

func (a *Adapter) markDeliveredLocked(message OutgoingMessage) {
	switch {
	case strings.HasPrefix(message.Identity, "task-prompt:"):
		workerRef := strings.TrimPrefix(message.Identity, "task-prompt:")
		if mapping, ok := a.state.Topics[workerRef]; ok {
			mapping.PromptDelivered = true
			a.state.Topics[workerRef] = mapping
		}
	case strings.HasPrefix(message.Identity, "terminal:"):
		identity := strings.TrimPrefix(message.Identity, "terminal:")
		if identity != "" {
			a.state.TerminalNotified[identity] = a.now()
		}
	case strings.HasPrefix(message.Identity, "request:"):
		identity := strings.TrimPrefix(message.Identity, "request:")
		if identity != "" {
			a.state.RequestNotified[identity] = a.now()
		}
	case strings.HasPrefix(message.Identity, "inbound:"):
		identity := strings.TrimPrefix(message.Identity, "inbound:")
		if identity != "" {
			a.state.InboundNotified[identity] = a.now()
		}
	}
}

func removeOutboxMessage(outbox []OutgoingMessage, wanted OutgoingMessage) []OutgoingMessage {
	identity := outgoingMessageIdentity(wanted)
	for index, pending := range outbox {
		if outgoingMessageIdentity(pending) == identity {
			return append(outbox[:index], outbox[index+1:]...)
		}
	}
	return outbox
}

func (a *Adapter) sendMessage(ctx context.Context, message OutgoingMessage) error {
	if message.ChatID == 0 {
		return ErrUnauthorized
	}
	a.mu.Lock()
	if message.Identity != "" {
		if strings.HasPrefix(message.Identity, "task-prompt:") {
			if a.state.Topics[strings.TrimPrefix(message.Identity, "task-prompt:")].PromptDelivered {
				a.mu.Unlock()
				return nil
			}
		}
		if strings.HasPrefix(message.Identity, "terminal:") {
			if _, delivered := a.state.TerminalNotified[strings.TrimPrefix(message.Identity, "terminal:")]; delivered {
				a.mu.Unlock()
				return nil
			}
		}
		if strings.HasPrefix(message.Identity, "request:") {
			if _, delivered := a.state.RequestNotified[strings.TrimPrefix(message.Identity, "request:")]; delivered {
				a.mu.Unlock()
				return nil
			}
		}
		if strings.HasPrefix(message.Identity, "inbound:") {
			if _, delivered := a.state.InboundNotified[strings.TrimPrefix(message.Identity, "inbound:")]; delivered {
				a.mu.Unlock()
				return nil
			}
		}
	}
	alreadyPending := false
	identity := outgoingMessageIdentity(message)
	for _, pending := range a.state.Outbox {
		if outgoingMessageIdentity(pending) == identity {
			alreadyPending = true
			break
		}
	}
	if !alreadyPending {
		a.state.Outbox = append(a.state.Outbox, message)
		if err := a.saveLocked(); err != nil {
			a.mu.Unlock()
			return err
		}
	}
	a.mu.Unlock()
	return a.drainOutbox(ctx)
}

func (a *Adapter) drainOutbox(ctx context.Context) error {
	a.mu.Lock()
	outbox := append([]OutgoingMessage(nil), a.state.Outbox...)
	a.mu.Unlock()
	for _, message := range outbox {
		fences := telegramFenceBlocks(message.Text)
		if message.ChatID == 0 {
			a.mu.Lock()
			a.state.Outbox = removeOutboxMessage(a.state.Outbox, message)
			err := a.saveLocked()
			a.mu.Unlock()
			if err != nil {
				return err
			}
			continue
		}
		for {
			chunk := message
			prepared := prepareTelegramChunk(message.Text, message.DeliveredBytes, fences)
			if prepared.sourceBytes <= 0 {
				return errors.New("telegram: chunker made no source progress")
			}
			chunk.Text, chunk.ParseMode = prepared.formatted, "HTML"
			if message.PlainFallback {
				chunk.Text, chunk.ParseMode = prepared.plain, ""
			}
			err := a.transport.SendMessage(ctx, chunk)
			if errors.Is(err, ErrFormatting) && chunk.ParseMode != "" {
				// A known parse rejection proves this send was not accepted.
				// Persist fallback before attempting it; transport failures with
				// uncertain acceptance must never trigger a second immediate send.
				message.PlainFallback = true
				a.mu.Lock()
				for index, pending := range a.state.Outbox {
					if outgoingMessageIdentity(pending) == outgoingMessageIdentity(message) {
						a.state.Outbox[index].PlainFallback = true
					}
				}
				saveErr := a.saveLocked()
				a.mu.Unlock()
				if saveErr != nil {
					return saveErr
				}
				chunk.Text, chunk.ParseMode = prepared.plain, ""
				err = a.transport.SendMessage(ctx, chunk)
			}
			if err != nil {
				return err
			}
			// Checkpoint only consumed original UTF-8 source bytes. Escaped HTML
			// and generated balanced-fence tags are presentation, not source.
			message.DeliveredBytes += prepared.sourceBytes
			done := message.DeliveredBytes >= len(message.Text)
			a.mu.Lock()
			if done {
				a.state.Outbox = removeOutboxMessage(a.state.Outbox, message)
				a.markDeliveredLocked(message)
			} else {
				for index, pending := range a.state.Outbox {
					if outgoingMessageIdentity(pending) == outgoingMessageIdentity(message) {
						a.state.Outbox[index].DeliveredBytes = message.DeliveredBytes
						break
					}
				}
			}
			err = a.saveLocked()
			a.mu.Unlock()
			if err != nil {
				return err
			}
			if done {
				break
			}
		}
	}
	return nil
}

type preparedTelegramChunk struct {
	sourceBytes int
	formatted   string
	plain       string
}

type telegramFenceBlock struct {
	start, bodyStart, bodyEnd, end int
}

func telegramFenceBlocks(text string) []telegramFenceBlock {
	var blocks []telegramFenceBlock
	for lineStart := 0; lineStart < len(text); {
		lineEnd, nextLine := telegramLineEnd(text, lineStart)
		line := text[lineStart:lineEnd]
		if !strings.HasPrefix(line, "```") {
			lineStart = nextLine
			continue
		}
		for closeStart := nextLine; closeStart <= len(text); {
			closeEnd, closeNext := telegramLineEnd(text, closeStart)
			if strings.TrimSpace(text[closeStart:closeEnd]) == "```" {
				blocks = append(blocks, telegramFenceBlock{start: lineStart, bodyStart: nextLine, bodyEnd: closeStart, end: closeNext})
				lineStart = closeNext
				break
			}
			if closeEnd == len(text) {
				lineStart = len(text)
				break
			}
			closeStart = closeNext
		}
	}
	return blocks
}

func telegramLineEnd(text string, start int) (end, next int) {
	if start >= len(text) {
		return len(text), len(text)
	}
	if relative := strings.IndexByte(text[start:], '\n'); relative >= 0 {
		end = start + relative
		return end, end + 1
	}
	return len(text), len(text)
}

func prepareTelegramChunk(text string, offset int, fences []telegramFenceBlock) preparedTelegramChunk {
	if offset < 0 || offset >= len(text) {
		return preparedTelegramChunk{}
	}
	for _, block := range fences {
		if offset < block.start || offset >= block.end || offset >= block.bodyEnd {
			continue
		}
		bodyOffset := offset
		if bodyOffset < block.bodyStart {
			bodyOffset = block.bodyStart
		}
		body := telegramCodeBodyChunk(text[bodyOffset:block.bodyEnd])
		consumed := bodyOffset - offset + len(body)
		if bodyOffset+len(body) >= block.bodyEnd {
			consumed += block.end - block.bodyEnd
		}
		formatted, plain := formatTelegramCodeBlock(body)
		return preparedTelegramChunk{sourceBytes: consumed, formatted: formatted, plain: plain}
	}
	raw := telegramMessageChunk(text[offset:])
	formatted, plain := formatTelegramChunk(raw)
	return preparedTelegramChunk{sourceBytes: len(raw), formatted: formatted, plain: plain}
}

func telegramCodeBodyChunk(text string) string {
	graphemes := uniseg.NewGraphemes(text)
	units, hardEnd, lineEnd := 0, 0, 0
	for graphemes.Next() {
		clusterUnits := telegramUTF16Units(graphemes.Str())
		if units+clusterUnits > 4096 {
			break
		}
		units += clusterUnits
		_, hardEnd = graphemes.Positions()
		if strings.HasSuffix(text[:hardEnd], "\n") {
			lineEnd = hardEnd
		}
	}
	if hardEnd == len(text) {
		return text
	}
	if lineEnd > 0 {
		return text[:lineEnd]
	}
	if hardEnd > 0 {
		return text[:hardEnd]
	}
	return telegramCodePointChunk(text, 4096)
}

// Keep chunks within Telegram's parsed-text limit. Source syntax is counted
// conservatively; source-byte checkpoints exclude rendered HTML and synthetic tags.
func telegramMessageChunk(text string) string {
	graphemes := uniseg.NewGraphemes(text)
	units, hardEnd := 0, 0
	for graphemes.Next() {
		clusterUnits := telegramUTF16Units(graphemes.Str())
		if units+clusterUnits > 4096 {
			break
		}
		units += clusterUnits
		_, hardEnd = graphemes.Positions()
	}
	if hardEnd == len(text) {
		return text
	}
	atoms := telegramMarkdownAtoms(text, hardEnd)
	graphemes.Reset()
	safeEnd, paragraphEnd, listEnd, sentenceEnd := 0, 0, 0, 0
	for graphemes.Next() {
		_, end := graphemes.Positions()
		if end > hardEnd {
			break
		}
		if telegramInsideAtom(atoms, end) {
			continue
		}
		safeEnd = end
		if graphemes.IsSentenceBoundary() && telegramSentenceTerminator(text[:end]) {
			sentenceEnd = end
		}
		prefix := text[:end]
		if strings.HasSuffix(prefix, "\n\n") {
			paragraphEnd = end
		}
		if strings.HasSuffix(prefix, "\n") {
			lineEnd := end - 1
			lineStart := strings.LastIndexByte(text[:lineEnd], '\n') + 1
			if isTelegramListItem(text[lineStart:lineEnd]) {
				listEnd = end
			}
		}
	}
	semanticEnd := paragraphEnd
	if listEnd > semanticEnd {
		semanticEnd = listEnd
	}
	if sentenceEnd > semanticEnd {
		semanticEnd = sentenceEnd
	}
	if semanticEnd > 0 {
		return text[:semanticEnd]
	}
	if safeEnd > 0 {
		return text[:safeEnd]
	}
	if hardEnd > 0 {
		return text[:hardEnd]
	}
	return telegramCodePointChunk(text, 4096)
}

type telegramTextRange struct{ start, end int }

func telegramMarkdownAtoms(text string, maxStart int) []telegramTextRange {
	var ranges []telegramTextRange
	for lineStart := 0; lineStart < len(text) && lineStart < maxStart; {
		lineEnd, nextLine := telegramLineEnd(text, lineStart)
		line := text[lineStart:lineEnd]
		if isTelegramListItem(line) && telegramFitsUTF16(text[lineStart:nextLine], 4096) {
			ranges = append(ranges, telegramTextRange{lineStart, nextLine})
		}
		if strings.HasPrefix(line, "```") {
			for closeStart := nextLine; closeStart <= len(text); {
				closeEnd := closeStart + strings.IndexByte(text[closeStart:], '\n')
				if closeEnd < closeStart {
					closeEnd = len(text)
				}
				closeNext := closeEnd
				if closeNext < len(text) {
					closeNext++
				}
				if strings.TrimSpace(text[closeStart:closeEnd]) == "```" {
					ranges = append(ranges, telegramTextRange{lineStart, closeNext})
					break
				}
				if closeEnd == len(text) {
					break
				}
				closeStart = closeNext
			}
			lineStart = nextLine
			continue
		}
		for index := 0; index < len(line) && lineStart+index < maxStart; {
			if line[index] == '[' {
				if end, ok := telegramMarkdownLinkEnd(line, index); ok {
					ranges = append(ranges, telegramTextRange{lineStart + index, lineStart + end})
					index = end
					continue
				}
			}
			token := ""
			for _, candidate := range []string{"**", "__", "`"} {
				if strings.HasPrefix(line[index:], candidate) {
					token = candidate
					break
				}
			}
			if token != "" {
				close := strings.Index(line[index+len(token):], token)
				if close > 0 {
					end := index + len(token) + close + len(token)
					ranges = append(ranges, telegramTextRange{lineStart + index, lineStart + end})
					index = end
					continue
				}
			}
			_, size := utf8.DecodeRuneInString(line[index:])
			index += size
		}
		lineStart = nextLine
	}
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].start < ranges[j].start })
	merged := ranges[:0]
	for _, current := range ranges {
		if len(merged) == 0 || current.start >= merged[len(merged)-1].end {
			merged = append(merged, current)
			continue
		}
		if current.end > merged[len(merged)-1].end {
			merged[len(merged)-1].end = current.end
		}
	}
	return merged
}

func telegramMarkdownLinkEnd(line string, start int) (int, bool) {
	labelEnd := strings.Index(line[start+1:], "](")
	if labelEnd <= 0 {
		return 0, false
	}
	targetStart := start + 1 + labelEnd + 2
	depth, end := 1, targetStart
	for end < len(line) && depth > 0 {
		if line[end] == '(' {
			depth++
		} else if line[end] == ')' {
			depth--
		}
		end++
	}
	return end, depth == 0
}

func telegramInsideAtom(ranges []telegramTextRange, position int) bool {
	index := sort.Search(len(ranges), func(i int) bool { return ranges[i].end >= position })
	return index < len(ranges) && ranges[index].start < position && position < ranges[index].end
}

func isTelegramListItem(line string) bool {
	line = strings.TrimLeft(line, " \t")
	if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "+ ") || strings.HasPrefix(line, "* ") {
		return true
	}
	index := 0
	for index < len(line) && line[index] >= '0' && line[index] <= '9' {
		index++
	}
	return index > 0 && index+1 < len(line) && (line[index] == '.' || line[index] == ')') && line[index+1] == ' '
}

func telegramSentenceTerminator(prefix string) bool {
	trimmed := strings.TrimRightFunc(prefix, unicode.IsSpace)
	last, _ := utf8.DecodeLastRuneInString(trimmed)
	return unicode.Is(unicode.Properties["Sentence_Terminal"], last)
}

func telegramFitsUTF16(text string, limit int) bool {
	units := 0
	for _, r := range text {
		units++
		if r > 0xffff {
			units++
		}
		if units > limit {
			return false
		}
	}
	return true
}

func telegramUTF16Units(text string) int {
	units := 0
	for _, r := range text {
		units++
		if r > 0xffff {
			units++
		}
	}
	return units
}

// A single extended grapheme can itself exceed Telegram's limit (for example,
// a base followed by thousands of combining marks). In that impossible case,
// split only at UTF-8 code-point boundaries so delivery always makes progress.
func telegramCodePointChunk(text string, limit int) string {
	units := 0
	for index, r := range text {
		nextUnits := 1
		if r > 0xffff {
			nextUnits = 2
		}
		if units+nextUnits > limit {
			return text[:index]
		}
		units += nextUnits
	}
	return text
}

func isRequestEvent(kind string) bool {
	switch kind {
	case "worker.approval_requested", "worker.needs_input", "approval.requested":
		return true
	default:
		return false
	}
}

func isApprovalResolution(kind string) bool {
	switch kind {
	case "worker.approval_resolved", "approval.resolved", "approval.approved", "approval.denied", "approval.revoked":
		return true
	default:
		return false
	}
}

func importantWorkerEvent(kind string) bool {
	return kind == "worker.tool_started" || kind == "worker.tool_finished" || kind == "worker.tool_failed" || strings.Contains(kind, "approval") || strings.Contains(kind, "needs_input") || strings.HasSuffix(kind, ".failed") || strings.HasSuffix(kind, ".completed") || strings.HasSuffix(kind, ".succeeded") || strings.HasSuffix(kind, ".canceled") || strings.HasSuffix(kind, ".offline") || strings.HasSuffix(kind, ".completion")
}

func workerEventLabel(kind string) string {
	if label := map[string]string{"worker.approval_requested": "Нужно разрешение", "worker.needs_input": "Нужен ответ", "worker.failed": "Ошибка Worker", "worker.completed": "Worker завершён", "worker.succeeded": "Worker завершён", "worker.canceled": "Worker отменён", "worker.offline": "Node offline"}[kind]; label != "" {
		return label
	}
	return "Статус Worker"
}

func renderWorkerEvent(event Event) string {
	if event.Kind == "worker.tool_started" || event.Kind == "worker.tool_finished" || event.Kind == "worker.tool_failed" {
		tool := safeText(event.Tool)
		if tool == "" {
			return ""
		}
		state := "▶"
		if event.Kind == "worker.tool_finished" {
			state = "✓"
		} else if event.Kind == "worker.tool_failed" {
			state = "✗"
		}
		preview := safeText(event.ToolPreview)
		if preview != "" {
			return state + " " + tool + " " + preview
		}
		return state + " " + tool
	}
	label := workerEventLabel(event.Kind)
	text := safeText(event.Text)
	if text == "" {
		return label
	}
	return label + ": " + text
}

func topicName(workerRef, title string) string {
	name := strings.Join(strings.Fields(safeText(title)), " ")
	if name == "" {
		name = "Worker"
	}
	characters := []rune(name)
	if len(characters) > 60 {
		name = string(characters[:60])
	}
	return name
}

var sensitiveText = regexp.MustCompile(`(?i)\b(?:node|channel|callback|runtime|task|session|native|worker|attempt|turn|api|credential|token|secret|password|analysis|reasoning|thought)[_ -]?(?:token|secret|capability|session|id|ref|key|credential)?\s*[:=]\s*[^,;[:space:]]+`)
var sensitiveMarkerValue = regexp.MustCompile(`(?i)\b(?:task|session|native|worker|attempt|turn|node|channel|callback|token|secret|reasoning|thought|analysis|credential|password)[_-][a-z0-9][a-z0-9._/-]*\b`)
var secretPrefix = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(?:sk-|ghp_|xoxb-|xoxb_)[a-z0-9_-]+`)
var rawCoTMarker = regexp.MustCompile(`(?i)\b(?:raw[-_ ]?cot|cot)\b`)
var sensitiveAssignment = regexp.MustCompile(`(?i)\b(?:analysis|reasoning|thought|password|token|secret|credential|task|session|native|channel)\s*[:=]`)

func safeText(text string) string {
	return sanitizeTelegramText(strings.TrimSpace(text), true)
}

func safeDelta(text string) string {
	return sanitizeTelegramText(text, false)
}

func sanitizeTelegramText(text string, normalize bool) string {
	if text == "" || (normalize && strings.TrimSpace(text) == "") {
		return ""
	}
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		var value any
		if json.Unmarshal([]byte(trimmed), &value) == nil {
			cleaned, _ := sanitizeTelegramValue(value)
			encoded, err := json.Marshal(cleaned)
			if err == nil {
				return string(encoded)
			}
			return "[redacted]"
		}
	}
	if forbiddenTelegramText(text) {
		return "[redacted]"
	}
	text = sensitiveText.ReplaceAllString(text, "[redacted]")
	text = sensitiveMarkerValue.ReplaceAllString(text, "[redacted]")
	if normalize {
		text = strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	}
	return text
}

func sanitizeTelegramValue(value any) (any, bool) {
	switch current := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(current))
		for key, child := range current {
			if forbiddenTelegramKey(key) {
				continue
			}
			cleaned, _ := sanitizeTelegramValue(child)
			result[key] = cleaned
		}
		return result, true
	case []any:
		result := make([]any, len(current))
		for index, child := range current {
			result[index], _ = sanitizeTelegramValue(child)
		}
		return result, true
	case string:
		trimmed := strings.TrimSpace(current)
		if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
			var nested any
			if json.Unmarshal([]byte(trimmed), &nested) == nil {
				cleaned, _ := sanitizeTelegramValue(nested)
				encoded, err := json.Marshal(cleaned)
				if err == nil {
					return string(encoded), true
				}
				return "[redacted]", true
			}
		}
		return sanitizeTelegramText(current, false), true
	default:
		return value, true
	}
}

func forbiddenTelegramKey(key string) bool {
	compact := strings.ToLower(strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return -1
	}, key))
	for _, marker := range []string{"secret", "credential", "callback", "token", "password", "authorization", "apikey", "accesskey", "privatekey", "task", "session", "native", "channel", "analysis", "reasoning", "thought", "chainofthought"} {
		if strings.Contains(compact, marker) {
			return true
		}
	}
	return false
}

func forbiddenTelegramText(value string) bool {
	lower := strings.ToLower(value)
	for _, marker := range []string{"chain-of-thought", "chain of thought", "chain_of_thought", "raw thought", "raw_thought", "internal reasoning", "internal_reasoning", "thought process", "thought_process", "<think>", "</think>", "bearer ", "api_key=", "apikey=", "access_token", "api_token", "runtime_session_id", "session_id", "sessionid", "native_id"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return sensitiveAssignment.MatchString(value) || secretPrefix.MatchString(value) || rawCoTMarker.MatchString(value)
}

func requestNotificationIdentity(event Event) string {
	requestID := requestIDFromPayload(event.Payload)
	if requestID == "" {
		return ""
	}
	kind := ""
	switch event.Kind {
	case "worker.needs_input":
		kind = "needs_input"
	case "worker.approval_requested":
		kind = "approval_requested"
	case "approval.requested":
		var payload map[string]any
		if json.Unmarshal(event.Payload, &payload) == nil {
			payloadKind, _ := payload["kind"].(string)
			if strings.EqualFold(payloadKind, "input") {
				kind = "needs_input"
			} else {
				kind = "approval_requested"
			}
		} else {
			kind = "approval_requested"
		}
	}
	if kind == "" {
		return ""
	}
	return kind + ":" + requestID
}

func requestIDFromPayload(payload json.RawMessage) string {
	if len(payload) == 0 {
		return ""
	}
	var value map[string]any
	if json.Unmarshal(payload, &value) != nil {
		return ""
	}
	return requestIDInMap(value)
}

func requestIDInMap(value map[string]any) string {
	if candidate, ok := value["request_id"].(string); ok && strings.TrimSpace(candidate) != "" {
		return strings.TrimSpace(candidate)
	}
	for _, nested := range value {
		switch child := nested.(type) {
		case map[string]any:
			if requestID := requestIDInMap(child); requestID != "" {
				return requestID
			}
		case []any:
			for _, item := range child {
				if object, ok := item.(map[string]any); ok {
					if requestID := requestIDInMap(object); requestID != "" {
						return requestID
					}
				}
			}
		}
	}
	return ""
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
