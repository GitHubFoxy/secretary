package telegram

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type ASRTranscriber interface {
	Transcribe(ctx context.Context, audioBytes []byte) (string, error)
}

type NemoASR struct {
	EnginePath      string
	ModelPath       string
	Timeout         time.Duration
	FFmpegPath      string
	CommandRun      func(ctx context.Context, name string, args ...string) ([]byte, error)
	MaxAudioSeconds int
	TempRoot        string
	slots           chan struct{}
}

func NewNemoASR(enginePath, modelPath string, timeout time.Duration) *NemoASR {
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	if enginePath == "" || enginePath == "nemo-speech" {
		if _, err := os.Stat("/home/coder/.local/bin/nemo-speech"); err == nil {
			enginePath = "/home/coder/.local/bin/nemo-speech"
		} else if p, err := exec.LookPath("nemo-speech"); err == nil {
			enginePath = p
		} else {
			enginePath = "nemo-speech"
		}
	}
	if modelPath == "" {
		if _, err := os.Stat("/home/coder/.local/share/nemo-speech/models/parakeet-tdt-0.6b-v3.q8_0.gguf"); err == nil {
			modelPath = "/home/coder/.local/share/nemo-speech/models/parakeet-tdt-0.6b-v3.q8_0.gguf"
		}
	}
	ffmpegPath := "ffmpeg"
	if p, err := exec.LookPath("ffmpeg"); err == nil {
		ffmpegPath = p
	}
	return &NemoASR{
		EnginePath:      enginePath,
		ModelPath:       modelPath,
		Timeout:         timeout,
		FFmpegPath:      ffmpegPath,
		MaxAudioSeconds: 120, slots: make(chan struct{}, 1),
	}
}

func (n *NemoASR) runCmd(ctx context.Context, name string, args ...string) ([]byte, error) {
	if n.CommandRun != nil {
		return n.CommandRun(ctx, name, args...)
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), "OMP_NUM_THREADS=2", "OPENBLAS_NUM_THREADS=2")
	return cmd.Output()
}

func (n *NemoASR) Transcribe(ctx context.Context, audioBytes []byte) (string, error) {
	if len(audioBytes) == 0 {
		return "", errors.New("asr: audio data is empty")
	}

	select {
	case n.slots <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	defer func() { <-n.slots }()

	timeoutCtx, cancel := context.WithTimeout(ctx, n.Timeout)
	defer cancel()

	tempDir, err := os.MkdirTemp(n.TempRoot, "secretary-asr-")
	if err != nil {
		return "", fmt.Errorf("asr: create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	inputPath := filepath.Join(tempDir, "voice.oga")
	if err := os.WriteFile(inputPath, audioBytes, 0o600); err != nil {
		return "", fmt.Errorf("asr: write input audio: %w", err)
	}

	wavPath := filepath.Join(tempDir, "voice.wav")

	ffmpegArgs := []string{"-nostdin", "-loglevel", "error", "-y", "-threads", "1", "-i", inputPath, "-t", fmt.Sprint(n.MaxAudioSeconds + 1), "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", wavPath}
	if _, err := n.runCmd(timeoutCtx, n.FFmpegPath, ffmpegArgs...); err != nil {
		return "", fmt.Errorf("asr: ffmpeg conversion failed: %w", err)
	}

	if duration, err := wavDuration(wavPath); err != nil {
		return "", err
	} else if duration > float64(n.MaxAudioSeconds) {
		return "", errors.New("asr: audio exceeds duration limit")
	}
	nemoArgs := []string{"--quiet", "transcribe", wavPath, "--model", n.ModelPath, "--device", "cpu"}
	output, err := n.runCmd(timeoutCtx, n.EnginePath, nemoArgs...)
	if err != nil {
		if errors.Is(timeoutCtx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("asr: nemo-speech timed out after %v", n.Timeout)
		}
		return "", fmt.Errorf("asr: nemo-speech execution failed: %w", err)
	}

	text := strings.TrimSpace(string(output))
	if text == "" {
		return "", errors.New("asr: empty transcription result")
	}
	return text, nil
}

func (n *NemoASR) Validate() error {
	for _, name := range []string{n.EnginePath, n.FFmpegPath} {
		if _, err := exec.LookPath(name); err != nil {
			return fmt.Errorf("asr: required executable unavailable: %s", filepath.Base(name))
		}
	}
	f, err := os.Open(n.ModelPath)
	if err != nil {
		return errors.New("asr: model weights are unreadable")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return errors.New("asr: model weights must be a non-empty regular file")
	}
	return nil
}

func wavDuration(path string) (float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	header := make([]byte, 12)
	if _, err = io.ReadFull(f, header); err != nil {
		return 0, err
	}
	if string(header[:4]) != "RIFF" || string(header[8:]) != "WAVE" {
		return 0, errors.New("asr: invalid WAV")
	}
	var rate uint32
	for {
		chunk := make([]byte, 8)
		if _, err := io.ReadFull(f, chunk); err != nil {
			return 0, errors.New("asr: missing WAV data")
		}
		size := binary.LittleEndian.Uint32(chunk[4:])
		if string(chunk[:4]) == "fmt " {
			if size < 16 {
				return 0, errors.New("asr: invalid WAV format")
			}
			format := make([]byte, 16)
			if _, err := io.ReadFull(f, format); err != nil {
				return 0, err
			}
			rate = binary.LittleEndian.Uint32(format[8:12])
			size -= 16
		} else if string(chunk[:4]) == "data" {
			if rate == 0 {
				return 0, errors.New("asr: invalid WAV byte rate")
			}
			return float64(size) / float64(rate), nil
		}
		if _, err := f.Seek(int64(size)+int64(size%2), io.SeekCurrent); err != nil {
			return 0, err
		}
	}
}
