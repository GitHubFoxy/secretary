package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

func TestInstalledNodeDeploymentPreflightWithoutDaemon(t *testing.T) {
	root:=t.TempDir()
	data:=filepath.Join(root,"data")
	if err:=os.MkdirAll(data,0700);err!=nil{t.Fatal(err)}
	path:=filepath.Join(root,"config.json")
	config:=node.DeploymentConfig{ServerURL:"https://example.invalid",Node:"node-one",DataDir:data,Capacity:1,Standalone:true}
	if err:=node.SaveDeploymentConfig(path,config);err!=nil{t.Fatal(err)}
	identity:=node.NodeIdentity{Node:core.NodeReference("node-one"),Credential:base64.RawURLEncoding.EncodeToString(make([]byte,32)),ConnectURL:"wss://example.invalid/v1/nodes/connect"}
	identityPath:=filepath.Join(data,"identity.json")
	if err:=node.SaveNodeIdentity(identityPath,identity);err!=nil{t.Fatal(err)}
	if err:=checkInstalledNodeDeployment(path);err!=nil{t.Fatalf("valid staged deployment rejected: %v",err)}
	// This check must not create a daemon-owned node-state.json or rotate the identity.
	if _,err:=os.Stat(filepath.Join(data,"node-state.json"));!os.IsNotExist(err){t.Fatalf("preflight started or changed Node state: %v",err)}
	if err:=os.Remove(identityPath);err!=nil{t.Fatal(err)}
	if err:=os.Symlink(path,identityPath);err==nil {
		if err:=checkInstalledNodeDeployment(path);err==nil || !strings.Contains(err.Error(),"regular file") { t.Fatalf("symlink identity accepted: %v",err) }
		_ = os.Remove(identityPath)
	}
	identity.Node="node-two"
	if err:=node.SaveNodeIdentity(identityPath,identity);err!=nil{t.Fatal(err)}
	if err:=checkInstalledNodeDeployment(path);err==nil || !strings.Contains(err.Error(),"mismatch"){t.Fatalf("mismatched Node accepted: %v",err)}
	if err:=os.WriteFile(path,[]byte("{"),0600);err!=nil{t.Fatal(err)}
	if err:=checkInstalledNodeDeployment(path);err==nil{t.Fatal("corrupt deployment accepted")}
}
