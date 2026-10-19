//go:build windows

package main

import (
 "strings"
 "testing"
)

func TestW03NodeDataDirectoryExclusiveOwnership(t *testing.T) {
 root:=t.TempDir()
 release,err:=claimNodeInstance(root)
 if err!=nil {t.Fatal(err)}
 _,err=claimNodeInstance(root)
 if err==nil||!strings.Contains(err.Error(),"already active"){t.Fatalf("duplicate Node accepted: %v",err)}
 release()
 second,err:=claimNodeInstance(root)
 if err!=nil{t.Fatalf("Node lock not released: %v",err)}
 second()
}
