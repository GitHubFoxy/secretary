package node

import (
 "encoding/json"
 "fmt"
 "sort"
 "strings"
)

func codexACPCommandExit(payload map[string]any, activity Activity) (int, bool) {
 if activity.CallID == "" || activity.Tool != "exec_command" { return 0, false }
 meta, _ := payload["_meta"].(map[string]any)
 terminal, _ := meta["terminal_exit"].(map[string]any)
 if terminal != nil {
  id, _ := terminal["terminal_id"].(string)
  if id != activity.CallID { return 0, false }
  if n, ok := terminal["exit_code"].(float64); ok && n >= 0 && n <= 255 && n == float64(int(n)) { return int(n), true }
 }
 raw, _ := payload["rawOutput"].(map[string]any)
 if raw != nil {
  if n, ok := raw["exit_code"].(float64); ok && n >= 0 && n <= 255 && n == float64(int(n)) { return int(n), true }
 }
 return 0, false
}

func codexExplicitRecovery(summary string, failures map[string]int) ([]string, string, bool) {
 const delimiter = "\n<secretary-recovery>"
 index := strings.LastIndex(summary, delimiter)
 if index < 0 || !strings.HasSuffix(strings.TrimSpace(summary), "</secretary-recovery>") { return nil, "", false }
 suffix := strings.TrimSpace(summary[index+len(delimiter):])
 suffix = strings.TrimSuffix(suffix, "</secretary-recovery>")
 var claim struct { Recovered []string `json:"recovered_tool_use_ids"` }
 if json.Unmarshal([]byte(strings.TrimSpace(suffix)), &claim) != nil || len(claim.Recovered) != len(failures) { return nil, "", false }
 seen := map[string]bool{}
 for _, id := range claim.Recovered {
  if seen[id] { return nil, "", false }
  if _, ok := failures[id]; !ok { return nil, "", false }
  seen[id] = true
 }
 userText := strings.TrimSpace(summary[:index])
 if userText == "" { return nil, "", false }
 return claim.Recovered, userText, true
}

func codexCommandFailureDetails(failures map[string]int) string {
 ids := make([]string, 0, len(failures))
 for id := range failures { ids = append(ids, id) }
 sort.Strings(ids)
 parts := make([]string, 0, len(ids))
 for _, id := range ids { parts = append(parts, fmt.Sprintf("%s: exit code %d", id, failures[id])) }
 return "Codex native command failed: " + strings.Join(parts, "; ")
}

func codexRecoveryText(summary string) (string, bool) {
 const start = "<secretary-recovery>"
 const minimum = "<secretary-"
 index := strings.LastIndex(summary, "\n")
 for index >= 0 {
  candidate := strings.TrimSpace(summary[index+1:])
  if strings.HasPrefix(candidate, start) || (len(candidate) >= len(minimum) && strings.HasPrefix(start, candidate)) {
   return strings.TrimSpace(summary[:index]), true
  }
  index = strings.LastIndex(summary[:index], "\n")
 }
 return summary, false
}
