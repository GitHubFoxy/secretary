//go:build linux

package nativeexec

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestT09UserInstalledCLIAssetsAccessibleWithoutNodeSecrets(t *testing.T) {
	base:=t.TempDir()
	node:=filepath.Join(base,"node-private")
	workspace:=filepath.Join(base,"workspace")
	pkg:=filepath.Join(base,"user-install","node_modules","@openai","codex")
	if err:=os.MkdirAll(node,0700);err!=nil {t.Fatal(err)}
	if err:=os.MkdirAll(workspace,0700);err!=nil {t.Fatal(err)}
	if err:=os.MkdirAll(filepath.Join(pkg,"bin"),0700);err!=nil {t.Fatal(err)}
	if err:=os.MkdirAll(filepath.Join(pkg,"assets"),0700);err!=nil {t.Fatal(err)}
	if err:=os.WriteFile(filepath.Join(node,"identity.json"),[]byte("NODE-CREDENTIAL"),0600);err!=nil {t.Fatal(err)}
	if err:=os.WriteFile(filepath.Join(pkg,"assets","model.txt"),[]byte("NATIVE-ASSETS-OK\n"),0600);err!=nil {t.Fatal(err)}
	script:=filepath.Join(pkg,"bin","launcher.sh")
	if err:=os.WriteFile(script,[]byte("#!/bin/sh\ncat \"$(dirname \"$0\")/../assets/model.txt\"\ntest ! -r \"$1\"\n"),0700);err!=nil {t.Fatal(err)}
	ctx:=WithWorkerSandbox(context.Background(),WorkerSandbox{ProtectedDir:node,Workspace:workspace})
	cmd,err:=CommandContext(ctx,script,filepath.Join(node,"identity.json"))
	if err!=nil {t.Fatal(err)}
	out,err:=cmd.CombinedOutput()
	if err!=nil {t.Fatalf("user-installed CLI failed to load packaged assets: %v %s",err,out)}
	if !strings.Contains(string(out),"NATIVE-ASSETS-OK"){t.Fatalf("user-installed CLI asset missing: %q",out)}
}

func TestT09AlternateSystemNodeRootIsMasked(t *testing.T) {
	workspace:=t.TempDir()
	for _, privateRoot:=range []string{"/etc","/usr/local"} {
		t.Run(privateRoot,func(t *testing.T){
			// These are existing system roots, requiring no writes or root.
			// They simulate an administrator-selected Node --data-dir inside
			// a root which the Worker normally sees as a read-only mount.
			if _,err:=os.Stat(privateRoot);err!=nil {t.Skip(err)}
			ctx:=WithWorkerSandbox(context.Background(),WorkerSandbox{ProtectedDir:privateRoot,Workspace:workspace})
			cmd,err:=CommandContext(ctx,"/bin/sh","-c","test ! -r \"$1\"","t09",filepath.Join(privateRoot,"passwd"))
			if err!=nil {t.Fatal(err)}
			out,err:=cmd.CombinedOutput()
			if err!=nil {t.Fatalf("alternate Node root leaked or sandbox failed: %v %s",err,out)}
		})
	}
}
