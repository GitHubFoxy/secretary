# 01: Codex Secretary и Workers на local/remote Nodes

Type: task
Status: in-progress
Blocked by: None

## What to build

Из существующего единого чата пользователь получает настоящий ответ Secretary Codex, просит создать Codex Worker на своей и удалённой машине, видит Result и продолжает ту же native session после idle и restart. Выбор node/harness проходит через существующий Secretary MCP и сохраняемый Worker binding.

## Acceptance criteria

- [ ] Native Codex Secretary выполняет deferred Start/Prompt, видит существующие MCP tools и реально вызывает read-only tool, затем spawn_worker на enrolled local и remote Nodes.
- [ ] Secretary profile не требует пустого ответа после Dispatch: пользователь получает содержательное краткое подтверждение или обычный ответ. Обычный assistant final создаёт один canonical reply; используемый addressed reply contract доказан на фактическом adapter, пустой/error terminal даёт видимую ошибку. Completion guard не ослаблен.
- [ ] Codex/codex-acp readiness проверяет фактические executable, versions, auth, initialize/load/steering capabilities; model/reasoning поддерживаются, чужие OpenCode pins не наследуются. Глобальная пользовательская config не переписывается.
- [ ] Co-located Workers используют существующий durable secretary-node путь. На обоих Nodes runtime получает содержимое profile, подтверждённое уникальным marker, и доступный MCP endpoint/capability.
- [ ] Реальный Worker Result возвращается в Origin Conversation ровно один раз без автоматического Secretary turn.
- [ ] Idle Follow-up сохраняет native session ID и ранее сообщённый marker; после Node restart binding/history сохраняются, активная Attempt автоматически не повторяется. Missing session возвращает явную ошибку, а не новую session.
- [ ] Во время длинной безопасной работы Codex принимает обычное steering и применяет marker до естественного завершения в той же активной Attempt. Native acceptance различает injected и startedNewTurn.
- [ ] Существующий ACP остаётся при успешном сценарии; переход на app-server ограничен Codex adapter и обоснован воспроизведённой несовместимостью.

## Проверка

Public seams: server HTTP/MCP и native Session. Переиспользовать Secretary reply/completion, public idle follow-up и continuation tests; новый дефект сначала воспроизвести через seam. Fake process проверяет fail-closed и identities; separate live evidence содержит versions, hosts и native session/turn/Attempt события. Не засчитывать fixture как model call. Общий Web renderer и `/q` — отдельные tickets, не blockers этого round-trip.

## Answer

Реализован минимальный Codex ACP adapter: native profile, phase-aware canonical final, fail-closed terminal/pins и различение injected/startedNewTurn. Clean defaults/setup/Doctor и Node discovery используют Codex/CC без обязательных FX/OpenCode; legacy bindings/config не удаляются. Scoped tests и локальные native profile/resume/active steering checks проходят. Свидетельства: `docs/research/phase5-codex-native-evidence.md`. Enrolled local/remote Secretary MCP, deployment/channel round-trip и остальные live criteria ещё требуют gate 05; ticket не закрыт.

## Comments

Разбивка и минимальный ACP-first путь одобрены пользователем. Scope — Phase 5 spec; прежние Phase 4 defaults исторические.

Интеграционная проверка 2026-10-10: ветки 01/03/04 объединены, `go test ./...`, `go build ./...`, Web tests и production build проходят. Queue public fixture включает Codex adapter; Web SSR и Telegram Bot API seam показывают server-owned queued state. Это scoped evidence, не live acceptance.
