package telegram

// Document preserves Telegram's original ordinary-file identity. The T15
// ingress pipeline will resolve its file_id to a Server-owned Artifact; the
// Telegram file identifier is never used as a Node-local path.
type Document struct {
 FileID string `json:"file_id"`
 FileUniqueID string `json:"file_unique_id,omitempty"`
 FileName string `json:"file_name,omitempty"`
 MimeType string `json:"mime_type,omitempty"`
 FileSize int64 `json:"file_size,omitempty"`
}
