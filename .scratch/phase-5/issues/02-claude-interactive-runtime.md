# 02: интерактивный Claude Code с MCP, profile и настоящим steering

Type: task
Status: ready-for-agent
Blocked by: None

## What to build

Пользователь выбирает CC для Secretary/Worker, получает ответы из настоящего Claude Code, запускает Worker на доступном enrolled Node и уточняет активную работу обычным прямым сообщением. Текущий one-shot print adapter заменить минимальной интерактивной native Session вокруг доказанного streaming CLI или официального Agent SDK.

## Acceptance criteria

- [ ] Первым шагом native spike отправляет уникальный input во время длинного безопасного действия, фиксирует session/turn events и различает same-turn применение, delivery после terminal и interrupt + новый запрос.
- [ ] Выбранный transport поддерживает deferred Start, Prompt, постоянное чтение событий и сериализованную отправку input, MCP config, native profile delivery и resume. Не создаётся собственный model loop или новый server lifecycle.
- [ ] CC Secretary реально вызывает существующий MCP read-only tool и lifecycle tool с правильной server identity; Worker на доступном авторизованном Node получает profile content, доказанный marker, и MCP endpoint, доступный с соответствующего host.
- [ ] CC Worker возвращает ровно один Result по Attempt; partial/final native events не удваивают текст. Cancel/interrupt корректно завершает старую Attempt, не связывает её terminal с новым query.
- [ ] Idle Follow-up и restart продолжают прежнюю native session с ранее заданным marker; missing session fails closed. Auth/config ошибки видимы пользователю.
- [ ] Обычный direct input применяется в текущей активной Attempt на safe boundary до её естественного завершения. CapabilitySteering выставлена только после такого native evidence.
- [ ] Если spike подтверждает лишь queue/interrupt, критерий steering остаётся незакрытым, unsupported выражается явно, blocker записан. Не делать silent queue, не называть interrupt + Follow-up steering и не требовать смены пользовательского default как условия продолжения.
- [ ] Обычный Secretary final сохраняет canonical ответ, MCP addressed reply не дублирует его, пустой/error terminal сохраняет видимую ошибку.

## Проверка

Public seams: native Session и server HTTP/MCP. Использовать имеющиеся native adapter, reply/completion и continuation tests; существенные изменения — через red/green на seam. CLI flags или SDK async generator не являются свидетельством steering. Live evidence хранится отдельно от fixtures, с версиями и identities, без credentials/reasoning. Записать фактическую семантику до окончательного выбора transport. Неподтверждённый steering блокирует закрытие этого ticket и gate 05, а не начало независимых 03/04.

## Answer

Пока отсутствует.

## Comments

User одобрил native spike и минимальный SDK bridge при необходимости; обязательное требование immediate steering сохранено.

Текущий минимальный acceptance setup использует авторизованный CC на Mac. Не требуется копировать auth в omarchy или устанавливать CC на каждый host ради полной декартовой матрицы; обе топологии и оба harness проверяются в 05.
