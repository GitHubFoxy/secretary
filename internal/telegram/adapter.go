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

	"github.com/beruseruko/secretary/internal/config"
	"github.com/rivo/uniseg"
)

var (
	ErrUnauthorized = errors.New("telegram: chat is not allowlisted")
	ErrPairingUsed  = errors.New("telegram: pairing code is expired or already used")
)

type Update struct {
	ID              int64    `json:"update_id"`
	WorkerRequestID *string  `json:"worker_request_id,omitempty"`
	Message         *Message `json:"message,omitempty"`
}

type Message struct {
	ChatType  string `json:"chat_type,omitempty"`
	IsForum   bool   `json:"is_forum,omitempty"`
	ChatID    int64  `json:"chat_id"`
	FromID    int64  `json:"from_id"`
	MessageID int64  `json:"message_id"`
	ThreadID  int64  `json:"message_thread_id,omitempty"`
	Text      string `json:"text,omitempty"`
	Voice     *Voice `json:"voice,omitempty"`
}

type OutgoingMessage struct {
	ChatID             int64
	ThreadID           int64
	Text               string
	ReplyToMessageID   int64  `json:"reply_to_message_id,omitempty"`
	Identity           string `json:"identity,omitempty"`
	DurableSequence    int64  `json:"durable_sequence,omitempty"`
	DeliveredBytes     int    `json:"delivered_bytes,omitempty"`
	ParseMode          string `json:"parse_mode,omitempty"`
	PlainFallback      bool   `json:"plain_fallback,omitempty"`
	DisableLinkPreview bool   `json:"disable_link_preview,omitempty"`
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
	ResumeWorker(context.Context, string) error
}

type WorkerTopic struct {
	WorkerRef string
	Title     string
	Intent    string
}

type Config struct {
	StatePath      string
	OwnerChatID    int64
	// nil preserves the legacy one-chat mode; non-nil enables DM-first routing.
	// Zero means the optional forum supergroup has not yet been linked.
	GroupChatID    *int64
	// ExistingWorkers reads the authoritative Server when linking a group.
	ExistingWorkers func(context.Context) ([]WorkerTopic, error)
	ShowToolCalls  *bool
	PollInterval   time.Duration
	FlushInterval  time.Duration
	BotUsername    string
	TitleGenerator func(context.Context, string) (string, error)
	TitleTimeout   time.Duration
	ASR            config.ASRConfig
	ASRPrompt      string
	ASRTranscriber ASRTranscriber
	Formatter      TextFormatter
}

type Pairing struct {
	Code       string    `json:"code"`
	DeepLink   string    `json:"deep_link"`
	ExpiresAt  time.Time `json:"expires_at"`
	Credential string    `json:"-"`
}

type Event struct {
	EventID                string
	Sequence               int64
	Kind                   string
	Source                 string
	WorkerRef              string
	AttemptID              string
	Title                  string
	TaskPrompt             string
	Text                   string
	Tool                   string
	ToolPreview            string
	CallID                 string
	NodeID                 string
	HasOutput              bool
	TerminalIdentity       string
	CanonicalReplyRequired bool
	WorkerAttemptTerminal  bool
	Payload                json.RawMessage
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
	Version                 int                            `json:"version"`
	OwnerChat               int64                          `json:"owner_chat_id"`
	OwnerUser               int64                          `json:"owner_user_id,omitempty"`
	GroupChat               int64                          `json:"group_chat_id,omitempty"`
	Delivered               map[string]time.Time           `json:"delivered_messages,omitempty"`
	DeliveredSequence       map[string]int64               `json:"delivered_sequences,omitempty"`
	Offset                  int64                          `json:"offset"`
	LastEventSeq            int64                          `json:"last_event_seq"`
	Processed               map[string]time.Time           `json:"processed_updates"`
	Topics                  map[string]TopicMapping        `json:"topics"`
	Pairings                map[string]pairingRecord       `json:"pairings"`
	Outbox                  []OutgoingMessage              `json:"outbox,omitempty"`
	PendingSecretaryText    string                         `json:"pending_secretary_text,omitempty"`
	PendingSecretaryTools   []string                       `json:"pending_secretary_tools,omitempty"`
	PendingEventSeqs        []int64                        `json:"pending_event_seqs,omitempty"`
	SecretaryReady          bool                           `json:"secretary_ready,omitempty"`
	SecretaryDelegated      bool                           `json:"secretary_delegated,omitempty"`
	TerminalNotified        map[string]time.Time           `json:"terminal_notified,omitempty"`
	RequestNotified         map[string]time.Time           `json:"request_notified,omitempty"`
	InboundNotified         map[string]time.Time           `json:"inbound_notified,omitempty"`
	ToolNotified            map[string]time.Time           `json:"tool_notified,omitempty"`
	ToolPreviews            map[string]string              `json:"tool_previews,omitempty"`
	WorkerTyping            map[string]workerTypingAttempt `json:"worker_typing,omitempty"`
	WorkerTypingInitialized bool                           `json:"worker_typing_initialized"`
	PreviousOwnerBound      bool                           `json:"previous_owner_bound,omitempty"`
	ReboundAt               time.Time                      `json:"rebound_at,omitempty"`
	RebindReason            string                         `json:"rebind_reason,omitempty"`
	RebindWatermark         int64                          `json:"rebind_watermark,omitempty"`
	PendingInputs           []Update                       `json:"pending_inputs,omitempty"`
	Transcripts             map[string]VoiceTranscript     `json:"transcripts,omitempty"`
}

