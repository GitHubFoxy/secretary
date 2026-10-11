# Поддержка Google Antigravity CLI harness

## Цель

Пользователь может выбрать Google Antigravity как отдельный локальный harness для Worker. Запуск, поток событий и управление сессией идут через официальный ACP server Google `agy_acp_server`; отдельный `HarnessInstance` остаётся привязан к `antigravity` и не подменяется другим ACP harness.

## Решения

- Запускать официальный Antigravity ACP Registry agent `agy_acp_server`. T3 Code использует тот же сервер. Не строить bridge поверх `agy -p` и не подключать IDE extension.
- Оставить `internal/acp.Client` общим JSON-RPC transport, а `ACPRuntime`/`acpSession` — переводчиком стандартного ACP в Secretary `Runtime`/`Session`/`Activity`/`Result`. Добавить узкий `AntigravityRuntime` compatibility adapter по образцу `CodexRuntime` для отдельного launch/auth profile и Google-specific resume, config, request и terminal-result семантики. Не создавать второй transport или ещё один универсальный ACP proxy.
- В первой версии бинарник и требуемый companion устанавливает оператор отдельно. Поддержать PATH/явный executable override, проверять соседний `localharness_external` и строить args/env из официального Registry entry; не добавлять загрузчик/обновлятор.
- CLI `agy` и ACP server имеют отдельные состояния авторизации. Успешный `agy auth` не является доказательством, что ACP server авторизован. Не читать или копировать внутренние токены. Неавторизованный ACP instance должен оставаться недоступным с понятной причиной.
- Записывать в `HarnessInstance` только непосредственно обнаруженные модели, уровни reasoning и capabilities. Не выводить steering из наличия ACP; не считать cancel эквивалентом steering. Если capability не проверена, она отсутствует.
- Config options брать из `session/new`/resume response и `config_option_update`, не из несуществующего `session/config_options` RPC. `session/load` и `session/resume` выбирать согласно объявленным session capabilities; не менять поведение Codex/OpenCode.
- Discovery обновляется каждые 5 минут. Не стартовать тяжёлый bundle/session при каждом refresh; кэшировать дорогое auth/catalog observation и сообщать причину недоступности.
- Не переносить из T3 многопрофильную систему аккаунтов, browser callback relay, установщик/обновлятор, usage probing или UI. Не менять другие harness и не добавлять новую ACP-библиотеку.

## Границы

- Включить новый kind `antigravity`, конфигурационную валидацию, обнаружение и точную маршрутизацию runtime.
- Переиспользовать ACP transport, request handlers, передачу MCP и нормализацию событий Secretary там, где они соответствуют поведению Google ACP server.
- Подтвердить реальным `agy_acp_server` базовый запуск prompt→result и перечисленные в acceptance criteria lifecycle/API. Не делать частые readiness probes, создающие ACP-сессии или запускающие тяжёлый агентский процесс.
- Не включать автоматическую установку бинарника и не обещать одинаковое поведение на платформах, где официальный registry artifact недоступен.

## Источники и анализ

- [Исследование первичных источников и исходников T3 Code](research.md)
- [ACP Session Config Options](https://agentclientprotocol.com/protocol/v1/session-config-options)
- [ACP session/resume capability](https://agentclientprotocol.com/rfds/session-resume)
- [Google Antigravity ACP Registry manifest](https://github.com/agentclientprotocol/registry/blob/main/antigravity-acp/agent.json)
- [T3 Code: Antigravity provider guide](https://github.com/pingdotgg/t3code/blob/f23d0084b6e023ffb1af9e332d8666affc99d542/docs/user/providers-antigravity.md)
- [T3 Code: Antigravity ACP adapter](https://github.com/pingdotgg/t3code/blob/f23d0084b6e023ffb1af9e332d8666affc99d542/apps/server/src/orchestration-v2/Adapters/AntigravityAdapterV2.ts)
- [T3 Code: Antigravity ACP support](https://github.com/pingdotgg/t3code/blob/f23d0084b6e023ffb1af9e332d8666affc99d542/apps/server/src/provider/acp/AntigravityAcpSupport.ts)

## Задача

[01 — Добавить Antigravity harness через официальный ACP server](https://github.com/GitHubFoxy/secretary/issues/6)
