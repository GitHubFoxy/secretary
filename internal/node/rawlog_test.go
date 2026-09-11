package node

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotatingLogKeepsBoundedFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "acp.jsonl")
	log, err := openRotatingLog(path, 5, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := log.Write([]byte("first\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := log.Write([]byte("second\n")); err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	rotated, err := os.ReadFile(path + ".1")
	if err != nil || !strings.Contains(string(rotated), "first") {
		t.Fatalf("rotated=%q err=%v", rotated, err)
	}
	current, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(current), "second") {
		t.Fatalf("current=%q err=%v", current, err)
	}
}
