package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type TextFormatter interface {
	Format(ctx context.Context, rawText string) (string, error)
}

type OpenAIFormatter struct {
	Enabled    bool
	Model      string
	Reasoning  string
	Prompt     string
	APIKey     string
	APIKeyEnv  string
	Timeout    time.Duration
	BaseURL    string
	HTTPClient *http.Client
}

func (f *OpenAIFormatter) client() *http.Client {
	if f.HTTPClient != nil {
		return f.HTTPClient
	}
	return http.DefaultClient
}

func (f *OpenAIFormatter) Format(ctx context.Context, rawText string) (string, error) {
	if !f.Enabled {
		return rawText, nil
	}
	rawText = strings.TrimSpace(rawText)
	if rawText == "" {
		return "", errors.New("formatter: input text is empty")
	}

	if strings.TrimSpace(f.Model) == "" {
		return "", errors.New("formatter: model is not configured")
	}
	if strings.TrimSpace(f.Prompt) == "" {
		return "", errors.New("formatter: prompt is empty")
	}
	if f.Reasoning != "" && f.Reasoning != "none" {
		return "", errors.New("formatter: only reasoning none is supported")
	}

	key := f.APIKey
	if key == "" && f.APIKeyEnv != "" {
		key = strings.TrimSpace(os.Getenv(f.APIKeyEnv))
	}
	if key == "" {
		return "", errors.New("formatter: api key is not set")
	}

	timeout := f.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	baseURL := strings.TrimRight(f.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	endpoint := baseURL + "/chat/completions"

	model := f.Model

	type chatMessage struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	type chatRequest struct {
		Model           string        `json:"model"`
		Messages        []chatMessage `json:"messages"`
		ReasoningEffort string        `json:"reasoning_effort,omitempty"`
	}

	reqBody := chatRequest{
		Model: model,
		Messages: []chatMessage{
			{Role: "system", Content: f.Prompt},
			{Role: "user", Content: rawText},
		},
	}
	reqBody.ReasoningEffort = "none"

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("formatter: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(timeoutCtx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", errors.New("formatter: invalid API endpoint")
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+key)

	resp, err := f.client().Do(httpReq)
	if err != nil {
		if errors.Is(timeoutCtx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("formatter: request timed out after %v", timeout)
		}
		return "", errors.New("formatter: request failed")
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024+1))
	if err != nil {
		return "", errors.New("formatter: could not read response")
	}

	if resp.StatusCode != http.StatusOK {
		// API bodies may contain credentials or private transcript text.
		return "", fmt.Errorf("formatter: API returned HTTP status %d", resp.StatusCode)
	}
	if len(body) > 64*1024 {
		return "", errors.New("formatter: response exceeded size limit")
	}

	var chatResp struct {
		Choices []struct {
			Message struct {
				Content string  `json:"content"`
				Refusal *string `json:"refusal"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return "", errors.New("formatter: invalid API response")
	}
	if len(chatResp.Choices) == 0 {
		return "", errors.New("formatter: response contained no choices")
	}

	if chatResp.Choices[0].FinishReason != "stop" || chatResp.Choices[0].Message.Refusal != nil {
		return "", errors.New("formatter: response was incomplete or refused")
	}

	formatted := strings.TrimSpace(chatResp.Choices[0].Message.Content)
	if err := ValidateFormatting(rawText, formatted); err != nil {
		return "", fmt.Errorf("formatter: validation failed: %w", err)
	}

	return formatted, nil
}

// These conservative checks catch detectable changes; they do not prove semantic
// equivalence. In particular, lower-case names and moved negation can escape them.
var (
	preambleRegex  = regexp.MustCompile(`(?i)^\s*(?:конечно|вот(?:\s+(?:исправленный|отформатированный))?\s+текст|отформатированный текст|расшифровка|как языковая модель|here is|certainly|sure|here's|formatted text)(?:\s|[:,.!\n]|$)`)
	wordRegex      = regexp.MustCompile(`[\p{L}\p{N}]+(?:['’][\p{L}]+)?`)
	numberRegex    = regexp.MustCompile(`\p{N}+(?:[.,:]\p{N}+)*`)
	technicalRegex = regexp.MustCompile("`[^`]+`|https?://[^\\s]+|[\\p{L}\\p{N}._%+-]+@[\\p{L}\\p{N}.-]+\\.[\\p{L}]{2,}|(?:[~./][\\p{L}\\p{N}_.-]+)+(?:/[\\p{L}\\p{N}_.-]+)*|--?[\\p{L}][\\p{L}\\p{N}_-]*|[\\p{L}\\p{N}]+(?:[_.][\\p{L}\\p{N}]+)+")
)

func wordCounts(text string) map[string]int {
	counts := make(map[string]int)
	for _, word := range wordRegex.FindAllString(text, -1) {
		counts[strings.ToLower(strings.ReplaceAll(word, "’", "'"))]++
	}
	return counts
}

func ValidateFormatting(rawText, formattedText string) error {
	formattedText = strings.TrimSpace(formattedText)
	if formattedText == "" {
		return errors.New("formatted text is empty")
	}
	if preambleRegex.MatchString(formattedText) || strings.HasPrefix(formattedText, "```") {
		return errors.New("formatted text contains preamble or wrapper")
	}
	if len([]rune(rawText)) >= 20 && len([]rune(formattedText)) < len([]rune(rawText))/4 {
		return errors.New("formatted text is excessively truncated")
	}

	rawWords, formattedWords := wordCounts(rawText), wordCounts(formattedText)
	// Preserve counts, not just existence: losing one of two negations is unsafe.
	protected := strings.Fields("не нет ни никогда нигде никак ничуть нисколько нельзя без not no never none neither nor nowhere hardly barely cannot can't don't doesn't didn't won't shouldn't wouldn't isn't aren't wasn't weren't один одна одно два две три четыре пять шесть семь восемь девять десять одиннадцать двенадцать тринадцать четырнадцать пятнадцать шестнадцать семнадцать восемнадцать девятнадцать двадцать тридцать сорок пятьдесят шестьдесят семьдесят восемьдесят девяносто сто тысяча миллион zero one two three four five six seven eight nine ten eleven twelve thirteen fourteen fifteen sixteen seventeen eighteen nineteen twenty thirty forty fifty sixty seventy eighty ninety hundred thousand million rm sudo mv cp chmod chown git curl wget ssh")
	for _, word := range protected {
		if rawWords[word] != formattedWords[word] {
			return errors.New("formatted text changed a protected word")
		}
	}
	// Capitalized tokens are the detectable subset of names; tolerate casing changes.
	for _, word := range wordRegex.FindAllString(rawText, -1) {
		first, _ := utf8.DecodeRuneInString(word)
		lower := strings.ToLower(word)
		if unicode.IsUpper(first) && formattedWords[lower] < rawWords[lower] {
			return errors.New("formatted text dropped a capitalized token")
		}
	}
	for _, re := range []*regexp.Regexp{numberRegex, technicalRegex} {
		counts := func(text string) map[string]int {
			result := make(map[string]int)
			for _, token := range re.FindAllString(text, -1) {
				// Sentence punctuation is not part of a URL or path.
				result[strings.TrimRight(token, ".,;!?")]++
			}
			return result
		}
		rawTokens, formattedTokens := counts(rawText), counts(formattedText)
		if len(rawTokens) != len(formattedTokens) {
			return errors.New("formatted text added or removed a number or technical token")
		}
		for token, count := range rawTokens {
			if formattedTokens[token] != count {
				return errors.New("formatted text changed a number or technical token")
			}
		}
	}
	return nil
}
