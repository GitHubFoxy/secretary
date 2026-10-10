# Phase 5: работающий Minimal MVP

Status: resolved

## Цель и основание

Пользователь получает одну Personal Conversation в Web и Telegram. Secretary отвечает и создаёт Workers Codex либо Claude Code (CC) на своей машине и на другой подключённой машине. Пользователь пишет Worker напрямую через простой Worker observer или Telegram topic. Обычный ввод направляет активную работу на ближайшей безопасной границе текущей Attempt; `/q` сохраняет Follow-up и ждёт idle.

Основание: [Minimal MVP](../../spec/THE%20spec.md#minimal-mvp), [словарь](../../GLOSSARY.md), [runtime contracts](../../docs/architecture/runtime-contracts.md), исследования [runtime](../../docs/research/minimal-mvp-runtime-audit.md), [чат](../../docs/research/minimal-mvp-chat-audit.md), [native protocols](../../docs/research/minimal-mvp-native-protocols.md) и [готовность](../../docs/research/minimal-mvp-readiness-20261009.md). Предыдущая [Phase 4](../.archived/phase-4/spec.md) — исторический контекст, а не дополнительные обязательные требования этого gate.

Пять tickets и реализация до работающего MVP одобрены пользователем. Выбор public seams и минимального пути также одобрен; повторное подтверждение разбивки не требуется. Текущая Codex live acceptance завершена; результат и границы scope приведены ниже.

### Текущая приёмка: Codex

2026-10-10 пользователь явно поручил отложить Claude Code и довести Codex до работающего Minimal MVP. Текущий gate требует все перечисленные ниже сценарии для Codex Secretary и Codex Workers на local/remote Nodes, включая Web и настоящий Telegram. CC ticket 02 и его native steering отложены; прежние требования к CC сохранены как последующий этап и не считаются выполненными. Успешная текущая приёмка означает Codex MVP, а не доказанную поддержку обоих harness. Авторизация Codex на omarchy восстановлена; развёрнутый путь Secretary/Workers проверяется в gate 05.

## Сквозное поведение

1. Обычный запрос Secretary в Web или Telegram General получает один сохраняемый canonical ответ, видимый в обоих каналах. Worker Result добавляется в общую переписку без автоматического Secretary turn и без повторного пересказа.
2. Secretary через существующий MCP выбирает Codex/CC и enrolled Execution node, создаёт Task/Worker binding, показывает Worker и возвращает Result. Co-located Workers используют существующий durable `secretary-node`, как remote Workers.
3. Native runtime реально получает содержимое профиля и MCP, сохраняет свою session identity и историю. Idle Follow-up продолжает прежнюю session. После restart Worker binding сохраняется; активная Attempt не повторяется автоматически. Отсутствие прежней session приводит к явной ошибке.
4. Прямой ввод активному Worker по умолчанию — Steering message. Успех подтверждает применение требования до естественного окончания текущей работы в той же Attempt. Принятие transport input, запуск следующего turn, очередь или interrupt + новый запрос сами по себе не доказывают steering.
5. `/q текст` создаёт durable Queued message в Secretary server, немедленно видимую пользователю. Prefix не попадает в prompt. Она доставляется только после terminal Result текущей Attempt как новый Follow-up. FIFO, идемпотентность, restart и закрытие Task имеют определённое наблюдаемое поведение.
6. Telegram — группа с topics, бот с правами администратора, General для Secretary и отдельный topic для каждого Worker. Сохраняемые mappings переживают restart. Result появляется один раз в topic и один раз в General. Неизвестный topic не адресует случайного Worker.
7. Web показывает текст, Markdown, безопасные ссылки и code blocks; Worker activity читается как текст и tool/status, а не JSON. Telegram сохраняет читаемость длинных сообщений, escaping и fallback.

## Минимальный путь

Сохранить Secretary server, store, pairing, Worker lifecycle и outbound Node transport. Codex сначала проверить через установленный `codex-acp`; app-server — ограниченный резервный путь при доказанной несовместимости. Проверять тот executable/version/capabilities, который действительно запускает runtime. Secretary config не наследует OpenCode model/reasoning pins и не меняет глобальные пользовательские настройки harness.

Claude требует интерактивной native Session с deferred Start, Prompt, MCP, profile, resume и событийным reader. Сначала проверить реальный streaming CLI либо официальный Agent SDK. Очередь SDK не означает same-turn steering. Если реальное immediate steering не подтверждается, оставить этот acceptance незакрытым, сохранить явную ошибку неподдерживаемой операции и документировать blocker. Нельзя молча направлять обычный ввод в очередь, объявлять interrupt + новый запрос эквивалентом или требовать, чтобы пользователь отказался от согласованного default. Исследование альтернатив внутри native CC продолжается; изменение продуктового требования возможно только по отдельному явному решению пользователя.

Для canonical reply можно сохранить работающий legacy контракт Codex либо доказать addressed-reply-v1 с необходимыми native grouping capabilities. Не ослаблять guard пустого/error terminal, не выводить успех без ответа и не повторять неизвестное выполнение автоматически.

## Границы реализации и проверки

Публичные seams: server HTTP/MCP, native Session interface, Web rendering и Telegram Bot API channel output. Server владеет identity, очередью, Task/Attempt/Result; runtime выполняет native работу; Channel adapters отображают состояние и отправляют команды. Прямые пользовательские Worker inputs не синхронизируются с model context Secretary.

Работа идёт на одной integration branch. Tickets 01–04 могут выполняться независимо на существующих контрактах; изменения общего seam согласуются до правок. Короткоживущие ticket branches/worktrees интегрируются в одну integration branch; отдельных долгоживущих линий разработки нет. 05 интегрирует сборку и проверяет полную матрицу. Ticket закрывается по своим критериям, Phase 5 — только после gate 05.

Использовать существующие regression/acceptance tests. Для нового или исправляемого поведения сначала воспроизвести ошибку через соответствующую публичную границу, затем минимальная правка и green. Не писать тесты приватных методов, зеркала реализации или отдельный framework. Fake runtime/Bot API допустимы для детерминированных негативных сценариев и crash/replay, но live evidence хранится отдельно с версиями, hosts, session/Attempt/input identities, observed events и выводом. Credentials, private prompts и reasoning не публиковать.

## Не входит в Phase 5

FX/OpenCode/Pi не требуются для нового default, предлагаемого MVP выбора или acceptance. Их установленные binaries, исторические bindings и данные не удаляются ради MVP. Не завершать все старые Phase 4 tickets. Не строить новые pairing/transport/MCP системы, облачную установку, account management, native GUI, sandbox/worktree систему, глобальный skills manager или синхронизацию прямого диалога Worker с Secretary. Не разрабатывать собственный model agent loop.

## Definition of Done

- [x] Tickets 01, 03, 04 закрыты по своим критериям; 02 отложен по решению пользователя, scoped checks и сборка проходят.
- [x] На реальных enrolled Execution nodes проверен Codex и обе топологии: local и remote относительно Secretary server. Минимальный доступный setup — Secretary и Codex Worker на omarchy, Codex Worker на paired Mac с существующей авторизацией. Каждое сочетание harness × host не является обязательным требованием; credentials между машинами не копируются.
- [x] Для Codex на обоих авторизованных Nodes доказаны profile delivery, MCP, Result, same-session idle Follow-up, steering в активной Attempt, durable `/q` и restart/resume без незаметной новой identity.
- [x] В Web и Telegram проходит одна общая Personal Conversation, canonical Secretary reply, прямой Worker input, readable text/code, отдельные Worker topics и Result без дублей.
- [x] Offline/auth/unsupported/empty terminal дают понятные ошибки и не вызывают hidden retry; исторические данные сохранены.
- [x] Развёрнутая версия и активные services проверены на работающих hosts; gate 05 содержит реальные свидетельства и PASS. Fixtures не засчитываются как live evidence. CC steering не входит в текущий Codex gate и остаётся отдельной незавершённой задачей.

## Результат текущего scope

2026-10-10: Codex Minimal MVP PASS, gate05 resolved. [Матрица](reports/codex-mvp-final-20261010.md). Поддержка Claude Code не объявлена доказанной;02 остаётся отложенным отдельным продолжением.
