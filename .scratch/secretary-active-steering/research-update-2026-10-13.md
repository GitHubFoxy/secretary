# Актуализация исследования: durable steering Secretary

Дата: 2026-10-13. Issue: [#24](https://github.com/GitHubFoxy/secretary/issues/24). Сверены исходники текущего checkout и upstream primary sources. Production-код не менялся; диагностический runner не запускался.

## Установленные факты

- В активном checkout (`feature/issue-24-active-secretary`, `28cb4f5`) durable-ветка `Runtime.HandleMessage` по-прежнему безусловно ставит ввод в очередь и вызывает `startNextDurable`; до `Session.Steer` она не доходит. `acpSession.Steer` вызывает ACP `_session/steering` с `{sessionId, prompt}`. [runtime.go](../../internal/secretary/runtime.go), [acp_runtime.go](../../internal/node/acp_runtime.go).
- Локальный `main` — `7b55d6e`; `main...HEAD` меняет только Telegram usage-limit файлы. Steering runtime и ACP adapter относительно `main` не менялись. До добавления этой заметки рабочее дерево было чистым; итоговое незакоммиченное изменение — только эта Markdown-заметка.
- ACP v1 резервирует методы с `_` для пользовательских расширений. Поэтому `_session/steering` — расширение конкретного ACP-агента, не стандартный метод ACP v1. Базовый контракт `session/prompt` оставляет владельцем terminal response исходный prompt request. [ACP Extensibility](https://agentclientprotocol.com/protocol/v1/extensibility), [ACP Prompt Turn](https://agentclientprotocol.com/protocol/v1/prompt-turn).
- Текущий официальный [Codex ACP handler](https://github.com/agentclientprotocol/codex-acp/blob/202e66e63343ec4f5a5e35ab590251d813d129ca/src/CodexAcpServer.ts#L2070) передаёт native `turn/steer`; если он завершается успешно, ACP handler отвечает `injected`. При проигрыше гонки с terminal handler может создать новый prompt и ответить `startedNewTurn`; это значит, что сообщение уже потреблено, а новый prompt может жить в фоне. Версия и поведение зависят от установленного `codex-acp`; в прежней заметке описан сервер `1.12.0`, тогда как upstream source проверен на commit `202e66e` / release `v2.2.2`. Не переносить нынешнее поведение upstream на старый runtime без проверки его pinned source. [Объявление capability и обработка extension](https://github.com/agentclientprotocol/codex-acp/blob/202e66e63343ec4f5a5e35ab590251d813d129ca/src/CodexAcpServer.ts#L513), [release v2.2.2](https://github.com/agentclientprotocol/codex-acp/releases/tag/v2.2.2).
- Codex app-server `turn/steer` — другой RPC: он адресует `threadId`, input и `expectedTurnId`. Это не протокол, который Secretary напрямую вызывает: его ACP adapter вызывает расширение ACP, а Codex ACP уже может транслировать его в app-server RPC. [Codex app-server `TurnSteerParams`](https://github.com/openai/codex/blob/main/codex-rs/app-server-protocol/schema/json/v2/TurnSteerParams.json).
- `injected` подтверждает исход ACP handoff, а не семантическое выполнение уточнения моделью. В проверенном upstream Codex ACP это outcome выставляется после успешного вызова native steer; сам response не содержит доказательства, что модель успела учесть текст до terminal. Это вывод из границы transport/API: доказательство влияния требует отдельной native same-turn acceptance с уникальным marker в наблюдаемом действии/ответе до terminal и прежними session/turn IDs.

## Ограничения fixture

`.scratch/secretary-active-steering/diagnostic/run.py` через Go overlay добавляет тест к настоящему пакету и вызывает `Runtime.HandleMessage` с настоящим Store и подготовленным активным durable turn. Fake session записывает вызов `Steer`; единственная проверка ожидает, что он состоялся. Fixture полезна как узкий регрессионный сигнал для выбранной ветки handler, но сама по себе:

- не проверяет отсутствие queued turn, durable input identity/seq, replay-idempotency, ошибочные/unknown receipts, terminal races или recovery;
- не наблюдает реальный ACP процесс, native event log либо воздействие сообщения на модель;
- её прежний красный результат зафиксирован в `incident.md` на старом HEAD `1f7f58f`; этот запуск здесь не повторялся.

## Вывод и следующий шаг реализации

Для Antigravity implementer: сначала перенести fixture в штатный тест и расширить assertions на «steer вызван, второй durable turn не создан»; затем реализовать общий ordered durable ingress с несколькими immutable input identities на одном Secretary turn, явные состояния handoff и terminal race по [spec.md](spec.md). Сохранять разницу между `injected`, `startedNewTurn`, отказом и неизвестным receipt: не считать только `injected` доказательством модельного эффекта; `startedNewTurn` нельзя молча повторно отправлять или оформлять как тот же turn. После unit/store/runtime проверок отдельно выполнить native acceptance на закреплённых версиях ACP/Codex: уточнение должно появиться в проверяемом результате до terminal исходной работы, с теми же session/turn IDs. ACP v1 имеет другой lifecycle-контракт, поэтому не подменять его app-server protocol предположениями.

## Первичные источники

- [ACP v1 Extensibility](https://agentclientprotocol.com/protocol/v1/extensibility) — underscore methods являются custom extensions.
- [ACP v1 Prompt Turn](https://agentclientprotocol.com/protocol/v1/prompt-turn) — lifecycle `session/prompt`, session updates и terminal response.
- [Codex ACP server at pinned upstream commit](https://github.com/agentclientprotocol/codex-acp/blob/202e66e63343ec4f5a5e35ab590251d813d129ca/src/CodexAcpServer.ts) — объявление capability, ACP steering, native handoff и idle fallback.
- [Codex ACP extension types at pinned upstream commit](https://github.com/agentclientprotocol/codex-acp/blob/202e66e63343ec4f5a5e35ab590251d813d129ca/src/AcpExtensions.ts#L122) — request/response shape и outcomes.
- [Codex app-server v2 TurnSteerParams](https://github.com/openai/codex/blob/main/codex-rs/app-server-protocol/schema/json/v2/TurnSteerParams.json) — отдельный RPC-контракт `turn/steer`.