type workerTypingAttempt struct {
	WorkerRef string `json:"worker_ref"`
	AttemptID string `json:"attempt_id"`
	ChatID    int64  `json:"chat_id"`
	ThreadID  int64  `json:"thread_id"`
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
	asr       ASRTranscriber
	formatter TextFormatter

	mu                         sync.Mutex
	topicMu                    sync.Mutex
	flushMu                    sync.Mutex
	typingMu                   sync.Mutex
	state                      persistedState
	workerTypingRecoveryNeeded bool
	topicsSynchronized         bool
	pending                    pendingBatch
	lastTyping                 map[string]time.Time
	outboxMu                   sync.Mutex
	inputWG                    sync.WaitGroup
	inputMu                    sync.Mutex
	inputLanes                 map[string]*sync.Mutex
	inputWorkers               map[string]bool
	claims                     map[int64]*updateClaim
	flushTimer                 *time.Timer
	now                        func() time.Time
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
	adapter.state = persistedState{Version: 1, OwnerChat: config.OwnerChatID, Delivered: map[string]time.Time{}, DeliveredSequence: map[string]int64{}, Processed: map[string]time.Time{}, Topics: map[string]TopicMapping{}, Pairings: map[string]pairingRecord{}, TerminalNotified: map[string]time.Time{}, RequestNotified: map[string]time.Time{}, InboundNotified: map[string]time.Time{}, ToolNotified: map[string]time.Time{}, ToolPreviews: map[string]string{}, WorkerTyping: map[string]workerTypingAttempt{}, WorkerTypingInitialized: true, Outbox: []OutgoingMessage{}, Transcripts: map[string]VoiceTranscript{}}
	if err := adapter.load(); err != nil {
		return nil, err
	}
	asr := config.ASRTranscriber
	if asr == nil && config.ASR.Mode == "local" {
		nemo := NewNemoASR(config.ASR.Engine, config.ASR.Model, time.Duration(config.ASR.TimeoutSeconds)*time.Second)
		if err := nemo.Validate(); err != nil {
			return nil, err
		}
		if config.ASR.MaxAudioSeconds > 0 {
			nemo.MaxAudioSeconds = config.ASR.MaxAudioSeconds
		}
		nemo.TempRoot = filepath.Join(filepath.Dir(config.StatePath), "asr-tmp")
		if err := os.MkdirAll(nemo.TempRoot, 0700); err != nil {
			return nil, err
		}
		entries, err := os.ReadDir(nemo.TempRoot)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), "secretary-asr-") {
				if err := os.RemoveAll(filepath.Join(nemo.TempRoot, entry.Name())); err != nil {
					return nil, err
				}
			}
		}
		asr = nemo
	}
	formatter := config.Formatter
	if formatter == nil && config.ASR.Formatter.Enabled {
		formatter = &CodexFormatter{
			Enabled:   config.ASR.Formatter.Enabled,
			Model:     config.ASR.Formatter.Model,
			Reasoning: config.ASR.Formatter.Reasoning,
			Prompt:    config.ASRPrompt,
			Timeout:   time.Duration(config.ASR.Formatter.TimeoutSeconds) * time.Second,
		}
	}
	adapter.asr = asr
	adapter.formatter = formatter
	// Pre-T04 installations used OwnerChat for the forum itself. An
	// explicit private owner ID upgrades durable state without clearing Topics,
	// outbox, cursors or pairing metadata.
	if adapter.state.OwnerChat < 0 && config.OwnerChatID <= 0 || config.OwnerChatID < 0 {
		adapter.config.GroupChatID = nil
	}
	if adapter.state.OwnerChat < 0 && config.OwnerChatID > 0 {
		if adapter.state.OwnerUser != 0 && adapter.state.OwnerUser != config.OwnerChatID {
			return nil, errors.New("telegram: private owner differs from paired legacy owner user")
		}
		if adapter.state.GroupChat != 0 && adapter.state.GroupChat != adapter.state.OwnerChat {
			return nil, errors.New("telegram: legacy forum differs from linked group")
		}
		if config.GroupChatID != nil && *config.GroupChatID != 0 && *config.GroupChatID != adapter.state.OwnerChat {
			return nil, errors.New("telegram: configured forum differs from legacy owner chat")
		}
		adapter.state.GroupChat = adapter.state.OwnerChat
		adapter.state.OwnerChat = config.OwnerChatID
		adapter.state.OwnerUser = config.OwnerChatID
	}
	if config.OwnerChatID != 0 && adapter.state.OwnerChat != 0 && config.OwnerChatID != adapter.state.OwnerChat {
		return nil, errors.New("telegram: configured owner does not match durable allowlist")
	}
	if adapter.state.OwnerChat == 0 {
		adapter.state.OwnerChat = config.OwnerChatID
	}
	if adapter.config.GroupChatID != nil && *adapter.config.GroupChatID != 0 {
		if *adapter.config.GroupChatID >= 0 {
			return nil, errors.New("telegram: forum group chat id must be negative")
		}
		if adapter.state.GroupChat != 0 && adapter.state.GroupChat != *adapter.config.GroupChatID {
			return nil, errors.New("telegram: configured group does not match linked group")
		}
		adapter.state.GroupChat = *adapter.config.GroupChatID
	}
	// A persisted two-chat installation must keep both projections even if
	// callers omit the newly added optional config field on a later restart.
	if adapter.config.GroupChatID == nil && adapter.state.OwnerChat > 0 && adapter.state.GroupChat < 0 {
		linked := adapter.state.GroupChat
		adapter.config.GroupChatID = &linked
	}
	if err := adapter.saveLocked(); err != nil {
		return nil, err
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
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return fmt.Errorf("telegram: decode state: %w", err)
	}
	if err := json.Unmarshal(data, &a.state); err != nil {
		return fmt.Errorf("telegram: decode state: %w", err)
	}
	if _, ok := fields["worker_typing_initialized"]; !ok {
		a.workerTypingRecoveryNeeded = a.state.LastEventSeq > 0
		a.state.WorkerTypingInitialized = !a.workerTypingRecoveryNeeded
	} else {
		a.workerTypingRecoveryNeeded = !a.state.WorkerTypingInitialized
	}
	if a.state.Version == 0 {
		a.state.Version = 1
	}
	if a.state.Processed == nil {
		a.state.Processed = map[string]time.Time{}
	}
	if a.state.Delivered == nil {
		a.state.Delivered = map[string]time.Time{}
	}
	if a.state.DeliveredSequence == nil {
		a.state.DeliveredSequence = map[string]int64{}
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
	if a.state.ToolNotified == nil {
		a.state.ToolNotified = map[string]time.Time{}
	}
	if a.state.ToolPreviews == nil {
		a.state.ToolPreviews = map[string]string{}
	}
	if a.state.WorkerTyping == nil {
		a.state.WorkerTyping = map[string]workerTypingAttempt{}
	}
	if a.state.Transcripts == nil {
		a.state.Transcripts = map[string]VoiceTranscript{}
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
	state.WorkerTyping = map[string]workerTypingAttempt{}
	state.WorkerTypingInitialized = true
	state.Pairings = map[string]pairingRecord{}
	state.Outbox = nil
	state.PendingInputs = nil
	state.Transcripts = nil
	state.PendingSecretaryText = ""
	state.PendingSecretaryTools = nil
	state.PendingEventSeqs = nil
	state.SecretaryReady = false
	state.SecretaryDelegated = false
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
	if a.config.GroupChatID != nil && (chatID < 0 || fromID == 0 || fromID != chatID) {
		return ErrUnauthorized
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
	if chatID == 0 || a.state.OwnerChat == 0 {
		return false
	}
	return chatID == a.state.OwnerChat ||
		(a.config.GroupChatID != nil && chatID < 0 && chatID == a.state.GroupChat)
}

func (a *Adapter) senderAllowed(fromID int64) bool {
	if fromID == 0 && a.config.GroupChatID != nil {
		return false
	}
	if fromID == 0 {
		return true
	}
	if a.state.OwnerUser != 0 {
		return fromID == a.state.OwnerUser
	}
	return fromID == a.state.OwnerChat
}

// linkGroup accepts a command only from the already-paired owner, sent from a
// forum supergroup. Group membership alone never grants control.
func (a *Adapter) linkGroup(ctx context.Context, update Update) error {
	m := update.Message
	if a.config.GroupChatID == nil || m == nil || m.ChatID >= 0 || m.ChatType != "supergroup" || !m.IsForum || m.FromID == 0 {
		return nil
	}
	a.mu.Lock()
	if a.state.OwnerChat <= 0 || !a.senderAllowed(m.FromID) {
		a.mu.Unlock()
		return nil
	}
	if a.state.GroupChat != 0 && a.state.GroupChat != m.ChatID {
		a.mu.Unlock()
		return nil
	}
	a.state.GroupChat = m.ChatID
	a.topicsSynchronized = false
	err := a.saveLocked()
	a.mu.Unlock()
	if err != nil {
		return err
	}
	return a.SyncWorkerTopics(ctx)
}

// SyncWorkerTopics restores mappings for Workers created before the group was
// linked. It can be retried after restart without duplicate Topic creation.
func (a *Adapter) SyncWorkerTopics(ctx context.Context) error {
	a.mu.Lock()
	needed := a.config.GroupChatID != nil && a.state.GroupChat != 0 &&
		!a.topicsSynchronized && a.config.ExistingWorkers != nil
	lookup := a.config.ExistingWorkers
	a.mu.Unlock()
	if !needed {
		return nil
	}
	workers, err := lookup(ctx)
	if err != nil {
		return err
	}
	for _, worker := range workers {
		if worker.WorkerRef == "" {
			continue
		}
		if _, err := a.ensureTopic(ctx, worker.WorkerRef, worker.Title, worker.Intent); err != nil {
			return err
		}
	}
	a.mu.Lock()
	a.topicsSynchronized = true
	a.mu.Unlock()
	return nil
}

func (a *Adapter) personalChats() []int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.state.OwnerChat == 0 {
		return nil
	}
	chats := []int64{a.state.OwnerChat}
	if a.config.GroupChatID != nil && a.state.GroupChat != 0 && a.state.GroupChat != a.state.OwnerChat {
		chats = append(chats, a.state.GroupChat)
	}
	return chats
}

// sendPersonal sends projections, never another canonical Server input.
func (a *Adapter) sendPersonal(ctx context.Context, message OutgoingMessage) error {
	for _, chatID := range a.personalChats() {
		copy := message
		copy.ChatID = chatID
		copy.ThreadID = 0
		copy.ReplyToMessageID = 0
		if err := a.sendMessage(ctx, copy); err != nil {
			return err
		}
	}
	return nil
}

func (a *Adapter) processUpdate(ctx context.Context, update Update) error {
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
			if err == nil {
				a.compactProcessedReceiptsLocked(update.ID)
				err = a.saveLocked()
			}
			claim.err = err
		}
		delete(a.claims, update.ID)
		close(claim.done)
		a.mu.Unlock()
		return err
	}
}

