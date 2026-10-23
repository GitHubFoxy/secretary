package node

import (
 "encoding/json"
 "strings"
)

func codexNativeExecExit(arguments json.RawMessage, payload map[string]any) (int, bool) {
 var script string
 if json.Unmarshal(arguments, &script) != nil {
  var input map[string]any
  if json.Unmarshal(arguments, &input) != nil { return 0, false }
  script, _ = input["input"].(string)
  if script == "" { script, _ = input["code"].(string) }
 }
 if !strings.Contains(script, "await tools.exec_command(") || !strings.Contains(script, "text(JSON.stringify(") {
  return 0, false
 }
 for _, key := range []string{"rawOutput", "output", "content"} {
  if code, ok := codexNativeExecOutput(payload[key]); ok { return code, true }
 }
 return 0, false
}

func codexNativeExecOutput(value any) (int, bool) {
 switch item := value.(type) {
 case map[string]any:
  if n, ok := item["exit_code"].(float64); ok {
   _, chunk := item["chunk_id"].(string)
   _, output := item["output"].(string)
   if chunk && output && n >= 0 && n <= 255 && n == float64(int(n)) { return int(n), true }
  }
  for _, key := range []string{"output", "content", "result", "text"} {
   if code, ok := codexNativeExecOutput(item[key]); ok { return code, true }
  }
 case []any:
  for _, item := range item {
   if code, ok := codexNativeExecOutput(item); ok { return code, true }
  }
 case string:
  var decoded map[string]any
  if json.Unmarshal([]byte(strings.TrimSpace(item)), &decoded) == nil {
   return codexNativeExecOutput(decoded)
  }
 }
 return 0, false
}
