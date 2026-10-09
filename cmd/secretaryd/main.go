package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/beruseruko/secretary/internal/app"
	"github.com/beruseruko/secretary/internal/config"
	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/ctl"
	"github.com/beruseruko/secretary/internal/node"
	secretaryruntime "github.com/beruseruko/secretary/internal/secretary"
	"github.com/beruseruko/secretary/internal/webapi"
	webclient "github.com/beruseruko/secretary/web"
)

func main() {
	_ = syscall.Umask(0o077)
	dataDir := flag.String("data-dir", defaultDataDir(), "directory for Secretary durable state")
	listen := flag.String("listen", "127.0.0.1:8081", "web listener address")
	configPath := flag.String("config", "", "path to Secretary config.toml")
	debug := flag.Bool("debug", false, "enable the local-only Control Room")
	selectNativeStores := flag.Bool("select-opencode-stores", false, "initialize the selected shared, private, or legacy OpenCode stores and exit")
	printNativeStores := flag.Bool("print-opencode-stores", false, "print validated Secretary and local Node store mode/path records")
	selectSharedNativeStore := flag.Bool("select-shared-opencode-store", false, "explicitly select one private OpenCode store for Secretary and the co-located Node, then exit")
	checkOpenCodeAuth := flag.Bool("check-opencode-auth", false, "check stored OpenCode credentials in the selected Secretary store and exit")
	opencodeCommand := flag.String("opencode-command", "opencode", "OpenCode executable used for the selected-store auth check")
	flag.Parse()
	if *checkOpenCodeAuth {
		if *selectNativeStores || *printNativeStores || *selectSharedNativeStore {
			log.Fatal("--check-opencode-auth cannot be combined with OpenCode store selection")
		}
		selection, err := selectRuntimeNativeStoreSelection(*dataDir)
		if err != nil || (selection.Secretary.Mode != node.OpenCodeNativeStoreModeIsolated && selection.Secretary.Mode != node.OpenCodeNativeStoreModeShared) {
			os.Exit(1)
		}
		if err := node.CheckOpenCodeAuthentication(context.Background(), *opencodeCommand, selection.Secretary.DataHome); err != nil {
			os.Exit(1)
		}
		return
	}
	if *selectSharedNativeStore {
		if *printNativeStores {
			log.Fatal("--print-opencode-stores requires --select-opencode-stores")
		}
		secretaryStore, workerStore, err := node.SelectSharedOpenCodeNativeStores(*dataDir, filepath.Join(*dataDir, "node", "data"))
		if err != nil {
			log.Fatalf("select shared OpenCode native store: %v", err)
		}
		if secretaryStore.DataHome != workerStore.DataHome {
			log.Fatal("select shared OpenCode native store: role selections do not agree")
		}
		log.Print("shared Secretary and local Worker Node OpenCode store selected")
		return
	}
	var selection runtimeNativeStoreSelection
	var err error
	if *selectNativeStores {
		selection, err = selectRuntimeNativeStoreSelection(*dataDir)
		if err != nil {
			log.Fatalf("select OpenCode native stores: %v", err)
		}
	}
	secretaryStore, workerStore := selection.Secretary, selection.Worker
	if secretaryStore.Mode == node.OpenCodeNativeStoreModeLegacy {
		log.Print("Secretary OpenCode legacy native store preserved; owner-approved migration is required before switching stores")
	}
	if workerStore.Mode == node.OpenCodeNativeStoreModeLegacy {
		log.Print("local Worker Node OpenCode legacy store preserved; owner-approved migration is required before switching stores")
	}
	if *printNativeStores && !*selectNativeStores {
		log.Fatal("--print-opencode-stores requires --select-opencode-stores")
	}
	if *selectNativeStores {
		if secretaryStore.Mode != node.OpenCodeNativeStoreModeLegacy && workerStore.Mode != node.OpenCodeNativeStoreModeLegacy {
			if secretaryStore.Mode == node.OpenCodeNativeStoreModeShared && workerStore.Mode == node.OpenCodeNativeStoreModeShared {
				log.Print("shared Secretary and local Worker Node OpenCode store selected")
			} else {
				log.Print("private Secretary and local Worker Node OpenCode stores selected")
			}
		}
		if *printNativeStores {
			record, err := node.EncodeSecretaryOpenCodeNativeStoreRecord(secretaryStore, workerStore, selection.NodeDataDir, selection.NodeStandalone)
			if err != nil {
				log.Fatalf("encode selected OpenCode native stores: %v", err)
			}
			if _, err := fmt.Fprintln(os.Stdout, record); err != nil {
				log.Fatalf("print selected OpenCode native stores: %v", err)
			}
		}
		return
	}
	if err := validateListen(*listen); err != nil {
		log.Fatal(err)
	}
	if *configPath == "" {
		*configPath = filepath.Join(*dataDir, "config.toml")
	}
	bootstrapToken := os.Getenv("SECRETARY_BOOTSTRAP_TOKEN")
	if bootstrapToken == "" {
		log.Fatal("SECRETARY_BOOTSTRAP_TOKEN is required")
	}
	if err := os.MkdirAll(*dataDir, 0o700); err != nil {
		log.Fatalf("create data directory: %v", err)
	}
	profiles, err := config.Open(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if profiles.Snapshot().Config.EffectiveSecretaryPolicy().Harness == "opencode" || profiles.Snapshot().Config.EffectiveWorkerPolicy().DefaultHarness == "opencode" {
		selection, err = selectRuntimeNativeStoreSelection(*dataDir)
		if err != nil {
			log.Fatalf("select OpenCode native stores: %v", err)
		}
		secretaryStore, workerStore = selection.Secretary, selection.Worker
	}
	userDocument, err := config.OpenUserDocument(*dataDir)
	if err != nil {
		log.Printf("load user.md: %v; keeping the last valid document snapshot", err)
		log.Printf("loaded config version %s", profiles.Snapshot().Version)
	} else {
		log.Printf("loaded config version %s and user.md revision %d", profiles.Snapshot().Version, userDocument.Snapshot().Revision)
	}
	store, err := core.Open(context.Background(), filepath.Join(*dataDir, "secretary.db"))
	if err != nil {
		log.Fatalf("open Secretary state: %v", err)
	}
	defer store.Close()
	compiledConfig, err := json.Marshal(profiles.Snapshot())
	if err != nil {
		log.Fatalf("encode config version: %v", err)
	}
	if err := store.RecordConfigVersion(context.Background(), profiles.Snapshot().Version, profiles.Path(), string(compiledConfig)); err != nil {
		log.Fatalf("record config version: %v", err)
	}
	profiles.SetChangeRecorder(func(previous, next config.Snapshot) error {
		compiled, err := json.Marshal(next)
		if err != nil {
			return err
		}
		diff, err := json.Marshal(config.Diff(previous, next))
		if err != nil {
			return err
		}
		return store.RecordConfigChange(context.Background(), previous.Version, next.Version, profiles.Path(), string(compiled), string(diff))
	})
	if err := store.RecoverInterrupted(context.Background()); err != nil {
		log.Fatalf("recover interrupted Attempts: %v", err)
	}
	if _, err := store.RecoverApprovalResolutionCommands(context.Background()); err != nil {
		log.Fatalf("recover Approval resolution handoffs: %v", err)
	}
	web, err := webapi.New(context.Background(), store, bootstrapToken)
	if err != nil {
		log.Fatalf("initialize web API: %v", err)
	}
	web.SetDebug(*debug)
	web.AttachUserDocument(filepath.Join(*dataDir, "user.md"))
	web.AttachDiagnosticLogDir(filepath.Join(*dataDir, "logs", "acp"))

	var remoteNodes *node.ServerManager
	nodePairingTokens := configuredNodePairingTokens()
	nodeAdminToken := strings.TrimSpace(os.Getenv("SECRETARY_NODE_ADMIN_TOKEN"))
	switch {
	case len(nodePairingTokens) == 0 && nodeAdminToken == "":
		log.Printf("remote Node service disabled; set SECRETARY_NODE_PAIRING_TOKEN(S) and SECRETARY_NODE_ADMIN_TOKEN to enable it")
	case len(nodePairingTokens) == 0 || nodeAdminToken == "":
		log.Fatal("SECRETARY_NODE_PAIRING_TOKEN(S) and SECRETARY_NODE_ADMIN_TOKEN must be configured together")
	default:
		remoteNodes, err = node.NewServerManagerWithConfig(context.Background(), store, node.ServerConfig{
			PairingTokens: nodePairingTokens, AdminToken: nodeAdminToken, ClientBootstrapToken: bootstrapToken,
			HeartbeatTimeout: 45 * time.Second, WatchdogInterval: 5 * time.Second,
		})
		if err != nil {
			log.Fatalf("initialize remote Node service: %v", err)
		}
		remoteNodes.SetEventSink(node.NewStoreEventSink(store))
		remoteNodes.SetCommandOutcomeSink(node.NewStoreCommandOutcomeSink(store))
		web.AttachNodeService(remoteNodes)
		log.Printf("remote Node pairing and protocol service enabled")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if _, telegramErr := attachProductionTelegram(ctx, *dataDir, *listen, store, web, profiles, secretaryStore); telegramErr != nil {
		log.Fatalf("initialize Telegram adapter: %v", telegramErr)
	} else if parseBoolEnv("SECRETARY_TELEGRAM_ENABLED") {
		log.Printf("Telegram adapter enabled with polling and durable event bridge")
	}
	if remoteNodes != nil {
		go func() {
			if err := remoteNodes.Run(ctx); err != nil {
				log.Printf("Node heartbeat watchdog: %v", err)
			}
		}()
	}
	reloads := make(chan os.Signal, 1)
	signal.Notify(reloads, syscall.SIGHUP)
	defer signal.Stop(reloads)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-reloads:
				snapshot, err := profiles.Reload()
				if err != nil {
					log.Printf("reload config: %v", err)
					continue
				}
				log.Printf("reloaded config version %s", snapshot.Version)
			}
		}
	}()
	maxAge, maxBytes, forever, retentionErr := profiles.Snapshot().Config.Retention.RawLogPolicy()
	if retentionErr != nil {
		log.Fatalf("read raw log retention: %v", retentionErr)
	}
	if !forever {
		if err := node.PruneLogs(filepath.Join(*dataDir, "logs", "acp"), node.LogRetention{MaxAge: maxAge, MaxBytes: maxBytes}); err != nil {
			log.Printf("prune raw ACP logs: %v", err)
		}
	}
	var persistentSecretary *secretaryruntime.Runtime
	var secretaryMu sync.Mutex
	workerDataDir := filepath.Join(*dataDir, "node", "data")
	if err := os.MkdirAll(workerDataDir, 0o700); err != nil || os.Chmod(workerDataDir, 0o700) != nil {
		log.Fatalf("prepare local Worker Node data directory")
	}
	secretaryHarnessRuntime, runtimeCommand := configuredRuntimeWithStore(profiles.Snapshot(), *dataDir, secretaryStore)
	workerHarnessRuntime, workerRuntimeCommand := configuredRuntimeWithStore(profiles.Snapshot(), workerDataDir, workerStore)
	log.Printf("configured Secretary harness %s (%s)", profiles.Snapshot().Config.EffectiveSecretaryPolicy().Harness, runtimeCommand)
	log.Printf("configured local Worker Node harness %s (%s)", profiles.Snapshot().Config.EffectiveWorkerPolicy().DefaultHarness, workerRuntimeCommand)
	mcpCommand, mcpErr := secretaryMCPCommand()
	if mcpErr != nil {
		log.Fatalf("find secretary-mcp: %v", mcpErr)
	}
	workerLocal := node.NewLocal(workerHarnessRuntime)
	secretaryLocal := node.NewLocal(secretaryHarnessRuntime)
	if err := recoverProductionPhase4Attempts(ctx, store, workerLocal, remoteNodes); err != nil {
		log.Fatalf("recover Phase 4 Attempts: %v", err)
	}
	web.AttachNode(workerLocal)
	conversation, conversationErr := store.ConversationForPerson(ctx, web.OwnerID())
	if conversationErr != nil {
		log.Fatalf("find owner conversation: %v", conversationErr)
	}
	secretaryIdentity, identityErr := store.EnsureSecretaryIdentity(ctx, web.OwnerID(), conversation.ID)
	if identityErr != nil {
		log.Fatalf("ensure Secretary identity: %v", identityErr)
	}
	dispatcher := &app.Dispatcher{Store: store, Node: workerLocal,
		Profile:             func() core.BindingProfile { return bindingProfile(managedProfile(profiles.Snapshot(), "worker")) },
		ManagedProfile:      func() node.ManagedProfile { return managedProfile(profiles.Snapshot(), "worker") },
		ChildProfile:        func() core.BindingProfile { return bindingProfile(managedProfile(profiles.Snapshot(), "child_worker")) },
		ManagedChildProfile: func() node.ManagedProfile { return managedProfile(profiles.Snapshot(), "child_worker") },
	}
	go (app.Runner{Store: store, Dispatcher: dispatcher, Conversation: conversation.ID}).Run(ctx)
	capability := os.Getenv("SECRETARY_CAPABILITY")
	if capability == "" {
		// Capability tokens are intentionally not recoverable from SQLite. A
		// terminal or launchd start without the environment token rotates one,
		// so the persistent Secretary is never silently omitted.
		var capabilityErr error
		capability, capabilityErr = store.RotateSecretaryCapability(context.Background(), web.OwnerID())
		if capabilityErr != nil {
			log.Fatalf("create Secretary capability: %v", capabilityErr)
		}
		if path := strings.TrimSpace(os.Getenv("SECRETARY_CAPABILITY_FILE")); path != "" {
			if err := writeSecretFile(path, capability); err != nil {
				log.Fatalf("save Secretary capability: %v", err)
			}
		} else {
			log.Printf("generated Secretary runtime credential")
		}
	}
	attachProductionWorkerServices(ctx, web, store, web.OwnerID(), capability, workerLocal, remoteNodes, func(binding core.BindingProfile) node.ManagedProfile {
		compiled, err := store.ConfigVersion(ctx, binding.Version)
		if err == nil {
			var snapshot config.Snapshot
			if json.Unmarshal([]byte(compiled), &snapshot) == nil {
				profile := managedProfile(snapshot, binding.Name)
				if binding.Delivery != "" {
					profile.Delivery = binding.Delivery
				}
				return profile
			}
		}
		profile := managedProfile(profiles.Snapshot(), binding.Name)
		if binding.Delivery != "" {
			profile.Delivery = binding.Delivery
		}
		return profile
	}, func() (node.ManagedProfile, error) { return managedProfile(profiles.Snapshot(), "worker"), nil },
		func() core.HarnessPolicy { return configuredWorkerPolicy(profiles.Snapshot().Config) })
	web.AttachSecretaryReplyContract(func() string { return profiles.Snapshot().Config.Secretary.ReplyContract })
	if remoteNodes != nil {
		trustedNode := core.NodeReference(strings.TrimSpace(os.Getenv("SECRETARY_TRUSTED_LOCAL_NODE")))
		trustedPolicy := core.TrustedLocalApprovalPolicy{Enabled: trustedNode != "", Explicit: trustedNode != "", LocalNode: trustedNode != "", Node: trustedNode}
		trustedLocalService := ctl.WorkerService{Store: store, PersonID: web.OwnerID(), Capability: capability, Runtime: ctl.NodeRuntime{Manager: remoteNodes}}
		remoteNodes.SetEventSink(node.NewStoreEventSinkWithTrustedLocalApproval(store, func(applyCtx context.Context, requestID string, nodeRef core.NodeReference) error {
			if trustedPolicy.Node != nodeRef {
				return core.ErrTrustedLocalApprovalDenied
			}
			_, applyErr := trustedLocalService.ApplyTrustedLocalApproval(applyCtx, requestID, trustedPolicy)
			return applyErr
		}))
	}
	restartSecretary := func(restartCtx context.Context) error {
		secretaryMu.Lock()
		defer secretaryMu.Unlock()
		if persistentSecretary == nil {
			return errors.New("persistent Secretary is not configured")
		}
		if err := persistentSecretary.Stop(restartCtx); err != nil {
			return err
		}
		return persistentSecretary.Start(restartCtx)
	}
	startPersistentSecretary := func() error {
		if capability == "" {
			return nil
		}
		allowed, capabilityErr := store.AuthorizeSecretaryCapability(ctx, web.OwnerID(), capability)
		if capabilityErr != nil {
			return fmt.Errorf("authorize Secretary capability: %w", capabilityErr)
		}
		if !allowed {
			return errors.New("SECRETARY_CAPABILITY is not authorized")
		}
		persistentSecretary = secretaryruntime.NewRuntime(secretaryLocal, capability)
		persistentSecretary.AttachIdentity(secretaryIdentity)
		persistentSecretary.AttachMCPServer(mcpCommand, *dataDir, secretaryMCPServerURL(*listen))
		persistentSecretary.AttachProfile(func() node.ManagedProfile {
			return secretaryProfile(profiles.Snapshot(), store)
		})
		persistentSecretary.AttachConversation(store, conversation.ID)
		if err := persistentSecretary.Start(ctx); err != nil {
			return fmt.Errorf("start persistent Secretary: %w", err)
		}
		web.AttachSecretary(persistentSecretary)
		return nil
	}
	web.AttachSecretaryModelCatalog(
		func() map[string]string { return secretaryModels(profiles.Snapshot()) },
		func() string { return "default" },
		func(string) error { return restartSecretary(context.Background()) },
	)
	controlService := ctl.Service{Store: store, PersonID: web.OwnerID(), Capability: capability, Dispatcher: dispatcher, Node: workerLocal}
	web.AttachControl(webapi.ControlOptions{
		ConfigPath:              profiles.Path(),
		ConfigContent:           func() (string, error) { content, err := os.ReadFile(profiles.Path()); return string(content), err },
		RequireExpectedRevision: true,
		ConfigSnapshot:          func() any { return profiles.Snapshot() },
		ApplyConfig:             func(content []byte) (any, error) { return applyConfig(profiles, content) },
		ReloadConfig:            func() (any, error) { return profiles.Reload() },
		ProfileFiles:            func() ([]webapi.ProfileFile, error) { return profileFiles(profiles.Snapshot()), nil },
		ApplyProfile:            func(name string, content []byte) error { return applyProfile(profiles, name, content) },
		RuntimeRestart:          restartSecretary,
		RetryTask: func(retryCtx context.Context, taskID string) (core.Task, error) {
			return controlService.Retry(retryCtx, taskID)
		},
		CloseTask: func(closeCtx context.Context, taskID string) (core.CloseOutcome, error) {
			return controlService.Close(closeCtx, taskID)
		},
		RawLogDir: filepath.Join(*dataDir, "logs", "acp"),
	})

	apiHandler := web.Handler()
	controlAPI := web.ControlHandler()
	staticHandler := webclient.Handler()
	controlStaticHandler := webclient.ControlHandler()
	handler := rootHandler(apiHandler, controlAPI, staticHandler, controlStaticHandler, remoteNodes, *debug)
	server := &http.Server{Addr: *listen, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Fatalf("listen web API: %v", err)
	}
	go func() {
		log.Printf("Secretary web API listening on %s", *listen)
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Fatalf("serve web API: %v", err)
		}
	}()
	if err := startPersistentSecretary(); err != nil {
		log.Fatal(err)
	}
	<-ctx.Done()
	if persistentSecretary != nil {
		if err := persistentSecretary.Stop(context.Background()); err != nil {
			log.Printf("stop persistent Secretary: %v", err)
		}
	}
	if workerLocal != nil {
		if err := workerLocal.Close(); err != nil {
			log.Printf("stop local Workers: %v", err)
		}
	}
	if secretaryLocal != nil {
		if err := secretaryLocal.Close(); err != nil {
			log.Printf("stop local Secretary runtime: %v", err)
		}
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdown); err != nil {
		log.Printf("shutdown web API: %v", err)
	}
}

