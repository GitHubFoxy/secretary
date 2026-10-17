package telegram

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

type Image struct {
	FileID string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id,omitempty"`
	FileName string `json:"file_name,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
	FileSize int64 `json:"file_size,omitempty"`
}

type ImageArtifactStore interface {
	StoreImageOriginal(context.Context, int64, Message, string, []byte) (string, error)
}

func isStillImageName(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".webp":
		return true
	}
	return false
}

func imageArtifactName(u Update) (string, error) {
	if u.Message == nil || u.Message.Image == nil {
		return "", errors.New("telegram: missing image")
	}
	name := u.Message.Image.FileName
	if filepath.Base(name) != name || strings.ContainsAny(name, "/\\") {
		return "", errors.New("telegram: invalid image filename")
	}
	name = voiceNameUnsafe.ReplaceAllString(name, "_")
	if len(name) == 0 || len(name) > 180 || strings.HasPrefix(name, ".") || !isStillImageName(name) {
		return "", errors.New("telegram: unsupported image filename")
	}
	return name, nil
}

func (a *Adapter) imageNotice(ctx context.Context, u Update, text string) error {
	return a.sendMessage(ctx, OutgoingMessage{ChatID:u.Message.ChatID,ThreadID:u.Message.ThreadID,ReplyToMessageID:u.Message.MessageID,Identity:fmt.Sprintf("inbound:image-warning:%d",u.ID),Text:text})
}

func (a *Adapter) deliverImageUpdate(ctx context.Context, u Update, allowed bool) error {
	if !allowed || u.Message == nil || u.Message.Image == nil {
		return nil
	}
	m := u.Message
	if m.ThreadID != 0 && m.ThreadID != 1 {
		a.mu.Lock()
		_, known := a.mappingForThreadLocked(m.ChatID, m.ThreadID)
		a.mu.Unlock()
		if !known {
			return nil
		}
	}
	fail := func(reason string) error {
		return a.imageNotice(ctx, u, "Не удалось обработать изображение: "+reason)
	}
	name, err := imageArtifactName(u)
	if err != nil {
		return fail("поддерживаются только PNG, JPEG и WebP с корректным именем файла.")
	}
	if m.Image.FileID == "" {
		return fail("Telegram не передал file_id.")
	}
	if m.Image.FileSize < 0 || m.Image.FileSize > core.MaxArtifactBytes {
		return fail("файл превышает лимит 8 МиБ.")
	}
	download, ok := a.transport.(VoiceDownloadTransport)
	if !ok {
		return fail("загрузка через Bot API недоступна.")
	}
	artifactStore, ok := a.server.(ImageArtifactStore)
	if !ok {
		return fail("хранилище изображений недоступно.")
	}
	downloadCtx, stop := context.WithTimeout(ctx, 30*time.Second)
	file, err := download.GetFile(downloadCtx, m.Image.FileID)
	if err != nil {
		stop()
		return fmt.Errorf("telegram: get image: %w", err)
	}
	if file.FileSize < 0 || file.FileSize > core.MaxArtifactBytes {
		stop()
		return fail("файл превышает лимит 8 МиБ.")
	}
	data, err := download.DownloadFile(downloadCtx, file.FilePath, core.MaxArtifactBytes)
	stop()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("telegram: download image: %w", err)
	}
	if len(data) == 0 || len(data) > core.MaxArtifactBytes {
		return fail("изображение пустое или превышает лимит 8 МиБ.")
	}
	artifactID, err := artifactStore.StoreImageOriginal(ctx, u.ID, *m, name, data)
	if err != nil {
		var rejected *ImageSourceRejection
		if errors.As(err, &rejected) {
			return fail(rejected.Reason)
		}
		return fmt.Errorf("telegram: store image: %w", err)
	}
	if artifactID == "" {
		return errors.New("telegram: missing original image Artifact ID")
	}
	caption := strings.TrimSpace(m.Caption)
	if caption == "" {
		caption = "Проанализируй прикреплённое изображение."
	}
	prompt := fmt.Sprintf("%s\n\nTelegram image (update %d, chat %d, message %d, file_unique_id %s): artifact://%s", caption, u.ID, m.ChatID, m.MessageID, m.Image.FileUniqueID, artifactID)
	copyMessage := *m
	copyMessage.Image = nil
	copyMessage.Text = prompt
	u.Message = &copyMessage
	return a.deliverTextUpdate(ctx, u, allowed, u.WorkerRequestID)
}
