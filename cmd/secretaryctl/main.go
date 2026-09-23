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
	"strconv"
	"strings"
	"syscall"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/ctl"
	"github.com/beruseruko/secretary/internal/telegram"
)

func main() {
	dataDir := flag.String("data-dir", defaultDataDir(), "directory containing Secretary durable state")
	flag.Parse()
	if flag.NArg() == 0 {
		usage()
		os.Exit(2)
	}
	if flag.Arg(0) == "telegram-rebind" {
		if err := runTelegramRebind(*dataDir, flag.Args()[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if err := json.NewEncoder(os.Stdout).Encode(map[string]string{"status": "rebound"}); err != nil {
			log.Fatal(err)
		}
		return
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

func runTelegramRebind(dataDir string, args []string) error {
	reason := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "--reason" && i+1 < len(args) {
			reason = args[i+1]
			i++
		} else if strings.HasPrefix(args[i], "--reason=") {
			reason = strings.TrimPrefix(args[i], "--reason=")
		} else {
			return fmt.Errorf("unknown argument %q", args[i])
		}
	}
	if reason != "private-to-forum" {
		return fmt.Errorf("reason must be private-to-forum")
	}
	ownerChatStr := strings.TrimSpace(os.Getenv("SECRETARY_TELEGRAM_OWNER_CHAT_ID"))
	if ownerChatStr == "" {
		return errors.New("SECRETARY_TELEGRAM_OWNER_CHAT_ID must be set to the new forum group id before rebind")
	}
	configOwner, err := strconv.ParseInt(ownerChatStr, 10, 64)
	if err != nil || configOwner == 0 {
		return errors.New("SECRETARY_TELEGRAM_OWNER_CHAT_ID must be a non-zero integer")
	}
	// Ensure daemon is stopped: check pid file if present
	pidPath := filepath.Join(dataDir, "server.pid")
	if data, err := os.ReadFile(pidPath); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
			if processExists(pid) {
				return fmt.Errorf("secretaryd is running (pid %d); stop it before rebind", pid)
			}
		}
	}
	// Also check systemd on hosts that use it
	if _, err := os.Stat(filepath.Join(dataDir, "telegram", "state.json")); err != nil && !os.IsNotExist(err) {
		return err
	}
	store, err := core.Open(context.Background(), filepath.Join(dataDir, "secretary.db"))
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer store.Close()
	watermark, err := store.MaxEventSeq(context.Background())
	if err != nil {
		return fmt.Errorf("read watermark: %w", err)
	}
	statePath := filepath.Join(dataDir, "telegram", "state.json")
	if err := telegram.Rebind(statePath, configOwner, reason, watermark); err != nil {
		return err
	}
	return nil
}

func processExists(pid int) bool {
	if pid <= 0 {
		return false
	}
	// Use syscall kill 0 to test existence without sending signal.
	if err := syscall.Kill(pid, 0); err == nil {
		return true
	} else if err == syscall.ESRCH {
		return false
	} else if err == syscall.EPERM {
		return true
	}
	// Fallback via os.FindProcess
	if proc, err := os.FindProcess(pid); err == nil {
		_ = proc
		return true
	}
	return false
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: secretaryctl [-data-dir DIR] {create TEXT|retry TASK_ID|close TASK_ID|list|show TASK_ID|rotate-capability|telegram-rebind --reason private-to-forum}")
}

func defaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".secretary"
	}
	return filepath.Join(home, ".secretary")
}
