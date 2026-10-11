# 01: передавать уточнения активному Secretary через durable steering

Type: task
Status: ready-for-agent
Blocked by: None

## Проблема

Уточнение из Telegram General приходит во время активного Secretary, но durable ветка `Runtime.HandleMessage` всегда вызывает `QueueMessage` и выходит до `Session.Steer`. В [инциденте](../incident.md) уточнение было сохранено за 36 секунд до создания Worker, однако Codex Secretary получил его только после terminal первого запроса. Worker получил отдельный Follow-up и выдал второй Result.

## Что реализовать

Выполнить [спецификацию](../spec.md) по выбранному дизайну из [исследования](../research.md): durable ingress по Conversation entry identity/seq, несколько inputs одного Secretary turn, немедленный native steering, явный `/q`, точная адресация и видимые ошибки. Исправлять общий server/runtime путь для Web и Telegram, а не добавлять Telegram-only обход.

## Порядок реализации

1. Добавить в штатные тесты сценарий из [диагностической fixture](../diagnostic/repro_test.go.txt). Проверить падение до изменения runtime.
2. Расширить Store additive реестром inputs и delivery outcomes; backfill исходных input identities старых turns без переписывания entry/history. Сохранить idempotency и immutable context snapshot.
3. Передавать ID/seq canonical inbound entry в общий ingress; исключить потерю между `AppendInbound` и detached обработчиком, сериализовать приём по durable seq. Зафиксировать исход ввода до `202 saved`; native handoff может быть асинхронным.
4. Согласовать origin validation, acknowledgement/reply slots, Worker origin links, MCP observer и completion guard с несколькими inputs. Не заменять исходный `input_id` последним уточнением.
5. Добавить gate/state transitions runtime для handoff и terminal commit, не блокируя SQL/runtime mutex на native RPC. Только настоящий `injected` считается принятой инъекцией; непредвиденный native новый turn требует reconciliation, а не успешного ответа.
6. Провести scoped tests и race, затем native same-turn acceptance в изолированной Conversation/workspace. Документировать новый контракт, доказательства и границы поддержки.

## Критерии приёмки

- [ ] Обычный ввод активному durable Secretary вызывает `Session.Steer` до terminal текущего turn; не создаёт второй queued Secretary turn.
- [ ] Уточнение до Dispatch влияет на prompt Worker; в контролируемом сценарии исходный и уточняющий ввод дают одну работу и один terminal Result.
- [ ] Уточнение после Dispatch занятому Worker сохраняет Worker reference и Attempt, если Worker остаётся активным при `message_worker`; нельзя автоматически обещать один Result, если первая Attempt уже закончилась.
- [ ] `/q` остаётся durable FIFO очередью; idle обычный ввод создаёт новый turn. Состояние до принятия native prompt, offline/unsupported и пустой `/q` дают определённый видимый исход.
- [ ] Несколько быстрых вводов из Web/Telegram обрабатываются по canonical seq. Replay entry не повторяет handoff; одинаковый текст с разными entry identities не сливается.
- [ ] Каждый принятый input имеет стабильную identity, точный generation/binding и определённый terminal/delivery исход. Старые адресованные операции сохраняют корректные origin links после уточнения.
- [ ] Acknowledgements/final/replies и Worker Results доставляются без повторного terminal echo; одно сохранение final может иметь несколько явных input links.
- [ ] Tool callback, пришедший до сохранения steering receipt, корректно проходит provisional authorization только текущего `sending` input/generation. Ошибка handoff не удаляет уже принятые действия и origin links; reconciliation видима.
- [ ] Addressed-v1 сохраняет exact reply/Result guard для принятых inputs; acknowledgement не заменяет reply. Legacy default сохраняется, addressed-v1 не включается автоматически.
- [ ] Проверены terminal перед handoff, во время RPC, до receipt persistence, timeout после отправки, `startedNewTurn`, смена generation и restart во всех handoff стадиях. Нет скрытого нового turn, replay RPC или потери сообщения.
- [ ] Ошибки после `202 saved` видны пользователю; логи не заявляют `applied` на основании только transport receipt.
- [ ] Старый SQLite мигрирует без потери Conversation entries, очереди `/q`, Results и history; новые записи читаются после reopen.
- [ ] Native Codex acceptance показывает marker уточнения в действии/ответе до естественного terminal, те же session/turn IDs, без cancel+new turn. Fake test и `injected` receipt сами по себе не считаются native доказательством.

## Проверки

До реализации — ожидаемое падение:

```sh
python3 .scratch/secretary-active-steering/diagnostic/run.py
```

После реализации — штатные тесты actual Store/runtime/HTTP seams, а не fixture, которая только повторяет новую ветку. Начать с `go test ./internal/core ./internal/secretary ./internal/webapi ./internal/telegram ./internal/ctl ./internal/node`, затем focused `-race` для concurrent ingress/terminal. Native acceptance выполнять отдельно с pinned Codex/ACP версиями и sanitized трассой.

## Comments

2026-10-11: пользователь поручил создать тикет и исследовать задачу до готовности к реализации. Runtime-код, commits, push и production deploy этим запросом не заказаны. Диагностика воспроизводит проблему без изменения исходников через Go overlay.

2026-10-11: исследование завершено, status `ready-for-agent`. Выбраны additive per-input registry, ordered ingress, bounded handoff/terminal gate и существующий Codex ACP. Product decisions не блокируют реализацию; residual native fallback и unknown receipt включены в обязательные проверки. Источники, trade-offs, migration и test matrix находятся в `research.md`.
