# Понятные ошибки при прямом сообщении Worker

Status: ready-for-agent

## Проблема

При отправке сообщения непосредственно Worker (как в режиме idle follow-up, так и в active steering) пользователь сталкивается с малоинформативной ошибкой `acp rpc -32603: Internal error`. Это сообщение не объясняет источник сбоя (исчерпание квоты модели, истечение срока действия токена, отказ шлюза или сетевой таймаут) и скрывает дальнейшие действия.

Конкретный инцидент от 11 октября 2026 г. (16:24:33 +07) верифицирован независимыми проверками в production Result `res_cee355ff10419cbf0263bff2a9ea6997` и нативном транскрипте сессии Codex `/home/coder/.codex/sessions/2026/10/11/rollout-2026-10-11T15-18-46-01a12a0b-023a-7251-aa36-4cf6452b61c4.jsonl` (строка 25: `task_complete` со свойством `codex_error_info: "usage_limit_exceeded"` и предложением повторить в 4:37 PM). Адаптер `@agentclientprotocol/codex-acp@1.12.0` возвращает JSON-RPC ошибку `-32603` с объектом деталей в `error.data`, но клиент `internal/acp/jsonl.go` отбрасывает `data`, а рантайм `internal/node/acp_runtime.go` заменяет `Summary` на сухой `err.Error()`.

Кроме того, при ошибке active steering (`_session/steering`) сетевой сбой возвращается через `ServerManager.SendCommandAndWait`, который преобразует структурированный `CommandOutcome` в плоскую строку `fmt.Errorf`, стирая типизацию между процессами. В результате Web API отдаёт текстовый HTTP 502, а черновик ввода пользователя в Web UI очищается.

## Ожидаемое поведение

1. **Сохранение контекста провайдера через ACP boundary**:
   - `acp.RPCError` в `internal/acp/jsonl.go` сохраняет поле `Data json.RawMessage` из ответов JSON-RPC 2.0.
   - Рантайм `acp_runtime.go` анализирует причину сбоя и формирует понятный `Result.Summary`, `Result.FailureCode`, `Result.Category` и технический код.
   - Если в стриминге активности уже присутствовало верифицированное сообщение об ошибке от провайдера, оно используется как надёжный fallback вместо сырого `-32603`. Произвольный текст ассистента без подтверждения провайдера лимитом не считается.
2. **Обработка двух путей сбоя (Actual Paths)**:
   - **Path A (Idle follow-up terminal)**: Приводит к записи человекочитаемого терминального Result в `phase4_results` с категорией ошибки (`FailureCategory = "usage_limit"`), отображаемого в Web observer и Telegram topic.
   - **Path B (Active steering transport error)**: Ошибка вызова `_session/steering` передаётся через `CommandOutcome` с типизированными полями `ErrorCode`, `ErrorMessage`, `FailureCategory`, `ProviderCode`. `ServerManager.SendCommandAndWait` возвращает типизированный `NodeCommandError`, а `writeClientMutationError` сериализует структурированный JSON с `application/json`. Черновик пользовательского ввода в Web UI не стирается, а в топике Telegram выдаётся сервисное пояснение.
3. **Строгая классификация ошибок**:
   - Ошибки типизируются: `usage_limit`, `auth_required`, `billing_credits`, `rate_limit`, `internal_error`.
   - Произвольные ошибки `-32603`, HTTP 403 или HTTP 429 не маскируются под `usage_limit` без наличия явных признаков квоты.
4. **Безопасность данных (Redaction & Bounds)**:
   - Все сообщения об ошибках проходят через safe allowlist и очищаются от секретов (API-ключи, токены, заголовки).
   - Длина сообщения строго ограничена 1000 рунами.
   - Технический код ошибки `-32603` и метод RPC сохраняются в диагностическом поле для отладки.

## Спецификация и зависимости

- [Issue 01: Сохранить и показать исходную причину ошибки Worker](https://github.com/GitHubFoxy/secretary/issues/33).
- [Исследование пути ошибок и верификация инцидента](../../docs/research/worker-acp-incident-gemini-verification.md).
- [Зависимая задача: автовозобновление после лимита](../usage-limit-auto-resume/spec.md). Порядок внедрения: сначала эта спецификация, затем планировщик автовозобновления.
