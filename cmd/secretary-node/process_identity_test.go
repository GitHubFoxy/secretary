package main

import (
 "os"
 "os/exec"
 "path/filepath"
 "strconv"
 "strings"
 "testing"
)

func TestMAC12PublishNodeProcessIdentity(t *testing.T) {
 root := t.TempDir()
 config := filepath.Join(root,"config.json")
 if err:=os.WriteFile(config,[]byte("{}"),0600);err!=nil {t.Fatal(err)}
 publishNodeProcessIdentity(config,"enrolled-node")
 file:=config+".process"
 info,err:=os.Lstat(file)
 if err!=nil {t.Fatal(err)}
 if !info.Mode().IsRegular() || info.Mode().Perm()!=0600 {t.Fatalf("invalid snapshot mode %v",info.Mode())}
 bytes,err:=os.ReadFile(file)
 if err!=nil {t.Fatal(err)}
 lines:=strings.Split(strings.TrimSpace(string(bytes)),"\n")
 if len(lines)!=3 {t.Fatalf("snapshot has %d fields",len(lines))}
 if lines[0]!=strconv.Itoa(os.Getpid()) || lines[2]!="enrolled-node" {t.Fatalf("snapshot pid/identity mismatch")}
 start,err:=exec.Command("ps","-p",strconv.Itoa(os.Getpid()),"-o","lstart=").Output()
 if err!=nil {t.Fatal(err)}
 if lines[1]!=strings.TrimSpace(string(start)) {t.Fatal("snapshot start differs from live process")}
 if len(lines[1])==0 {t.Fatal("empty start")}
 publishNodeProcessIdentity(config,"new-enrolled-node")
 newBytes,err:=os.ReadFile(file)
 if err!=nil {t.Fatal(err)}
 if !strings.HasSuffix(string(newBytes),"\nnew-enrolled-node\n") {t.Fatal("atomic replacement missing")}
 publishNodeProcessIdentity(config,"invalid\nnode")
 refused,err:=os.ReadFile(file)
 if err!=nil {t.Fatal(err)}
 if string(refused)!=string(newBytes) {t.Fatal("invalid identity replaced snapshot")}
}
