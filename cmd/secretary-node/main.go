package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

var invalidNodeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func main() {
	_ = syscall.Umask(0o077)
	configPath := flag.String("config", "", "non-secret Node deployment config JSON")
	dataDir := flag.String("data-dir", defaultNodeDataDir(), "directory for Node identity and durable local state")
	serverURL := flag.String("server", envOr("SECRETARY_NODE_SERVER", "http://127.0.0.1:8081"), "Secretary server URL used for first pairing")
	pairingToken := flag.String("pair-token", os.Getenv("SECRETARY_NODE_PAIRING_TOKEN"), "one-time/owner-approved Node pairing token")
	nodeName := flag.String("name", envOr("SECRETARY_NODE_NAME", defaultNodeName()), "stable Node reference requested during pairing")
	capacity := flag.Int("capacity", 1, "maximum advertised concurrent Worker capacity")
	includeOpenCode := flag.Bool("include-opencode", true, "include the OpenCode ACP inventory probe")
	selectOpenCodeStore := flag.Bool("select-opencode-store", false, "initialize the selected shared, private, or legacy OpenCode store and exit")
	standaloneSelection := flag.Bool("standalone", false, "select an independent host-local OpenCode store")
	printStoreRecord := flag.Bool("print-opencode-store", false, "print the validated Node store and deployment config record")
	flag.Parse()
	if *printStoreRecord && !*selectOpenCodeStore {
		log.Fatal("--print-opencode-store requires --select-opencode-store")
	}
	if *selectOpenCodeStore {
		selectedDataDir, standalone, includeOpenCode := *dataDir, *standaloneSelection, *includeOpenCode
		if strings.TrimSpace(*configPath) != "" {
			deployment, err := node.LoadDeploymentConfig(*configPath)
			if err != nil {
				log.Fatalf("load Node deployment config: %v", err)
			}
			selectedDataDir, standalone, includeOpenCode = deployment.DataDir, deployment.Standalone, deployment.IncludeOpenCode
		}
		selectedStore, err := selectNodeNativeStore(selectedDataDir, standalone)
		if err != nil {
			log.Fatalf("select OpenCode native store: %v", err)
		}
		if selectedStore.Mode == node.OpenCodeNativeStoreModeLegacy {
			log.Print("OpenCode legacy native store preserved; owner-approved migration is required before switching stores")
		} else if selectedStore.Mode == node.OpenCodeNativeStoreModeShared {
			log.Print("shared Secretary and local Node OpenCode store selected")
		} else {
			log.Print("private Node OpenCode store selected")
		}
		if *printStoreRecord {
			record, err := node.EncodeNodeOpenCodeNativeStoreRecord(selectedStore, selectedDataDir, standalone, includeOpenCode)
			if err != nil {
				log.Fatalf("encode selected Node OpenCode native store: %v", err)
			}
			if _, err := fmt.Fprintln(os.Stdout, record); err != nil {
				log.Fatalf("print selected Node OpenCode native store: %v", err)
			}
		}
		return
	}

	var deployment node.DeploymentConfig
	if strings.TrimSpace(*configPath) != "" {
		loaded, err := node.LoadDeploymentConfig(*configPath)
		if err != nil {
			log.Fatalf("load Node deployment config: %v", err)
		}
		deployment = loaded
		*dataDir, *serverURL, *nodeName, *capacity, *includeOpenCode = loaded.DataDir, loaded.ServerURL, string(loaded.Node), loaded.Capacity, loaded.IncludeOpenCode
		if *capacity == 0 {
			*capacity = 1
		}
	} else {
		deployment = node.DeploymentConfig{ServerURL: *serverURL, Node: core.NodeReference(normalizeNodeName(*nodeName)), DataDir: *dataDir, Capacity: *capacity, IncludeOpenCode: *includeOpenCode}
	}
	if err := deployment.Validate(); err != nil {
		log.Fatalf("invalid Node deployment: %v", err)
	}
	workspaces, err := deployment.ProtocolWorkspaces()
	if err != nil {
		log.Fatalf("load Node workspace mappings: %v", err)
	}

	selectedStore, err := selectNodeNativeStore(*dataDir, deployment.Standalone)
	if err != nil {
		log.Fatalf("select OpenCode native store: %v", err)
	}
	if selectedStore.Mode == node.OpenCodeNativeStoreModeLegacy {
		log.Printf("OpenCode legacy native store preserved; owner-approved migration is required before switching stores")
	}
	if err := os.MkdirAll(*dataDir, 0o700); err != nil {
		log.Fatalf("create Node data directory: %v", err)
	}
	identityPath := filepath.Join(*dataDir, "identity.json")
	identity, err := node.LoadNodeIdentity(identityPath)
	if errors.Is(err, os.ErrNotExist) {
		if strings.TrimSpace(*pairingToken) == "" {
			log.Fatal("Node is not paired; --pair-token or SECRETARY_NODE_PAIRING_TOKEN is required for first enrollment")
		}
		identity, err = node.EnrollNode(context.Background(), nil, *serverURL, *pairingToken, core.NodeReference(normalizeNodeName(*nodeName)))
		if err != nil {
			log.Fatalf("pair Node: %v", err)
		}
		if err := node.SaveNodeIdentity(identityPath, identity); err != nil {
			log.Fatalf("save Node identity: %v", err)
		}
		log.Printf("paired Node %s", identity.Node)
	} else if err != nil {
		log.Fatalf("load Node identity: %v", err)
	}

	store, err := node.OpenLocalStore(filepath.Join(*dataDir, "node-state.json"))
	if err != nil {
		log.Fatalf("open Node local state: %v", err)
	}
	defer store.Close()

	runtime := configuredNodeRuntimeWithStore(*dataDir, selectedStore)
	discovery := configuredNodeDiscovery(identity.Node, *includeOpenCode, selectedStore)
	daemon := &node.Daemon{
		Identity:   identity,
		Store:      store,
		Runtime:    runtime,
		Inventory:  discovery,
		Workspaces: workspaces,
		Capacity:   *capacity,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log.Printf("secretary-node %s connecting outbound to %s", identity.Node, identity.ConnectURL)
	if err := daemon.Run(ctx); err != nil {
		log.Fatalf("run Node: %v", err)
	}
}

func selectNodeNativeStore(dataDir string, standalone bool) (node.OpenCodeNativeStore, error) {
	_, stateErr := os.Stat(filepath.Join(dataDir, "node-state.json"))
	if stateErr != nil && !errors.Is(stateErr, os.ErrNotExist) {
		return node.OpenCodeNativeStore{}, errors.New("inspect Node local state before selecting OpenCode store")
	}
	_, configErr := os.Stat(filepath.Join(filepath.Dir(filepath.Clean(dataDir)), "config.json"))
	if configErr != nil && !errors.Is(configErr, os.ErrNotExist) {
		return node.OpenCodeNativeStore{}, errors.New("inspect Node deployment config before selecting OpenCode store")
	}
	legacyDataHome := strings.TrimSpace(os.Getenv("XDG_DATA_HOME"))
	sharedDataHome := ""
	if !standalone {
		absoluteDataDir, absErr := filepath.Abs(dataDir)
		if absErr != nil {
			return node.OpenCodeNativeStore{}, errors.New("opencode: Node data directory is unavailable")
		}
		serverRoot := filepath.Dir(filepath.Dir(absoluteDataDir))
		if filepath.Clean(absoluteDataDir) == filepath.Join(serverRoot, "node", "data") {
			sharedDataHome = node.OpenCodeNativeDataHome(serverRoot)
		}
	}
	return node.SelectOpenCodeNativeStoreWithOptions(dataDir, node.OpenCodeNativeStoreOptions{
		HasExistingState: stateErr == nil || configErr == nil,
		LegacyDataHome:   legacyDataHome,
		SharedDataHome:   sharedDataHome,
		Standalone:       standalone,
	})
}

func installedHarnesses() map[core.HarnessKind]string {
	commands := map[core.HarnessKind]string{
		core.HarnessFX: "fx", core.HarnessClaudeCode: "claude", core.HarnessCodex: "codex", core.HarnessOpenCode: envOr("SECRETARY_OPENCODE_COMMAND", "opencode"),
	}
	resolved := make(map[core.HarnessKind]string, len(commands))
	for kind, command := range commands {
		if path, err := exec.LookPath(command); err == nil {
			resolved[kind] = path
		} else if kind == core.HarnessOpenCode && strings.TrimSpace(os.Getenv("SECRETARY_OPENCODE_COMMAND")) != "" {
			// Keep an unavailable explicit binary unavailable in discovery too.
			resolved[kind] = command
		}
	}
	return resolved
}

func configuredNodeRuntime(dataDir string) node.Runtime {
	return configuredNodeRuntimeWithStore(dataDir, node.OpenCodeNativeStore{DataHome: node.OpenCodeNativeDataHome(dataDir)})
}

func configuredNodeDiscovery(identity core.NodeReference, includeOpenCode bool, store node.OpenCodeNativeStore) node.HarnessDiscovery {
	return node.HarnessDiscovery{
		Node: identity, Runner: node.ExecCommandRunner{}, IncludeOpenCode: includeOpenCode,
		OpenCodeDataHome: store.DataHome, LegacyOpenCodeDataHome: store.Mode == node.OpenCodeNativeStoreModeLegacy,
		BinaryOverrides: installedHarnesses(),
	}
}

func configuredNodeRuntimeWithStore(dataDir string, store node.OpenCodeNativeStore) node.Runtime {
	logDir := filepath.Join(dataDir, "logs", "acp")
	codexCommand := envOr("SECRETARY_ACP_COMMAND", "codex-acp")
	codexArgs := strings.Fields(os.Getenv("SECRETARY_ACP_ARGS"))
	claudeCommand := envOr("SECRETARY_CLAUDE_COMMAND", "claude")
	claudeArgs := strings.Fields(os.Getenv("SECRETARY_CLAUDE_ARGS"))
	fxCommand := envOr("SECRETARY_FX_COMMAND", "fx")
	fxArgs := strings.Fields(os.Getenv("SECRETARY_FX_ARGS"))
	if len(fxArgs) == 0 {
		fxArgs = []string{"acp"}
	}
	openCodeCommand := envOr("SECRETARY_OPENCODE_COMMAND", "opencode")
	openCodeArgs := strings.Fields(os.Getenv("SECRETARY_OPENCODE_ARGS"))
	if len(openCodeArgs) == 0 {
		openCodeArgs = []string{"acp"}
	}
	return node.RuntimeRouter{
		DefaultHarness: "opencode",
		ACP:            node.ACPRuntime{Command: codexCommand, Arguments: codexArgs, RawLogDir: logDir, RawLogMaxBytes: 10 << 20, RawLogFiles: 5},
		Claude:         node.ClaudeCodeRuntime{Command: claudeCommand, Arguments: claudeArgs, RawLogDir: logDir, RawLogMaxBytes: 10 << 20, RawLogFiles: 5},
		FX:             node.FXRuntime{ACPRuntime: node.ACPRuntime{Command: fxCommand, Arguments: fxArgs, RawLogDir: logDir, RawLogMaxBytes: 10 << 20, RawLogFiles: 5}},
		OpenCode:       node.OpenCodeRuntime{Command: openCodeCommand, Arguments: openCodeArgs, DataHome: store.DataHome, LegacyDataHome: store.Mode == node.OpenCodeNativeStoreModeLegacy, RawLogDir: logDir, RawLogMaxBytes: 10 << 20, RawLogFiles: 5},
	}
}

func defaultNodeDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ".secretary-node"
	}
	return filepath.Join(home, ".secretary", "node")
}

func defaultNodeName() string {
	hostname, err := os.Hostname()
	if err != nil || strings.TrimSpace(hostname) == "" {
		return "node"
	}
	return normalizeNodeName(hostname)
}

func normalizeNodeName(value string) string {
	value = strings.TrimSpace(value)
	value = invalidNodeName.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-._")
	if value == "" {
		value = "node"
	}
	if len(value) > 64 {
		value = value[:64]
	}
	return value
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
