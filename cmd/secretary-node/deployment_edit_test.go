package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/beruseruko/secretary/internal/node"
)

func TestTicketT06NodeSetupPreservesMultipleWorkspacesOnPartialEdit(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.json")
	dataDir := filepath.Join(root, "data")
	initial := node.DeploymentConfig{
		ServerURL: "http://127.0.0.1:8081",
		Node:      "local-node",
		DataDir:   dataDir,
		Capacity:  1,
		Workspaces: []node.WorkspaceMapping{
			{ProjectID: "frontend", Path: "/src/frontend"},
			{ProjectID: "backend", Path: "/src/backend"},
		},
	}
	if err := node.SaveDeploymentConfig(path, initial); err != nil {
		t.Fatal(err)
	}
	capacity := 2
	if err := editDeploymentConfig(path, deploymentEdits{Capacity: &capacity}); err != nil {
		t.Fatal(err)
	}
	loaded, err := node.LoadDeploymentConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Capacity != 2 || !reflect.DeepEqual(loaded.Workspaces, initial.Workspaces) {
		t.Fatalf("partial update erased workspace mappings: %#v", loaded)
	}
	if err := editDeploymentConfig(path, deploymentEdits{
		Workspaces: []node.WorkspaceMapping{
			{ProjectID: "frontend", Path: "/new/frontend"},
			{ProjectID: "docs", Path: "/src/docs"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	loaded, err = node.LoadDeploymentConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []node.WorkspaceMapping{
		{ProjectID: "frontend", Path: "/new/frontend"},
		{ProjectID: "backend", Path: "/src/backend"},
		{ProjectID: "docs", Path: "/src/docs"},
	}
	if !reflect.DeepEqual(loaded.Workspaces, want) {
		t.Fatalf("explicit workspace changes should merge without deleting other mappings: got %#v", loaded.Workspaces)
	}
	if err := editDeploymentConfig(path, deploymentEdits{ClearWorkspaces: true}); err != nil {
		t.Fatal(err)
	}
	loaded, err = node.LoadDeploymentConfig(path)
	if err != nil || len(loaded.Workspaces) != 0 {
		t.Fatalf("explicit clear did not remove mappings: %#v %v", loaded.Workspaces, err)
	}
}

func TestTicketT06NodeConfigInvalidEditIsAtomic(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.json")
	initial := node.DeploymentConfig{
		ServerURL: "http://127.0.0.1:8081",
		Node: "local-node",
		DataDir: filepath.Join(root, "data"),
		Capacity: 2,
		Workspaces: []node.WorkspaceMapping{{ProjectID: "project", Path: "/src/project"}},
	}
	if err := node.SaveDeploymentConfig(path, initial); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	invalid := -1
	if err := editDeploymentConfig(path, deploymentEdits{Capacity: &invalid}); err == nil {
		t.Fatal("invalid capacity accepted")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatalf("failed edit modified config: %v", err)
	}
	if err := os.MkdirAll(initial.DataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(initial.DataDir, "identity.json"), []byte("{\"node\":\"local-node\"}"), 0o600); err != nil {
		t.Fatal(err)
	}
	name := "replaced-node"
	if err := editDeploymentConfig(path, deploymentEdits{NodeName: &name}); err == nil || !strings.Contains(err.Error(), "paired Node") {
		t.Fatalf("paired identity changed: %v", err)
	}
	after, err = os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatalf("paired Node edit changed config: %v", err)
	}
}

func TestTicketT06RepeatedWorkspaceFlagsRejectDuplicate(t *testing.T) {
	var values workspaceEdits
	if err := values.Set("a=/src/a"); err != nil {
		t.Fatal(err)
	}
	if err := values.Set("b=/src/b"); err != nil {
		t.Fatal(err)
	}
	if err := values.Set("a=/other"); err == nil {
		t.Fatal("duplicate workspace flag should be rejected")
	}
	if err := values.Set("bad=relative/path"); err == nil {
		t.Fatal("relative workspace path accepted")
	}
}
