# Worker activity: независимый Spec review, round 2

Дата: 2026-10-10. Диапазон `43cd444...6bf4380219c39bf2622a86b78e0e6597053d2523`, commits `7adc71e` и `6bf4380`. Основание: [ticket 04](../issues/04-readable-unified-chat.md), пункт 7 Phase 5 и явно согласованный Codex-only scope. Реализация не изменялась reviewer.

## Findings

**0 P1/P2.** Вывод round 1 по grouping/rendering сохраняется. Указанный ранее риск потери whitespace-only native chunks исправлен в обеих реальных границах: Node normalization сохраняет любой непустой assistant text, core payload validation разрешает такой текст. `thinking_summary` по-прежнему требует непустой после trim текст. Status, tool, metadata и capability checks не ослаблены. Это исправляет исходную потерю данных перед renderer, а не пытается угадывать пробелы в UI.

Требование ticket: «Worker text activity отображается текстом, tool/status — компактно и понятно». Изменение необходимо для сохранения обычных пробелов, абзацев и fenced code в таком тексте; расширения продуктового scope не найдено.

## Независимая проверка

- Новые `TestNativeTextWhitespaceSurvivesAuthenticatedNodeWire` и `TestWhitespaceTextKeepsActivityValidationBoundaries`: **PASS**, отдельно повторены с `-race`: **PASS**.
- Wire test действительно проходит public normalization → exact-instance `ValidateFor` → durable LocalStore → настоящий HMAC-authenticated WebSocket → verified sequence ACK → production StoreEventSink → durable canonical `attempt.activity`. Проверка сравнивает количество и точную конкатенацию восьми chunks, включая отдельно пришедшие пробел и два переноса, Markdown heading и fence. Восемь fixtures не объявляются реальным Codex run; enrollment handshake использует существующий тестовый handler.
- Negative test сохраняет запрет empty assistant text, отсутствующей Attempt identity, неподдерживаемой capability и whitespace-only thinking/status. `Validate`/`ValidatePayload`/`ValidateFor` по-прежнему проверяют исходные metadata и точную Node/HarnessInstance identity; transport auth/ACK в diff не менялись.
- Предыдущие production-component SSR проверки readability/privacy/replay и отдельные identity-boundary assertions остаются применимы: renderer diff между rounds отсутствует, round 1 Web **19/19 PASS**.

Live acceptance новой версии этим reviewer не выполнялась. Этот отчёт подтверждает scoped implementation и regression evidence, а не production PASS.
