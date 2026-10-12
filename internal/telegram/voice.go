package telegram

import (
	"context"
	"fmt"
	"log"
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

type VoiceTiming struct {
	DownloadMS      int64  `json:"download_ms"`
	ASRMS           int64  `json:"asr_ms"`
	FormatterMS     int64  `json:"formatter_ms"`
	ProcessingMS    int64  `json:"processing_ms"`
	FormatterStatus string `json:"formatter_status"`
}

type VoiceTranscript struct {
	Timing           *VoiceTiming `json:"timing,omitempty"`
	Raw              string       `json:"raw"`
	Formatted        string       `json:"formatted,omitempty"`
	Delivered        string       `json:"delivered"`
	ChatID           int64        `json:"chat_id"`
	ThreadID         int64        `json:"thread_id"`
	MessageID        int64        `json:"message_id"`
	WorkerRequestID  string       `json:"worker_request_id,omitempty"`
	FormatterSkipped bool         `json:"formatter_skipped,omitempty"`
	CreatedAt        time.Time    `json:"created_at"`
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
		processingStart := time.Now()
		timing := &VoiceTiming{FormatterStatus: "disabled"}
		if a.config.ASR.Formatter.Enabled {
			timing.FormatterStatus = "not_run"
		}
		recordTiming := func(stage, status string) {
			timing.ProcessingMS = time.Since(processingStart).Milliseconds()
			logVoiceTiming(u, stage, status, timing)
		}
		if a.asr == nil {
			recordTiming("asr", "unavailable")
			return fail("Локальное распознавание речи не настроено.")
		}
		vt, ok := a.transport.(VoiceDownloadTransport)
		if !ok {
			recordTiming("download", "unavailable")
			return fail("Не удалось загрузить голосовое сообщение.")
		}
		downloadStart := time.Now()
		downloadCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		file, err := vt.GetFile(downloadCtx, m.Voice.FileID)
		if err != nil {
			cancel()
			timing.DownloadMS = time.Since(downloadStart).Milliseconds()
			recordTiming("download", "failed")
			return fail("Не удалось получить голосовое сообщение.")
		}
		if file.FileSize > maxBytes {
			cancel()
			timing.DownloadMS = time.Since(downloadStart).Milliseconds()
			recordTiming("download", "failed")
			return fail("Голосовое сообщение превышает лимит размера.")
		}
		data, err := vt.DownloadFile(downloadCtx, file.FilePath, maxBytes)
		cancel()
		timing.DownloadMS = time.Since(downloadStart).Milliseconds()
		if err != nil || int64(len(data)) > maxBytes {
			recordTiming("download", "failed")
			return fail("Не удалось загрузить голосовое сообщение в пределах лимита размера.")
		}
		asrStart := time.Now()
		raw, err := a.asr.Transcribe(ctx, data)
		timing.ASRMS = time.Since(asrStart).Milliseconds()
		if err != nil || strings.TrimSpace(raw) == "" {
			recordTiming("asr", "failed")
			return fail("Не удалось распознать речь в голосовом сообщении.")
		}
		transcript = VoiceTranscript{Raw: strings.TrimSpace(raw), Delivered: "raw", ChatID: m.ChatID, ThreadID: m.ThreadID, MessageID: m.MessageID, CreatedAt: a.now(), Timing: timing}
		if a.config.ASR.Formatter.Enabled {
			if a.formatter == nil {
				timing.FormatterStatus = "unavailable"
				transcript.FormatterSkipped = true
			} else {
				formatterStart := time.Now()
				formatted, err := a.formatter.Format(ctx, transcript.Raw)
				timing.FormatterMS = time.Since(formatterStart).Milliseconds()
				timing.FormatterStatus = "failed"
				if ctx.Err() != nil {
					recordTiming("formatter", "failed")
					return ctx.Err()
				}
				if err != nil || ValidateFormatting(transcript.Raw, formatted) != nil {
					transcript.FormatterSkipped = true
				} else {
					transcript.Formatted = formatted
					transcript.Delivered = "formatted"
					timing.FormatterStatus = "succeeded"
				}
			}
		}
		timing.ProcessingMS = time.Since(processingStart).Milliseconds()
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
		logVoiceTiming(u, "processing", "completed", timing)
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
	copyMessage := *m
	copyMessage.Voice = nil
	copyMessage.Text = final
	u.Message = &copyMessage
	return a.deliverTextUpdate(ctx, u, allowed, &transcript.WorkerRequestID)
}

func logVoiceTiming(u Update, stage, status string, t *VoiceTiming) {
	log.Printf("telegram voice timing update_id=%d message_id=%d stage=%s status=%s download_ms=%d asr_ms=%d formatter_ms=%d processing_ms=%d formatter_status=%s", u.ID, u.Message.MessageID, stage, status, t.DownloadMS, t.ASRMS, t.FormatterMS, t.ProcessingMS, t.FormatterStatus)
}
