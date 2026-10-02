package telegram

import (
	"context"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Генерация относится только к доставке в Telegram, не к Dispatch Worker.
// Даже генератор, который не соблюдает context, не удерживает event bridge
// после истечения таймаута. Поздний ответ уже не меняет название Topic.
func (a *Adapter) taskTopicTitle(ctx context.Context, prompt string) string {
	fallback := fallbackTopicTitle(prompt)
	if a.config.TitleGenerator == nil {
		return fallback
	}
	ctx, cancel := context.WithTimeout(ctx, a.config.TitleTimeout)
	defer cancel()
	type response struct {
		title string
		err   error
	}
	done := make(chan response, 1)
	go func() {
		title, err := a.config.TitleGenerator(ctx, prompt)
		done <- response{title, err}
	}()
	select {
	case result := <-done:
		if result.err == nil && ctx.Err() == nil {
			if title := validTopicTitle(result.title); title != "" {
				return title
			}
		}
	case <-ctx.Done():
	}
	return fallback
}

// Принимается только одна строка текста. JSON, Markdown, ссылки, служебные
// маркеры и reasoning не становятся названием даже при успешном вызове.
func validTopicTitle(text string) string {
	if !utf8.ValidString(text) {
		return ""
	}
	text = strings.TrimSpace(text)
	if text == "" || strings.IndexFunc(text, func(r rune) bool {
		return unicode.IsControl(r) || unicode.Is(unicode.Cf, r)
	}) >= 0 {
		return ""
	}
	text = strings.Trim(text, "\"")
	if text == "" || strings.ContainsAny(text, "{}[]`<>*") || strings.HasPrefix(text, "#") ||
		strings.Contains(strings.ToLower(text), "://") || safeText(text) != text || strings.Contains(text, "[redacted]") {
		return ""
	}
	return shortenTopicTitle(text)
}

func fallbackTopicTitle(prompt string) string {
	prompt = strings.ToValidUTF8(prompt, "")
	for _, line := range strings.Split(prompt, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimLeft(line, "#*- ")
		line = strings.Map(func(r rune) rune {
			if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
				return -1
			}
			return r
		}, line)
		line = safeText(line)
		if line != "" && !strings.Contains(line, "[redacted]") {
			return shortenTopicTitle(line)
		}
	}
	return "Задача Worker"
}

func shortenTopicTitle(text string) string {
	characters := []rune(text)
	if len(characters) <= 60 {
		return text
	}
	prefix := characters[:59]
	for index := len(prefix) - 1; index >= 30; index-- {
		if unicode.IsSpace(prefix[index]) {
			prefix = prefix[:index]
			break
		}
	}
	return strings.TrimSpace(string(prefix)) + "…"
}
