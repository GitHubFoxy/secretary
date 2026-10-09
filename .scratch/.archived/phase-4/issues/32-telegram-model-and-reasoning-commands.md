# 32 Выбирать model и reasoning через Telegram group commands

Type: task
Status: needs-info

## Work

Пользователь хочет `/model` и `/reasoning` в forum group, где General соответствует Secretary, а Worker Topics конкретным Workers. Это не private bot chat: command mention, thread routing, видимость inline buttons и owner authorization требуют явного contract.

Настройки должны принадлежать Secretary server/target runtime, а не только локальной памяти Telegram adapter. Одинаковый target виден другим clients.

## Proposed UX, ещё не утверждён

- General: commands меняют только Secretary.
- Worker Topic: commands меняют только Worker, связанного с этим Topic.
- `/model` и `/reasoning` без arguments показывают текущее значение и выбор через inline buttons.
- `/model <provider/model-id>` и `/reasoning <level>` позволяют прямой выбор.
- `/model@<наш_bot_username>` и `/reasoning@<наш_bot_username>` равнозначны unqualified commands. Commands другим bots игнорируются.
- Future Worker defaults меняются отдельно и явно, не в результате изменения конкретного Worker или Secretary.
- При active execution настройка становится pending для следующего turn. Команда не отменяет работу и не стирает history.
- Confirmation указывает target, фактическую model/reasoning и момент применения.

## Acceptance

- Поддержаны forum General и Worker Topics, Telegram bot_command entities, qualified command names и callback_query.
- Только owner может изменять настройки; одного chat_id недостаточно для authorization участников группы.
- Inline callback связан с target/chat/topic и допустимым snapshot/version. Нажатие stale button не меняет чужой target или новый Worker после перемены mappings.
- Выбор берётся из реально доступных models и reasoning variants выбранного harness/provider, а не из fixed fabricated list.
- Runtime применяет выбранное значение. Нельзя подтвердить успех, лишь поменяв label в Telegram.
- Unsupported reasoning/model, unavailable Node, closed Worker и отсутствие безопасной reconfiguration дают понятную ошибку без silent fallback.
- При смене model не происходит скрытого сброса native session/history или нарушения immutable Worker binding.
- Callback retries/replay идемпотентны; значения durable и согласованы между Telegram/Web.
- Tests покрывают unauthorized member, чужой bot mention, General/Topic routing, stale callback, active/idle runtime и restart.

## Investigation, 3 октября 2026

- `internal/telegram/adapter.go`: `Update`, `Message`, `HandleUpdate`, `deliverUpdate` сейчас не разбирают model/reasoning commands и bot mentions. Кроме `/start`, текст уходит как обычное сообщение по General/Topic routing.
- `internal/telegram/http.go`: `botMessageDTO`/`GetUpdates` не читают command entities; polling запрашивает только `message`. `callback_query`, inline keyboards и command menu не реализованы.
- Owner authorization уже существует через `ownerAllowed`/`senderAllowed`. Сохранить её, не заменить проверкой принадлежности чату.
- `internal/webapi/user.go::setSecretaryModel` меняет только Secretary model; reasoning и Worker model/reasoning routes отсутствуют.
- `StartRequest.validateBinding` проверяет закреплённые HarnessInstance/model/reasoning. User-facing command не должна обходить этот contract. Нужно сначала определить explicit server/runtime reconfiguration API и допустимость изменений уже существующего Worker.
- Обычный Chat/Thread routing не является runtime configuration API. Отправлять текст `/model` модели и считать это переключением запрещено.

## Open decisions

- Q2: General -> Secretary, Topic -> текущий Worker? Рекомендация: да.
- Q3: применение со следующего turn без interrupt/reset? Рекомендация: да.
- Q4: buttons для bare command и direct arguments одновременно? Рекомендация: да.
- Необходимые изменения native session configuration определяются по фактическому OpenCode v2 ACP contract. Отсутствующие capabilities нельзя выдумывать.

## Related

Ticket31 определяет default harness и initial model/reasoning. Existing fx Workers не превращаются в OpenCode Workers этой командой. Если fx не поддерживает безопасную live reconfiguration, показывать ограничение, а не мигрировать Worker.
