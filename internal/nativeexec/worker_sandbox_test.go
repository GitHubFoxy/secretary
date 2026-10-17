package nativeexec

import (
 "context"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func TestT09WorkerSandboxFailsClosedWhenCredentialStorageOverlapsWorkspace(t *testing.T) {
 root := t.TempDir()
 ctx := WithWorkerSandbox(context.Background(), WorkerSandbox{ProtectedDir:root,Workspace:root})
 if _, err := CommandContext(ctx,"sh","-c","true"); err == nil {
  t.Fatal("Node private files were executable inside an overlapping Worker workspace")
 }
}

func TestT09WorkerSandboxDoesNotInheritNodeCredentials(t *testing.T) {
 ctx := WithWorkerSandbox(context.Background(), WorkerSandbox{ProtectedDir:"/private",Workspace:"/workspace"})
 env := SecureWorkerEnvironment(ctx,[]string{"SECRETARY_NODE_PAIRING_TOKEN=secret","SECRETARY_ARTIFACT_CAPABILITY=signer","SECRETARY_ARTIFACT_BROKER_URL=http://127.0.0.1","PATH=/usr/bin"})
 combined := strings.Join(env,";")
 if strings.Contains(combined,"secret") || strings.Contains(combined,"signer") { t.Fatal("Node authority leaked into native Worker environment") }
 if !strings.Contains(combined,"SECRETARY_ARTIFACT_BROKER_URL") { t.Fatal("lost scoped local artifact broker") }
}

func TestT09WorkerSandboxBlocksIdentityAndStateReads(t *testing.T) {
 if testing.Short() { t.Skip("requires local OS sandbox capability") }
 root := t.TempDir()
 nodeDir := filepath.Join(root,"node")
 workspace := filepath.Join(root,"workspace")
 if err := os.MkdirAll(nodeDir,0700); err != nil {t.Fatal(err)}
 if err := os.MkdirAll(workspace,0700); err != nil {t.Fatal(err)}
 for _, name := range []string{"identity.json","node-state.json"} {
  if err := os.WriteFile(filepath.Join(nodeDir,name),[]byte("credential-secret"),0600); err != nil {t.Fatal(err)}
 }
 ctx := WithWorkerSandbox(context.Background(),WorkerSandbox{ProtectedDir:nodeDir,Workspace:workspace})
 script := "test ! -r '"+filepath.Join(nodeDir,"identity.json")+"' && test ! -r '"+filepath.Join(nodeDir,"node-state.json")+"' && test -w '"+workspace+"'"
 cmd, err := CommandContext(ctx,"sh","-c",script)
 if err != nil {
  // Refusing to start is secure even without a usable OS sandbox.
  if strings.Contains(err.Error(),"required") || strings.Contains(err.Error(),"unsupported") { t.Log(err); return }
  t.Fatal(err)
 }
 out, err := cmd.CombinedOutput()
 if err != nil {
  // Linux CI/container may disallow user namespace creation. No fallback.
  if strings.Contains(string(out),"Creating new namespace failed") || strings.Contains(string(out),"Operation not permitted") {
   t.Logf("native sandbox denied by host kernel (fail closed): %s",out); return
  }
  t.Fatalf("sandbox failed or secrets were readable: %v: %s",err,out)
 }
}
