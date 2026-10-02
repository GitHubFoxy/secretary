package telegram

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"strings"
	"time"
)

func runOpenCodeTitleCommand(ctx context.Context, request titleCommand) ([]byte, error) {
	command := exec.CommandContext(ctx, "opencode", request.Arguments...)
	command.Dir = request.Directory
	command.Env = request.Environment
	command.Stdin = strings.NewReader(request.Input)
	command.Stderr = io.Discard
	command.WaitDelay = 300 * time.Millisecond
	waitForTermination := configureTitleProcess(command)
	var output limitedTitleOutput
	command.Stdout = &output
	err := command.Run()
	waitForTermination()
	if err != nil || output.exceeded {
		return nil, errors.New("telegram: title harness failed")
	}
	return output.Bytes(), nil
}

type limitedTitleOutput struct {
	buffer   bytes.Buffer
	exceeded bool
}

func (b *limitedTitleOutput) Bytes() []byte { return b.buffer.Bytes() }

func (b *limitedTitleOutput) Write(data []byte) (int, error) {
	if b.buffer.Len()+len(data) > 32*1024 {
		b.exceeded = true
		return 0, errors.New("telegram: title output limit exceeded")
	}
	return b.buffer.Write(data)
}
