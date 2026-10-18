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


func TestT09CLIAssetManifestExcludesIndependentServerJSONAndExtensionless(t *testing.T) {
 root:=t.TempDir()
 node:=filepath.Join(root,"node-private")
 workspace:=filepath.Join(root,"workspace")
 bin:=filepath.Join(root,".local","bin")
 assets:=filepath.Join(root,".local","share","codex")
 for _,dir:=range []string{node,workspace,bin,assets} {if err:=os.MkdirAll(dir,0700);err!=nil{t.Fatal(err)}}
 for name,content:=range map[string]string{
  "model.dat":"ASSET-OK",
  "runtime.js":"module.exports = true",
  "server-secret.json":"SERVER-HMAC-JSON",
   "secret.dat":"SERVER-HMAC-DAT",
   "secret.txt":"SERVER-HMAC-TXT",
  "transport-token":"SERVER-HMAC-NO-EXT",
 } { if err:=os.WriteFile(filepath.Join(assets,name),[]byte(content),0600);err!=nil{t.Fatal(err)} }
 executable:=filepath.Join(bin,"codex")
 if err:=os.WriteFile(executable,[]byte("#!/bin/sh\ncat "+filepath.Join(assets,"model.dat")+"\n"),0700);err!=nil{t.Fatal(err)}
 t.Setenv("HOME",root)
 _,args,err:=sandboxInvocation(WorkerSandbox{ProtectedDir:node,Workspace:workspace},executable,nil)
 if err!=nil{t.Fatal(err)}
 mounts:=map[string]bool{}
 for i:=0;i+2<len(args);i++ {if args[i]=="--ro-bind"{mounts[args[i+2]]=true}}
 for _,name:=range []string{"model.dat","runtime.js"} {
  if !mounts[filepath.Join(assets,name)] {t.Fatalf("runtime asset %q not mounted",name)}
 }
 for _,name:=range []string{"server-secret.json","transport-token","secret.dat","secret.txt"} {
  if mounts[filepath.Join(assets,name)] {t.Fatalf("Server secret %q exposed to Worker",name)}
 }
}

func TestT09NodeModulesPackageExcludesServerJSONAndExtensionless(t *testing.T) {
 root:=t.TempDir()
 packageDir:=filepath.Join(root,"node_modules","@openai","codex")
 if err:=os.MkdirAll(packageDir,0700);err!=nil{t.Fatal(err)}
 for name:=range map[string]bool{"index.js":true,"secret.json":true,"secret":true,"secret.dat":true,"secret.txt":true,"model.dat":true} {
  if err:=os.WriteFile(filepath.Join(packageDir,name),[]byte(name),0600);err!=nil{t.Fatal(err)}
 }
 args:=[]string{}
 if err:=mountRuntimePackageFiles(&args,packageDir);err!=nil{t.Fatal(err)}
 seen:=map[string]bool{}
 for i:=0;i+2<len(args);i++ {if args[i]=="--ro-bind"{seen[args[i+2]]=true}}
 if !seen[filepath.Join(packageDir,"index.js")] {t.Fatal("missing package JS runtime")}
 if !seen[filepath.Join(packageDir,"model.dat")] {t.Fatal("declared model.dat runtime asset missing")}
 for _,name:=range []string{"secret.json","secret","secret.dat","secret.txt"} {
  if seen[filepath.Join(packageDir,name)] {t.Fatalf("unexpected secret mount: %s",name)}
 }
}
