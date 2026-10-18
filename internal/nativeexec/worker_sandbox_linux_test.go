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


func TestT09LinuxNestedWorkspaceRemountedAfterProtectedNodeRoot(t *testing.T) {
	privateRoot:=t.TempDir()
	workspace:=filepath.Join(privateRoot,"workspace")
	if err:=os.MkdirAll(workspace,0700);err!=nil{t.Fatal(err)}
	if err:=os.WriteFile(filepath.Join(privateRoot,"identity.json"),[]byte("NODE-KEY"),0600);err!=nil{t.Fatal(err)}
	if err:=os.WriteFile(filepath.Join(workspace,"output.txt"),[]byte("PROJECT-DATA"),0600);err!=nil{t.Fatal(err)}
	ctx:=WithWorkerSandbox(context.Background(),WorkerSandbox{ProtectedDir:privateRoot,Workspace:workspace})
	cmd,err:=CommandContext(ctx,"/bin/sh","-c","test ! -r \"$1\" && test \"$(cat \"$2\")\" = PROJECT-DATA","worker",
		filepath.Join(privateRoot,"identity.json"),filepath.Join(workspace,"output.txt"))
	if err!=nil{t.Fatal(err)}
	out,err:=cmd.CombinedOutput()
	if err!=nil{t.Fatalf("nested Workspace was not remounted safely: %v %s",err,out)}
}

func TestT09LocalBinCLIReadsSelectedLocalShareAssets(t *testing.T) {
	base:=t.TempDir()
	t.Setenv("HOME",base)
	nodeDir:=filepath.Join(base,"node-private")
	workspace:=filepath.Join(base,"workspace")
	installBin:=filepath.Join(base,".local","bin")
	assets:=filepath.Join(base,".local","share","codex")
	for _,d:=range []string{nodeDir,workspace,installBin,assets}{
		if err:=os.MkdirAll(d,0700);err!=nil {t.Fatal(err)}
	}
	if err:=os.WriteFile(filepath.Join(nodeDir,"identity.json"),[]byte("SECRET"),0600);err!=nil{t.Fatal(err)}
	if err:=os.WriteFile(filepath.Join(assets,"model.dat"),[]byte("CLI-ASSETS-OK"),0600);err!=nil{t.Fatal(err)}
	command:=filepath.Join(installBin,"codex")
	script:="#!/bin/sh\ncat '"+filepath.Join(assets,"model.dat")+"'\n"
	if err:=os.WriteFile(command,[]byte(script),0700);err!=nil {t.Fatal(err)}
	ctx:=WithWorkerSandbox(context.Background(),WorkerSandbox{ProtectedDir:nodeDir,Workspace:workspace})
	cmd,err:=CommandContext(ctx,command)
	if err!=nil{t.Fatal(err)}
	out,err:=cmd.CombinedOutput()
	if err!=nil||!strings.Contains(string(out),"CLI-ASSETS-OK"){
		t.Fatalf("user-installed ~/.local/share native CLI runtime inaccessible: %v %s",err,out)
	}
}
