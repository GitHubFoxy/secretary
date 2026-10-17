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
	Client *http.Client
}

func (n *ExecutionNode) SetArtifactPublisher(identity NodeIdentity, command string) error {
	if err := identity.Validate(); err != nil {
		return err
	}
	if command == "" {
		return errors.New("node artifact: publisher command required")
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	copy := identity
	n.artifactPublisher = &copy
	n.artifactPublisherCommand = command
	return nil
}

func (n *ExecutionNode) workerArtifactMCPServers(workerRef, attemptID, workspace string) []MCPServer {
	servers := n.workerMCPServers()
	n.mu.Lock()
	identity := n.artifactPublisher
	binary := n.artifactPublisherCommand
	n.mu.Unlock()
	if identity == nil || workerRef == "" || attemptID == "" || workspace == "" {
		return servers
	}
	auth, err := identity.Authenticator()
	if err != nil {
		return servers
	}
	return append(servers, MCPServer{
		Name: "secretary_artifacts", Command: binary, Args: []string{"--artifact-mcp"},
		Env: []MCPEnv{
			{Name: "SECRETARY_ARTIFACT_SERVER", Value: identity.ConnectURL},
			{Name: "SECRETARY_ARTIFACT_NODE", Value: string(identity.Node)},
			{Name: "SECRETARY_ARTIFACT_WORKER", Value: workerRef},
			{Name: "SECRETARY_ARTIFACT_ATTEMPT", Value: attemptID},
			{Name: "SECRETARY_ARTIFACT_WORKSPACE", Value: workspace},
			{Name: "SECRETARY_ARTIFACT_CAPABILITY", Value: auth.SignPublishCapability(identity.Node, workerRef, attemptID)},
		},
	})
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

func (p ArtifactPublisher) Publish(ctx context.Context, relativePath, name string) (core.Artifact, error) {
	if len(relativePath) == 0 || len(relativePath) > 1024 || !filepath.IsLocal(relativePath) || filepath.IsAbs(relativePath) ||
		strings.Contains(relativePath, "\\") && os.PathSeparator != '\\' {
		return core.Artifact{}, errors.New("artifact publish: path must be relative to the Worker workspace")
	}
	for _, component := range strings.FieldsFunc(filepath.ToSlash(relativePath), func(r rune) bool { return r == '/' }) {
		if component == ".." || strings.HasPrefix(component, ".") {
			return core.Artifact{}, errors.New("artifact publish: hidden or parent paths are forbidden")
		}
	}
	if name == "" {
		name = filepath.Base(relativePath)
	}
	if !ValidPublishedName(name) {
		return core.Artifact{}, errors.New("artifact publish: unsupported or unsafe output name")
	}
	if p.Node == "" || p.WorkerRef == "" || p.AttemptID == "" || p.Capability == "" || p.Workspace == "" {
		return core.Artifact{}, errors.New("artifact publish: Worker binding is unavailable")
	}
	root, err := filepath.EvalSymlinks(p.Workspace)
	if err != nil {
		return core.Artifact{}, errors.New("artifact publish: Worker workspace is unavailable")
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return core.Artifact{}, err
	}
	target := filepath.Join(root, relativePath)
	rel, err := filepath.Rel(root, target)
	if err != nil || !filepath.IsLocal(rel) || rel == "." {
		return core.Artifact{}, errors.New("artifact publish: path escapes Worker workspace")
	}
	current := root
	for _, component := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			return core.Artifact{}, errors.New("artifact publish: output file not found")
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return core.Artifact{}, errors.New("artifact publish: symlink paths are forbidden")
		}
	}
	file, err := os.Open(target)
	if err != nil {
		return core.Artifact{}, errors.New("artifact publish: output file not readable")
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
	sum := sha256.Sum256([]byte(p.AttemptID + "\\x00" + rel))
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
	request.Header.Set("X-Artifact-Publish-Token", p.Capability)
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
