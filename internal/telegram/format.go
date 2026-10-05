package telegram

import (
	"html"
	"net/url"
	"strings"
	"unicode"
)

// Format one existing size-only chunk, never the whole Result before splitting.
// Emit only Telegram's supported HTML subset. Unknown/incomplete Markdown and
// raw HTML remain escaped readable text. No user input becomes an HTML tag.
func formatTelegramCodeBlock(text string) (formatted, plain string) {
	return "<pre>" + html.EscapeString(text) + "</pre>", text
}

func formatTelegramChunk(text string) (formatted, plain string) {
	var out, fallback strings.Builder
	lines := strings.SplitAfter(text, "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if strings.HasPrefix(line, "```") {
			end := i + 1
			for end < len(lines) && strings.TrimSpace(lines[end]) != "```" {
				end++
			}
			if end < len(lines) {
				body := strings.Join(lines[i+1:end], "")
				out.WriteString("<pre>" + html.EscapeString(body) + "</pre>")
				fallback.WriteString(strings.Join(lines[i:end+1], ""))
				if strings.HasSuffix(lines[end], "\n") {
					out.WriteByte('\n')
				}
				i = end
				continue
			}
			// A split/unclosed fence cannot prove code scope in this chunk.
			out.WriteString(html.EscapeString(strings.Join(lines[i:], "")))
			fallback.WriteString(strings.Join(lines[i:], ""))
			break
		}
		content := strings.TrimSuffix(line, "\n")
		heading := 0
		for heading < len(content) && heading < 6 && content[heading] == '#' {
			heading++
		}
		if heading > 0 && len(content) > heading && content[heading] == ' ' {
			body, safePlain := telegramInline(content[heading+1:])
			out.WriteString("<b>" + body + "</b>")
			fallback.WriteString(content[:heading+1] + safePlain)
		} else {
			body, safePlain := telegramInline(content)
			out.WriteString(body)
			fallback.WriteString(safePlain)
		}
		if strings.HasSuffix(line, "\n") {
			out.WriteByte('\n')
			fallback.WriteByte('\n')
		}
	}
	return out.String(), fallback.String()
}

func telegramInline(text string) (string, string) {
	var out, plain strings.Builder
	for len(text) > 0 {
		if text[0] == '[' {
			labelEnd := strings.Index(text, "](")
			if labelEnd > 0 {
				start := labelEnd + 2
				depth, end := 1, start
				for end < len(text) && depth > 0 {
					if text[end] == '(' {
						depth++
					}
					if text[end] == ')' {
						depth--
					}
					end++
				}
				if depth == 0 {
					label, target := text[1:labelEnd], text[start:end-1]
					if safeTelegramLink(target) {
						out.WriteString(`<a href="` + html.EscapeString(target) + `">` + html.EscapeString(label) + `</a>`)
						plain.WriteString(label + " (" + target + ")")
					} else {
						out.WriteString(html.EscapeString(label))
						plain.WriteString(label)
					}
					text = text[end:]
					continue
				}
			}
		}
		matched := false
		for _, mark := range []struct{ token, tag string }{{"`", "code"}, {"**", "b"}, {"__", "b"}} {
			if !strings.HasPrefix(text, mark.token) {
				continue
			}
			end := strings.Index(text[len(mark.token):], mark.token)
			if end <= 0 {
				continue
			}
			body := text[len(mark.token) : len(mark.token)+end]
			out.WriteString("<" + mark.tag + ">" + html.EscapeString(body) + "</" + mark.tag + ">")
			plain.WriteString(mark.token + body + mark.token)
			text = text[2*len(mark.token)+end:]
			matched = true
			break
		}
		if matched {
			continue
		}
		// Escape runs of ordinary text, preserving Unicode/whitespace verbatim.
		end := 1
		for end < len(text) && !strings.ContainsRune("[`*_", rune(text[end])) {
			end++
		}
		out.WriteString(html.EscapeString(text[:end]))
		plain.WriteString(text[:end])
		text = text[end:]
	}
	return out.String(), plain.String()
}

func safeTelegramLink(target string) bool {
	if strings.IndexFunc(target, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return false
	}
	u, err := url.Parse(target)
	if err != nil || u.User != nil || u.Hostname() == "" {
		return false
	}
	return u.Scheme == "https" || u.Scheme == "http"
}
