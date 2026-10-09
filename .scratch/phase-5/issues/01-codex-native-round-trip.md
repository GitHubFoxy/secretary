# 01: Codex Secretary и Workers на local/remote Nodes

Type: task
Status: ready-for-agent
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

Пока отсутствует.

## Comments

Разбивка и минимальный ACP-first путь одобрены пользователем. Scope — Phase 5 spec; прежние Phase 4 defaults исторические.
