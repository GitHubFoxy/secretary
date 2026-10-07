package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

// RemoteMCPObserver carries only a launch-scoped observation capability. It
// cannot read Conversation, call tools or grant execution permissions.
type RemoteMCPObserver struct {
	BaseURL    string
	Capability string
	Client     *http.Client
}

func (r RemoteMCPObserver) Observe(ctx context.Context, observation core.SecretaryMCPObservation) error {
	if r.Capability == "" || strings.TrimSpace(r.BaseURL) == "" {
		return errors.New("mcp: observation configuration missing")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	body, err := json.Marshal(observation)
	if err != nil {
		return errors.New("mcp: invalid observation")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(r.BaseURL, "/")+"/v1/internal/secretary/mcp/observe", bytes.NewReader(body))
	if err != nil {
		return errors.New("mcp: observation request unavailable")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+r.Capability)
	client := r.Client
	if client == nil {
		client = &http.Client{}
	}
	// Never forward this credential through a redirect, even if a supplied client
	// would normally follow one. No response/error payload enters diagnostics.
	bounded := *client
	bounded.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := bounded.Do(request)
	if err != nil {
		return errors.New("mcp: observation unavailable")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1024))
	if response.StatusCode != http.StatusNoContent {
		return errors.New("mcp: observation rejected")
	}
	return nil
}
