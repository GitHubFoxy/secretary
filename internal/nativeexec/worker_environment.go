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
  // The trusted Node may be co-located with secretaryd. Do not inherit
  // any Server/Node token, bootstrap credential, transport key or password.
  // Only explicit, Worker-authorized MCP configuration crosses the boundary.
  case strings.HasPrefix(name,"SECRETARY_") &&
       name != "SECRETARY_ARTIFACT_BROKER_URL" &&
       !strings.HasPrefix(name,"SECRETARY_CLAUDE_MCP_"):
  default: result = append(result,entry)
  }
 }
 return workerPlatformEnvironment(ctx, result)
}
