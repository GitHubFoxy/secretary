package telegram

import (
	"context"
	"bytes"
	"image"
	_ "image/jpeg"
	_ "image/png"
	_ "golang.org/x/image/webp"
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
	if name == "" {
		return "", nil
	}
	if filepath.Base(name) != name || strings.ContainsAny(name, "/\\") {
		return "", errors.New("telegram: invalid image filename")
	}
	name = voiceNameUnsafe.ReplaceAllString(name, "_")
	if len(name) == 0 || len(name) > 180 || strings.HasPrefix(name, ".") || !isStillImageName(name) {
		return "", errors.New("telegram: unsupported image filename")
	}
	return name, nil
}

// Persist a terminal image warning independently of Bot API availability.
// Never retry the original getFile operation because Telegram sendMessage
// temporarily failed. The regular outbox is responsible for delivery/retry.
// Terminal failures must checkpoint the warning AND remove the image in one
// durable state write. A crash between these operations must not re-run
// getFile, re-upload an Artifact or replay a terminal native rejection.
// Telegram delivery itself is best effort against the independent outbox.
func (a *Adapter) imageNotice(ctx context.Context, u Update, text string) error {
	if u.Message == nil || u.Message.ChatID == 0 {
		return ErrUnauthorized
	}
	message := OutgoingMessage{
		ChatID: u.Message.ChatID, ThreadID: u.Message.ThreadID,
		ReplyToMessageID: u.Message.MessageID,
		Identity: fmt.Sprintf("inbound:image-warning:%d", u.ID), Text: text,
	}
	a.mu.Lock()
	identity := outgoingMessageIdentity(message)
	alreadyDelivered := false
	if a.config.GroupChatID != nil {
		_, alreadyDelivered = a.state.Delivered[identity]
	} else {
		_, alreadyDelivered = a.state.InboundNotified[strings.TrimPrefix(message.Identity, "inbound:")]
	}
	alreadyQueued := false
	for _, pending := range a.state.Outbox {
		if outgoingMessageIdentity(pending) == identity {
			alreadyQueued = true
			break
		}
	}
	// Copy for rollback if the atomic durable checkpoint fails.
	previousImages := append([]Update(nil), a.state.PendingImages...)
	previousOutbox := append([]OutgoingMessage(nil), a.state.Outbox...)
	imageRemoved := false
	for i, pending := range a.state.PendingImages {
		if pending.ID == u.ID {
			a.state.PendingImages = append(a.state.PendingImages[:i], a.state.PendingImages[i+1:]...)
			imageRemoved = true
			break
		}
	}
	warningQueued := !alreadyDelivered && !alreadyQueued
	if warningQueued {
		a.state.Outbox = append(a.state.Outbox, message)
	}
	var err error
	if imageRemoved || warningQueued {
		err = a.saveLocked()
		if err != nil {
			a.state.PendingImages = previousImages
			a.state.Outbox = previousOutbox
		}
	}
	a.mu.Unlock()
	if err != nil {
		return err
	}
	if alreadyDelivered {
		return nil
	}
	// No network IO here: imageNotice runs under the bounded image slot.
	// The lane will flush the durable warning independently after releasing
	// that slot; crash recovery uses the normal outbox loops.
	return nil
}

// Re-evaluate authorization on every durable replay; a queued update must
// never inherit an authorization decision from a previous owner or topic.
func (a *Adapter) imageAuthorized(u Update) bool {
	if u.Message == nil || u.Message.Image == nil && u.Message.Video == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	m := u.Message
	if !a.ownerAllowed(m.ChatID) || !a.senderAllowed(m.FromID) {
		return false
	}
	if m.ThreadID != 0 && m.ThreadID != 1 {
		mapping, known := a.mappingForThreadLocked(m.ChatID, m.ThreadID)
		if !known {
			return false
		}
		if u.ImageWorkerRef == "" || u.WorkerRequestID == nil || mapping.WorkerRef != u.ImageWorkerRef {
			// Unbound legacy queue entries and reused topics fail closed.
			// Never acquire a binding from the latest pending question.
			return false
		}
	}
	return true
}