// rootHandler keeps one loopback server for the whole product: control routes
// stay debug-only, and every other /v1 path reaches the API where credential checks live.
func rootHandler(apiHandler, controlAPI, staticHandler, controlStaticHandler http.Handler, remoteNodes *node.ServerManager, debug bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if remoteNodes != nil {
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(r.Header.Get("Authorization"))), "bearer ") && r.URL.Path != "/v1/nodes/connect" {
				apiHandler.ServeHTTP(w, r)
				return
			}
			if r.URL.Path == "/v1/nodes/connect" {
				remoteNodes.ServeProtocolHTTP(w, r)
				return
			}
		}
		if strings.HasPrefix(r.URL.Path, "/v1/control/") {
			if !debug {
				http.NotFound(w, r)
				return
			}
			controlAPI.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/v1/") {
			apiHandler.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/control-room" || strings.HasPrefix(r.URL.Path, "/control-room/") {
			if !debug {
				http.NotFound(w, r)
				return
			}
			controlStaticHandler.ServeHTTP(w, r)
			return
		}
		staticHandler.ServeHTTP(w, r)
	})
}

// validateListen enforces the supported topology: secretaryd binds loopback
// only and remote access goes through the Tailscale Serve proxy. Only literal
// loopback IPs are accepted, because localhost can resolve off-loopback.
func validateListen(addr string) error {
	host, _, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return fmt.Errorf("invalid -listen %q: %w; use a loopback host:port such as 127.0.0.1:8081", addr, err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("-listen %q must be a literal loopback IP such as 127.0.0.1 or [::1]; hostnames can resolve off-loopback and secretaryd must publish through Tailscale Serve (see docs/always-on-runbook.md)", addr)
	}
	return nil
}

