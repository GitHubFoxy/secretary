package telegram

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

func probeVoiceDuration(ctx context.Context, data []byte, name string) (float64, error) {
	if len(data) == 0 {
		return 0, errors.New("telegram: empty audio")
	}
	ext := filepath.Ext(name)
	if len(ext) > 8 {
		ext = ""
	}
	input, err := os.CreateTemp("", "secretary-voice-duration-*"+ext)
	if err != nil {
		return 0, err
	}
	defer os.Remove(input.Name())
	if err := input.Chmod(0o600); err != nil {
		input.Close()
		return 0, err
	}
	if _, err := input.Write(data); err != nil {
		input.Close()
		return 0, err
	}
	if err := input.Close(); err != nil {
		return 0, err
	}
	output, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", input.Name()).Output()
	if err != nil {
		return 0, fmt.Errorf("telegram: audio duration probe: %w", err)
	}
	duration, err := strconv.ParseFloat(strings.TrimSpace(string(output)), 64)
	if err != nil {
		return 0, fmt.Errorf("telegram: invalid audio duration: %w", err)
	}
	return duration, nil
}
