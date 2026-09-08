package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/beruseruko/secretary/internal/app"
	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
	secretaryruntime "github.com/beruseruko/secretary/internal/secretary"
	"github.com/beruseruko/secretary/internal/webapi"
	webclient "github.com/beruseruko/secretary/web"
)

func main() {
	dataDir := flag.String("data-dir", defaultDataDir(), "directory for Secretary durable state")
	listen := flag.String("listen", "127.0.0.1:8081", "web listener address")
	flag.Parse()
	bootstrapToken := os.Getenv("SECRETARY_BOOTSTRAP_TOKEN")
	if bootstrapToken == "" {
		log.Fatal("SECRETARY_BOOTSTRAP_TOKEN is required")
	}
	if err := os.MkdirAll(*dataDir, 0o700); err != nil {
		log.Fatalf("create data directory: %v", err)
	}
	store, err := core.Open(context.Background(), filepath.Join(*dataDir, "secretary.db"))
	if err != nil {
		log.Fatalf("open Secretary state: %v", err)
	}
	defer store.Close()
	web, err := webapi.New(context.Background(), store, bootstrapToken)
	if err != nil {
		log.Fatalf("initialize web API: %v", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var persistentSecretary *secretaryruntime.Runtime
	if acpCommand := os.Getenv("SECRETARY_ACP_COMMAND"); acpCommand != "" {
		local := node.NewLocal(node.ACPRuntime{Command: acpCommand, Arguments: strings.Fields(os.Getenv("SECRETARY_ACP_ARGS"))})
		web.AttachNode(local)
		conversation, conversationErr := store.ConversationForPerson(ctx, web.OwnerID())
		if conversationErr != nil {
			log.Fatalf("find owner conversation: %v", conversationErr)
		}
		dispatcher := &app.Dispatcher{Store: store, Node: local}
		go (app.Runner{Store: store, Dispatcher: dispatcher, Conversation: conversation.ID}).Run(ctx)
		capability := os.Getenv("SECRETARY_CAPABILITY")
		if capability == "" {
			hasCapability, capabilityErr := store.HasSecretaryCapability(context.Background(), web.OwnerID())
			if capabilityErr != nil {
				log.Fatalf("check Secretary capability: %v", capabilityErr)
			}
			if !hasCapability {
				capability, capabilityErr = store.RotateSecretaryCapability(context.Background(), web.OwnerID())
				if capabilityErr != nil {
					log.Fatalf("create Secretary capability: %v", capabilityErr)
				}
				log.Printf("generated SECRETARY_CAPABILITY=%s", capability)
			}
		}
		if capability != "" {
			persistentSecretary = secretaryruntime.NewRuntime(local, capability)
			if err := persistentSecretary.Start(ctx); err != nil {
				log.Fatalf("start persistent Secretary: %v", err)
			}
			web.AttachSecretary(persistentSecretary)
		}
	}

	apiHandler := web.Handler()
	staticHandler := webclient.Handler()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/") {
			apiHandler.ServeHTTP(w, r)
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
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdown); err != nil {
		log.Printf("shutdown web API: %v", err)
	}
}

func defaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".secretary"
	}
	return filepath.Join(home, ".secretary")
}