func recoverProductionPhase4Attempts(ctx context.Context, store *core.Store, local *node.LocalNode, remote *node.ServerManager) error {
	resolver := node.Phase4RecoveryResolver{Store: store, Local: local, LocalNodeRef: "local", Remote: remote}
	return store.RecoverPhase4Attempts(ctx, resolver)
}

func configuredWorkerPolicy(c config.Config) core.HarnessPolicy {
	policy := c.EffectiveWorkerPolicy()
	preferred := make([]core.HarnessKind, len(policy.PreferredHarnesses))
	for i, kind := range policy.PreferredHarnesses {
		preferred[i] = core.HarnessKind(kind)
	}
	model := policy.Model
	switch model {
	case "default", "fast", "smart", "cheap":
		model = ""
	}
	reasoning := policy.Reasoning
	if reasoning == "default" {
		reasoning = ""
	}
	return core.HarnessPolicy{DefaultHarness: core.HarnessKind(policy.DefaultHarness), PreferredHarnesses: preferred, ModelID: model, Reasoning: reasoning}
}

func attachProductionWorkerServices(ctx context.Context, web *webapi.Server, store *core.Store, personID, capability string, local *node.LocalNode, remote *node.ServerManager, managedProfile func(core.BindingProfile) node.ManagedProfile, workerProfile func() (node.ManagedProfile, error), workerPolicy func() core.HarnessPolicy) {
	controller := &app.WorkerController{Store: store, Node: local, ManagedProfile: managedProfile}
	web.AttachWorkerController(controller)
	workerService := ctl.WorkerService{Store: store, PersonID: personID, Capability: capability, WorkerPolicySource: workerPolicy, WorkerProfileSource: workerProfile, Runtime: ctl.NodeRuntime{Manager: remote, Local: local}}
	web.AttachWorkerResponder(workerService)
	web.AttachSecretaryWorkerTools(workerService)
	go workerService.RunQueuedWorkerMessages(ctx)
}

