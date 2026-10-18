//go:build linux

package nativeexec

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestT09UnverifiedInstalledCLIDenied(t *testing.T) {
 root:=t.TempDir()
 pkg:=filepath.Join(root,"node_modules","@openai","codex")
 if err:=os.MkdirAll(filepath.Join(pkg,"bin"),0700);err!=nil{t.Fatal(err)}
 args:=[]string{}
 if err:=mountRuntimePackageFiles(&args,pkg);err==nil {t.Fatal("unverified package accepted without provenance")}
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

func TestT09UnverifiedLocalShareDenied(t *testing.T) {
 root:=t.TempDir()
 assets:=filepath.Join(root,".local","share","codex")
 if err:=os.MkdirAll(assets,0700);err!=nil{t.Fatal(err)}
 args:=[]string{}
 if err:=mountRuntimePackageFiles(&args,assets);err==nil {t.Fatal("unsigned local CLI directory was accepted")}
}

func TestT09LinuxExplicitServerDataRootMaskedEvenInsideSharedSystemDirectory(t *testing.T) {
	root:=t.TempDir()
	workspace:=filepath.Join(root,"workspace")
	node:=filepath.Join(root,"node")
	server:=filepath.Join(root,"server")
	for _,p:=range []string{workspace,node,server}{if err:=os.MkdirAll(p,0700);err!=nil{t.Fatal(err)}}
	cmd,args,err:=sandboxInvocation(WorkerSandbox{ProtectedDir:node,ServerDataDir:server,Workspace:workspace},"/bin/sh",[]string{"-c","true"})
	if err!=nil{t.Fatal(err)}
	if cmd==""{t.Fatal("missing trusted sandbox binary")}
	seen:=false
	for i:=0;i+1<len(args);i++{
		if args[i]=="--tmpfs" && args[i+1]==server{seen=true}
		if args[i]=="--ro-bind" && (args[i+1]=="/etc" || args[i+1]=="/usr"){
			t.Fatalf("whole system secret-bearing subtree still readable by Worker: %s",args[i+1])
		}
	}
	if !seen{t.Fatal("co-located Server SQLite/HMAC state was not masked")}
}

// Independent Server/Node roots must remain isolated even without the Node
// knowing the Server's --data-dir. Production layout may put Node and Server
// under a shared system prefix, but never expose its unselected siblings.
func TestT09IndependentServerRootNotSharedWithoutServerDataDir(t *testing.T) {
  root:=t.TempDir()
  node:=filepath.Join(root,"node","data")
  workspace:=filepath.Join(root,"workspace")
  for _, p:=range []string{node,workspace}{if err:=os.MkdirAll(p,0700);err!=nil{t.Fatal(err)}}
  _,args,err:=sandboxInvocation(WorkerSandbox{ProtectedDir:node,Workspace:workspace},"/bin/sh",nil)
  if err!=nil{t.Fatal(err)}
  for i:=0;i+2<len(args);i++ {
    if args[i]=="--ro-bind" {
      root:=args[i+1]
      if root=="/usr/local/lib" || root=="/usr/local/bin" || root=="/usr/local" {
        t.Fatalf("unselected Server state exposed through broad mount %s",root)
      }
    }
  }
  // A separate server root under /usr/local/lib/secretary must not be
  // reachable when the Node was started with an unrelated data directory.
  ctx:=WithWorkerSandbox(context.Background(),WorkerSandbox{ProtectedDir:node,Workspace:workspace})
  cmd,err:=CommandContext(ctx,"/bin/sh","-c","test ! -e /usr/local/lib/secretary/secretary.db && test ! -e /usr/local/lib/secretary/hmac.key")
  if err!=nil{t.Fatal(err)}
  out,err:=cmd.CombinedOutput();if err!=nil{t.Fatalf("independent Server root leaked: %v %s",err,out)}
}

func TestT09NoBroadSystemOrCLIInstallMounts(t *testing.T) {
 base:=t.TempDir()
 node:=filepath.Join(base,"independent-node")
 ws:=filepath.Join(base,"workspace")
 bin:=filepath.Join(base,".local","bin")
 for _,dir:=range []string{node,ws,bin} { if err:=os.MkdirAll(dir,0700);err!=nil {t.Fatal(err)} }
 cli:=filepath.Join(bin,"codex")
 if err:=os.WriteFile(cli,[]byte("#!/bin/sh\necho ok\n"),0700);err!=nil {t.Fatal(err)}
 if err:=os.MkdirAll(filepath.Join(bin,"server-data"),0700);err!=nil {t.Fatal(err)}
 secret:=filepath.Join(bin,"server-data","secretary.db")
 if err:=os.WriteFile(secret,[]byte("HMAC-SECRET"),0600);err!=nil {t.Fatal(err)}
 _,args,err:=sandboxInvocation(WorkerSandbox{ProtectedDir:node,Workspace:ws},cli,nil)
 if err!=nil {t.Fatal(err)}
 banned:=[]string{"/usr/bin","/usr/lib","/usr/lib64","/usr/share/nodejs","/usr/share/zoneinfo","/usr/share/ca-certificates","/lib","/lib64","/bin",bin}
 for i:=0;i+2<len(args);i++ {
  if args[i]!="--ro-bind" && args[i]!="--bind" {continue}
  for _,root:=range banned {if args[i+1]==root {t.Fatalf("broad bind to %s exposes independent Server: %v",root,args)}}
  if args[i+1]==secret {t.Fatal("independent Server database mounted")}
 }
 // The main runtime entrypoint remains accessible as an individual file.
 found:=false
 for i:=0;i+2<len(args);i++ { if args[i]=="--ro-bind" && args[i+2]==cli {found=true} }
 if !found {t.Fatal("CLI executable not individually mounted")}
}

func TestT09SystemSecretMarkersNotReachableInSandbox(t *testing.T) {
 base:=t.TempDir()
 node:=filepath.Join(base,"node")
 ws:=filepath.Join(base,"workspace")
 for _,dir:=range []string{node,ws} {if err:=os.MkdirAll(dir,0700);err!=nil {t.Fatal(err)}}
 _,args,err:=sandboxInvocation(WorkerSandbox{ProtectedDir:node,Workspace:ws},"/bin/sh",nil)
 if err!=nil {t.Fatal(err)}
 for _,prefix:=range []string{"/usr/lib","/usr/lib64","/usr/share/nodejs","/usr/bin"} {
  for i:=0;i+2<len(args);i++ {
   if args[i]=="--ro-bind" && args[i+1]==prefix {t.Fatalf("secret marker under %s would leak via full-root bind",prefix)}
  }
 }
}

func TestT09MarkerSecretsBesideRuntimeFilesNotMounted(t *testing.T) {
 root:=t.TempDir()
 // Simulate three independent, arbitrarily located Server data directories
 // below historically shared system roots; Node data lives elsewhere.
 for _,name:=range []string{"usr/lib","usr/lib64","usr/share/nodejs"} {
  dir:=filepath.Join(root,name)
  if err:=os.MkdirAll(dir,0700);err!=nil {t.Fatal(err)}
  runtime:=filepath.Join(dir,"runtime.so")
  marker:=filepath.Join(dir,"secretary.db")
  if err:=os.WriteFile(runtime,[]byte("runtime"),0600);err!=nil{t.Fatal(err)}
  if err:=os.WriteFile(marker,[]byte("HMAC-MARKER"),0600);err!=nil{t.Fatal(err)}
  args:=[]string{}
  if err:=mountRuntimeFile(&args,runtime);err!=nil{t.Fatal(err)}
  for i:=0;i+2<len(args);i++ {
   if args[i]!="--ro-bind" {continue}
   if args[i+1]==dir || args[i+1]==marker {t.Fatalf("exposed independent Server marker under %s",name)}
  }
 }
}


func TestT09CLIMissingProvenanceCannotExposeServerSecrets(t *testing.T) {
 root:=t.TempDir()
 pkg:=filepath.Join(root,"node_modules","@openai","codex")
 if err:=os.MkdirAll(pkg,0700);err!=nil{t.Fatal(err)}
 for _,name:=range []string{"transport-secret.js","secret.wasm","model.dat"} {
   if err:=os.WriteFile(filepath.Join(pkg,name),[]byte("SECRET"),0600);err!=nil{t.Fatal(err)}
 }
 args:=[]string{}
 if err:=mountRuntimePackageFiles(&args,pkg);err==nil{t.Fatal("missing trusted package provenance accepted")}
 if len(args)!=0 {t.Fatalf("untrusted package caused mounts: %v",args)}
}

func TestT09NodeModulesRequiresVerifiedProviderVersion(t *testing.T) {
 root:=t.TempDir()
 pkg:=filepath.Join(root,"node_modules","@openai","codex")
 if err:=os.MkdirAll(pkg,0700);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(filepath.Join(pkg,"package.json"),[]byte(`{"name":"@openai/codex","version":"invalid/../version"}`),0600);err!=nil{t.Fatal(err)}
 args:=[]string{}
 if err:=mountRuntimePackageFiles(&args,pkg);err==nil{t.Fatal("unsafe CLI version accepted")}
 if len(args)!=0{t.Fatal("mounted invalid CLI package")}
}

func TestT09ExecutableLookingServerSecretsAreNotAutoMounted(t *testing.T) {
 root:=t.TempDir()
 pkg:=filepath.Join(root,"node_modules","@openai","codex")
 if err:=os.MkdirAll(pkg,0700);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(filepath.Join(pkg,"package.json"),[]byte(`{"name":"@openai/codex","version":"0.0.0-unprovisioned"}`),0600);err!=nil{t.Fatal(err)}
 for _,name:=range []string{"transport-secret.js","transport-secret.wasm","transport-secret.map","index.js"}{
  if err:=os.WriteFile(filepath.Join(pkg,name),[]byte("SECRET"),0600);err!=nil{t.Fatal(err)}
 }
 args:=[]string{}
 if err:=mountRuntimePackageFiles(&args,pkg);err==nil{t.Fatal("unprovisioned version accepted")}
 if len(args)!=0{t.Fatalf("untrusted executable-looking assets mounted: %v",args)}
}
