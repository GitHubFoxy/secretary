# Готовность Minimal MVP — 9 октября 2026

Исследование текущего checkout `42721bb` с незакоммиченными пользовательскими дополнениями в [THE spec](../../spec/THE%20spec.md). Код продукта, конфигурация, база и процессы Secretary не изменялись. На локальном Mac включён ранее остановленный Tailscale командой `tailscale up`, чтобы получить доступ по SSH. Три нативных субагента GPT-6.1 Sol, medium исследовали чат, runtime и официальные протоколы.

## Вывод

Основа продукта уже реализована, но минимальный MVP не готов к использованию. Нельзя оценивать готовность числом закрытых Phase 4 tickets: прежняя Phase 4 требует OpenCode/FX, а актуальный MVP требует Codex/Claude Code, прямую переписку, steering, `/q` и Telegram topics. Нужна одна ограниченная работа по сквозному MVP поверх существующего server/Node/store, а не переписывание проекта или завершение всей старой Phase 4.

| Требование актуального MVP | Результат исследования |
| --- | --- |
| Общая сохраняемая переписка | Реализована; доступность ответа зависит от runtime и доставки |
| Локальные и удалённые Workers | Server/Node transport, binding, dispatch и recovery реализованы; реальный Codex/CC round-trip надо доказать |
| Codex | ACP adapter есть; локальный codex-acp поддерживает steering/load. Текущий omarchy deployment настроен на OpenCode, не на Codex |
| Claude Code | Одноразовый native print adapter есть, полноценный интерактивный контракт Secretary/Worker не реализован |
| Прямое сообщение Worker | Web/Telegram → единый MessageWorker путь существует |
| Steering по умолчанию | Working branch вызывает Steer; CC adapter его всегда отвергает |
| `/q` | Не разбирается в Phase 4 Worker message path; сейчас уйдёт как обычный steering с префиксом |
| Telegram group/Worker topics | Routing, durable mapping, results и outbox есть; полноценная live acceptance актуального MVP не выполнена |
| Читаемый simplified UI | Observer есть, но Markdown показывается plain text, часть activity — JSON |

Подробные ссылки на исходники и scoped проверки: [чат](minimal-mvp-chat-audit.md), [runtime](minimal-mvp-runtime-audit.md), [официальные native protocols](minimal-mvp-native-protocols.md).

## Фактическое состояние omarchy

Read-only SSH через alias `omarchy`:

- `systemctl --user show secretaryd.service secretary-node.service`: обе `inactive/dead`, обе `disabled`, `Result=success`. Journal фиксирует штатную остановку 9 октября в 14:17:26 UTC+7. Кто и зачем остановил службы, этим исследованием не установлено. Службы не перезапускались.
- `curl -fsS --max-time 3 http://127.0.0.1:8081/v1/health`: connection refused. Сейчас отсутствие ответа объясняется также отсутствием запущенного server, но это не объясняет предыдущие эпизоды.
- Установленный `secretaryd` имеет SHA256 `faa525aa1e58ec21808a34f99eab47d7411c585276f71594be3d659a6f031133`, совпадает с `projects/secretary-release-0d1ca27/release-bin/secretaryd`. `RELEASE_COMMIT=0d1ca27`. Файл `deployed-commit` содержит старый `459709c...`; он не является достоверной ревизией бинарника.
- Config Secretary и Worker: `opencode`, `openai/gpt-6-luna`, `low`. Это не целевой Codex/CC deployment. Journal последнего запуска подтверждает обе OpenCode runtime selections.
- Installed native CLIs: Codex `0.150.1`, Claude Code `2.1.270`. `codex login status` exit 0. `claude auth status` exit 1, `loggedIn=false`, `authMethod=none`; проверка проводилась в обычной SSH-среде, поэтому не доказывает отсутствие всех возможных API способов авторизации. В сохранённом Secretary environment нет ключей с именами ANTHROPIC/CLAUDE/CODEX/ACP. Значения credentials не выводились.
- В стандартных проверенных directories `.local/bin`, `.npm-global/bin`, `.bun/bin`, `node_modules/.bin` не найден codex-acp. Это не исчерпывающий поиск всего диска.
- SQLite открывалась через `mode=ro`. Сохранены 69 Secretary turns, 175 Conversation entries, 7392 events, 35 Workers. Worker statuses: 27 closed, 6 idle, 2 offline. Telegram state содержит 32 topic mappings, cursor 7392. Эти counts не доказывают успешные Codex/CC runs.

## Почему Secretary не отвечает: два разных эпизода

Историческую потерю двух ответов разобрали и исправили в [ticket 35](../../.scratch/.archived/phase-4/issues/35-telegram-missing-reply-and-stalls-after-second-message.md). Старый bridge пропускал canonical `conversation.entry`; текущий код содержит mapping и regression test, fix входит в установленную сборку `0d1ca27`. Не следует повторно приписывать нынешний симптом уже исправленной причине. Старые пропущенные events автоматически не переотправлены, поскольку cursor их прошёл.

