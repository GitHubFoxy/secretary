package telegram

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"regexp"

	"github.com/beruseruko/secretary/internal/core"
	"log"
	"strings"
	"time"
)

type Voice struct {
	FileID       string `json:"file_id"`
	FileName     string `json:"file_name,omitempty"`
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
	SourceArtifactID string `json:"source_artifact_id,omitempty"`
	TranscriptArtifactID string `json:"transcript_artifact_id,omitempty"`
	FormatterSkipped bool         `json:"formatter_skipped,omitempty"`
	CreatedAt        time.Time    `json:"created_at"`
}

type VoiceFailure struct {
	SourceArtifactID string `json:"source_artifact_id,omitempty"`
	Notice string `json:"notice"`
	CreatedAt time.Time `json:"created_at"`
}

type VoiceArtifactResult struct {
	UpdateID int64 `json:"update_id"`
	ChatID int64 `json:"chat_id"`
	MessageID int64 `json:"message_id"`
	SourceArtifactID string `json:"source_artifact_id"`
	Transcript string `json:"transcript,omitempty"`
	Raw string `json:"raw,omitempty"`
	Failed bool `json:"failed,omitempty"`
}

type VoiceArtifactStore interface {
	StoreVoiceOriginal(context.Context, int64, Message, string, []byte) (string, error)
	CompleteVoice(context.Context, VoiceArtifactResult) (string, error)
}

var voiceNameUnsafe = regexp.MustCompile(`[^A-Za-z0-9._-]`)

func supportedAudioDocument(name, mimeType string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".oga", ".ogg", ".opus", ".mp3", ".m4a", ".mp4", ".wav", ".flac", ".webm", ".aac":
		return strings.HasPrefix(strings.ToLower(mimeType), "audio/")
	default:
		return false
	}
}

