package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/beruseruko/secretary/internal/node"
)

// A read-only activation preflight. It never enrolls, starts a second daemon,
// contacts the Server or changes native provider credentials.
func checkInstalledNodeDeployment(configPath string) error {
	if strings.TrimSpace(configPath) == "" {
		return errors.New("installed Node config path is required")
	}
	config, err := node.LoadDeploymentConfig(configPath)
	if err != nil { return fmt.Errorf("read deployment: %w", err) }
	info, err := os.Stat(config.DataDir)
	if err != nil { return fmt.Errorf("Node state directory: %w", err) }
	if !info.IsDir() { return errors.New("Node state path is not a directory") }
	identityPath := filepath.Join(config.DataDir, "identity.json")
	identityInfo, err := os.Lstat(identityPath)
	if err != nil { return fmt.Errorf("Node identity: %w", err) }
	if !identityInfo.Mode().IsRegular() {
		return errors.New("Node identity must be a regular file, not a symlink")
	}
	identity, err := node.LoadNodeIdentity(identityPath)
	if err != nil { return fmt.Errorf("read Node identity: %w", err) }
	if identity.Node != config.Node {
		return fmt.Errorf("Node deployment identity mismatch: config %q, enrolled %q", config.Node, identity.Node)
	}
	return nil
}
