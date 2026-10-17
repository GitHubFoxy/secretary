package node

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/beruseruko/secretary/internal/core"
)

type ArtifactPublisher struct {
	ServerURL string
	Node core.NodeReference
	WorkerRef string
	AttemptID string
	Workspace string
	Capability string
	// Auth is private Node-only material, never copied into a Worker MCP env.
	Auth *Authenticator
	Client *http.Client
}

func (n *ExecutionNode) SetArtifactPublisher(identity NodeIdentity, command string) error {
	if err := identity.Validate(); err != nil { return err }
	if command == "" { return errors.New("node artifact: publisher executable required") }
	broker, err := newWorkerPublishBroker(identity)
	if err != nil { return err }
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.publishBroker != nil {
		_ = broker.Close()
		return errors.New("node artifact: publisher already initialized")
	}
	n.publishBroker = broker
	n.artifactPublisherCommand = command
	return nil
}

func (n *ExecutionNode) CloseArtifactPublisher() error {
	n.mu.Lock()
	broker := n.publishBroker
	n.publishBroker = nil
	n.mu.Unlock()
	if broker != nil { return broker.Close() }
	return nil
}

func (n *ExecutionNode) unregisterArtifactPublisher(attemptID string) {
	n.mu.Lock()
	broker := n.publishBroker
	n.mu.Unlock()
	if broker != nil { broker.Unregister(attemptID) }
}

func (n *ExecutionNode) workerArtifactMCPServers(workerRef, attemptID, workspace string) ([]MCPServer, error) {
	servers := n.workerMCPServers()
	n.mu.Lock()
	broker := n.publishBroker
	binary := n.artifactPublisherCommand
	n.mu.Unlock()
	if broker == nil { return servers, nil }
	if workerRef == "" || attemptID == "" || workspace == "" {
		return nil, errors.New("node artifact: incomplete Worker publication binding")
	}
	endpoint, err := broker.Register(workerRef, attemptID, workspace)
	if err != nil { return nil, err }
	// The harness and its MCP children receive only a loopback path-limited broker URL.
	// Neither the Node credential nor a Server-valid publish signature is exported.
	return append(servers, MCPServer{
		Name: "secretary_artifacts", Command: binary, Args: []string{"--artifact-mcp"},
		Env: []MCPEnv{{Name: "SECRETARY_ARTIFACT_BROKER_URL", Value: endpoint}},
	}), nil
}

func ValidPublishedName(name string) bool {
	if len(name) == 0 || len(name) > 180 || name == "." || name == ".." ||
		strings.ContainsAny(name, "/\\") || strings.HasPrefix(name, ".") {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".txt", ".md", ".pdf", ".csv", ".json", ".xml", ".yaml", ".yml",
		".html", ".log", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx",
		".odt", ".ods", ".rtf", ".zip", ".png", ".jpg", ".jpeg", ".webp":
		return true
	}
	return false
}

func (p ArtifactPublisher) authority() (Authenticator, error) {
	if p.Auth == nil { return Authenticator{}, errors.New("artifact publish: trusted Node signing authority unavailable") }
	return *p.Auth, nil
}

