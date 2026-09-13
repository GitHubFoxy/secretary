package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/telegram"
	"github.com/beruseruko/secretary/internal/webapi"
)

type telegramDeploymentConfig struct {
	Enabled          bool
	BotToken         string
	BotUsername      string
	ServerCredential string
	OwnerChatID      int64
	BaseURL          string
	PollInterval     time.Duration
	FlushInterval    time.Duration
}

func configuredTelegramDeployment() (telegramDeploymentConfig, error) {
	if !parseBoolEnv("SECRETARY_TELEGRAM_ENABLED") {
		return telegramDeploymentConfig{}, nil
	}
	config := telegramDeploymentConfig{
		Enabled:          true,
		BotToken:         strings.TrimSpace(os.Getenv("SECRETARY_TELEGRAM_BOT_TOKEN")),
		BotUsername:      strings.TrimSpace(os.Getenv("SECRETARY_TELEGRAM_BOT_USERNAME")),
		ServerCredential: strings.TrimSpace(os.Getenv("SECRETARY_TELEGRAM_SERVER_CREDENTIAL")),
		BaseURL:          strings.TrimRight(strings.TrimSpace(os.Getenv("SECRETARY_TELEGRAM_API_BASE_URL")), "/"),
		PollInterval:     30 * time.Second,
		FlushInterval:    2 * time.Second,
	}
	if config.BotToken == "" || config.ServerCredential == "" {
		return telegramDeploymentConfig{}, errors.New("Telegram is enabled but private Bot token and server credential are not configured")
	}
	if value := strings.TrimSpace(os.Getenv("SECRETARY_TELEGRAM_OWNER_CHAT_ID")); value != "" {
		owner, err := strconv.ParseInt(value, 10, 64)
		if err != nil || owner == 0 {
			return telegramDeploymentConfig{}, errors.New("SECRETARY_TELEGRAM_OWNER_CHAT_ID must be a non-zero integer")
		}
		config.OwnerChatID = owner
	}
	if value := strings.TrimSpace(os.Getenv("SECRETARY_TELEGRAM_POLL_INTERVAL")); value != "" {
		interval, err := time.ParseDuration(value)
		if err != nil || interval <= 0 {
			return telegramDeploymentConfig{}, errors.New("SECRETARY_TELEGRAM_POLL_INTERVAL must be a positive duration")
		}
		config.PollInterval = interval
	}
	if value := strings.TrimSpace(os.Getenv("SECRETARY_TELEGRAM_FLUSH_INTERVAL")); value != "" {
		interval, err := time.ParseDuration(value)
		if err != nil || interval <= 0 {
			return telegramDeploymentConfig{}, errors.New("SECRETARY_TELEGRAM_FLUSH_INTERVAL must be a positive duration")
		}
		config.FlushInterval = interval
	}
	return config, nil
}

func parseBoolEnv(name string) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func attachProductionTelegram(ctx context.Context, dataDir, listen string, store *core.Store, web *webapi.Server) (*telegram.Adapter, error) {
	deployment, err := configuredTelegramDeployment()
	if err != nil {
		return nil, err
	}
	if !deployment.Enabled {
		return nil, nil
	}
	adapter, err := telegram.New(telegram.Config{
		StatePath: filepath.Join(dataDir, "telegram", "state.json"), OwnerChatID: deployment.OwnerChatID,
		PollInterval: deployment.PollInterval, FlushInterval: deployment.FlushInterval, BotUsername: deployment.BotUsername,
	}, &telegram.BotAPITransport{BaseURL: deployment.BaseURL, BotToken: deployment.BotToken}, &telegram.HTTPServerClient{
		BaseURL: "http://" + listen, Credential: deployment.ServerCredential,
	})
	if err != nil {
		return nil, err
	}
	web.AttachTelegramPairer(adapter)
	go func() {
		if err := adapter.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("Telegram adapter stopped: %v", err)
		}
	}()
	go bridgeTelegramEvents(ctx, store, adapter)
	return adapter, nil
}

func bridgeTelegramEvents(ctx context.Context, store *core.Store, adapter *telegram.Adapter) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if events, err := store.EventsAfterSeq(ctx, 0, 500); err == nil {
			for _, event := range events {
				if err := adapter.HandleDurableEvent(ctx, telegramEvent(event)); err != nil {
					break
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func telegramEvent(event core.Event) telegram.Event {
	workerRef := event.WorkerRef
	if workerRef == "" && event.AggregateType == "worker" {
		workerRef = event.AggregateID
	}
	result := telegram.Event{EventID: event.ID, Sequence: event.Seq, Kind: event.Kind, WorkerRef: workerRef, Payload: event.Payload}
	var payload map[string]any
	_ = json.Unmarshal(event.Payload, &payload)
	result.Title = stringField(payload, "title", "text", "summary")
	result.Text = stringField(payload, "text", "summary", "result", "error", "response")
	result.Tool = stringField(payload, "tool")
	switch {
	case strings.HasPrefix(event.Kind, "secretary."):
		return result
	case event.Kind == "worker.spawned":
		result.Kind = "worker.created"
	case event.Kind == "worker.started":
		result.Kind = "worker.started"
	case event.Kind == "attempt.activity":
		activityKind := stringField(payload, "kind")
		result.Kind = "worker.activity"
		switch activityKind {
		case "permission_request":
			result.Kind = "worker.approval_requested"
		case "user_input_request":
			result.Kind = "worker.needs_input"
		case "tool_call":
			if result.Tool == "" {
				if call, ok := payload["tool_call"].(map[string]any); ok {
					result.Tool = stringField(call, "name")
				}
			}
		case "tool_result":
			if result.Tool == "" {
				if tool, ok := payload["tool_result"].(map[string]any); ok {
					result.Tool = stringField(tool, "name")
				}
			}
		}
	case event.Kind == "approval.requested":
		result.Kind = "worker.approval_requested"
	case event.Kind == "approval.approved" || event.Kind == "approval.denied" || event.Kind == "approval.revoked":
		result.Kind = "worker.approval_resolved"
	case event.Kind == "attempt.outcome_recorded" || event.Kind == "result.accepted":
		classification := strings.ToLower(stringField(payload, "classification"))
		if classification == "retryable" {
			result.Kind = ""
			break
		}
		status := strings.ToLower(stringField(payload, "status"))
		result.Kind = "worker.completed"
		if strings.Contains(status, "interrupt") || strings.Contains(status, "offline") {
			result.Kind = "worker.offline"
		} else if strings.Contains(status, "fail") || strings.Contains(status, "error") {
			result.Kind = "worker.failed"
		}
	default:
		result.Kind = ""
	}
	return result
}

func stringField(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
