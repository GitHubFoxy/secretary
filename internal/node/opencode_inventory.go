package node

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

// OpenCode's plaintext `models` command contains IDs only. V2's native model
// API also exposes enabled state and provider-owned variant settings. Observe
// those in the same isolated environment/provider store as the ACP runtime;
// never manufacture reasoning support from a configured request or variant ID.
type OpenCodeModelObserver interface {
	ObserveOpenCodeModels(context.Context, string) ([]core.ObservedModelID, []core.ObservedReasoningLevel, error)
}

func (r ExecCommandRunner) ObserveOpenCodeModels(ctx context.Context, command string) ([]core.ObservedModelID, []core.ObservedReasoningLevel, error) {
	return observeOpenCodeModels(ctx, command, r.OpenCodeDataHome, r.LegacyOpenCodeDataHome, nil)
}

func observeOpenCodeModels(ctx context.Context, command, dataHome string, legacyDataHome bool, observe func(openCodeModelCatalog)) ([]core.ObservedModelID, []core.ObservedReasoningLevel, error) {
	workspace, err := os.MkdirTemp("", "secretary-opencode-inventory-")
	if err != nil {
		return nil, nil, fmt.Errorf("opencode inventory: private directory unavailable")
	}
	defer os.RemoveAll(workspace)
	config := `{"permissions":[{"action":"*","resource":"*","effect":"deny"}]}`
	configPath := filepath.Join(workspace, "opencode.json")
	if os.WriteFile(configPath, []byte(config), 0o600) != nil {
		return nil, nil, fmt.Errorf("opencode inventory: private config unavailable")
	}
	env, err := openCodeEnvironment(workspace, configPath, config, nil, dataHome, legacyDataHome)
	if err != nil {
		return nil, nil, fmt.Errorf("opencode inventory: isolated environment unavailable")
	}
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, nil, fmt.Errorf("opencode inventory: private transport unavailable")
	}
	// Ephemeral control credential, not a provider credential. The stdio server
	// consumes it before tool environment setup; no auth material is copied.
	password := hex.EncodeToString(nonce[:])
	env = append(env, "OPENCODE_PASSWORD="+password)
	processCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(processCtx, command, "serve", "--stdio", "--port", "0")
	cmd.Env, cmd.Dir = env, workspace
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("opencode inventory: private transport unavailable")
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("opencode inventory: private transport unavailable")
	}
	if cmd.Start() != nil {
		return nil, nil, fmt.Errorf("opencode inventory: private server unavailable")
	}
	defer func() { _ = stdin.Close(); cancel(); _ = cmd.Wait() }()
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() {
		return nil, nil, fmt.Errorf("opencode inventory: private server readiness unavailable")
	}
	var ready struct {
		URL string `json:"url"`
	}
	if json.Unmarshal(scanner.Bytes(), &ready) != nil {
		return nil, nil, fmt.Errorf("opencode inventory: invalid private server readiness")
	}
	endpoint, err := url.Parse(ready.URL)
	if err != nil || endpoint.Scheme != "http" || endpoint.Hostname() != "127.0.0.1" || endpoint.User != nil {
		return nil, nil, fmt.Errorf("opencode inventory: unexpected private server endpoint")
	}
	go func() { _, _ = io.Copy(io.Discard, stdout) }()
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	get := func(path string, target any) error {
		u := *endpoint
		u.Path = path
		query := u.Query()
		query.Set("location[directory]", workspace)
		u.RawQuery = query.Encode()
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return fmt.Errorf("opencode inventory: request unavailable")
		}
		request.SetBasicAuth("opencode", password)
		response, err := client.Do(request)
		if err != nil {
			return fmt.Errorf("opencode inventory: native metadata unavailable")
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(response.Body, 16<<20)).Decode(target) != nil {
			return fmt.Errorf("opencode inventory: invalid native metadata")
		}
		return nil
	}
	// Plugin inventory is published after activation. Catalog IDs alone can be
	// visible earlier (the same race that affects ACP load/Resume).
	for {
		var plugins struct {
			Data []struct {
				ID    string `json:"id"`
				State struct {
					Status string `json:"status"`
				} `json:"state"`
			} `json:"data"`
		}
		if err := get("/api/plugin", &plugins); err != nil {
			return nil, nil, err
		}
		configured := false
		for _, plugin := range plugins.Data {
			if plugin.State.Status != "active" {
				return nil, nil, fmt.Errorf("opencode inventory: native plugin not active")
			}
			configured = configured || plugin.ID == "opencode.config.agent"
		}
		if configured {
			break
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return nil, nil, fmt.Errorf("opencode inventory: native activation timed out")
		}
	}
	var catalog openCodeModelCatalog
	if err := get("/api/model", &catalog); err != nil {
		return nil, nil, err
	}
	models, levels := catalog.observed()
	if len(models) == 0 {
		return nil, nil, fmt.Errorf("opencode inventory: no enabled models")
	}
	if observe != nil {
		observe(catalog)
	}
	return models, levels, nil
}

type openCodeModelCatalog struct {
	Data []struct {
		ID         string `json:"id"`
		ProviderID string `json:"providerID"`
		Enabled    bool   `json:"enabled"`
		Variants   []struct {
			Settings struct {
				ReasoningEffort string `json:"reasoningEffort"`
			} `json:"settings"`
		} `json:"variants"`
	} `json:"data"`
}

func (catalog openCodeModelCatalog) supportsEffort(id, effort string) bool {
	for _, model := range catalog.Data {
		if !model.Enabled || model.ProviderID+"/"+model.ID != id {
			continue
		}
		for _, variant := range model.Variants {
			if variant.Settings.ReasoningEffort == effort {
				return true
			}
		}
	}
	return false
}

func (catalog openCodeModelCatalog) observed() ([]core.ObservedModelID, []core.ObservedReasoningLevel) {
	models := map[core.ObservedModelID]bool{}
	levels := map[core.ObservedReasoningLevel]bool{}
	for _, model := range catalog.Data {
		if !model.Enabled || model.ID == "" || model.ProviderID == "" {
			continue
		}
		models[core.ObservedModelID(model.ProviderID+"/"+model.ID)] = true
		for _, variant := range model.Variants {
			if variant.Settings.ReasoningEffort != "" {
				levels[core.ObservedReasoningLevel(variant.Settings.ReasoningEffort)] = true
			}
		}
	}
	ids := make([]core.ObservedModelID, 0, len(models))
	for id := range models {
		ids = append(ids, id)
	}
	efforts := make([]core.ObservedReasoningLevel, 0, len(levels))
	for level := range levels {
		efforts = append(efforts, level)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	sort.Slice(efforts, func(i, j int) bool { return efforts[i] < efforts[j] })
	return ids, efforts
}
