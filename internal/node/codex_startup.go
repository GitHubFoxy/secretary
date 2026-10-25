package node

import (
 "context"
 "errors"
 "fmt"
 "os"
 "strings"
)

func CheckCodexStartup(ctx context.Context, scope string) error {
 return checkCodexStartupWithProbe(ctx, scope, codexNativePairReady)
}

func checkCodexStartupWithProbe(ctx context.Context, scope string, probe func(context.Context, CodexPairInventory) error) error {
 pair, err := ReadCodexPairInventory(scope)
 if err != nil {
  return CodexInventoryDiagnostic(scope, err)
 }
 if err := probe(ctx, pair); err != nil {
  reason := strings.ToLower(err.Error())
  switch {
  case strings.Contains(reason, "code-mode host"):
   return errors.New("Codex MCP code-mode host unavailable in selected managed pair; reinstall the managed native pair, run secretary doctor and restart")
  case strings.Contains(reason, "authentication required"):
   return fmt.Errorf("Codex authorization required: run %s login using the selected executable, then secretary doctor and restart", pair.Codex.Path)
  case strings.Contains(reason, "quota/provider"):
   return errors.New("Codex provider quota or availability prevents startup; check the selected Codex account and retry with secretary doctor")
  case strings.Contains(reason, "provider outage"):
   return errors.New("Codex provider is unavailable; retry secretary doctor when service recovers")
  case strings.Contains(reason, "acp"), strings.Contains(reason, "protocol"):
   return errors.New("Codex ACP protocol readiness failed (initialize/session/new/session/load); verify the selected adapter, run secretary doctor, then restart")
  default:
   return errors.New("Codex runtime readiness failed (executable or login-status probe); check the selected Codex executable and run secretary doctor")
  }
 }
 return nil
}

func CodexInventoryDiagnostic(scope string, err error) error {
 if err == nil { return nil }
 reason := strings.ToLower(err.Error())
 switch {
 case strings.Contains(reason, "code-mode host"):
  return fmt.Errorf("Codex MCP code-mode host unavailable or altered for %s: reinstall the managed native pair, run secretary doctor and restart", scope)
 case strings.HasPrefix(reason, "codex acp:"):
  return fmt.Errorf("Codex ACP unavailable or altered for %s: verify the selected ACP executable, run secretary doctor and restart", scope)
 case strings.HasPrefix(reason, "codex cli:"):
  return fmt.Errorf("Codex CLI unavailable or altered for %s: verify the selected Codex executable, run secretary doctor and restart", scope)
 case errors.Is(err, os.ErrNotExist):
  return fmt.Errorf("Codex inventory missing for %s: install or configure a native pair with secretary-node --codex-pair-action install --codex-pair-scope %s, then run secretary doctor and restart", scope, scope)
 case strings.Contains(reason, "protocol has not been validated"):
  return fmt.Errorf("Codex ACP protocol validation missing for %s: run secretary-node --codex-pair-action readiness --codex-pair-scope %s, then secretary doctor and restart", scope, scope)
 default:
  return fmt.Errorf("Codex inventory invalid for %s: verify the selected native pair with secretary doctor before restart", scope)
 }
}
