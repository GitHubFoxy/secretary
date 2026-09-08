package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/ctl"
)

func main() {
	dataDir := flag.String("data-dir", defaultDataDir(), "directory containing Secretary durable state")
	flag.Parse()
	if flag.NArg() == 0 {
		usage()
		os.Exit(2)
	}
	store, err := core.Open(context.Background(), filepath.Join(*dataDir, "secretary.db"))
	if err != nil {
		log.Fatalf("open Secretary state: %v", err)
	}
	defer store.Close()
	owner, _, err := store.EnsureOwner(context.Background())
	if err != nil {
		log.Fatalf("find owner: %v", err)
	}
	service := ctl.Service{Store: store, PersonID: owner.ID, Capability: os.Getenv("SECRETARY_CAPABILITY")}
	ctx := context.Background()

	var value any
	switch flag.Arg(0) {
	case "create":
		value, err = service.Create(ctx, strings.TrimSpace(strings.Join(flag.Args()[1:], " ")))
	case "retry":
		if flag.NArg() != 2 {
			usage()
			os.Exit(2)
		}
		value, err = service.Retry(ctx, flag.Arg(1))
	case "close":
		if flag.NArg() != 2 {
			usage()
			os.Exit(2)
		}
		value, err = service.Close(ctx, flag.Arg(1))
	case "list":
		value, err = service.List(ctx)
	case "show":
		if flag.NArg() != 2 {
			usage()
			os.Exit(2)
		}
		value, err = service.Show(ctx, flag.Arg(1))
	case "rotate-capability":
		if err = authorizeForRotation(ctx, store, owner.ID, os.Getenv("SECRETARY_CAPABILITY")); err == nil {
			var token string
			token, err = store.RotateSecretaryCapability(ctx, owner.ID)
			value = map[string]string{"capability": token}
		}
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		if errors.Is(err, ctl.ErrUnauthorized) {
			fmt.Fprintln(os.Stderr, "unauthorized")
		} else {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(value); err != nil {
		log.Fatal(err)
	}
}

func authorizeForRotation(ctx context.Context, store *core.Store, personID, capability string) error {
	if capability == "" {
		return ctl.ErrUnauthorized
	}
	allowed, err := store.AuthorizeSecretaryCapability(ctx, personID, capability)
	if err != nil {
		return err
	}
	if !allowed {
		return ctl.ErrUnauthorized
	}
	return nil
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: secretaryctl [-data-dir DIR] {create TEXT|retry TASK_ID|close TASK_ID|list|show TASK_ID|rotate-capability}")
}

func defaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".secretary"
	}
	return filepath.Join(home, ".secretary")
}
