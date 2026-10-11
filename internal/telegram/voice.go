package telegram

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type Voice struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	Duration     int    `json:"duration"`
	MimeType     string `json:"mime_type,omitempty"`
	FileSize     int64  `json:"file_size,omitempty"`
}
type VoiceDownloadTransport interface {
	GetFile(context.Context, string) (TelegramFile, error)
	DownloadFile(context.Context, string, int64) ([]byte, error)
}
type VoiceTranscript struct {
	Raw              string    `json:"raw"`
	Formatted        string    `json:"formatted,omitempty"`
	Delivered        string    `json:"delivered"`
	ChatID           int64     `json:"chat_id"`
	ThreadID         int64     `json:"thread_id"`
	MessageID        int64     `json:"message_id"`
	WorkerRequestID  string    `json:"worker_request_id,omitempty"`
	FormatterSkipped bool      `json:"formatter_skipped,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

func (a *Adapter) Transcript(id string) (VoiceTranscript, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	t, ok := a.state.Transcripts[id]
	return t, ok
}
func (a *Adapter) voiceNotice(ctx context.Context, u Update, kind, text string) error {
	return a.sendMessage(ctx, OutgoingMessage{ChatID: u.Message.ChatID, ThreadID: u.Message.ThreadID, ReplyToMessageID: u.Message.MessageID, Text: text, Identity: fmt.Sprintf("inbound:voice-%s:%d", kind, u.ID)})
}
func (a *Adapter) deliverVoiceUpdate(ctx context.Context, u Update, allowed bool) error {
	if !allowed || u.Message == nil || u.Message.Voice == nil {
		return nil
	}
	m := u.Message
	if m.ThreadID != 0 && m.ThreadID != 1 {
		a.mu.Lock()
		_, ok := a.mappingForThreadLocked(m.ChatID, m.ThreadID)
		a.mu.Unlock()
		if !ok {
			return nil
		}
	}
	fail := func(text string) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return a.voiceNotice(ctx, u, "error", text)
	}
	maxSeconds := a.config.ASR.MaxAudioSeconds
	if maxSeconds <= 0 {
		maxSeconds = 120
	}
	maxBytes := a.config.ASR.MaxFileBytes
	if maxBytes <= 0 {
		maxBytes = 10000000
	}
	if m.Voice.Duration < 0 || m.Voice.Duration > maxSeconds {
		return fail(fmt.Sprintf("Голосовое сообщение превышает лимит длительности (%d сек).", maxSeconds))
	}
	if m.Voice.FileSize < 0 || m.Voice.FileSize > maxBytes {
		return fail("Голосовое сообщение превышает лимит размера.")
	}
	id := fmt.Sprint(u.ID)
	transcript, ready := a.Transcript(id)
	if !ready {
		if a.asr == nil {
			return fail("Локальное распознавание речи не настроено.")
		}
		vt, ok := a.transport.(VoiceDownloadTransport)
		if !ok {
			return fail("Не удалось загрузить голосовое сообщение.")
		}
		downloadCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		file, err := vt.GetFile(downloadCtx, m.Voice.FileID)
		if err != nil {
			cancel()
			return fail("Не удалось получить голосовое сообщение.")
		}
		if file.FileSize > maxBytes {
			cancel()
			return fail("Голосовое сообщение превышает лимит размера.")
		}
		data, err := vt.DownloadFile(downloadCtx, file.FilePath, maxBytes)
		cancel()
		if err != nil || int64(len(data)) > maxBytes {
			return fail("Не удалось загрузить голосовое сообщение в пределах лимита размера.")
		}
		raw, err := a.asr.Transcribe(ctx, data)
		if err != nil || strings.TrimSpace(raw) == "" {
			return fail("Не удалось распознать речь в голосовом сообщении.")
		}
		transcript = VoiceTranscript{Raw: strings.TrimSpace(raw), Delivered: "raw", ChatID: m.ChatID, ThreadID: m.ThreadID, MessageID: m.MessageID, CreatedAt: a.now()}
		if a.config.ASR.Formatter.Enabled {
			if a.formatter == nil {
				transcript.FormatterSkipped = true
			} else {
				formatted, err := a.formatter.Format(ctx, transcript.Raw)
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if err != nil || ValidateFormatting(transcript.Raw, formatted) != nil {
					transcript.FormatterSkipped = true
				} else {
					transcript.Formatted = formatted
					transcript.Delivered = "formatted"
				}
			}
		}
		a.mu.Lock()
		if a.state.Transcripts == nil {
			a.state.Transcripts = map[string]VoiceTranscript{}
		}
		if mapping, ok := a.mappingForThreadLocked(m.ChatID, m.ThreadID); ok {
			transcript.WorkerRequestID = mapping.PendingRequestID
		}
		a.state.Transcripts[id] = transcript
		err = a.saveLocked()
		a.mu.Unlock()
		if err != nil {
			return err
		}
	}
	if transcript.FormatterSkipped {
		if err := a.voiceNotice(ctx, u, "formatter", "Форматирование пропущено; используется исходная расшифровка."); err != nil {
			return err
		}
	}
	final := transcript.Raw
	if transcript.Delivered == "formatted" {
		final = transcript.Formatted
	}
	if a.config.ASR.ShowASRInChat {
		if err := a.voiceNotice(ctx, u, "transcript", final); err != nil {
			return err
		}
	}
	// Reuse text routing, request/steering semantics and stable external identity.
	copyMessage := *m
	copyMessage.Voice = nil
	copyMessage.Text = final
	u.Message = &copyMessage
	return a.deliverTextUpdate(ctx, u, allowed, &transcript.WorkerRequestID)
}