func (a *Adapter) deliverUpdate(ctx context.Context, update Update, allowed bool) error {
	if update.Message == nil {
		return nil
	}
	if update.Message.Voice != nil {
		return a.deliverVoiceUpdate(ctx, update, allowed)
	}
	return a.deliverTextUpdate(ctx, update, allowed, nil)
}

func (a *Adapter) deliverTextUpdate(ctx context.Context, update Update, allowed bool, requestOverride *string) error {
	text := strings.TrimSpace(update.Message.Text)
	if strings.HasPrefix(text, "/start ") {
		if a.config.GroupChatID != nil && ((update.Message.ChatType != "" && update.Message.ChatType != "private") || update.Message.ChatID != update.Message.FromID) {
			return nil
		}
		return a.RedeemPairingWithSender(strings.TrimSpace(strings.TrimPrefix(text, "/start ")), update.Message.ChatID, update.Message.FromID)
	}
	if text == "/linkgroup" || (a.config.BotUsername != "" && strings.EqualFold(text, "/linkgroup@"+strings.TrimPrefix(a.config.BotUsername, "@"))) {
		return a.linkGroup(ctx, update)
	}
	if !allowed || text == "/start" || text == "" {
		return nil
	}
	externalID := fmt.Sprint(update.ID)
	trimmed := strings.TrimSpace(text)
	if trimmed == "/resume" {
		if update.Message.ThreadID == 0 || update.Message.ThreadID == 1 {
			return nil
		}
		a.mu.Lock()
		mapping, ok := a.mappingForThreadLocked(update.Message.ChatID, update.Message.ThreadID)
		a.mu.Unlock()
		if !ok {
			return nil
		}
		if err := a.server.ResumeWorker(ctx, mapping.WorkerRef); err != nil {
			return err
		}
		a.sendTyping(update.Message.ChatID, update.Message.ThreadID)
		return nil
	}
	if update.Message.ThreadID != 0 {
		a.mu.Lock()
		mapping, ok := a.mappingForThreadLocked(update.Message.ChatID, update.Message.ThreadID)
		a.mu.Unlock()
		if !ok {
			if update.Message.ThreadID != 1 {
				return nil
			}
		} else {
			if requestOverride != nil {
				mapping.PendingRequestID = *requestOverride
			} else {
				requestID, err := a.bindWorkerRequest(update.ID, mapping.PendingRequestID)
				if err != nil {
					return err
				}
				mapping.PendingRequestID = requestID
			}
			err := a.server.SendWorkerMessage(ctx, WorkerMessage{WorkerRef: mapping.WorkerRef, ExternalMessageID: externalID, RequestID: mapping.PendingRequestID, Text: text})
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if isSteerRejection(err) {
					serviceText := formatWorkerSteerErrorMessage(err)
					sendErr := a.sendMessage(ctx, OutgoingMessage{
						Identity: fmt.Sprintf("steer-error:%d", update.ID),
						ChatID:   update.Message.ChatID,
						ThreadID: update.Message.ThreadID,
						Text:     serviceText,
					})
					if sendErr != nil {
						return sendErr
					}
					return nil
				}
				return err
			}
			a.sendTyping(update.Message.ChatID, update.Message.ThreadID)
			if mapping.PendingRequestID != "" {
				a.mu.Lock()
				if current, exists := a.state.Topics[mapping.WorkerRef]; exists && current.ThreadID == mapping.ThreadID && current.PendingRequestID == mapping.PendingRequestID {
					current.PendingRequestID = ""
					a.state.Topics[mapping.WorkerRef] = current
					_ = a.saveLocked()
				}
				a.mu.Unlock()
			}
			return nil
		}
	}
	if err := a.server.SendMessage(ctx, InboundMessage{ExternalMessageID: externalID, Body: text}); err != nil {
		return err
	}
	// The Server receives each Telegram update once. Mirrored messages only
	// create outbox deliveries; they never re-enter the input queue.
	if a.config.GroupChatID != nil {
		for _, target := range a.personalChats() {
			if target == update.Message.ChatID {
				continue
			}
			if err := a.sendMessage(ctx, OutgoingMessage{ChatID: target, Text: text, Identity: "mirror-input:" + externalID}); err != nil {
				return err
			}
		}
	}
	thread := update.Message.ThreadID
	if thread == 0 && a.config.GroupChatID == nil {
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

func (a *Adapter) sendTyping(chatID, threadID int64) {
	a.sendTypingContext(context.Background(), chatID, threadID)
}

func (a *Adapter) sendTypingContext(ctx context.Context, chatID, threadID int64) {
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
	requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_ = a.transport.SendChatAction(requestCtx, chatID, threadID, "typing")
}

func (a *Adapter) refreshWorkerTypingLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.refreshWorkerTyping(ctx)
		}
	}
}

