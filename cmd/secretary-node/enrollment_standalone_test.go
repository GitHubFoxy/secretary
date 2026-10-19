package main

import (
 "crypto/x509"
 "encoding/base64"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "os"
 "os/exec"
 "path/filepath"
 "strings"
 "sync/atomic"
 "testing"

 "github.com/beruseruko/secretary/internal/node"
)

func TestStandaloneEnrollmentHTTPSAndNoOverwrite(t *testing.T) {
 secret := base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("a", 32)))
 var calls atomic.Int32
 server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  if r.URL.Path != "/v1/nodes/pair" || r.Method != http.MethodPost { http.Error(w,"path",404); return }
  var req map[string]string
  if err := json.NewDecoder(r.Body).Decode(&req); err != nil { http.Error(w,"bad",400); return }
  if req["pairing_token"] != "owner-approved" { http.Error(w,"unauthorized",403); return }
  calls.Add(1)
  w.Header().Set("Content-Type","application/json")
  w.WriteHeader(http.StatusCreated)
  _ = json.NewEncoder(w).Encode(map[string]string{"node":"fixture-node","credential":secret,"connect_url":"wss://node.test/v1/nodes/connect"})
 }))
 defer server.Close()
 certFile := filepath.Join(t.TempDir(),"ca.pem")
 cert := server.TLS.Certificates[0].Certificate[0]
 parsed,err := x509.ParseCertificate(cert)
 if err != nil { t.Fatal(err) }
 _ = parsed
 pemData := "-----BEGIN CERTIFICATE-----\n"+base64.StdEncoding.EncodeToString(cert)+"\n-----END CERTIFICATE-----\n"
 if err := os.WriteFile(certFile, []byte(pemData),0600); err != nil { t.Fatal(err) }
 bin := filepath.Join(t.TempDir(),"node")
 build := exec.Command("go","build","-o",bin,".")
 if out,err:=build.CombinedOutput(); err!=nil { t.Fatalf("build test executable: %v %s",err,out) }
 dir:=t.TempDir()
 run:=func() error {
  command:=exec.Command(bin,"--enroll-only","--server",server.URL,"--data-dir",dir,"--name","fixture-node")
  command.Env=append(os.Environ(),"SSL_CERT_FILE="+certFile,"SECRETARY_NODE_PAIRING_TOKEN=owner-approved")
  out,err:=command.CombinedOutput()
  if err!=nil { t.Logf("enroll output: %s",out) }
  return err
 }
 // Exercise the real one-shot executable with a TLS endpoint.
 serverURL:=server.URL
 cmd:=exec.Command(bin,"--enroll-only","--server",serverURL,"--data-dir",dir,"--name","fixture-node")
 cmd.Env=append(os.Environ(),"SSL_CERT_FILE="+certFile,"SECRETARY_NODE_PAIRING_TOKEN=owner-approved")
 if out,err:=cmd.CombinedOutput(); err!=nil { t.Fatalf("first HTTPS enrollment: %v %s",err,out) }
 identity,err:=node.LoadNodeIdentity(filepath.Join(dir,"identity.json"))
 if err!=nil { t.Fatal(err) }
 if identity.Node!="fixture-node" || identity.Credential!=secret { t.Fatalf("wrong persisted credentials: %+v",identity.Node) }
 if calls.Load()!=1 { t.Fatalf("expected one approved pairing, got %d",calls.Load()) }
 if err:=run(); err==nil { t.Fatal("re-enrollment overwrote identity") }
 if calls.Load()!=1 { t.Fatal("duplicate pairing request was sent") }
 if info,err:=os.Stat(filepath.Join(dir,"identity.json")); err!=nil || info.Mode().Perm()!=0600 { t.Fatalf("identity permissions: %v %v",info,err) }
}
