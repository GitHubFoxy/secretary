package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
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
	dataDir := flag.String("data-dir", defaultDataDir(), "directory for Secretary durable state")
	listen := flag.String("listen", "127.0.0.1:8081", "web listener address")
	configPath := flag.String("config", "", "path to Secretary config.toml")
	debug := flag.Bool("debug", false, "enable the local-only Control Room")
	flag.Parse()
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
	web, err := webapi.New(context.Background(), store, bootstrapToken)
	if err != nil {
		log.Fatalf("initialize web API: %v", err)
	}
	web.SetDebug(*debug)

	var remoteNodes *node.ServerManager
	nodePairingToken := strings.TrimSpace(os.Getenv("SECRETARY_NODE_PAIRING_TOKEN"))
	nodeAdminToken := strings.TrimSpace(os.Getenv("SECRETARY_NODE_ADMIN_TOKEN"))
	switch {
	case nodePairingToken == "" && nodeAdminToken == "":
		log.Printf("remote Node service disabled; set SECRETARY_NODE_PAIRING_TOKEN and SECRETARY_NODE_ADMIN_TOKEN to enable it")
	case nodePairingToken == "" || nodeAdminToken == "":
		log.Fatal("SECRETARY_NODE_PAIRING_TOKEN and SECRETARY_NODE_ADMIN_TOKEN must be configured together")
	default:
		remoteNodes, err = node.NewServerManager(context.Background(), store, nodePairingToken, nodeAdminToken)
		if err != nil {
			log.Fatalf("initialize remote Node service: %v", err)
		}
		log.Printf("remote Node pairing and protocol service enabled")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
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
	runtime, runtimeCommand := configuredRuntime(profiles.Snapshot(), *dataDir)
	log.Printf("configured Secretary harness %s (%s)", profiles.Snapshot().Config.EffectiveSecretaryPolicy().Harness, runtimeCommand)
	mcpCommand, mcpErr := exec.LookPath("secretary-mcp")
	if mcpErr != nil {
		log.Fatalf("find secretary-mcp: %v", mcpErr)
	}
	local := node.NewLocal(runtime)
	web.AttachNode(local)
	conversation, conversationErr := store.ConversationForPerson(ctx, web.OwnerID())
	if conversationErr != nil {
		log.Fatalf("find owner conversation: %v", conversationErr)
	}
	secretaryIdentity, identityErr := store.EnsureSecretaryIdentity(ctx, web.OwnerID(), conversation.ID)
	if identityErr != nil {
		log.Fatalf("ensure Secretary identity: %v", identityErr)
	}
	dispatcher := &app.Dispatcher{Store: store, Node: local, MCPCommand: mcpCommand, MCPDataDir: *dataDir,
		Profile:             func() core.BindingProfile { return bindingProfile(managedProfile(profiles.Snapshot(), "worker")) },
		ManagedProfile:      func() node.ManagedProfile { return managedProfile(profiles.Snapshot(), "worker") },
		ChildProfile:        func() core.BindingProfile { return bindingProfile(managedProfile(profiles.Snapshot(), "child_worker")) },
		ManagedChildProfile: func() node.ManagedProfile { return managedProfile(profiles.Snapshot(), "child_worker") },
	}
	web.AttachWorkerController(&app.WorkerController{Store: store, Node: local, MCPCommand: mcpCommand, MCPDataDir: *dataDir,
		ManagedProfile: func(binding core.BindingProfile) node.ManagedProfile {
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
		},
	})
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
		log.Printf("generated SECRETARY_CAPABILITY=%s", capability)
	}
	if capability != "" {
		allowed, capabilityErr := store.AuthorizeSecretaryCapability(ctx, web.OwnerID(), capability)
		if capabilityErr != nil {
			log.Fatalf("authorize Secretary capability: %v", capabilityErr)
		}
		if !allowed {
			log.Fatal("SECRETARY_CAPABILITY is not authorized")
		}
		persistentSecretary = secretaryruntime.NewRuntime(local, capability)
		persistentSecretary.AttachIdentity(secretaryIdentity)
		persistentSecretary.AttachMCP(mcpCommand, *dataDir)
		persistentSecretary.AttachProfile(func() node.ManagedProfile {
			return secretaryProfile(profiles.Snapshot(), store)
		})
		persistentSecretary.AttachConversation(store, conversation.ID)
		if err := persistentSecretary.Start(ctx); err != nil {
			log.Fatalf("start persistent Secretary: %v", err)
		}
		web.AttachSecretary(persistentSecretary)
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
	web.AttachSecretaryModelCatalog(
		func() map[string]string { return secretaryModels(profiles.Snapshot()) },
		func() string { return "default" },
		func(string) error { return restartSecretary(context.Background()) },
	)
	controlService := ctl.Service{Store: store, PersonID: web.OwnerID(), Capability: capability, Dispatcher: dispatcher, Node: local}
	web.AttachControl(webapi.ControlOptions{
		ConfigPath:     profiles.Path(),
		ConfigContent:  func() (string, error) { content, err := os.ReadFile(profiles.Path()); return string(content), err },
		ConfigSnapshot: func() any { return profiles.Snapshot() },
		ApplyConfig:    func(content []byte) (any, error) { return applyConfig(profiles, content) },
		ReloadConfig:   func() (any, error) { return profiles.Reload() },
		ProfileFiles:   func() ([]webapi.ProfileFile, error) { return profileFiles(profiles.Snapshot()), nil },
		ApplyProfile:   func(name string, content []byte) error { return applyProfile(profiles, name, content) },
		RuntimeRestart: restartSecretary,
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
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if remoteNodes != nil {
			if r.URL.Path == "/v1/nodes/connect" {
				remoteNodes.ServeProtocolHTTP(w, r)
				return
			}
			if r.URL.Path == "/v1/nodes" || strings.HasPrefix(r.URL.Path, "/v1/nodes/") {
				remoteNodes.ServeHTTP(w, r)
				return
			}
		}
		if strings.HasPrefix(r.URL.Path, "/v1/control/") {
			if !*debug {
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
			if !*debug {
				http.NotFound(w, r)
				return
			}
			controlStaticHandler.ServeHTTP(w, r)
			return
		}
		staticHandler.ServeHTTP(w, r)
	})
	server := &http.Server{Addr: *listen, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		log.Printf("Secretary web API listening on %s", *listen)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("serve web API: %v", err)
		}
	}()
	<-ctx.Done()
	if persistentSecretary != nil {
		if err := persistentSecretary.Stop(context.Background()); err != nil {
			log.Printf("stop persistent Secretary: %v", err)
		}
	}
	if local != nil {
		if err := local.Close(); err != nil {
			log.Printf("stop local Workers: %v", err)
		}
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdown); err != nil {
		log.Printf("shutdown web API: %v", err)
	}
}

