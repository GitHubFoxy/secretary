package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

type ImageSourceRejection struct {
	Reason string
}

func (e *ImageSourceRejection) Error() string { return e.Reason }

func (c *HTTPServerClient) StoreImageOriginal(ctx context.Context, updateID int64, message Message, name string, data []byte) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/v1/internal/telegram/image/source", bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.Credential)
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-Image-Update-ID", strconv.FormatInt(updateID, 10))
	req.Header.Set("X-Image-Chat-ID", strconv.FormatInt(message.ChatID, 10))
	req.Header.Set("X-Image-Message-ID", strconv.FormatInt(message.MessageID, 10))
	req.Header.Set("X-Image-Name", name)
	req.Header.Set("X-Image-Mime-Type", message.Image.MimeType)
	req.Header.Set("X-Image-Unique-ID", message.Image.FileUniqueID)
	res, err := c.client().Do(req)
	if err != nil {
		return "", fmt.Errorf("telegram: image Artifact upload failed: %w", err)
	}
	defer res.Body.Close()
	limited, err := io.ReadAll(io.LimitReader(res.Body, maxServerErrorBody+1))
	if err != nil {
		return "", err
	}
	if len(limited) > maxServerErrorBody {
		return "", errors.New("telegram: invalid image upload response")
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		switch res.StatusCode {
		case http.StatusBadRequest, http.StatusConflict, http.StatusRequestEntityTooLarge, http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
			return "", &ImageSourceRejection{Reason:strings.TrimSpace(string(limited))}
		default:
			return "", fmt.Errorf("telegram: image Artifact server HTTP %d", res.StatusCode)
		}
	}
	var result struct {
		Artifact struct { ID string `json:"id"` } `json:"artifact"`
	}
	if err := json.Unmarshal(limited, &result); err != nil {
		return "", err
	}
	if !strings.HasPrefix(result.Artifact.ID, "art_") {
		return "", errors.New("telegram: invalid image Artifact ID")
	}
	return result.Artifact.ID, nil
}
