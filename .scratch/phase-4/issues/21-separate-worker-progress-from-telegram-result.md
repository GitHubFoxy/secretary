# 21 Keep Worker progress out of the final Result and preserve Telegram formatting

Type: task
Status: ready-for-agent

## Work

The Worker output shown in Telegram currently mixes progress narration into the final Result and loses its formatting. The confirmed causes are:

1. The ACP adapter appends all Worker text for a turn to one `turnText`, so intermediate updates such as "Попробую получить…" are stored with the final answer.
2. Telegram `safeText()` normalizes whitespace with `strings.Join(strings.Fields(text), " ")`, removing paragraph breaks, headings and list layout. The saved Result still has its newlines; they disappear during Telegram delivery.
3. Telegram sends ordinary text without `parse_mode`, so Markdown links such as `[36:21](...)` are shown literally.

Keep progress/activity separate from the terminal Result. Preserve meaningful newlines and render supported Markdown safely, including clickable links. If Telegram parsing fails or content cannot be represented safely, fall back to readable plain text rather than dropping the Result. Continue applying Telegram's content-safety redaction.

## Acceptance

- The terminal Result contains the Worker's final answer, not intermediate progress narration accumulated during the turn.
- Progress remains available through the appropriate Worker activity/status path and is not silently mistaken for final output.
- Paragraphs, headings and lists retain their line breaks in Telegram.
- Supported Markdown links render as clickable links; unsafe or invalid markup falls back to readable plain text.
- Tests prove that sanitization preserves newlines while still removing forbidden internal data.
- Tests cover mixed progress/final ACP output, Telegram formatting, link parsing failures and duplicate delivery.
