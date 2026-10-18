package node

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"net/url"
	"sync"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

// A Worker has permission only to request the trusted Node to publish a path.
// The Node privately reads that path through os.Root, checks its type, and
// authenticates to the Server. Neither credential nor signed token is present
// in the harness or its MCP subprocess environment.
type workerPublishBroker struct {
	mu          sync.Mutex
	identity    NodeIdentity
	listener    net.Listener
	server      *http.Server
	registrations map[string]workerPublishBinding
	byAttempt   map[string]string
	closed      bool
}

type workerPublishBinding struct {
	workerRef string
	attemptID string
	workspace string
	root *os.Root
}

func newWorkerPublishBroker(identity NodeIdentity) (*workerPublishBroker, error) {
	if err := identity.Validate(); err != nil { return nil, err }
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil { return nil, fmt.Errorf("node artifact: bind private publisher: %w", err) }
	broker := &workerPublishBroker{
		identity: identity, listener: listener,
		registrations: make(map[string]workerPublishBinding),
		byAttempt: make(map[string]string),
	}
	broker.server = &http.Server{Handler: broker, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = broker.server.Serve(listener) }()
	return broker, nil
}

func (b *workerPublishBroker) Register(workerRef, attemptID, workspace string) (string, error) {
	if workerRef == "" || attemptID == "" || workspace == "" {
		return "", errors.New("node artifact: incomplete Worker publication binding")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed { return "", errors.New("node artifact: publisher is closed") }
	if existing, ok := b.byAttempt[attemptID]; ok {
		binding := b.registrations[existing]
		if binding.workerRef != workerRef || binding.workspace != workspace {
			return "", errors.New("node artifact: publisher Attempt binding changed")
		}
		return "http://" + b.listener.Addr().String() + "/publish/" + existing, nil
	}
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil { return "", err }
	identifier := hex.EncodeToString(random[:])
	root, openErr := os.OpenRoot(workspace)
	if openErr != nil {return "", fmt.Errorf("node artifact: pin Worker workspace: %w",openErr)}
	b.registrations[identifier] = workerPublishBinding{workerRef: workerRef, attemptID: attemptID, workspace: workspace, root: root}
	b.byAttempt[attemptID] = identifier
	return "http://" + b.listener.Addr().String() + "/publish/" + identifier, nil
}

func (b *workerPublishBroker) Unregister(attemptID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if identifier, ok := b.byAttempt[attemptID]; ok {
		delete(b.byAttempt, attemptID)
		if existing := b.registrations[identifier]; existing.root != nil {_ = existing.root.Close()}
		delete(b.registrations, identifier)
	}
}

func (b *workerPublishBroker) Close() error {
	b.mu.Lock()
	if b.closed { b.mu.Unlock(); return nil }
	b.closed = true
	for _, binding := range b.registrations {
		if binding.root != nil {_ = binding.root.Close()}
	}
	b.registrations = map[string]workerPublishBinding{}
	b.byAttempt = map[string]string{}
	b.mu.Unlock()
	return b.server.Close()
}

type workerPublishRequest struct {
	Path string `json:"path"`
	Name string `json:"name,omitempty"`
}

func (b *workerPublishBroker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	remoteHost, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || remoteHost != "127.0.0.1" || r.Host != b.listener.Addr().String() {
		http.Error(w, "local publication only", http.StatusForbidden)
		return
	}
	// No byte-carrying upload route is exposed to the harness.
	// Only two short JSON strings are permitted; all file I/O is Node-owned.
	if r.ContentLength > 4096 {
		http.Error(w, "publication request too large", http.StatusRequestEntityTooLarge)
		return
	}
	var request workerPublishRequest
	decoder := json.NewDecoder(io.LimitReader(r.Body, 4097))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		http.Error(w, "invalid publication request", http.StatusBadRequest)
		return
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		http.Error(w, "invalid publication request", http.StatusBadRequest)
		return
	}
	const prefix = "/publish/"
	if len(r.URL.Path) != len(prefix)+64 || r.URL.Path[:len(prefix)] != prefix {
		http.NotFound(w, r)
		return
	}
	identifier := r.URL.Path[len(prefix):]
	b.mu.Lock()
	binding, ok := b.registrations[identifier]
	b.mu.Unlock()
	if !ok {
		http.Error(w, "publication Attempt is not active", http.StatusForbidden)
		return
	}
	auth, err := b.identity.Authenticator()
	if err != nil { http.Error(w, "Node credential unavailable", http.StatusServiceUnavailable); return }
	publisher := ArtifactPublisher{
		Node: b.identity.Node, WorkerRef: binding.workerRef,
		AttemptID: binding.attemptID, Workspace: binding.workspace,
		ServerURL: b.identity.ConnectURL,
		WorkspaceRoot: binding.root,
		Capability: auth.SignPublishCapability(b.identity.Node, binding.workerRef, binding.attemptID),
		Auth: &auth,
	}
	artifact, err := publisher.Publish(r.Context(), request.Path, request.Name)
	if err != nil {
		http.Error(w, "publication rejected: "+err.Error(), http.StatusUnprocessableEntity)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Artifact core.Artifact `json:"artifact"`
	}{Artifact: artifact})
}

// RequestWorkerArtifact is an unprivileged client: its only operation is to
// name a workspace-relative path; it cannot provide bytes or a Server token.
func RequestWorkerArtifact(ctx context.Context, endpoint, path, name string) (core.Artifact, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.User != nil ||
		u.RawQuery != "" || u.Fragment != "" {
		return core.Artifact{}, errors.New("artifact MCP: invalid local Node broker")
	}
	encoded, err := json.Marshal(workerPublishRequest{Path: path, Name: name})
	if err != nil { return core.Artifact{}, err }
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(encoded))
	if err != nil { return core.Artifact{}, err }
	request.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("artifact MCP: redirect refused")
	}}
	resp, err := client.Do(request)
	if err != nil { return core.Artifact{}, fmt.Errorf("artifact MCP: local broker unavailable: %w", err) }
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// Never echo privileged Server headers or credential-bearing details.
		return core.Artifact{}, fmt.Errorf("artifact MCP: local publication rejected (HTTP %d)", resp.StatusCode)
	}
	var response struct { Artifact core.Artifact `json:"artifact"` }
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8192)).Decode(&response); err != nil ||
		response.Artifact.ID == "" {
		return core.Artifact{}, errors.New("artifact MCP: malformed publication acknowledgement")
	}
	return response.Artifact, nil
}