func (a *Adapter) refreshWorkerTyping(ctx context.Context) {
	a.mu.Lock()
	attempts := make([]workerTypingAttempt, 0, len(a.state.WorkerTyping))
	for _, attempt := range a.state.WorkerTyping {
		attempts = append(attempts, attempt)
	}
	a.mu.Unlock()
	for _, attempt := range attempts {
		a.sendTypingContext(ctx, attempt.ChatID, attempt.ThreadID)
	}
}

func workerTypingKey(workerRef, attemptID string) string {
	return fmt.Sprintf("%q:%q", workerRef, attemptID)
}

func isWorkerTerminalEvent(event Event) bool {
	if event.WorkerAttemptTerminal {
		return true
	}
	switch event.Kind {
	case "worker.completed", "worker.succeeded", "worker.failed", "worker.canceled", "worker.offline":
		return true
	default:
		return false
	}
}

func (a *Adapter) WorkerTypingRecoveryNeeded() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.workerTypingRecoveryNeeded
}

func (a *Adapter) RestoreWorkerTyping(events []Event) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.workerTypingRecoveryNeeded {
		return nil
	}
	active := make(map[string]workerTypingAttempt, len(events))
	for _, event := range events {
		if event.Kind != "worker.started" || event.WorkerRef == "" || event.AttemptID == "" {
			continue
		}
		mapping, ok := a.state.Topics[event.WorkerRef]
		if !ok {
			continue
		}
		active[workerTypingKey(event.WorkerRef, event.AttemptID)] = workerTypingAttempt{WorkerRef: event.WorkerRef, AttemptID: event.AttemptID, ChatID: mapping.ChatID, ThreadID: mapping.ThreadID}
	}
	a.state.WorkerTyping = active
	a.state.WorkerTypingInitialized = true
	if err := a.saveLocked(); err != nil {
		return err
	}
	a.workerTypingRecoveryNeeded = false
	return nil
}

