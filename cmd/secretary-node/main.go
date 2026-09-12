package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"
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
	dataDir := flag.String("data-dir", defaultNodeDataDir(), "directory for Node identity and durable local state")
	serverURL := flag.String("server", envOr("SECRETARY_NODE_SERVER", "http://127.0.0.1:8081"), "Secretary server URL used for first pairing")
	pairingToken := flag.String("pair-token", os.Getenv("SECRETARY_NODE_PAIRING_TOKEN"), "one-time/owner-approved Node pairing token")
	nodeName := flag.String("name", envOr("SECRETARY_NODE_NAME", defaultNodeName()), "stable Node reference requested during pairing")
	capacity := flag.Int("capacity", 1, "maximum advertised concurrent Worker capacity")
	includeOpenCode := flag.Bool("include-opencode", false, "include OpenCode compatibility inventory probe")
	flag.Parse()

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

	runtime := configuredNodeRuntime(*dataDir)
	discovery := node.HarnessDiscovery{Node: identity.Node, Runner: node.ExecCommandRunner{}, IncludeOpenCode: *includeOpenCode}
	daemon := &node.Daemon{
		Identity:  identity,
		Store:     store,
		Runtime:   runtime,
		Inventory: discovery,
		Capacity:  *capacity,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log.Printf("secretary-node %s connecting outbound to %s", identity.Node, identity.ConnectURL)
	if err := daemon.Run(ctx); err != nil {
		log.Fatalf("run Node: %v", err)
	}
}

func configuredNodeRuntime(dataDir string) node.Runtime {
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
		DefaultHarness: "fx",
		ACP:            node.ACPRuntime{Command: codexCommand, Arguments: codexArgs, RawLogDir: logDir, RawLogMaxBytes: 10 << 20, RawLogFiles: 5},
		Claude:         node.ClaudeCodeRuntime{Command: claudeCommand, Arguments: claudeArgs, RawLogDir: logDir, RawLogMaxBytes: 10 << 20, RawLogFiles: 5},
		FX:             node.FXRuntime{ACPRuntime: node.ACPRuntime{Command: fxCommand, Arguments: fxArgs, RawLogDir: logDir, RawLogMaxBytes: 10 << 20, RawLogFiles: 5}},
		OpenCode:       node.OpenCodeRuntime{Command: openCodeCommand, Arguments: openCodeArgs, RawLogDir: logDir, RawLogMaxBytes: 10 << 20, RawLogFiles: 5},
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
