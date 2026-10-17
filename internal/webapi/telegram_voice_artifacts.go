package webapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/telegram"
)

var supportedTelegramAudio = map[string]string{
	".oga": "audio/ogg",
	".ogg": "audio/ogg",
	".opus": "audio/ogg",
	".mp3": "audio/mpeg",
	".m4a": "audio/mp4",
	".mp4": "audio/mp4",
	".wav": "audio/wav",
	".flac": "audio/flac",
	".webm": "audio/webm",
	".aac": "audio/aac",
}

func (s *Server) authorizedTelegramVoice(w http.ResponseWriter, r *http.Request) (core.Conversation, bool) {
	_, client, ok := s.authorizedPerson(w, r)
	if !ok {
		return core.Conversation{}, false
	}
	if client == nil || client.ID != telegramInternalClientID {
		http.Error(w, "Telegram adapter credential required", http.StatusForbidden)
		return core.Conversation{}, false
	}
	conversation, err := s.store.ConversationForPerson(r.Context(), s.owner.ID)
	if err != nil {
		http.Error(w, "read conversation", http.StatusInternalServerError)
		return core.Conversation{}, false
	}
	return conversation, true
}

func telegramVoiceIdentity(updateID, chatID, messageID int64) (string, bool) {
	if updateID <= 0 || chatID == 0 || messageID <= 0 {
		return "", false
	}
	return fmt.Sprintf("telegram:%d:%d:%d", updateID, chatID, messageID), true
}

func (s *Server) telegramVoiceSource(w http.ResponseWriter, r *http.Request) {
	conversation, ok := s.authorizedTelegramVoice(w, r)
	if !ok {
		return
	}
	updateID, err1 := strconv.ParseInt(r.Header.Get("X-Voice-Update-ID"), 10, 64)
	chatID, err2 := strconv.ParseInt(r.Header.Get("X-Voice-Chat-ID"), 10, 64)
	messageID, err3 := strconv.ParseInt(r.Header.Get("X-Voice-Message-ID"), 10, 64)
	identity, valid := telegramVoiceIdentity(updateID, chatID, messageID)
	if err1 != nil || err2 != nil || err3 != nil || !valid {
		http.Error(w, "Invalid voice source identity", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.Header.Get("X-Voice-Name"))
	if len(name) == 0 || len(name) > 180 || name == "." || name == ".." || strings.HasPrefix(name, ".") ||
		strings.ContainsAny(name, "/\\") || filepath.Base(name) != name {
		http.Error(w, "Invalid audio filename", http.StatusUnsupportedMediaType)
		return
	}
	ext := strings.ToLower(filepath.Ext(name))
	mediaType, supported := supportedTelegramAudio[ext]
	if !supported {
		http.Error(w, "Unsupported audio format", http.StatusUnsupportedMediaType)
		return
	}
	if r.ContentLength > core.MaxArtifactBytes {
		http.Error(w, "Audio exceeds the 8 MiB artifact limit", http.StatusRequestEntityTooLarge)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, core.MaxArtifactBytes+1)
	data, err := io.ReadAll(r.Body)
	if err != nil || len(data) > core.MaxArtifactBytes {
		http.Error(w, "Audio exceeds the 8 MiB artifact limit", http.StatusRequestEntityTooLarge)
		return
	}
	if len(data) == 0 {
		http.Error(w, "Empty audio is not supported", http.StatusBadRequest)
		return
	}
	original, duplicate, err := s.store.SaveArtifact(r.Context(), conversation.PersonID, conversation.ID,
		identity+":source", name, mediaType, data)
	if errors.Is(err, core.ErrIdempotencyConflict) {
		http.Error(w, "Conflicting voice source", http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, "Store voice source failed", http.StatusInternalServerError)
		return
	}
	if err := s.store.RegisterVoiceSource(r.Context(), conversation.PersonID, original.ID, identity); err != nil {
		http.Error(w, "Register voice source failed", http.StatusInternalServerError)
		return
	}
	status := http.StatusCreated
	if duplicate {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{"artifact": original, "reference": "artifact://" + original.ID, "duplicate": duplicate})
}

func (s *Server) telegramVoiceResult(w http.ResponseWriter, r *http.Request) {
	conversation, ok := s.authorizedTelegramVoice(w, r)
	if !ok {
		return
	}
	var result telegram.VoiceArtifactResult
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		http.Error(w, "Invalid voice result", http.StatusBadRequest)
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		http.Error(w, "Invalid voice result", http.StatusBadRequest)
		return
	}
	identity, valid := telegramVoiceIdentity(result.UpdateID, result.ChatID, result.MessageID)
	if !valid || result.SourceArtifactID == "" {
		http.Error(w, "Invalid voice result identity", http.StatusBadRequest)
		return
	}
	if result.Failed {
		if result.Transcript != "" || result.Raw != "" {
			http.Error(w, "Failed ASR cannot have a transcript", http.StatusBadRequest)
			return
		}
		err := s.store.FailVoiceSource(r.Context(), conversation.PersonID, result.SourceArtifactID, identity)
		if err != nil {
			http.Error(w, "Mark voice processing failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "failed", "source_artifact_id": result.SourceArtifactID})
		return
	}
	if strings.TrimSpace(result.Transcript) == "" || strings.TrimSpace(result.Raw) == "" {
		http.Error(w, "Empty ASR transcript", http.StatusBadRequest)
		return
	}
	source, _, err := s.store.ArtifactForPerson(r.Context(), conversation.PersonID, result.SourceArtifactID)
	if err != nil || source.SourceIdentity != identity {
		http.Error(w, "Voice source identity mismatch", http.StatusConflict)
		return
	}
	text := "# Telegram voice transcript\n\n" + result.Transcript + "\n\nOriginal audio: artifact://" + result.SourceArtifactID + "\n"
	if result.Raw != result.Transcript {
		text += "\n## Raw ASR\n\n" + result.Raw + "\n"
	}
	name := fmt.Sprintf("telegram-voice-%d.md", result.UpdateID)
	artifact, duplicate, err := s.store.SaveArtifact(r.Context(), conversation.PersonID, conversation.ID,
		identity+":transcript", name, "text/markdown", []byte(text))
	if errors.Is(err, core.ErrIdempotencyConflict) {
		http.Error(w, "Conflicting voice transcript", http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, "Save voice transcript failed", http.StatusInternalServerError)
		return
	}
	if err := s.store.LinkVoiceTranscript(r.Context(), conversation.PersonID, result.SourceArtifactID, artifact.ID, identity); err != nil {
		http.Error(w, "Link voice transcript failed", http.StatusInternalServerError)
		return
	}
	status := http.StatusCreated
	if duplicate {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{"artifact": artifact, "reference": "artifact://" + artifact.ID, "duplicate": duplicate})
}