func configuredRuntime(snapshot config.Snapshot, dataDir string) (node.Runtime, string) {
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
		ACP:            node.ACPRuntime{Command: codexCommand, Arguments: codexArgs, RawLogDir: logDir, RawLogMaxBytes: 10 << 20, RawLogFiles: 5},
		FX:             node.FXRuntime{ACPRuntime: node.ACPRuntime{Command: fxCommand, Arguments: fxArgs, RawLogDir: logDir, RawLogMaxBytes: 10 << 20, RawLogFiles: 5}},
		OpenCode:       node.OpenCodeRuntime{Command: openCodeCommand, Arguments: openCodeArgs, RawLogDir: logDir, RawLogMaxBytes: 10 << 20, RawLogFiles: 5},
	}
	command := codexCommand
	arguments := codexArgs
	switch harness {
	case "opencode":
		command, arguments = openCodeCommand, openCodeArgs
	case "fx":
		command, arguments = fxCommand, fxArgs
	}
	return runtime, strings.TrimSpace(command + " " + strings.Join(arguments, " "))
}

func managedProfile(snapshot config.Snapshot, name string) node.ManagedProfile {
	profile, ok := snapshot.Profiles[name]
	if !ok {
		return node.ManagedProfile{}
	}
	delivery := "workspace_instructions"
	if profile.Runtime == "opencode" {
		delivery = "native"
	}
	skills := make([]node.ManagedSkill, 0, len(profile.Skills))
	for _, skill := range profile.Skills {
		skills = append(skills, node.ManagedSkill{Path: skill.Path, Content: skill.Content, Hash: skill.Hash})
	}
	return node.ManagedProfile{
		Version: snapshot.Version, Name: profile.Name, Content: profile.Content, Skills: skills,
		AllowTools: append([]string(nil), profile.AllowTools...), Hash: profile.Hash,
		Runtime: profile.Runtime, Model: profile.Model, Reasoning: profile.Reasoning, Delivery: delivery,
	}
}

func bindingProfile(profile node.ManagedProfile) core.BindingProfile {
	return core.BindingProfile{Version: profile.Version, Name: profile.Name, Hash: profile.Hash, Runtime: profile.Runtime, Model: profile.Model, Reasoning: profile.Reasoning, Tools: strings.Join(profile.AllowTools, ","), Delivery: profile.Delivery}
}

func secretaryModels(snapshot config.Snapshot) map[string]string {
	return map[string]string{
		"default": snapshot.Config.EffectiveSecretaryPolicy().Model,
		"fast":    snapshot.Config.Models.Fast,
		"smart":   snapshot.Config.Models.Smart,
		"cheap":   snapshot.Config.Models.Cheap,
	}
}

func secretaryProfile(snapshot config.Snapshot, store *core.Store) node.ManagedProfile {
	profile := managedProfile(snapshot, "secretary")
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
			Name: profile.Name, Path: profile.Path, Content: profile.Content, Hash: profile.Hash,
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

func defaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".secretary"
	}
	return filepath.Join(home, ".secretary")
}