func (p ArtifactPublisher) Publish(ctx context.Context, relativePath, name string) (core.Artifact, error) {
	if len(relativePath) == 0 || len(relativePath) > 1024 || filepath.IsAbs(relativePath) ||
		!filepath.IsLocal(relativePath) || filepath.Clean(relativePath) != relativePath ||
		strings.Contains(relativePath, "\\") && os.PathSeparator != '\\' {
		return core.Artifact{}, errors.New("artifact publish: path must be a clean relative workspace path")
	}
	for _, component := range strings.Split(filepath.ToSlash(relativePath), "/") {
		if component == "" || component == "." || component == ".." || strings.HasPrefix(component, ".") {
			return core.Artifact{}, errors.New("artifact publish: hidden or parent paths are forbidden")
		}
	}
	if name == "" {
		name = filepath.Base(relativePath)
	}
	if !ValidPublishedName(name) {
		return core.Artifact{}, errors.New("artifact publish: unsupported or unsafe output name")
	}
	if !ValidPublishedSourcePath(filepath.ToSlash(relativePath)) {
		return core.Artifact{}, errors.New("artifact publish: unsafe source path")
	}
	if p.Node == "" || p.WorkerRef == "" || p.AttemptID == "" || p.Capability == "" || p.Workspace == "" {
		return core.Artifact{}, errors.New("artifact publish: Worker binding is unavailable")
	}
	// Each component is opened via a pinned directory descriptor with
	// O_NOFOLLOW. No separated Lstat/Open checks: renaming a component into
	// a symlink between operations cannot redirect an already-open directory.
	file, err := openPublishedFileNoFollow(p.Workspace, relativePath)
	if err != nil {
		return core.Artifact{}, errors.New("artifact publish: unsafe or unreadable workspace file")
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil || !stat.Mode().IsRegular() {
		return core.Artifact{}, errors.New("artifact publish: output must be a regular file")
	}
	if stat.Size() == 0 || stat.Size() > core.MaxArtifactBytes {
		return core.Artifact{}, errors.New("artifact publish: output must be 1 byte to 8 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(file, core.MaxArtifactBytes+1))
	if err != nil || len(data) == 0 || len(data) > core.MaxArtifactBytes {
		return core.Artifact{}, errors.New("artifact publish: file read failed or size limit exceeded")
	}
	sum := sha256.Sum256([]byte(p.AttemptID + "\x00" + relativePath))
	key := "publish:" + hex.EncodeToString(sum[:])
	endpoint, err := url.Parse(p.ServerURL)
	if err != nil || endpoint.Host == "" {
		return core.Artifact{}, errors.New("artifact publish: server URL invalid")
	}
	if endpoint.Scheme == "wss" {
		endpoint.Scheme = "https"
	} else if endpoint.Scheme == "ws" {
		endpoint.Scheme = "http"
	} else if endpoint.Scheme != "http" && endpoint.Scheme != "https" {
		return core.Artifact{}, errors.New("artifact publish: server scheme invalid")
	}
	endpoint.Path = strings.TrimSuffix(endpoint.Path, "/v1/nodes/connect") + "/v1/artifacts/publish"
	endpoint.RawQuery = ""
	endpoint.Fragment = ""
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(data))
	if err != nil {
		return core.Artifact{}, err
	}
	request.Header.Set("X-Secretary-Node", string(p.Node))
	request.Header.Set("X-Artifact-Worker", p.WorkerRef)
	request.Header.Set("X-Artifact-Attempt", p.AttemptID)
	auth, err := p.authority()
	if err != nil { return core.Artifact{}, err }
	sourcePath := filepath.ToSlash(relativePath)
	checksum := sha256.Sum256(data)
	request.Header.Set("X-Artifact-Publish-Token", p.Capability)
	request.Header.Set("X-Artifact-Source-Path", sourcePath)
	request.Header.Set("X-Artifact-Source-Signature", auth.SignPublishedSource(p.Node, p.WorkerRef, p.AttemptID, sourcePath, name, hex.EncodeToString(checksum[:])))
	request.Header.Set("X-Artifact-Name", name)
	request.Header.Set("Idempotency-Key", key)
	request.Header.Set("Content-Type", "application/octet-stream")
	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		return core.Artifact{}, fmt.Errorf("artifact publish: transfer failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated && response.StatusCode != http.StatusOK {
		switch response.StatusCode {
		case http.StatusRequestEntityTooLarge:
			return core.Artifact{}, errors.New("artifact publish: size limit exceeded")
		case http.StatusForbidden, http.StatusUnauthorized, http.StatusNotFound:
			return core.Artifact{}, errors.New("artifact publish: Worker not authorized")
		case http.StatusConflict:
			return core.Artifact{}, errors.New("artifact publish: duplicate path has different contents")
		default:
			return core.Artifact{}, fmt.Errorf("artifact publish: server rejected publication (HTTP %d)", response.StatusCode)
		}
	}
	var result struct { Artifact core.Artifact `json:"artifact"` }
	if err := json.NewDecoder(io.LimitReader(response.Body, 8192)).Decode(&result); err != nil || result.Artifact.ID == "" {
		return core.Artifact{}, errors.New("artifact publish: invalid server acknowledgement")
	}
	digest := sha256.Sum256(data)
	if result.Artifact.SHA256 != hex.EncodeToString(digest[:]) || result.Artifact.Size != int64(len(data)) || result.Artifact.Name != name {
		return core.Artifact{}, errors.New("artifact publish: server checksum or metadata mismatch")
	}
	return result.Artifact, nil
}

func PublishedMediaType(name string) string {
	media := mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
	if media == "" {
		return "application/octet-stream"
	}
	return media
}