func (a *Adapter) updateWorkerTyping(event Event, mapping TopicMapping) error {
	if event.WorkerRef == "" || event.AttemptID == "" {
		return nil
	}
	key := workerTypingKey(event.WorkerRef, event.AttemptID)
	a.mu.Lock()
	if isWorkerTerminalEvent(event) {
		if _, ok := a.state.WorkerTyping[key]; !ok {
			a.mu.Unlock()
			return nil
		}
		delete(a.state.WorkerTyping, key)
		err := a.saveLocked()
		a.mu.Unlock()
		return err
	}
	if event.Kind != "worker.started" {
		a.mu.Unlock()
		return nil
	}
	a.state.WorkerTyping[key] = workerTypingAttempt{WorkerRef: event.WorkerRef, AttemptID: event.AttemptID, ChatID: mapping.ChatID, ThreadID: mapping.ThreadID}
	err := a.saveLocked()
	a.mu.Unlock()
	if err != nil {
		return err
	}
	a.sendTyping(mapping.ChatID, mapping.ThreadID)
	return nil
}

func (a *Adapter) PollOnce(ctx context.Context) error {
	if err := a.SyncWorkerTopics(ctx); err != nil {
		return err
	}
	if err := a.pollInputs(ctx); err != nil {
		return err
	}
	return a.drainInputs(ctx)
}

