package node

import (
 "errors"
 "strings"
 "testing"

 "github.com/beruseruko/secretary/internal/acp"
)

func TestCodexACPModelCacheDiagnosticKeepsOriginalReasonAndTimeout(t *testing.T) {
 var stderr codexACPStderr
 // A long unrelated start-up log must not suppress the late model-cache error.
 stderr.Write([]byte(strings.Repeat("unrelated startup log\n", 1400)))
 stderr.Write([]byte("secret=/tmp/should-not-expose: failed to load models cache: unknown variant max, expected one of none, minimal, low, medium, high, xhigh\n"))
 failure:=classifyCodexACPFailure("session/new",errors.New("context deadline exceeded"))
 diag:=stderr.diagnostic()
 if !strings.Contains(diag,"failed to load models cache: unknown variant max") || !strings.Contains(diag,"xhigh") {
  t.Fatalf("lost schema failure: %q",diag)
 }
 if strings.Contains(diag,"secret=") {t.Fatalf("diagnostic leaked stderr prefix: %q",diag)}
 if !strings.Contains(failure.Error(),"session/new") || !strings.Contains(failure.Error(),"context deadline exceeded") {t.Fatalf("lost timeout: %v",failure)}
}

func TestCodexACPProviderFailureClassification(t *testing.T) {
 fixtures:=[]struct{
  name string
  err error
  category string
 }{
  {"auth structured status", &acp.RPCError{Code:-32000,Message:"provider denied",Data:[]byte(`{"status":401}`)}, "authentication"},
  {"quota structured status", &acp.RPCError{Code:-32000,Message:"limit",Data:[]byte(`{"statusCode":429}`)}, "quota/provider"},
  {"outage structured status", &acp.RPCError{Code:-32000,Message:"offline",Data:[]byte(`{"httpStatus":503}`)}, "provider outage"},
  {"auth structured type", &acp.RPCError{Code:-32000,Message:"denied",Data:[]byte(`{"type":"unauthorized"}`)}, "authentication"},
  {"quota structured code", &acp.RPCError{Code:-32000,Message:"denied",Data:[]byte(`{"code":"resource_exhausted"}`)}, "quota/provider"},
  {"quota structured hyphenated type", &acp.RPCError{Code:-32000,Message:"denied",Data:[]byte(`{"type":"rate-limit"}`)}, "quota/provider"},
  {"unrelated structured code", &acp.RPCError{Code:-32000,Message:"401 found in payload",Data:[]byte(`{"code":"invalid_protocol_frame"}`)}, "protocol/transport"},
  {"auth provider prefix", &acp.RPCError{Code:-32000,Message:"unauthorized: invalid api key"}, "authentication"},
  {"quota provider prefix", &acp.RPCError{Code:-32000,Message:"quota exceeded"}, "quota/provider"},
  {"outage provider prefix", &acp.RPCError{Code:-32000,Message:"upstream service unavailable (503)"}, "provider outage"},
  {"method not found containing 401", &acp.RPCError{Code:-32601,Message:"method 401 not found"}, "protocol/transport"},
  {"invalid params containing 503", &acp.RPCError{Code:-32602,Message:"bad field 503"}, "protocol/transport"},
  {"opaque provider error mentioning 403", &acp.RPCError{Code:-32000,Message:"unexpected token 403 in ACP schema"}, "protocol/transport"},
  {"plain protocol error mentioning quota", errors.New("protocol broken around quota exceeded"), "protocol/transport"},
  {"plain protocol error mentioning 502", errors.New("ACP missing 502 field"), "protocol/transport"},
 }
 for _, fixture:=range fixtures {
  t.Run(fixture.name,func(t *testing.T){
   result:=classifyCodexACPFailure("session/new",fixture.err)
   if !strings.Contains(result.Error(),fixture.category) || !strings.Contains(result.Error(),"session/new") {t.Fatalf("unexpected classification: %v",result)}
  })
 }
}