func secretaryMCPCommand() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("SECRETARY_MCP_COMMAND")); configured != "" {
		return configured, nil
	}
	if executable, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(executable), "secretary-mcp")
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return candidate, nil
		}
	}
	return exec.LookPath("secretary-mcp")
}

func secretaryMCPServerURL(listen string) string {
	listen = strings.TrimSpace(listen)
	if host, port, err := net.SplitHostPort(listen); err == nil {
		switch host {
		case "", "0.0.0.0", "::", "[::]":
			host = "127.0.0.1"
		}
		return "http://" + net.JoinHostPort(host, port)
	}
	if strings.HasPrefix(listen, ":") {
		return "http://127.0.0.1" + listen
	}
	return "http://" + listen
}

type runtimeNativeStoreSelection struct {
	Secretary      node.OpenCodeNativeStore
	Worker         node.OpenCodeNativeStore
	NodeDataDir    string
	NodeStandalone bool
}

func selectRuntimeNativeStoreSelection(dataDir string) (runtimeNativeStoreSelection, error) {
	if strings.TrimSpace(dataDir) == "" {
		return runtimeNativeStoreSelection{}, errors.New("Secretary data directory is required")
	}
	absoluteDataDir, err := filepath.Abs(strings.TrimSpace(dataDir))
	if err != nil {
		return runtimeNativeStoreSelection{}, errors.New("Secretary data directory is unavailable")
	}
	dataDir = filepath.Clean(absoluteDataDir)
	serverStateExists, err := fileExists(filepath.Join(dataDir, "secretary.db"))
	if err != nil {
		return runtimeNativeStoreSelection{}, err
	}
	serverConfigExists, err := fileExists(filepath.Join(dataDir, "config.toml"))
	if err != nil {
		return runtimeNativeStoreSelection{}, err
	}
	workerDataDir := filepath.Join(dataDir, "node", "data")
	nodeConfigPath := filepath.Join(dataDir, "node", "config.json")
	nodeConfigExists, err := fileExists(nodeConfigPath)
	if err != nil {
		return runtimeNativeStoreSelection{}, err
	}
	nodeStandalone := false
	if nodeConfigExists {
		deployment, err := node.LoadDeploymentConfig(nodeConfigPath)
		if err != nil {
			return runtimeNativeStoreSelection{}, errors.New("opencode: local Node deployment config cannot be validated")
		}
		configuredNodeDataDir, err := filepath.Abs(deployment.DataDir)
		if err != nil {
			return runtimeNativeStoreSelection{}, errors.New("opencode: local Node data directory cannot be validated")
		}
		configuredNodeDataDir = filepath.Clean(configuredNodeDataDir)
		nodeStandalone = deployment.Standalone
		if nodeStandalone {
			workerDataDir = configuredNodeDataDir
		} else if configuredNodeDataDir != workerDataDir {
			return runtimeNativeStoreSelection{}, errors.New("opencode: co-located Node data directory does not match this Secretary installation")
		}
	}
	nodeStateExists, err := fileExists(filepath.Join(workerDataDir, "node-state.json"))
	if err != nil {
		return runtimeNativeStoreSelection{}, err
	}
	secretarySelectionExists, err := fileExists(filepath.Join(dataDir, "opencode-native-selection.json"))
	if err != nil {
		return runtimeNativeStoreSelection{}, err
	}
	nodeSelectionExists, err := fileExists(filepath.Join(workerDataDir, "opencode-native-selection.json"))
	if err != nil {
		return runtimeNativeStoreSelection{}, errors.New("inspect local Node OpenCode selection")
	}
	if !serverStateExists && !serverConfigExists && !nodeStateExists && !nodeConfigExists && !secretarySelectionExists && !nodeSelectionExists {
		secretaryStore, workerStore, err := node.SelectSharedOpenCodeNativeStores(dataDir, workerDataDir)
		return runtimeNativeStoreSelection{Secretary: secretaryStore, Worker: workerStore, NodeDataDir: workerDataDir}, err
	}
	if !nodeStandalone && secretarySelectionExists != nodeSelectionExists {
		return runtimeNativeStoreSelection{}, errors.New("opencode: shared store selection is incomplete; run secretary opencode select-shared-store")
	}
	legacyDataHome := strings.TrimSpace(os.Getenv("XDG_DATA_HOME"))
	hasExistingInstallation := serverStateExists || serverConfigExists || nodeStateExists || nodeConfigExists
	secretaryStore, err := node.SelectOpenCodeNativeStoreWithOptions(dataDir, node.OpenCodeNativeStoreOptions{
		HasExistingState: hasExistingInstallation,
		LegacyDataHome:   legacyDataHome,
		SharedDataHome:   node.OpenCodeNativeDataHome(dataDir),
	})
	if err != nil {
		return runtimeNativeStoreSelection{}, err
	}
	sharedNodeHome := ""
	if !nodeStandalone {
		sharedNodeHome = node.OpenCodeNativeDataHome(dataDir)
	}
	if secretaryStore.Mode == node.OpenCodeNativeStoreModeShared && !nodeStandalone {
		if !nodeSelectionExists {
			return runtimeNativeStoreSelection{}, errors.New("opencode: shared store selection is incomplete; run secretary opencode select-shared-store")
		}
		workerStore, err := node.SelectOpenCodeNativeStoreWithOptions(workerDataDir, node.OpenCodeNativeStoreOptions{
			HasExistingState: true, LegacyDataHome: legacyDataHome,
			SharedDataHome: secretaryStore.DataHome,
		})
		if err != nil || workerStore.Mode != node.OpenCodeNativeStoreModeShared || workerStore.DataHome != secretaryStore.DataHome {
			return runtimeNativeStoreSelection{}, errors.New("opencode: Secretary and local Node shared store selections do not match")
		}
		return runtimeNativeStoreSelection{Secretary: secretaryStore, Worker: workerStore, NodeDataDir: workerDataDir}, nil
	}
	if nodeSelectionExists {
		workerStore, err := node.SelectOpenCodeNativeStoreWithOptions(workerDataDir, node.OpenCodeNativeStoreOptions{
			HasExistingState: hasExistingInstallation, LegacyDataHome: legacyDataHome,
			SharedDataHome: sharedNodeHome, Standalone: nodeStandalone,
		})
		if err != nil {
			return runtimeNativeStoreSelection{}, err
		}
		if workerStore.Mode == node.OpenCodeNativeStoreModeShared {
			return runtimeNativeStoreSelection{}, errors.New("opencode: local Node selected a shared store but Secretary did not; run secretary opencode select-shared-store")
		}
		return runtimeNativeStoreSelection{Secretary: secretaryStore, Worker: workerStore, NodeDataDir: workerDataDir, NodeStandalone: nodeStandalone}, nil
	}
	if secretaryStore.Mode == node.OpenCodeNativeStoreModeLegacy || nodeStateExists || nodeConfigExists {
		workerStore, err := node.SelectOpenCodeNativeStoreWithOptions(workerDataDir, node.OpenCodeNativeStoreOptions{
			HasExistingState: true, LegacyDataHome: legacyDataHome, Standalone: nodeStandalone,
		})
		if err != nil {
			return runtimeNativeStoreSelection{}, err
		}
		return runtimeNativeStoreSelection{Secretary: secretaryStore, Worker: workerStore, NodeDataDir: workerDataDir, NodeStandalone: nodeStandalone}, nil
	}
	secretaryStore, workerStore, err := node.SelectSharedOpenCodeNativeStores(dataDir, workerDataDir)
	return runtimeNativeStoreSelection{Secretary: secretaryStore, Worker: workerStore, NodeDataDir: workerDataDir}, err
}

