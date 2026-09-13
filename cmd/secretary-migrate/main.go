package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"path/filepath"

	"github.com/beruseruko/secretary/internal/migration"
)

func main() {
	dataDir := flag.String("data-dir", defaultDataDir(), "directory containing Secretary durable state")
	source := flag.String("source", "", "Phase 3 database to migrate, defaults to data-dir/secretary.db")
	destination := flag.String("destination", "", "destination database, defaults to source")
	configPath := flag.String("config", "", "config.toml to split into Phase 4 sections")
	userSource := flag.String("user-md", "", "source external user.md")
	userDestination := flag.String("destination-user-md", "", "destination external user.md")
	flag.Parse()
	if *source == "" {
		*source = filepath.Join(*dataDir, "secretary.db")
	}
	if *destination == "" {
		*destination = *source
	}
	if *configPath == "" {
		candidate := filepath.Join(*dataDir, "config.toml")
		if _, err := os.Stat(candidate); err == nil {
			*configPath = candidate
		}
	}
	if *userSource != "" && *userDestination == "" {
		*userDestination = filepath.Join(filepath.Dir(*destination), "user.md")
	}
	report, err := migration.Run(context.Background(), migration.Options{
		SourcePath:                  *source,
		DestinationPath:             *destination,
		ConfigPath:                  *configPath,
		SourceUserDocumentPath:      *userSource,
		DestinationUserDocumentPath: *userDestination,
	})
	if err != nil {
		log.Fatal(err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
		log.Fatal(err)
	}
}

func defaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".secretary"
	}
	return filepath.Join(home, ".secretary")
}