func (a *Adapter) Run(ctx context.Context) error {
	refreshDone := make(chan struct{})
	go func() {
		defer close(refreshDone)
		a.refreshWorkerTypingLoop(ctx)
	}()
	defer func() {
		<-refreshDone
		a.inputWG.Wait()
	}()
	backoff := 100 * time.Millisecond
	maxBackoff := a.config.PollInterval
	if maxBackoff < backoff {
		maxBackoff = backoff
	}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err := a.SyncWorkerTopics(ctx)
		if err == nil {
			err = a.pollInputs(ctx)
		}
		a.startInputWorkers(ctx)
		if err == nil {
			err = a.Flush(ctx)
		}
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
		a.compactDeliveredLocked()
		err := a.saveLocked()
		a.mu.Unlock()
		return err
	}
	a.mu.Unlock()

	if event.Kind == "secretary.acknowledged" {
		if err := a.handleEvent(ctx, event); err != nil {
			return err
		}
	} else if strings.HasPrefix(event.Kind, "secretary.") {
		return a.handleSecretaryEvent(event, event.Sequence)
	} else if err := a.handleEvent(ctx, event); err != nil {
		return err
	}
	a.mu.Lock()
	if event.Sequence > a.state.LastEventSeq {
		a.state.LastEventSeq = event.Sequence
	}
	a.compactDeliveredLocked()
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
	if a.config.ShowToolCalls != nil && !*a.config.ShowToolCalls && (event.Kind == "worker.tool_started" || event.Kind == "worker.tool_finished" || event.Kind == "worker.tool_failed") {
		return nil
	}
	if isWorkerTerminalEvent(event) {
		if err := a.updateWorkerTyping(event, TopicMapping{}); err != nil {
			return err
		}
		if event.Kind == "" {
			return nil
		}
	}
	if event.Kind == "secretary.acknowledged" {
		a.mu.Lock()
		owner := a.state.OwnerChat
		a.mu.Unlock()
		if owner == 0 {
			return nil
		}
		return a.sendPersonal(ctx, OutgoingMessage{Text: event.Text, Identity: "inbound:acknowledgement:" + event.EventID, DurableSequence: event.Sequence})
	}
	if strings.HasPrefix(event.Kind, "secretary.") {
		return a.handleSecretaryEvent(event, 0)
	}
	if event.Kind == "message.saved" {
		if event.Source == "client:telegram-adapter" ||
			!(strings.HasPrefix(event.Source, "client:") && strings.TrimPrefix(event.Source, "client:") != "" ||
				a.config.GroupChatID != nil && event.Source == "web") {
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
		return a.sendPersonal(ctx, OutgoingMessage{Text: text, Identity: "inbound:" + identity, DurableSequence: event.Sequence})
	}
	if event.WorkerRef == "" {
		return nil
	}
	if event.Kind == "worker.started" {
		mapping, err := a.ensureTopic(ctx, event.WorkerRef, event.Title)
		if err != nil {
			return err
		}
		if mapping.ChatID == 0 {
			return nil
		}
		return a.updateWorkerTyping(event, mapping)
	}
	if event.Kind == "worker.created" {
		mapping, err := a.ensureTopic(ctx, event.WorkerRef, event.Title, event.TaskPrompt)
		if err != nil {
			return err
		}
		if mapping.ChatID == 0 {
			return nil
		}
		var parts []string
		if nodeID := safePublicNodeID(event.NodeID); nodeID != "" {
			parts = append(parts, "Узел: "+nodeID)
		}
		if prompt := safeDelta(event.TaskPrompt); strings.TrimSpace(prompt) != "" {
			parts = append(parts, prompt)
		}
		if len(parts) > 0 {
			return a.sendMessage(ctx, OutgoingMessage{
				ChatID: mapping.ChatID, ThreadID: mapping.ThreadID,
				Text: strings.Join(parts, "\n\n"), Identity: "task-prompt:" + event.WorkerRef,
				DisableLinkPreview: true, DurableSequence: event.Sequence,
			})
		}
		return nil
	}
	mapping, err := a.ensureTopic(ctx, event.WorkerRef, event.Title)
	if err != nil {
		return err
	}
	if mapping.ChatID == 0 && event.TerminalIdentity == "" {
		return nil
	}
	if mapping.ChatID != 0 {
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
	}
	if importantWorkerEvent(event.Kind) {
		if event.Kind == "worker.tool_started" {
			if event.CallID != "" && event.ToolPreview != "" {
				a.mu.Lock()
				a.state.ToolPreviews[toolPreviewKey(event.WorkerRef, event.AttemptID, event.CallID)] = event.ToolPreview
				_ = a.saveLocked()
				a.mu.Unlock()
			}
		} else if event.Kind == "worker.tool_finished" || event.Kind == "worker.tool_failed" {
			if event.CallID != "" {
				a.mu.Lock()
				if saved, ok := a.state.ToolPreviews[toolPreviewKey(event.WorkerRef, event.AttemptID, event.CallID)]; ok && saved != "" {
					event.ToolPreview = saved
				}
				a.mu.Unlock()
			}
		}
		var body string
		if event.TerminalIdentity != "" {
			body = safeText(event.Text)
			if body == "" {
				body = workerEventLabel(event.Kind)
			}
		} else if event.Kind == "worker.followup.delivered" {
			body = safeText(event.Text)
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
			key := event.TerminalIdentity
			if a.config.GroupChatID != nil {
				key = "complete:" + key
			}
			_, alreadyNotified = a.state.TerminalNotified[key]
			a.mu.Unlock()
		case event.Kind == "worker.limited":
			identity = "worker-limited:" + event.EventID
			if identity == "worker-limited:" {
				identity = fmt.Sprintf("worker-limited:%s:%d", event.WorkerRef, event.Sequence)
			}
		case event.Kind == "worker.followup.delivered":
			identity = "worker-followup:" + event.EventID
			if event.EventID == "" {
				identity = fmt.Sprintf("worker-followup:%s:%d", event.WorkerRef, event.Sequence)
			}
		case strings.HasPrefix(event.Kind, "worker.message."):
			identity = "worker-message:" + event.EventID
		case requestNotificationIdentity(event) != "":
			identity = "request:" + requestNotificationIdentity(event)
			a.mu.Lock()
			_, alreadyNotified = a.state.RequestNotified[requestNotificationIdentity(event)]
			a.mu.Unlock()
		case event.Kind == "worker.tool_started" || event.Kind == "worker.tool_finished" || event.Kind == "worker.tool_failed":
			callKey := event.CallID
			if callKey == "" {
				callKey = event.EventID
				if callKey == "" {
					callKey = fmt.Sprintf("seq:%d", event.Sequence)
				}
			}
			identity = toolMessageIdentity(event.Kind, event.WorkerRef, event.AttemptID, callKey)
			a.mu.Lock()
			_, alreadyNotified = a.state.ToolNotified[strings.TrimPrefix(identity, "tool:")]
			a.mu.Unlock()
		}
		if alreadyNotified && ((event.TerminalIdentity != "" && a.config.GroupChatID != nil) ||
			(event.TerminalIdentity == "" && a.config.GroupChatID == nil)) {
			return nil
		}
		if mapping.ChatID != 0 {
			if !isWorkerTerminalEvent(event) {
				a.sendTyping(mapping.ChatID, mapping.ThreadID)
			}
			if err := a.sendMessage(ctx, OutgoingMessage{ChatID: mapping.ChatID, ThreadID: mapping.ThreadID, Text: body, Identity: identity, DisableLinkPreview: true, DurableSequence: event.Sequence}); err != nil {
				return err
			}
		}
		if event.TerminalIdentity != "" {
			if a.config.GroupChatID != nil {
				if err := a.sendPersonal(ctx, OutgoingMessage{Text: body, Identity: identity + ":personal", DisableLinkPreview: true, DurableSequence: event.Sequence}); err != nil {
					return err
				}
				// Topic success alone is not completion: only commit this
				// semantic receipt after every Personal projection succeeded.
				a.mu.Lock()
				a.state.TerminalNotified["complete:"+event.TerminalIdentity] = a.now()
				err := a.saveLocked()
				a.mu.Unlock()
				if err != nil {
					return err
				}
			} else {
				a.mu.Lock()
				owner := a.state.OwnerChat
				a.mu.Unlock()
				if owner != 0 {
					mirror := OutgoingMessage{ChatID: owner, Text: body, Identity: identity + ":general", DisableLinkPreview: true, DurableSequence: event.Sequence}
					if err := a.sendMessage(ctx, mirror); err != nil {
						return err
					}
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
	if a.config.GroupChatID != nil {
		owner = a.state.GroupChat
	}
	a.mu.Unlock()
	if owner == 0 {
		return TopicMapping{}, nil // DM-only: no Worker Topics to create.
	}
	name := topicName(workerRef, title)
	if len(prompts) > 0 && strings.TrimSpace(prompts[0]) != "" {
		if a.config.TitleGenerator != nil {
			name = a.taskTopicTitle(ctx, prompts[0])
		} else if savedTitle := validTopicTitle(title); savedTitle != "" {
			name = savedTitle
		} else {
			name = fallbackTopicTitle(prompts[0])
		}
	}
	topic, err := a.transport.CreateForumTopic(ctx, owner, name)
	if err != nil {
		return TopicMapping{}, fmt.Errorf("telegram: не удалось создать Worker topic; проверьте, что группа поддерживает topics и бот имеет право управлять ими: %w", err)
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
	a.compactDeliveredLocked()
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
		if event.CanonicalReplyRequired && strings.TrimSpace(a.pending.SecretaryText.String()) == "" && event.Text == "" {
			event.Text = "Secretary: завершённый запрос не содержит сохранённого ответа. Отправьте новое сообщение, чтобы продолжить."
		}
		if text := safeText(event.Text); text != "" {
			if event.CanonicalReplyRequired && a.pending.SecretaryText.Len() > 0 {
				a.pending.SecretaryText.WriteString("\n\n")
			}
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
	if len(eventSeqs) > 0 {
		secretaryMessage.Identity = fmt.Sprintf("secretary:%v", eventSeqs)
		secretaryMessage.DurableSequence = eventSeqs[len(eventSeqs)-1]
	}
	outboxIdentities := make(map[string]struct{}, len(a.state.Outbox))
	for _, pending := range a.state.Outbox {
		outboxIdentities[outgoingMessageIdentity(pending)] = struct{}{}
	}
	a.mu.Unlock()
	if err := a.drainOutbox(ctx); err != nil {
		return err
	}
	if owner != 0 && (hasSecretary || turnOpen) {
		thread := int64(1)
		if a.config.GroupChatID != nil {
			thread = 0
		}
		a.sendTyping(owner, thread)
	}
	if hasSecretary && ready {
		for _, target := range a.personalChats() {
			destination := secretaryMessage
			destination.ChatID = target
			if _, queued := outboxIdentities[outgoingMessageIdentity(destination)]; !queued {
				if err := a.sendMessage(ctx, destination); err != nil {
					return err
				}
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
	a.compactDeliveredLocked()
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
		return fmt.Sprintf("%d:%d:%s", message.ChatID, message.ThreadID, message.Identity)
	}
	hash := sha256.Sum256([]byte(fmt.Sprintf("%d:%d:%s", message.ChatID, message.ThreadID, message.Text)))
	return "message:" + hex.EncodeToString(hash[:])
}

func (a *Adapter) markDeliveredLocked(message OutgoingMessage) {
	if message.Identity != "" {
		key := outgoingMessageIdentity(message)
		a.state.Delivered[key] = a.now()
		if message.DurableSequence > 0 {
			a.state.DeliveredSequence[key] = message.DurableSequence
		}
	}
	switch {
	case strings.HasPrefix(message.Identity, "task-prompt:"):
		workerRef := strings.TrimPrefix(message.Identity, "task-prompt:")
		if mapping, ok := a.state.Topics[workerRef]; ok {
			mapping.PromptDelivered = true
			a.state.Topics[workerRef] = mapping
		}
	case strings.HasPrefix(message.Identity, "terminal:"):
		identity := strings.TrimPrefix(message.Identity, "terminal:")
		if identity != "" && a.config.GroupChatID == nil {
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
	case strings.HasPrefix(message.Identity, "steer-error:"):
		a.state.InboundNotified[message.Identity] = a.now()
	case strings.HasPrefix(message.Identity, "tool:"):
		identity := strings.TrimPrefix(message.Identity, "tool:")
		if identity != "" {
			a.state.ToolNotified[identity] = a.now()
		}
	}
}

// compactDeliveredLocked removes a delivery proof only after the durable
// Server cursor has advanced beyond its event AND no pending outbox or
// Secretary batch could cause the same event to be sent again. This is safe
// for partial group/DM sends and for restart between individual sends.
func (a *Adapter) compactDeliveredLocked() {
	if len(a.state.DeliveredSequence) == 0 {
		return
	}
	pending := make(map[string]struct{}, len(a.state.Outbox))
	for _, msg := range a.state.Outbox {
		pending[outgoingMessageIdentity(msg)] = struct{}{}
	}
	for key, seq := range a.state.DeliveredSequence {
		if seq <= 0 || seq > a.state.LastEventSeq || containsEventSeq(a.pending.EventSeqs, seq) {
			continue
		}
		if _, queued := pending[key]; queued {
			continue
		}
		delete(a.state.Delivered, key)
		delete(a.state.DeliveredSequence, key)
	}
}

// Processed Telegram updates cannot be replayed into the Server, so receipts
// for their DM/General projections are no longer needed after that update's
// processed marker has been saved. Never compact a failed/queued update.
func (a *Adapter) compactProcessedReceiptsLocked(updateID int64) {
	id := fmt.Sprint(updateID)
	suffixes := []string{
		":mirror-input:" + id,
		":steer-error:" + id,
		":inbound:voice-error:" + id,
		":inbound:voice-formatter:" + id,
	}
	for key := range a.state.Delivered {
		for _, suffix := range suffixes {
			if strings.HasSuffix(key, suffix) {
				delete(a.state.Delivered, key)
				delete(a.state.DeliveredSequence, key)
				break
			}
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
	if message.Identity != "" && a.config.GroupChatID != nil {
		if _, delivered := a.state.Delivered[outgoingMessageIdentity(message)]; delivered {
			a.mu.Unlock()
			return nil
		}
	}
	if message.Identity != "" && a.config.GroupChatID == nil {
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
		if strings.HasPrefix(message.Identity, "steer-error:") {
			if _, delivered := a.state.InboundNotified[message.Identity]; delivered {
				a.mu.Unlock()
				return nil
			}
		}
		if strings.HasPrefix(message.Identity, "tool:") {
			if _, delivered := a.state.ToolNotified[strings.TrimPrefix(message.Identity, "tool:")]; delivered {
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
	a.outboxMu.Lock()
	defer a.outboxMu.Unlock()
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
			message.DeliveredBytes += prepared.sourceBytes
			done := message.DeliveredBytes >= len(message.Text)
			a.mu.Lock()
			if done {
				a.state.Outbox = removeOutboxMessage(a.state.Outbox, message)
				a.markDeliveredLocked(message)
				a.compactDeliveredLocked()
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
	return kind == "worker.limited" || kind == "worker.followup.delivered" || kind == "worker.message.delivered" || kind == "worker.tool_started" || kind == "worker.tool_finished" || kind == "worker.tool_failed" || strings.Contains(kind, "approval") || strings.Contains(kind, "needs_input") || strings.HasSuffix(kind, ".failed") || strings.HasSuffix(kind, ".completed") || strings.HasSuffix(kind, ".succeeded") || strings.HasSuffix(kind, ".canceled") || strings.HasSuffix(kind, ".offline") || strings.HasSuffix(kind, ".completion")
}

func workerEventLabel(kind string) string {
	if label := map[string]string{"worker.limited": "Лимит использования", "worker.message.queued": "В очереди", "worker.message.delivered": "Передано Worker", "worker.message.blocked": "Сообщение заблокировано", "worker.message.canceled": "Сообщение отменено", "worker.approval_requested": "Нужно разрешение", "worker.needs_input": "Нужен ответ", "worker.failed": "Ошибка Worker", "worker.completed": "Worker завершён", "worker.succeeded": "Worker завершён", "worker.canceled": "Worker отменён", "worker.offline": "Node offline"}[kind]; label != "" {
		return label
	}
	return "Статус Worker"
}

func renderWorkerEvent(event Event) string {
	if event.Kind == "worker.limited" {
		var p struct {
			Mode     string `json:"mode"`
			ResetsAt string `json:"resets_at"`
			Deadline string `json:"deadline"`
		}
		if len(event.Payload) > 0 {
			_ = json.Unmarshal(event.Payload, &p)
		}
		resumeAt, _ := time.Parse(time.RFC3339Nano, p.Deadline)
		if resumeAt.IsZero() {
			if resetAt, err := time.Parse(time.RFC3339Nano, p.ResetsAt); err == nil {
				resumeAt = resetAt.Add(time.Minute)
			}
		}
		if resumeAt.IsZero() {
			if p.Mode == "pending" {
				return "Достигнут лимит Codex. Продолжу автоматически, когда лимит сбросится."
			}
			return "Достигнут лимит Codex. Вы можете продолжить, когда лимит сбросится."
		}
		if p.Mode == "pending" {
			return fmt.Sprintf("Достигнут лимит Codex. Продолжу автоматически после %s.", resumeAt.Local().Format("15:04"))
		}
		return fmt.Sprintf("Достигнут лимит Codex. Вы можете продолжить после %s.", resumeAt.Local().Format("15:04"))
	}
	if event.Kind == "worker.tool_started" || event.Kind == "worker.tool_finished" || event.Kind == "worker.tool_failed" {
		tool := safeText(event.Tool)
		if tool == "" {
			return ""
		}
		preview := safeText(event.ToolPreview)
		switch event.Kind {
		case "worker.tool_started":
			if preview != "" {
				return tool + " " + preview
			}
			return tool
		case "worker.tool_finished":
			return ""
		case "worker.tool_failed":
			if preview != "" {
				return "✗ Ошибка: " + tool + " " + preview
			}
			return "✗ Ошибка: " + tool
		}
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

func isSteerRejection(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	if isUncertainWorkerCommandResponse(errStr) {
		return false
	}
	if strings.Contains(errStr, `"category"`) || strings.Contains(errStr, `"provider_code"`) || strings.Contains(errStr, `"error"`) {
		return true
	}
	if strings.Contains(errStr, "400 Bad Request") || strings.Contains(errStr, "server returned 4") {
		return true
	}
	if strings.Contains(errStr, "usage_limit") || strings.Contains(errStr, "usage_limit_exceeded") {
		return true
	}
	return false
}

func isUncertainWorkerCommandResponse(errText string) bool {
	start := strings.IndexByte(errText, '{')
	end := strings.LastIndexByte(errText, '}')
	if start < 0 || end <= start {
		return false
	}
	var response struct {
		Outcome   string `json:"outcome"`
		Uncertain bool   `json:"uncertain"`
	}
	if json.Unmarshal([]byte(errText[start:end+1]), &response) != nil {
		return false
	}
	return response.Uncertain || response.Outcome == "uncertain"
}

func formatWorkerSteerErrorMessage(err error) string {
	if err == nil {
		return "⚠️ Не удалось передать указание Worker."
	}
	errStr := strings.TrimSpace(err.Error())
	if idx := strings.Index(errStr, "{"); idx != -1 {
		if endIdx := strings.LastIndex(errStr, "}"); endIdx > idx {
			var payload struct {
				Error        string `json:"error"`
				Category     string `json:"category"`
				ProviderCode string `json:"provider_code"`
			}
			if json.Unmarshal([]byte(errStr[idx:endIdx+1]), &payload) == nil {
				return formatSteerFailureMessage(payload.Category, payload.ProviderCode, payload.Error)
			}
		}
	}
	if strings.Contains(errStr, "usage_limit") {
		return formatSteerFailureMessage("usage_limit", "usage_limit_exceeded", errStr)
	}
	return fmt.Sprintf("⚠️ Не удалось передать указание Worker: %s", errStr)
}

func formatSteerFailureMessage(category, code, msg string) string {
	msg = strings.TrimSpace(msg)
	if category == "usage_limit" || code == "usage_limit_exceeded" {
		cleaned := msg
		if strings.HasPrefix(strings.ToLower(cleaned), "usage_limit_exceeded:") {
			cleaned = strings.TrimSpace(cleaned[len("usage_limit_exceeded:"):])
		}
		lower := strings.ToLower(cleaned)
		hasResetTime := strings.Contains(lower, "try again") || strings.Contains(lower, "reset") || strings.Contains(lower, "retry") || strings.Contains(lower, "повтор") || strings.Contains(lower, "попробуй")
		if hasResetTime && cleaned != "" {
			return fmt.Sprintf("⚠️ Не удалось передать указание Worker: Лимит использования исчерпан (usage_limit_exceeded). %s", cleaned)
		}
		return "⚠️ Не удалось передать указание Worker: Лимит использования исчерпан (usage_limit_exceeded). Повторите попытку позже."
	}
	if msg != "" {
		return fmt.Sprintf("⚠️ Не удалось передать указание Worker: %s", msg)
	}
	return "⚠️ Не удалось передать указание Worker."
}

var validNodeIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)
var secretTokenMarker = regexp.MustCompile(`(?i)(?:token|secret|password|credential|bearer|apikey)`)

func safePublicNodeID(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsAny(raw, "\r\n/\\") {
		return ""
	}
	if !validNodeIDPattern.MatchString(raw) {
		return ""
	}
	if secretPrefix.MatchString(raw) || secretTokenMarker.MatchString(raw) || rawCoTMarker.MatchString(raw) {
		return ""
	}
	return raw
}

func toolPreviewKey(workerRef, attemptID, callID string) string {
	if attemptID != "" {
		return workerRef + ":" + attemptID + ":" + callID
	}
	return workerRef + ":" + callID
}

func toolMessageIdentity(kind, workerRef, attemptID, callKey string) string {
	if attemptID != "" {
		return "tool:" + kind + ":" + workerRef + ":" + attemptID + ":" + callKey
	}
	return "tool:" + kind + ":" + workerRef + ":" + callKey
}