func fileExists(path string) (bool, error) {
	_, err := os.Lstat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func configuredRuntimePair(snapshot config.Snapshot, dataDir string) (secretaryRuntime, workerRuntime node.Runtime, secretaryCommand, workerCommand string) {
	secretaryStore := node.OpenCodeNativeStore{DataHome: node.OpenCodeNativeDataHome(dataDir), Mode: node.OpenCodeNativeStoreModeShared}
	workerStore := secretaryStore
	secretaryRuntime, secretaryCommand = configuredRuntimeWithStore(snapshot, dataDir, secretaryStore)
	workerRuntime, workerCommand = configuredRuntimeWithStore(snapshot, filepath.Join(dataDir, "node", "data"), workerStore)
	return secretaryRuntime, workerRuntime, secretaryCommand, workerCommand
}

func configuredRuntime(snapshot config.Snapshot, dataDir string) (node.Runtime, string) {
	return configuredRuntimeWithStore(snapshot, dataDir, node.OpenCodeNativeStore{DataHome: node.OpenCodeNativeDataHome(dataDir)})
}

func configuredRuntimeWithStore(snapshot config.Snapshot, dataDir string, store node.OpenCodeNativeStore) (node.Runtime, string) {
	harness := snapshot.Config.EffectiveSecretaryPolicy().Harness
	logDir := filepath.Join(dataDir, "logs", "acp")
	codexCommand := os.Getenv("SECRETARY_ACP_COMMAND")
	codexArgs := strings.Fields(os.Getenv("SECRETARY_ACP_ARGS"))
	if codexCommand == "" {
		codexCommand = "codex-acp"
	}
	fxCommand := os.Getenv("SECRETARY_FX_COMMAND")
	fxArgs := strings.Fields(os.Getenv("SECRETARY_FX_ARGS"))
	if fxCommand == "" {
		fxCommand = "fx"
	}
	if len(fxArgs) == 0 {
		fxArgs = []string{"acp"}
	}
	claudeCommand := os.Getenv("SECRETARY_CLAUDE_COMMAND")
	if claudeCommand == "" {
		claudeCommand = "claude"
	}
	claudeArgs := strings.Fields(os.Getenv("SECRETARY_CLAUDE_ARGS"))
	openCodeCommand := os.Getenv("SECRETARY_OPENCODE_COMMAND")
	if openCodeCommand == "" {
		openCodeCommand = "opencode"
	}
	openCodeArgs := strings.Fields(os.Getenv("SECRETARY_OPENCODE_ARGS"))
	if len(openCodeArgs) == 0 {
		openCodeArgs = []string{"acp"}
	}
	runtime := node.RuntimeRouter{
		DefaultHarness: harness,
		ACP:            node.CodexRuntime{ACPRuntime: node.ACPRuntime{Command: codexCommand, Arguments: codexArgs, RawLogDir: logDir, RawLogMaxBytes: 10 << 20, RawLogFiles: 5}},
		Claude:         node.ClaudeCodeRuntime{Command: claudeCommand, Arguments: claudeArgs, RawLogDir: logDir, RawLogMaxBytes: 10 << 20, RawLogFiles: 5},
		FX:             node.FXRuntime{ACPRuntime: node.ACPRuntime{Command: fxCommand, Arguments: fxArgs, RawLogDir: logDir, RawLogMaxBytes: 10 << 20, RawLogFiles: 5}},
		OpenCode:       node.OpenCodeRuntime{Command: openCodeCommand, Arguments: openCodeArgs, DataHome: store.DataHome, LegacyDataHome: store.Mode == node.OpenCodeNativeStoreModeLegacy, RawLogDir: logDir, RawLogMaxBytes: 10 << 20, RawLogFiles: 5},
	}
	command := codexCommand
	arguments := codexArgs
	switch harness {
	case "opencode":
		command, arguments = openCodeCommand, openCodeArgs
	case "fx":
		command, arguments = fxCommand, fxArgs
	case "claude_code":
		command, arguments = claudeCommand, claudeArgs
	}
	return runtime, strings.TrimSpace(command + " " + strings.Join(arguments, " "))
}

func managedProfile(snapshot config.Snapshot, name string) node.ManagedProfile {
	profile, ok := snapshot.Profiles[name]
	if !ok {
		return node.ManagedProfile{}
	}
	delivery := "workspace_instructions"
	if profile.Runtime == "opencode" || profile.Runtime == "codex" {
		delivery = "native"
	}
	skills := make([]node.ManagedSkill, 0, len(profile.Skills))
	for _, skill := range profile.Skills {
		skills = append(skills, node.ManagedSkill{Path: skill.Path, Content: skill.Content, Hash: skill.Hash})
	}
	return node.ManagedProfile{
		Version: snapshot.Version, Name: profile.Name, Content: profile.Content, Skills: skills,
		AllowTools: append([]string(nil), profile.AllowTools...), Hash: profile.Hash, SourceHash: profile.Hash,
		Runtime: profile.Runtime, Model: profile.Model, Reasoning: profile.Reasoning, Delivery: delivery,
		ReplyContractVersion: profile.ReplyContractVersion,
	}
}

func bindingProfile(profile node.ManagedProfile) core.BindingProfile {
	return core.BindingProfile{Version: profile.Version, Name: profile.Name, Hash: profile.Hash, Runtime: profile.Runtime, Model: profile.Model, Reasoning: profile.Reasoning, Tools: strings.Join(profile.AllowTools, ","), Delivery: profile.Delivery}
}

func secretaryModels(snapshot config.Snapshot) map[string]string {
	models := map[string]string{"default": snapshot.Config.EffectiveSecretaryPolicy().Model}
	for _, model := range []string{snapshot.Config.Models.Fast, snapshot.Config.Models.Smart, snapshot.Config.Models.Cheap} {
		model = strings.TrimSpace(model)
		if model == "" || model == "fast" || model == "smart" || model == "cheap" {
			continue
		}
		models[model] = model
	}
	return models
}

func secretaryProfile(snapshot config.Snapshot, store *core.Store) node.ManagedProfile {
	profile := managedProfile(snapshot, "secretary")
	if profile.ReplyContractVersion == core.SecretaryReplyContractAddressedV1 {
		profile.Content = strings.TrimSpace(profile.Content) + `

## Addressed reply v1
Use the current server-issued secretary_turn_id and input_id from the runtime context. Send every independent user-facing answer, clarification, mixed answer plus Worker action, or error through reply_to_user exactly once. Include the same origin IDs in every spawn_worker and message_worker call. Ordinary assistant text is activity, not a durable reply: an addressed turn cannot succeed without one exact reply_to_user for its active input, even when there is assistant final text or an already delivered Worker Result. Worker Results themselves must never be paraphrased as a second reply.`
	}
	selected, found, err := store.GetSetting(context.Background(), "secretary.model")
	if err != nil || !found {
		return profile
	}
	if model, ok := secretaryModels(snapshot)[selected]; ok {
		profile.Model = model
	}
	return profile
}

func profileFiles(snapshot config.Snapshot) []webapi.ProfileFile {
	files := make([]webapi.ProfileFile, 0, 3)
	for _, name := range []string{"secretary", "worker", "child_worker"} {
		profile, ok := snapshot.Profiles[name]
		if !ok {
			continue
		}
		files = append(files, webapi.ProfileFile{
			Name: profile.Name, Path: profile.Path, Content: profile.Content, Hash: profile.Hash, Revision: profile.Hash, Editable: true,
			Runtime: profile.Runtime, Model: profile.Model, Reasoning: profile.Reasoning,
		})
	}
	return files
}

func applyProfile(manager *config.Manager, name string, content []byte) error {
	if name != "secretary" && name != "worker" && name != "child_worker" {
		return fmt.Errorf("unknown profile %q", name)
	}
	if len(strings.TrimSpace(string(content))) == 0 {
		return errors.New("profile content cannot be empty")
	}
	profile, ok := manager.Snapshot().Profiles[name]
	if !ok || profile.Path == "" {
		return fmt.Errorf("profile %q is not configured", name)
	}
	previous, err := os.ReadFile(profile.Path)
	if err != nil {
		return err
	}
	if err := writeConfig(profile.Path, content); err != nil {
		return err
	}
	if _, err := manager.Reload(); err != nil {
		_ = writeConfig(profile.Path, previous)
		return err
	}
	return nil
}

func applyConfig(manager *config.Manager, content []byte) (any, error) {
	path := manager.Path()
	previous, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := writeConfig(path, content); err != nil {
		return nil, err
	}
	snapshot, err := manager.Reload()
	if err != nil {
		// Keep the active config and restore the last known-good file.
		_ = writeConfig(path, previous)
		return nil, err
	}
	return snapshot, nil
}

func writeSecretFile(path, value string) error {
	if strings.TrimSpace(path) == "" || strings.TrimSpace(value) == "" {
		return errors.New("secret file path and value are required")
	}
	return writeConfig(path, []byte(value+"\n"))
}

func writeConfig(path string, content []byte) error {
	dir := filepath.Dir(path)
	temporary, err := os.CreateTemp(dir, ".config.toml.*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, path)
}

func configuredNodePairingTokens() []string {
	value := os.Getenv("SECRETARY_NODE_PAIRING_TOKENS")
	if strings.TrimSpace(value) == "" {
		value = os.Getenv("SECRETARY_NODE_PAIRING_TOKEN")
	}
	var tokens []string
	for _, token := range strings.Split(value, ",") {
		if token = strings.TrimSpace(token); token != "" {
			tokens = append(tokens, token)
		}
	}
	return tokens
}

func defaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".secretary"
	}
	return filepath.Join(home, ".secretary")
}
