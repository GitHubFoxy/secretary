package node

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/beruseruko/secretary/internal/core"
)

type NodeIdentity struct {
	Node       core.NodeReference `json:"node"`
	Credential string             `json:"credential"`
	ConnectURL string             `json:"connect_url"`
}

func (i NodeIdentity) Validate() error {
	if !validNodeReference(i.Node) {
		return errors.New("node: invalid persisted Node reference")
	}
	secret, err := base64.RawURLEncoding.DecodeString(i.Credential)
	if err != nil || len(secret) < 32 {
		return errors.New("node: invalid persisted Node credential")
	}
	u, err := url.Parse(i.ConnectURL)
	if err != nil || (u.Scheme != "ws" && u.Scheme != "wss") || strings.TrimSpace(u.Host) == "" {
		return errors.New("node: invalid persisted Node connect URL")
	}
	return nil
}

func (i NodeIdentity) Authenticator() (Authenticator, error) {
	if err := i.Validate(); err != nil {
		return Authenticator{}, err
	}
	secret, _ := base64.RawURLEncoding.DecodeString(i.Credential)
	return NewAuthenticator(secret), nil
}

func LoadNodeIdentity(path string) (NodeIdentity, error) {
	encoded, err := os.ReadFile(path)
	if err != nil {
		return NodeIdentity{}, err
	}
	var identity NodeIdentity
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&identity); err != nil {
		return NodeIdentity{}, fmt.Errorf("node: decode identity: %w", err)
	}
	if err := identity.Validate(); err != nil {
		return NodeIdentity{}, err
	}
	return identity, nil
}

func SaveNodeIdentity(path string, identity NodeIdentity) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("node: identity path is required")
	}
	if err := identity.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(identity, "", "  ")
	if err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, encoded, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(temporary, 0o600); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return os.Chmod(path, 0o600)
}

func EnrollNode(ctx context.Context, client *http.Client, serverURL, pairingToken string, requestedNode core.NodeReference) (NodeIdentity, error) {
	if client == nil {
		client = http.DefaultClient
	}
	if strings.TrimSpace(serverURL) == "" || strings.TrimSpace(pairingToken) == "" {
		return NodeIdentity{}, errors.New("node: server URL and pairing token are required")
	}
	base, err := url.Parse(serverURL)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || strings.TrimSpace(base.Host) == "" {
		return NodeIdentity{}, errors.New("node: pairing server URL must be http or https")
	}
	base.Path = strings.TrimSuffix(base.Path, "/") + "/v1/nodes/pair"
	base.RawQuery = ""
	base.Fragment = ""
	payload, err := json.Marshal(EnrollmentRequest{PairingToken: pairingToken, Node: requestedNode})
	if err != nil {
		return NodeIdentity{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, base.String(), bytes.NewReader(payload))
	if err != nil {
		return NodeIdentity{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return NodeIdentity{}, fmt.Errorf("node: pair with server: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		return NodeIdentity{}, fmt.Errorf("node: pairing failed with HTTP %d", response.StatusCode)
	}
	var enrolled EnrollmentResponse
	decoder := json.NewDecoder(response.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&enrolled); err != nil {
		return NodeIdentity{}, fmt.Errorf("node: decode pairing response: %w", err)
	}
	identity := NodeIdentity{Node: enrolled.Node, Credential: enrolled.Credential, ConnectURL: enrolled.ConnectURL}
	if err := identity.Validate(); err != nil {
		return NodeIdentity{}, err
	}
	return identity, nil
}
