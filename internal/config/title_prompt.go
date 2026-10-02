package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func ensureDefaultTitlePrompt(base string) error {
	body, err := defaults.ReadFile("defaults/" + DefaultTitlePrompt)
	if err != nil {
		return err
	}
	path := filepath.Join(base, DefaultTitlePrompt)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("create title-generation-prompt.md: %w", err)
	}
	_, writeErr := file.Write(body)
	closeErr := file.Close()
	if writeErr != nil {
		return fmt.Errorf("write title-generation-prompt.md: %w", writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close title-generation-prompt.md: %w", closeErr)
	}
	return nil
}
