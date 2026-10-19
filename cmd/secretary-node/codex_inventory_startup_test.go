package main

import (
 "os"
 "testing"
)

func TestInheritedCandidateEnvironmentNeverBypassesNormalNodeInventory(t *testing.T) {
 t.Setenv("SECRETARY_CODEX_PAIR_CANDIDATE","1")
 if !enforceCodexInventory(false) { t.Fatal("normal Node runtime bypassed chosen inventory") }
 if enforceCodexInventory(true) { t.Fatal("explicit readiness probe incorrectly forced selected inventory") }
 if got:=os.Getenv("SECRETARY_CODEX_PAIR_CANDIDATE");got!="1" { t.Fatal("test environment unexpectedly mutated") }
}
