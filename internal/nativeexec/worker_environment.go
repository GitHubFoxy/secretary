package nativeexec

import (
 "context"
 "os"
 "strings"
)

// SecureWorkerEnvironment drops any inherited Node-authority variables before
// starting a native process. Its descendants inherit this restricted env.
func SecureWorkerEnvironment(ctx context.Context, entries []string) []string {
 if _, ok := workerSandboxFromContext(ctx); !ok { return entries }
 if entries == nil { entries = os.Environ() }
 result := make([]string,0,len(entries))
 for _, entry := range entries {
  name, _, _ := strings.Cut(entry,"=")
  switch {
  case strings.HasPrefix(name,"SECRETARY_NODE_"):
  case strings.HasPrefix(name,"SECRETARY_WORKER_"):
  case name == "SECRETARY_ARTIFACT_CAPABILITY":
  case name == "SECRETARY_ADMIN_TOKEN":
  case name == "SECRETARY_SERVER_ADMIN_TOKEN":
  case name == "SECRETARY_PAIRING_TOKEN":
  default: result = append(result,entry)
  }
 }
 return workerPlatformEnvironment(ctx, result)
}
