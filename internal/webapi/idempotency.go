package webapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

var (
	errIdempotencyKeyRequired = errors.New("idempotency key is required")
	errIdempotencyKeyMismatch = errors.New("idempotency key in body and header must match")
)

func requireIdempotencyKey(w http.ResponseWriter, r *http.Request, bodyKey string) (string, bool) {
	bodyKey = strings.TrimSpace(bodyKey)
	headerKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if bodyKey != "" && headerKey != "" && bodyKey != headerKey {
		http.Error(w, errIdempotencyKeyMismatch.Error(), http.StatusBadRequest)
		return "", false
	}
	key := bodyKey
	if key == "" {
		key = headerKey
	}
	if key == "" {
		http.Error(w, errIdempotencyKeyRequired.Error(), http.StatusBadRequest)
		return "", false
	}
	return key, true
}

func writeClientMutationError(w http.ResponseWriter, err error) {
	if errors.Is(err, node.ErrCommandOutcomeUnknown) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":     "Worker command outcome is uncertain; retry with the same idempotency key",
			"outcome":   "uncertain",
			"retryable": true,
		})
		return
	}
	var cmdErr *node.NodeCommandError
	var steerErr *node.SteeringError
	if errors.As(err, &cmdErr) {
		status := http.StatusBadRequest
		errorMessage := cmdErr.Outcome.ErrorMessage
		if errorMessage == "" {
			errorMessage = cmdErr.Error()
		}
		errorMessage = node.SanitizeFailureMessage(errorMessage)
		category := core.SanitizeFailureCategory(cmdErr.Outcome.FailureCategory)
		if category == "" {
			category = core.FailureCategoryInternalError
		}
		payload := map[string]any{
			"error":         errorMessage,
			"category":      string(category),
			"provider_code": core.SanitizeFailureProviderCode(cmdErr.Outcome.ProviderCode),
		}
		technical := cmdErr.Outcome.Technical
		if technical == "" && cmdErr.Outcome.ErrorCode != "" && cmdErr.Outcome.ErrorCode != cmdErr.Outcome.ProviderCode {
			technical = cmdErr.Outcome.ErrorCode
		}
		if technical != "" {
			payload["technical"] = node.SanitizeFailureMessage(technical)
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(payload)
		return
	}
	if errors.As(err, &steerErr) {
		status := http.StatusBadRequest
		errorMessage := steerErr.Failure.Message
		if errorMessage == "" {
			errorMessage = steerErr.Error()
		}
		errorMessage = node.SanitizeFailureMessage(errorMessage)
		safeCategory := core.SanitizeFailureCategory(steerErr.Failure.Category)
		if safeCategory == "" {
			safeCategory = core.FailureCategoryInternalError
		}
		payload := map[string]any{
			"error":         errorMessage,
			"category":      string(safeCategory),
			"provider_code": core.SanitizeFailureProviderCode(steerErr.Failure.ProviderCode),
		}
		if steerErr.Failure.Technical != "" {
			payload["technical"] = node.SanitizeFailureMessage(steerErr.Failure.Technical)
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(payload)
		return
	}
	status := http.StatusBadRequest
	switch {
	case errors.Is(err, core.ErrIdempotencyConflict):
		status = http.StatusConflict
	case errors.Is(err, core.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, core.ErrPairingAlreadyUsed),
		errors.Is(err, core.ErrProjectRevisionConflict),
		errors.Is(err, core.ErrUserDocumentRevisionConflict):
		status = http.StatusConflict
	}
	http.Error(w, err.Error(), status)
}
