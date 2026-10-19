package node

import (
 "errors"
 "strings"
 "testing"
)

func TestCodexACPProviderFailureClassification(t *testing.T) {
 fixtures:=[]struct{response,category string}{
  {"rpc -32000: unauthorized: invalid api key","authentication"},
  {"rpc -32000: not logged in","authentication"},
  {"rpc -32000: quota exceeded","quota/provider"},
  {"rpc -32000: rate limit exceeded","quota/provider"},
  {"rpc -32000: upstream service unavailable (503)","provider outage"},
  {"rpc -32601: method not found","protocol/transport"},
 }
 for _, fixture:=range fixtures {
  t.Run(fixture.category+"_"+fixture.response,func(t *testing.T){
   err:=classifyCodexACPFailure("session/new",errors.New(fixture.response))
   if !strings.Contains(err.Error(),fixture.category) || !strings.Contains(err.Error(),"session/new") {t.Fatalf("unexpected classification: %v",err)}
  })
 }
}