// A delayed Telegram photo belongs to its original needs_input request.
// Never silently rebind it to another pending question while downloading.
func (a *Adapter) imageRequestStillCurrent(u Update) bool {
	if u.Message == nil || u.Message.ThreadID == 0 || u.Message.ThreadID == 1 {
		return true
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	mapping, ok := a.mappingForThreadLocked(u.Message.ChatID, u.Message.ThreadID)
	if !ok || (u.ImageWorkerRef != "" && u.ImageWorkerRef != mapping.WorkerRef) {
		return false
	}
	if u.WorkerRequestID == nil {
		return false
	}
	return *u.WorkerRequestID == mapping.PendingRequestID
}

func (a *Adapter) deliverImageUpdate(ctx context.Context, u Update, allowed bool) error {
	if u.Message != nil && u.Message.Video != nil { return a.deliverVideoUpdate(ctx, u, allowed) }
 if u.Message != nil && u.Message.Document != nil { return a.deliverDocumentUpdate(ctx,u,allowed) }
	if !allowed || !a.imageAuthorized(u) {
		return nil
	}
	m := u.Message
	fail := func(reason string) error {
		return a.imageNotice(ctx, u, "Не удалось обработать изображение: "+reason)
	}
	if !a.imageRequestStillCurrent(u) {
		return fail("вопрос Worker изменился до обработки фотографии; отправьте фотографию заново для текущего вопроса.")
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
		if errors.Is(err, ErrTelegramFileRejected) || errors.Is(err, ErrFileTooLarge) {
			return fail("Telegram отклонил файл (недоступен, слишком большой или некорректный file_id).")
		}
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
		if errors.Is(err, ErrFileTooLarge) {
			return fail("файл превышает лимит 8 МиБ.")
		}
		if errors.Is(err, ErrTelegramDownloadRejected) {
			return fail("Telegram отклонил скачивание файла (постоянная ошибка).")
		}
		return fmt.Errorf("telegram: download image: %w", err)
	}
	if len(data) == 0 || len(data) > core.MaxArtifactBytes {
		return fail("изображение пустое или превышает лимит 8 МиБ.")
	}
	if name == "" {
		_, format, probeErr := image.DecodeConfig(bytes.NewReader(data))
		if probeErr != nil {
			return fail("не удалось определить формат изображения.")
		}
		extension := map[string]string{"jpeg": ".jpg", "png": ".png", "webp": ".webp"}[format]
		if extension == "" {
			return fail("поддерживаются только PNG, JPEG и WebP.")
		}
		name = fmt.Sprintf("telegram-image-%d-%d%s", u.ID, m.MessageID, extension)
	}
	if !a.imageAuthorized(u) {
		return nil
	}
	if !a.imageRequestStillCurrent(u) {
		return fail("вопрос Worker изменился во время загрузки; отправьте фотографию заново для текущего вопроса.")
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
	if !a.imageAuthorized(u) {
		return nil
	}
	copyMessage := *m
	copyMessage.Image = nil
	copyMessage.Text = prompt
	u.Message = &copyMessage
	request := u.WorkerRequestID
	if m.ThreadID != 0 && m.ThreadID != 1 {
		a.mu.Lock()
		mapping, exists := a.mappingForThreadLocked(m.ChatID, m.ThreadID)
		a.mu.Unlock()
		if !exists || (u.ImageWorkerRef != "" && mapping.WorkerRef != u.ImageWorkerRef) {
			return nil
		}
		if request == nil || *request != mapping.PendingRequestID {
			return fail("вопрос Worker изменился; исходная фотография не может отвечать на другой вопрос.")
		}
	}
	return a.deliverTextUpdate(ctx, u, allowed, request)
}

func (a *Adapter) queueImageUpdate(u Update, allowed bool) error {
	if !allowed || u.Message == nil || u.Message.Image == nil && u.Message.Video == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if u.Message.ThreadID != 0 && u.Message.ThreadID != 1 {
		// Binding was saved atomically with PendingInputs. Never replace it
		// with whatever question is pending when this image finally drains.
		if u.ImageWorkerRef == "" || u.WorkerRequestID == nil {
			return nil
		}
	}
	for _, queued := range a.state.PendingImages {
		if queued.ID == u.ID {
			return nil
		}
	}
	a.state.PendingImages = append(a.state.PendingImages, u)
	return a.saveLocked()
}

func (a *Adapter) imageKeys() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	seen := map[string]bool{}
	var keys []string
	for _, u := range a.state.PendingImages {
		key := inputLane(u)
		if !seen[key] {
			seen[key] = true
			keys = append(keys, key)
		}
	}
	return keys
}

// Decoding a 24-megapixel image can allocate far more than its compressed
// 8 MiB original. Only a few images may download/decode/store at once,
// independent of the number of Telegram Topics.
const maxConcurrentTelegramImages = 2

func (a *Adapter) processImageWithSlot(ctx context.Context, u Update) error {
	select {
	case a.imageSlots <- struct{}{}:
		defer func() { <-a.imageSlots }()
	case <-ctx.Done():
		return ctx.Err()
	}
	return a.deliverImageUpdate(ctx, u, a.imageAuthorized(u))
}

func (a *Adapter) startImageWorkers(ctx context.Context) {
	for _, key := range a.imageKeys() {
		a.inputMu.Lock()
		if a.imageWorkers == nil {
			a.imageWorkers = map[string]bool{}
		}
		if a.imageWorkers[key] {
			a.inputMu.Unlock()
			continue
		}
		a.imageWorkers[key] = true
		a.imageWG.Add(1)
		a.inputMu.Unlock()
		go func(lane string) {
			defer a.imageWG.Done()
			defer func() {
				a.inputMu.Lock()
				delete(a.imageWorkers, lane)
				a.inputMu.Unlock()
			}()
			_ = a.drainImageLane(ctx, lane)
		}(key)
	}
}

func (a *Adapter) drainImageLane(ctx context.Context, lane string) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		a.mu.Lock()
		var next Update
		for _, u := range a.state.PendingImages {
			if inputLane(u) == lane {
				next = u
				break
			}
		}
		a.mu.Unlock()
		if next.ID == 0 {
			return nil
		}
		if err := a.processImageWithSlot(ctx, next); err != nil {
			return err
		}
		// processImageWithSlot has released its permit. A terminal failure
		// already removed this image and persisted its warning atomically.
		a.mu.Lock()
		terminal := true
		for _, queued := range a.state.PendingImages {
			if queued.ID == next.ID {
				terminal = false
				break
			}
		}
		a.mu.Unlock()
		if terminal {
			warning := OutgoingMessage{ChatID:next.Message.ChatID, ThreadID:next.Message.ThreadID}
			if a.isWorkerTopicMessage(warning) {
				_ = a.drainTopicOutbox(ctx)
			} else {
				_ = a.drainOutbox(ctx)
			}
		}
		a.mu.Lock()
		if len(a.state.PendingImages) != 0 {
			index := -1
			for i, queued := range a.state.PendingImages {
				if queued.ID == next.ID {
					index = i
					break
				}
			}
			if index >= 0 {
				a.state.PendingImages = append(a.state.PendingImages[:index], a.state.PendingImages[index+1:]...)
			}
		}
		err := a.saveLocked()
		a.mu.Unlock()
		if err != nil {
			return err
		}
		// Only this conversation lane depends on the preceding image.
		// Other Worker Topics sharing the group chat are independent.
		if err := a.drainLane(ctx, lane); err != nil {
			return err
		}
	}
}
