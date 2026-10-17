package node

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

var artifactReference = regexp.MustCompile(`artifact://art_[a-f0-9]{32}\b`)
var artifactIDPattern = regexp.MustCompile(`^art_[a-f0-9]{32}$`)

type ArtifactPayload struct {
	ID     string
	Name   string
	SHA256 string
	Data   []byte
}

type ArtifactFetcher func(context.Context, string) (ArtifactPayload, error)

func ArtifactIDs(text string) []string {
	matches := artifactReference.FindAllString(text, 32)
	seen := make(map[string]bool, len(matches))
	ids := make([]string, 0, len(matches))
	for _, value := range matches {
		id := strings.TrimPrefix(value, "artifact://")
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

func ValidArtifactID(id string) bool { return artifactIDPattern.MatchString(id) }

func (a Authenticator) SignArtifactRequest(nodeRef core.NodeReference, artifactID string, epoch int64) string {
	mac := hmac.New(sha256.New, a.secret)
	_, _ = fmt.Fprintf(mac, "artifact-get\n%s\n%s\n%d", nodeRef, artifactID, epoch)
	return hex.EncodeToString(mac.Sum(nil))
}

func (a Authenticator) VerifyArtifactRequest(nodeRef core.NodeReference, artifactID string, epoch int64, signature string, now time.Time) error {
	if !ValidArtifactID(artifactID) || epoch < now.Unix()-120 || epoch > now.Unix()+120 || len(a.secret) < 32 {
		return errors.New("node artifact: invalid request")
	}
	provided, err := hex.DecodeString(signature)
	if err != nil || !hmac.Equal(provided, mustDecodeArtifactSignature(a.SignArtifactRequest(nodeRef, artifactID, epoch))) {
		return errors.New("node artifact: invalid authorization")
	}
	return nil
}

func mustDecodeArtifactSignature(value string) []byte {
	decoded, _ := hex.DecodeString(value)
	return decoded
}

func (i NodeIdentity) FetchArtifact(ctx context.Context, id string) (ArtifactPayload, error) {
	if !ValidArtifactID(id) {
		return ArtifactPayload{}, errors.New("node artifact: invalid identifier")
	}
	auth, err := i.Authenticator()
	if err != nil {
		return ArtifactPayload{}, err
	}
	u, err := url.Parse(i.ConnectURL)
	if err != nil || !strings.HasSuffix(u.Path, "/v1/nodes/connect") {
		return ArtifactPayload{}, errors.New("node artifact: invalid server endpoint")
	}
	if u.Scheme == "wss" {
		u.Scheme = "https"
	} else {
		u.Scheme = "http"
	}
	u.Path = strings.TrimSuffix(u.Path, "/v1/nodes/connect") + "/v1/artifacts/" + id
	u.RawQuery, u.Fragment = "", ""
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return ArtifactPayload{}, err
	}
	now := time.Now().Unix()
	req.Header.Set("X-Secretary-Node", string(i.Node))
	req.Header.Set("X-Artifact-Timestamp", strconv.FormatInt(now, 10))
	req.Header.Set("X-Artifact-Signature", auth.SignArtifactRequest(i.Node, id, now))
	client := &http.Client{Timeout: 20 * time.Second}
	response, err := client.Do(req)
	if err != nil {
		return ArtifactPayload{}, fmt.Errorf("node artifact: fetch: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ArtifactPayload{}, fmt.Errorf("node artifact: HTTP %d", response.StatusCode)
	}
	if response.ContentLength > core.MaxArtifactBytes {
		return ArtifactPayload{}, errors.New("node artifact: response exceeds size limit")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, core.MaxArtifactBytes+1))
	if err != nil {
		return ArtifactPayload{}, err
	}
	if len(data) == 0 || len(data) > core.MaxArtifactBytes {
		return ArtifactPayload{}, errors.New("node artifact: invalid byte count")
	}
	checksum := sha256.Sum256(data)
	expected := response.Header.Get("X-Artifact-SHA256")
	if expected == "" || !hmac.Equal([]byte(expected), []byte(hex.EncodeToString(checksum[:]))) {
		return ArtifactPayload{}, errors.New("node artifact: checksum mismatch")
	}
	name, err := base64.RawURLEncoding.DecodeString(response.Header.Get("X-Artifact-Name"))
	if err != nil || len(name) == 0 || len(name) > 180 {
		return ArtifactPayload{}, errors.New("node artifact: invalid filename metadata")
	}
	return ArtifactPayload{ID: id, Name: string(name), SHA256: expected, Data: data}, nil
}
