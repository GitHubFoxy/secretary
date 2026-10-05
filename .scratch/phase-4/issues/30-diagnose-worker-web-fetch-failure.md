# 30 Выяснить причину сбоя Worker web_fetch и отличать её от недоступности источника

Type: research
Status: resolved

## Work

3 октября 2026 Worker `wrk_153669ff80fa91c6f9b779214962a817` под fx пытался получить текущую погоду для Барнаула. Telegram Topic показывает ошибку `web_fetch` для wttr.in. Result сообщает, что wttr.in и Open-Meteo недоступны. Сама причина ошибки tool по screenshot неизвестна.

Контрольный пример в локальном OpenCode v2 дал короткий успешный ответ с WeatherAPI. Интерфейс показывает "Explored: 1 search". Это другой источник и, возможно, search, а не тот же fetch. Нельзя заключать, что виноват fx, или обещать исправление заменой harness без сопоставимого теста.

## Investigation

- Установить безопасный error class/code для actual Worker invocation.
- Различить DNS, TLS, HTTP status, timeout, proxy/egress, tool permissions, недоступность native tool/provider и отсутствие результатов поиска.
- Проверить публичные weather URLs с самого omarchy, а не только с Mac.
- Сравнить fx и OpenCode v2 на одном Node и одинаковом URL/input. Runtime tool может использовать другой сетевой путь, чем shell curl.
- Проверить, как normalized tool status/error попадает в Worker context и Result. Ошибка capability/provider не должна называться недоступностью сайта.
- Отдельно зафиксировать, исправляет ли default harness change из ticket31 эту конкретную причину.

## Acceptance

- Есть доказуемая причина сбоя либо точный список отсутствующего evidence. Не заменять диагноз догадкой.
- Зафиксированы host/harness/version, tool name, безопасная категория ошибки и релевантный HTTP status без credentials/raw ACP/сырых tool outputs.
- Определено исправление или follow-up task с tests.
- При неподтверждённых погодных данных Worker не выдумывает температуру. Сообщает краткую точную причину, а не утверждает недоступность сайта по общей ошибке tool.
- Диагностика не отключает TLS checks и не расширяет capabilities/scopes.

## Evidence

Screenshots `clipboard-2026-10-03-154733-E341CA51.png` и `clipboard-2026-10-03-154839-116E8F48.png`; контрольный `clipboard-2026-10-03-154937-87A78503.png`. Локальные временные изображения не копируются в Git.

## Investigation, 3 октября 2026

Metadata данного Worker фиксирует два `web_fetch` со статусом `failed` около 15:47 Asia/Barnaul; сам Attempt завершился `succeeded`. У tool invocations нет сохранённых безопасных `error_code`/`http_status`. Доказан только неуточнённый сбой tool.

Ограниченные shell-пробы после инцидента с самого omarchy получили `curl code 0` и `HTTP 200` от обоих публичных hosts, `wttr.in` и `api.open-meteo.com`; DNS и TLS установились. Это не повторяет native tool path и не доказывает состояние источников в момент инцидента.

Точная причина неизвестна. `acpToolTracker`/`acpToolResult` и `redactACPLogFrame` в `internal/node/acp_runtime.go` оставляют безопасный статус, но исходные params/results ACP в логах редактируются; пригодного error code в доступной metadata нет.

Следующее исправление observability должно сохранять allowlisted error category/code/HTTP status, когда producer их действительно предоставляет, без credentials, raw payload или tool output. Нельзя заполнять отсутствующий HTTP status догадкой или ослаблять redaction.

## Related

Ticket31 меняет default harness, но не является автоматически доказанным решением этого сбоя.

## Answer

### Public-boundary observability

`core.ToolFailureMetadata` допускает только явные producer fields: allowlisted `category`/`code` и числовой `http_status` 100–599. ACP adapter читает только необязательный объект `rawOutput.metadata.failure` и только при фактическом `status=failed`; неизвестные значения остаются неизвестными. Статус, title, свободный error text и output не используются для классификации. `failed` результат не переносит свободный error/output дальше adapter boundary. Public Worker Activity observer скрывает tool arguments, outputs и free-form errors, но публикует проверенную metadata; UI показывает только allowlisted поля.

Эта схема намеренно не содержит категории `site_unavailable`. `provider` и `capability` могут оставаться отдельными явными категориями и не превращаются в отказ сайта. Изменения ограничены observability: не меняют auth, permissions, TLS или network scopes.

### Прочитанные producer contracts и ограничения

- Локальный OpenCode `v2.0.22`: `packages/cli/src/acp/tool.ts` формирует ACP `tool_call_update` с `status=failed`, `rawOutput.error` и необязательным producer metadata. Эта функция не выдаёт стандартизованные safe error category/code/HTTP status. `packages/cli/src/acp/translate.ts` передаёт `event.data.metadata ?? tool.metadata` и `error.message` в этот сериализатор.
- OpenCode `packages/core/src/tool/plugin/webfetch.ts` проверяет успешный HTTP status через `filterStatusOk`, а затем заворачивает failure в `ToolFailure`; его ACP-facing error — свободный текст, а HTTP status не сохраняется в описанном tool metadata contract.
- Локальные команды сообщают `fx 0.0.8` и `opencode v2.0.22`; их `--help` подтверждает ACP stdio entrypoints. Это текущие локальные версии, не доказательство исторических версий Worker. Source contract `fx` для safe error fields в доступных материалах не найден.
- Native runtime comparison на одном URL не запускался: в ходе этого исследования не был подготовлен общий unpaid fixture, запускающий оба native producer без model/provider state. Новые unpaid ACP fixtures проверяют только adapter contract, не native `fx`/OpenCode prompt path. Использовались только локальные `--version`/`--help`; production Worker, auth state, managed store и personal credentials не затрагивались. Поздние shell HTTP 200/DNS/TLS пробы из инцидентной записи не повторялись: они не отвечают на вопрос о native tool path.

### Исторический incident и оставшийся evidence gap

Доступная запись фиксирует два вызова `web_fetch` со статусом `failed` и успешный Attempt; safe `error_code`, `http_status`, error category, точный historical Worker host/node и runtime version отсутствуют. Запись называет `omarchy` host для поздних shell-проб, но это не устанавливает host/path инцидентного native invocation. Имя tool известно только из сохранённого пользовательского отображения, а не из пригодного native error metadata. Поздний `curl` HTTP 200 к `wttr.in` и Open-Meteo не доказывает доступность через native tool во время инцидента. Поэтому причина остаётся неизвестной; утверждать отказ сайта, виновный harness или погодные значения нельзя. Ticket31 сам по себе не доказывает исправление этой причины.

Follow-up для настоящего диагноза: если producer надёжно знает category/code/status, он должен выдать их явно по `rawOutput.metadata.failure`; затем тот же безопасный контракт следует подтвердить на native unpaid fixture. Если таких полей у producer нет, incident останется с точной причиной неизвестной, пока не появится безопасное native evidence.

### Verification

- RED: `go test ./internal/webapi -run '^TestWorkerObserverExposesSafeToolFailureMetadataWithoutToolPayloads$' -count=1` показал утечку tool output через публичный observer.
- GREEN: `go test ./internal/node ./internal/core ./internal/webapi` — PASS.
- GREEN: `cd web && npm test` — PASS (10 tests); `cd web && npm run build:all` — PASS; `go test ./web` — PASS; `go vet ./internal/node ./internal/core ./internal/webapi` и `git diff --check` по затронутым файлам — PASS.
