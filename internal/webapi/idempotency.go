package webapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/beruseruko/secretary/internal/core"
)

var (
	errIdempotencyKeyRequired = errors.New("idempotency key is required")
	errIdempotencyKeyMismatch = errors.New("idempotency key in body and header must match")
)

// requireIdempotencyKey is shared by every Client-facing mutation. A body key
// is accepted for JSON clients, while the standard header remains supported.
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
