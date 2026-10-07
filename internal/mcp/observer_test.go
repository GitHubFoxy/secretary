package mcp

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

type failedMCPWriter struct{ short bool }

func (w failedMCPWriter) Write(frame []byte) (int, error) {
	if w.short {
		return len(frame) - 1, nil
	}
	return 0, io.ErrClosedPipe
}

func TestMCPDiscoveryObservationRequiresWrittenAndFlushedResponse(t *testing.T) {
	for _, tc := range []struct {
		name    string
		out     io.Writer
		success bool
	}{
		{"flushed", bufio.NewWriter(&bytes.Buffer{}), true},
		{"failed write", failedMCPWriter{}, false},
		{"short write", failedMCPWriter{short: true}, false},
		{"failed flush", bufio.NewWriter(failedMCPWriter{}), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			observations := []core.SecretaryMCPObservation{}
			input := strings.NewReader("{\"jsonrpc\":\"2.0\",\"method\":\"initialize\"}\n{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\"}\n")
			err := (Server{Handler: Secretary{}, Observe: func(_ context.Context, o core.SecretaryMCPObservation) error {
				observations = append(observations, o)
				return nil
			}}).Serve(context.Background(), input, tc.out)
			if (err == nil) != tc.success || len(observations) != 2 || observations[0].Phase != "startup" || observations[1].Phase != "initialize" || observations[1].Success != tc.success {
				t.Fatal("notification/write/flush produced false discovery")
			}
		})
	}
}

func TestMCPObserverTimeoutAndRedirectNeverExposeCredentialOrInventSuccess(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(entered)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	observer := RemoteMCPObserver{BaseURL: server.URL, Capability: "private-observer-token"}
	if err := observer.Observe(ctx, core.SecretaryMCPObservation{Phase: "startup", Success: true}); err == nil || strings.Contains(err.Error(), "private-observer-token") || strings.Contains(err.Error(), server.URL) {
		t.Fatal("timeout was hidden or exposed private wiring")
	}
	select {
	case <-entered:
	default:
		t.Fatal("timeout fixture never reached external HTTP boundary")
	}
	var redirected bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected = true; w.WriteHeader(204) }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	if err := (RemoteMCPObserver{BaseURL: redirect.URL, Capability: "private-observer-token"}).Observe(context.Background(), core.SecretaryMCPObservation{Phase: "startup", Success: true}); err == nil || redirected {
		t.Fatal("observer followed credential redirect")
	}
	output := bytes.Buffer{}
	if err := (Server{Handler: Secretary{}, Observe: func(context.Context, core.SecretaryMCPObservation) error {
		return errors.New("observation unavailable")
	}}).Serve(context.Background(), strings.NewReader(`{"id":1,"method":"initialize"}`), &output); err == nil || output.Len() != 0 {
		t.Fatal("missing observer was presented as initialized")
	}
}
