# Исследование: официальный Codex API для продолжения после лимита

Дата: 11 октября 2026. Gemini исследовал реальный инцидент и существующий lifecycle в раундах 1–2. Раунд 3 остановлен лимитом Gemini до внесения изменений; основной агент проверил официальную документацию и local CLI schema и завершил план. Исходный код не изменён.

## Первичные источники

[Codex App Server, официальная документация OpenAI](https://learn.chatgpt.com/docs/app-server) описывает read RPC `account/rateLimits/read`, notification `account/rateLimits/updated`, buckets `rateLimitsByLimitId`, `usedPercent`, nullable `resetsAt` в Unix-секундах. Официальная documentation подтверждает API и единицы времени, но не гарантирует entitlement/reset timestamp для конкретного аккаунта.

Установленный CLI `codex-cli 0.162.1` позволяет сгенерировать version-specific protocol через `codex app-server generate-ts --out <temporary-directory>`. Основной агент выполнил это без model request и прочитал `ClientRequest.ts`, `InitializeParams.ts`, `GetAccountRateLimitsResponse.ts`, `RateLimitSnapshot.ts`, `RateLimitWindow.ts`, `RateLimitReachedType.ts`, `AccountRateLimitsUpdatedNotification.ts`. Snapshot поддерживает `normalModelSlug`; read response — optional account identity/permission; sparse notifications нельзя воспринимать как полную замену nullable metadata. На execution Node 0.162.0 новые поля могут отсутствовать: decode optional, unsupported/ambiguous read → manual hold.

## Подтверждённый инцидент

[Отчёт Gemini и независимая повторная проверка](../../docs/research/worker-acp-incident-gemini-verification.md) связывают production Result с native `usage_limit_exceeded`. В конкретном native `token_count` `limit_id=premium`, primary/secondary отсутствуют. Из этого нельзя вычислить reset и нельзя объявлять ошибку billing failure. Исторические JSONL snapshots с resets_at подтверждали только возможный формат внутренних данных. Они больше не являются operational source.

## Выбранный путь интеграции

`CodexRuntime` — обёртка `ACPRuntime`; `codex-acp` скрывает native connection. Официальный account RPC нельзя послать в ACP `session/*` channel. Выбран короткоживущий native app-server client на исходном Node: initialize/initialized/read/close, 5s deadline, те же selected binary/native config/account environment, без thread/turn/login/reset-credit calls. Generic JSON-RPC plumbing уже есть в `internal/acp/jsonl.go`; отдельный model runtime и внешний daemon не нужны.

Один read выполняется после confirmed usage limit. Multi-bucket scope должен однозначно соответствовать effective Worker model/account; брать max будущего reset всех exhausted windows. Отсутствие/неоднозначность времени → manual hold. Не угадывать дата/TZ из текста. Не менять category по отсутствию exhausted windows. Explicit backend credit/permission blockers запрещают auto. Reset timestamp означает время следующей попытки, не доказанное восстановление доступа.

## Lifecycle и гарантии

`finishRetryableAttempt` уже поддерживает next Attempt одного Turn без Result. План расширяет атомарную transaction расписанием, checkpoint и command intent. Scheduled/manual hold сохраняют FIFO; command claim/receipt identities разделяют prepared и unknown handoff при crash. Native continuation prompt сохраняет history и не переотправляет original input. Подробные решения и проверки встроены в [implementation ticket](issues/01-auto-resume-after-usage-limit.md); они обязательны для реализации.

## Ограничения исследования

Official read для живого Worker/account не выполнялся; API и generated protocol проверены, доступность timestamp в каждом аккаунте не заявлена. Query/auth/version failures имеют заданный fallback. Текущий Claude adapter не нормализует reset timestamp; ambiguous историческая quota-or-billing ошибка не запускает auto. Scope — Worker. Source-code regression implementation и deployment не выполнялись.