func voiceArtifactName(u Update) string {
	name := ""
	if u.Message != nil && u.Message.Voice != nil {
		name = u.Message.Voice.FileName
	}
	name = voiceNameUnsafe.ReplaceAllString(filepath.Base(name), "_")
	if len(name) > 130 {
		name = name[:130]
	}
	if !supportedAudioDocument(name, "audio/octet-stream") {
		ext := ".oga"
		if u.Message != nil && u.Message.Voice != nil {
			switch strings.ToLower(u.Message.Voice.MimeType) {
			case "audio/mpeg": ext = ".mp3"
			case "audio/mp4", "audio/x-m4a": ext = ".m4a"
			case "audio/wav", "audio/x-wav": ext = ".wav"
			case "audio/flac", "audio/x-flac": ext = ".flac"
			case "audio/webm": ext = ".webm"
			case "audio/aac": ext = ".aac"
			}
		}
		name = fmt.Sprintf("telegram-voice-%d%s", u.ID, ext)
	}
	return name
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

func (a *Adapter) deliverVoiceFailure(ctx context.Context, u Update, failure VoiceFailure) error {
	if store, ok := a.server.(VoiceArtifactStore); ok && failure.SourceArtifactID != "" {
		if _, err := store.CompleteVoice(ctx, VoiceArtifactResult{
			UpdateID: u.ID, ChatID: u.Message.ChatID, MessageID: u.Message.MessageID,
			SourceArtifactID: failure.SourceArtifactID, Failed: true,
		}); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return a.voiceNotice(ctx, u, "error", failure.Notice)
}

func (a *Adapter) persistVoiceFailure(ctx context.Context, u Update, sourceID, notice string) error {
	failure := VoiceFailure{SourceArtifactID: sourceID, Notice: notice, CreatedAt: a.now()}
	a.mu.Lock()
	if a.state.VoiceFailures == nil {
		a.state.VoiceFailures = map[string]VoiceFailure{}
	}
	a.state.VoiceFailures[fmt.Sprint(u.ID)] = failure
	err := a.saveLocked()
	a.mu.Unlock()
	if err != nil {
		return err
	}
	return a.deliverVoiceFailure(ctx, u, failure)
}
func (a *Adapter) deliverVoiceUpdate(ctx context.Context, u Update, allowed bool) error {
	if !allowed || u.Message == nil || u.Message.Voice == nil {
		return nil
	}
	m := u.Message
	a.mu.Lock()
	failure, previouslyFailed := a.state.VoiceFailures[fmt.Sprint(u.ID)]
	a.mu.Unlock()
	if previouslyFailed {
		return a.deliverVoiceFailure(ctx, u, failure)
	}
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
	if maxBytes > core.MaxArtifactBytes {
		maxBytes = core.MaxArtifactBytes
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
		sourceArtifactID := ""
		artifactStore, hasArtifacts := a.server.(VoiceArtifactStore)
		if hasArtifacts {
			sourceArtifactID, err = artifactStore.StoreVoiceOriginal(ctx, u.ID, *m, voiceArtifactName(u), data)
			if err != nil {
				return fmt.Errorf("telegram: persist original voice artifact: %w", err)
			}
		}
		if m.Voice.Duration == 0 {
			probe := a.config.VoiceDurationProbe
			if probe == nil {
				probe = probeVoiceDuration
			}
			probeCtx, stopProbe := context.WithTimeout(ctx, 15*time.Second)
			duration, probeErr := probe(probeCtx, data, voiceArtifactName(u))
			stopProbe()
			if probeErr != nil || duration <= 0 || math.IsNaN(duration) || math.IsInf(duration, 0) {
				recordTiming("duration", "failed")
				return a.persistVoiceFailure(ctx, u, sourceArtifactID, "Не удалось определить длительность аудио. Оригинал сохранён; распознавание не запускалось.")
			}
			if duration > float64(maxSeconds) {
				recordTiming("duration", "over_limit")
				return a.persistVoiceFailure(ctx, u, sourceArtifactID, fmt.Sprintf("Аудио превышает лимит длительности (%d сек). Оригинал сохранён.", maxSeconds))
			}
		}
		if a.asr == nil {
			recordTiming("asr", "unavailable")
			return a.persistVoiceFailure(ctx, u, sourceArtifactID, "Локальное распознавание речи не настроено. Оригинал аудио сохранён.")
		}
		asrStart := time.Now()
		raw, err := a.asr.Transcribe(ctx, data)
		timing.ASRMS = time.Since(asrStart).Milliseconds()
		if err != nil || strings.TrimSpace(raw) == "" {
			recordTiming("asr", "failed")
			return a.persistVoiceFailure(ctx, u, sourceArtifactID, "Не удалось распознать речь. Оригинал аудио сохранён.")
		}
		transcript = VoiceTranscript{Raw: strings.TrimSpace(raw), SourceArtifactID: sourceArtifactID, Delivered: "raw", ChatID: m.ChatID, ThreadID: m.ThreadID, MessageID: m.MessageID, CreatedAt: a.now(), Timing: timing}
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
	if store, ok := a.server.(VoiceArtifactStore); ok && transcript.SourceArtifactID != "" {
		finalText := transcript.Raw
		if transcript.Delivered == "formatted" {
			finalText = transcript.Formatted
		}
		ref, err := store.CompleteVoice(ctx, VoiceArtifactResult{
			UpdateID: u.ID, ChatID: m.ChatID, MessageID: m.MessageID,
			SourceArtifactID: transcript.SourceArtifactID, Transcript: finalText, Raw: transcript.Raw,
		})
		if err != nil {
			return err
		}
		if transcript.TranscriptArtifactID != ref {
			transcript.TranscriptArtifactID = ref
			a.mu.Lock()
			a.state.Transcripts[id] = transcript
			err = a.saveLocked()
			a.mu.Unlock()
			if err != nil {
				return err
			}
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
	copyMessage := *m
	copyMessage.Voice = nil
	copyMessage.Text = final
	if strings.TrimSpace(copyMessage.Caption) != "" {
		copyMessage.Text = strings.TrimSpace(copyMessage.Caption) + "\n\n" + copyMessage.Text
	}
	if transcript.SourceArtifactID != "" && transcript.TranscriptArtifactID != "" {
		copyMessage.Text += "\n\nVoice transcript: artifact://" + transcript.TranscriptArtifactID + "\nOriginal audio: artifact://" + transcript.SourceArtifactID
	}
	u.Message = &copyMessage
	return a.deliverTextUpdate(ctx, u, allowed, &transcript.WorkerRequestID)
}

func logVoiceTiming(u Update, stage, status string, t *VoiceTiming) {
	log.Printf("telegram voice timing update_id=%d message_id=%d stage=%s status=%s download_ms=%d asr_ms=%d formatter_ms=%d processing_ms=%d formatter_status=%s", u.ID, u.Message.MessageID, stage, status, t.DownloadMS, t.ASRMS, t.FormatterMS, t.ProcessingMS, t.FormatterStatus)
}
