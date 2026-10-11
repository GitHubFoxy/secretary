package telegram

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeTestWAV(path string, seconds int) error {
	b := make([]byte, 44+32000*seconds)
	copy(b, "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(len(b)-8))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1)
	binary.LittleEndian.PutUint16(b[22:], 1)
	binary.LittleEndian.PutUint32(b[24:], 16000)
	binary.LittleEndian.PutUint32(b[28:], 32000)
	binary.LittleEndian.PutUint16(b[32:], 2)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], uint32(len(b)-44))
	return os.WriteFile(path, b, 0600)
}
func TestNemoConversionDurationCleanupAndArgs(t *testing.T) {
	for _, duration := range []int{1, 121} {
		t.Run(map[int]string{1: "success", 121: "too-long"}[duration], func(t *testing.T) {
			n := NewNemoASR("nemo-speech", "model.gguf", time.Second)
			n.TempRoot = t.TempDir()
			calls := 0
			n.CommandRun = func(_ context.Context, name string, args ...string) ([]byte, error) {
				calls++
				if name == n.FFmpegPath {
					if !strings.Contains(strings.Join(args, " "), "-ac 1 -ar 16000 -c:a pcm_s16le") {
						t.Fatalf("conversion args=%v", args)
					}
					return nil, writeTestWAV(args[len(args)-1], duration)
				}
				if strings.Join(args[len(args)-2:], " ") != "--device cpu" {
					t.Fatal("wrong engine device")
				}
				return []byte("Распознанный текст.\n"), nil
			}
			text, err := n.Transcribe(context.Background(), []byte("ogg"))
			if duration == 1 {
				if err != nil || text != "Распознанный текст." || calls != 2 {
					t.Fatalf("text=%q err=%v calls=%d", text, err, calls)
				}
			} else if err == nil || calls != 1 {
				t.Fatalf("duration not checked: %v calls=%d", err, calls)
			}
			entries, _ := os.ReadDir(n.TempRoot)
			if len(entries) != 0 {
				t.Fatal("temporary audio leaked")
			}
		})
	}
}
func TestNemoTimeoutAndCanceledQueue(t *testing.T) {
	n := NewNemoASR("nemo-speech", "model", 20*time.Millisecond)
	n.TempRoot = t.TempDir()
	n.CommandRun = func(ctx context.Context, _ string, _ ...string) ([]byte, error) { <-ctx.Done(); return nil, ctx.Err() }
	_, err := n.Transcribe(context.Background(), []byte("ogg"))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout=%v", err)
	}
	n.slots <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = n.Transcribe(ctx, []byte("ogg"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("queued cancel=%v", err)
	}
	<-n.slots
	entries, _ := filepath.Glob(filepath.Join(n.TempRoot, "*"))
	if len(entries) != 0 {
		t.Fatal("timeout leaked files")
	}
}
