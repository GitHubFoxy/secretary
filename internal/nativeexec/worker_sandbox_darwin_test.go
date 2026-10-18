//go:build darwin

package nativeexec

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestT09SeatbeltProtectsAlternateNodeDataRoots(t *testing.T) {
	workspace:=t.TempDir()
	for _,root:=range []string{"/usr","/System","/Library","/Applications","/private/etc","/private/var/db","/private/tmp","/tmp","/opt/homebrew"} {
		t.Run(strings.ReplaceAll(root,"/","_"),func(t *testing.T){
			if _,err:=os.Stat(root);err!=nil{t.Skip("system root not present")}
			command,args,err:=sandboxInvocation(WorkerSandbox{ProtectedDir:root,Workspace:workspace},"/bin/sh",[]string{"-c","true"})
			if err!=nil{t.Logf("secure fail-closed Node data root: %v",err);return}
			if !strings.HasSuffix(command,"sandbox-exec"){t.Fatalf("unprotected Worker command: %s",command)}
			if len(args)<2{t.Fatal("Seatbelt profile omitted")}
			profile:=args[1]
			rootPath,err:=filepath.EvalSymlinks(root)
			if err!=nil{t.Fatal(err)}
			exposed:=false
			// Only actual read grants can expose this protected directory;
			// previously hardcoded roots may now be default-denied outright.
			for _,allow:=range []string{"/usr/bin","/usr/lib","/bin","/sbin","/System","/private/var/db/dyld","/private/etc/ssl","/dev"} {
				allowPath,err:=filepath.EvalSymlinks(allow)
				if err!=nil{continue}
				if (rootPath==allowPath||strings.HasPrefix(rootPath,allowPath+string(filepath.Separator))) &&
					strings.Contains(profile,"(allow file-read* (subpath \\""+allow+"\\"))") {exposed=true}
			}
			if exposed && !strings.Contains(profile,"(deny file-read* (subpath "){
				t.Fatalf("alternate Node root %s exposed by system allowlist",root)
			}
			if strings.Contains(profile,"(allow file-read* (subpath \"/private/tmp\"))") ||
				strings.Contains(profile,"(allow file-read* (subpath \"/tmp\"))"){
				t.Fatal("host-wide temporary files exposed to Worker")
			}
		})
	}
}

func TestT09DarwinDeniedNodeCredentialsForPrivateTempDataDir(t *testing.T) {
	root:=t.TempDir()
	protected:=filepath.Join(root,"node")
	workspace:=filepath.Join(root,"workspace")
	if err:=os.MkdirAll(protected,0700);err!=nil{t.Fatal(err)}
	if err:=os.MkdirAll(workspace,0700);err!=nil{t.Fatal(err)}
	if err:=os.WriteFile(filepath.Join(protected,"identity.json"),[]byte("private"),0600);err!=nil{t.Fatal(err)}
	ctx:=WithWorkerSandbox(context.Background(),WorkerSandbox{ProtectedDir:protected,Workspace:workspace})
	cmd,err:=CommandContext(ctx,"/bin/sh","-c","test ! -r \"$1\"","t09",filepath.Join(protected,"identity.json"))
	if err!=nil{t.Fatal(err)}
	output,err:=cmd.CombinedOutput()
	if err!=nil{t.Fatalf("Worker escaped Node data dir: %v %s",err,output)}
}


func TestT09SeatbeltDoesNotGrantBroadThirdPartySystemRoots(t *testing.T) {
 root:=t.TempDir()
 workspace:=filepath.Join(root,"workspace")
 node:=filepath.Join(root,"node")
 if err:=os.MkdirAll(workspace,0700);err!=nil{t.Fatal(err)}
 if err:=os.MkdirAll(node,0700);err!=nil{t.Fatal(err)}
 _,args,err:=sandboxInvocation(WorkerSandbox{ProtectedDir:node,Workspace:workspace},"/bin/sh",nil)
 if err!=nil{t.Fatal(err)}
 profile:=args[1]
 for _,root:=range []string{"/Applications","/Library/Frameworks","/Library/Developer","/opt/homebrew/lib","/opt/homebrew/bin"} {
   if strings.Contains(profile,"(allow file-read* (subpath \""+root+"\"))") {
     t.Fatalf("broad Seatbelt grant exposes co-located Server files: %s",root)
   }
 }
}