Последний input **после исправления**, 8 октября 08:09:36 UTC (15:09:36 UTC+7), принят и обработан за примерно 6 секунд, но turn завершён `failed` с `native_terminal_invalid`. Для него:

```text
MCP discovery: tool_count=9, has_spawn_worker=true, has_reply_to_user=true, failed=false
completion: branch=addressed_reply_only, terminal_class=end_turn
terminal_valid=true, rpc_succeeded=true, drain_completed=true
assistant_chunks=0, response_present=false, reply_count=0, entry_present=false
```

Это подтверждает отсутствие durable ответа при завершившемся native turn. Это не доказательство проблемы Telegram transport или hanging model. Точная причина, почему runtime не создал reply, не установлена; raw private prompts/reasoning не выводились. Current [completion guard](../../internal/core/secretary_completion.go:54) намеренно не считает пустой ответ успешным. Для нового runtime нужно проверить обычный assistant final → ровно один canonical ответ, addressed MCP reply → ровно один ответ, пустой/error terminal → видимая ошибка. Нельзя скрывать проблему простым ослаблением guard или повторять неизвестное выполнение автоматически.

## Форматирование

Web defect воспроизводится локально без модели: `formatActivityPayload({payload:{kind:'text',text:'Ответ воркера'}})` выводит JSON. Conversation/Result выводятся Svelte как plain text, без Markdown renderer. Подробности в [chat audit](minimal-mvp-chat-audit.md).

Telegram уже имеет ограниченный Markdown→HTML formatter, safe escape, splitting и fallback; scoped tests проходят. Конкретный текущий визуальный дефект Telegram этим исследованием не воспроизведён: server остановлен, нового сообщения владельца/скриншота нет. Успешные fixtures не означают корректное отображение любого реального отчёта. Нужно проверять реальную форму пользовательского сообщения, а не переписывать formatter по предположению.

## Кратчайший путь

1. **Зафиксировать текущий Minimal MVP как единственную цель этой работы.** Не завершать OpenCode/FX tickets ради старого gate; не удалять эти adapters ради косметического очищения. Исключить их из новой установки, defaults, предлагаемых harnesses и acceptance. Сохранить уже существующую историю/bindings.
2. **Собрать рабочий Codex vertical slice первым.** Проверить текущий codex-acp с native CLI, MCP, Secretary reply, Worker dispatch, resume и steering. Настроить явные поддерживаемые модели, без inherited OpenCode pins. Текущий configured Codex runtime не включает TerminalMessageGrouping, необходимый ему для opt-in addressed-reply-v1: нельзя просто переключить harness в прежнем deployment и ожидать готовности. Для первого slice проверить существующий legacy reply path либо отдельно доказать поддержку addressed contract. Сохранить adapter, если проверки проходят; прямой app-server — резервный путь при доказанном несовместимом поведении. Проверить доставку содержимого Worker profile на remote Node: metadata имени/version/hash сама по себе не передаёт модели инструкции. Для co-located Workers использовать существующий durable secretary-node путь вместо in-memory checkpoints direct LocalNode.
3. **Исправить пользовательский путь одновременно:** обычный ответ Secretary, Web Markdown/нормальные Worker text/tool messages, shared durable `/q` routing для Web и Telegram. Очередь должна переживать restart и не исполняться в активном ходе; UI показывает queued. Не подменять очередь steering.
4. **Завершить интерактивную интеграцию CC.** Current `--print` implementation не достаточно переключить в config. Нужны входящий stream/SDK, MCP, resume, входящие сообщения и явный контракт steering. Официальная последовательная SDK queue сама по себе не доказывает немедленное same-turn steering; interrupt+new query нельзя молча назвать тем же поведением.
5. **Развернуть проверенную сборку и пройти короткую настоящую acceptance на local+remote Nodes и Telegram.** Доказать обычный reply, dispatch обоих harnesses, сохранение истории, direct message, steer, `/q`, Result в General+topic, readable text/code и понятные ошибки offline/auth. Детерминированные tests дополняют это, а не заменяют.

Рекомендуемая организация: один parent MVP fix с этим gate, внутренние изменения группировать по runtime, messaging и presentation. Не создавать отдельный ticket для каждой строки UI или каждого сообщения об ошибке. Статус готовности — PASS только после сквозного native сценария, без оценки процентов или обещания срока до проверки двух runtime spikes.

## Проверки и ограничения

Root выполнил `go test ./internal/node -run '^TestClaudeCodeSessionSteeringIsExplicitlyUnsupported$' -count=1 -v` (PASS подтверждает unsupported limitation), `go test ./internal/core -run 'Test.*(Secretary.*Reply|Secretary.*Completion)' -count=1` (PASS), local formatter repro, SSH metadata/SQLite checks. Chat agent выполнил scoped Telegram/bridge/ctl/webapi и Web tests; подробные команды в его отчёте. Live model calls, отправка сообщений в Telegram, запуск Workers, рестарт server, provider login, deployment и изменение product files не выполнялись.
