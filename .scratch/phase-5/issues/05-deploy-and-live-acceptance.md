# 05: развёртывание и реальная приёмка Minimal MVP

Type: task
Status: resolved
Blocked by: None

## What to build

Текущий scope с 2026-10-10: пользователь явно отложил Claude Code и поручил завершить Codex MVP. Критерии ниже проверяются для Codex; прежние CC критерии сохранены для последующего этапа, ticket 02 не блокирует текущую Codex приёмку и не считается выполненным.

Развернуть проверенную интегрированную сборку на согласованных hosts и дать пользователю работающий Secretary: единый Web/Telegram chat, Codex Workers на local/remote Nodes, прямой steering, durable `/q`, resume и читаемые Results в Worker topics и General.

## Acceptance criteria

- [x] 01, 03, 04 resolved, существующие scoped checks и сборка проходят на одной integration branch. Ticket 02 явно отложен по решению пользователя.
- [x] Развёрнутые binary/version/config совпадают с проверяемой сборкой; services реально active, health отвечает. Credentials доступны service environment без вывода значений, actual runtime readiness успешна.
- [x] На используемых Nodes установлен совместимый adapter выбранного native runtime; текущий MVP default/выбор — Codex; CC отложен. Historical FX/OpenCode/Pi bindings/data и global harness settings не удалены.
- [x] Настоящий Secretary model turn в Web и Telegram General даёт один canonical reply в обоих каналах; второй turn тоже отвечает, пустой/error terminal видим и не маскируется успешным статусом.
- [x] Через Secretary MCP выполнены реальные Codex Workers и обе топологии local/remote: Task/Worker binding, native profile marker, доступный MCP, выполнение и Result. Минимальный setup — Secretary+Codex Worker на omarchy и Codex Worker на paired Mac с существующей авторизацией. Не требуется каждый harness на каждой машине; subscription credentials не копируются.
- [x] Для Codex на каждом из двух авторизованных Nodes обычный direct input во время безопасной долгой работы изменяет текущую Attempt на safe boundary до natural terminal. Сохранены события принятия и наблюдаемого применения, отсутствие подмены следующим turn/interrupt.
- [x] Для Codex на каждом из двух авторизованных Nodes `/q` виден сразу, не доставлен до terminal, запускает FIFO Follow-up после Result; replay не повторяет delivery, restart сохраняет queue.
- [x] Idle Follow-up и restart/resume сохраняют прежнюю session identity и context marker. Active Attempt при restart не запускается снова автоматически; отсутствующая session fails closed.
- [x] Настоящий Telegram bot admin/forum group создаёт topics; General→Secretary и topic→Worker работают, Result один раз в topic и General; длинный Markdown/code читается после splitting. Web показывает ту же durable переписку и читаемую activity.
- [x] Offline node, invalid/missing auth, unsupported operation и empty terminal дают понятные ошибки без hidden retry. Временные acceptance tasks завершены аккуратно, историческая пользовательская работа сохранена.
- [x] `## Answer` содержит PASS/FAIL каждого сценария, дату, build/runtime versions, hosts, identities, наблюдаемые события и ссылки на evidence. Scoped fixtures перечислены отдельно и не выдаются за native/Telegram evidence. Phase 5 PASS только когда все требования реально пройдены.

## Проверка

Public seams: server HTTP/MCP, native Session, Web rendering и Bot API. Проверять текущую service environment, не делать вывод об auth по случайной интерактивной SSH shell. На безопасных коротких model tasks подтвердить и transport acceptance, и применение требования. Результаты хранить отдельно от deterministic fixtures, без credentials/private prompts/reasoning. Если environment временно мешает одному сценарию, точно назвать blocker, продолжить независимые проверки и оставить gate открытым.

## Answer

**Codex MVP PASS — 2026-10-10**, по явно изменённому пользователем scope. Tickets01/03/04 resolved;02 отложен, не считается прошедшим. Исполняемый код `af1290a15bae1b345c20654e26143fc0dab859ec`, оба Nodes online, HTTPS healthok, services enabled. Full Go `-p1`,19 Web tests/build, две независимые проверки без замечаний.

[Матрица каждого сценария и ограничения](../reports/codex-mvp-final-20261010.md), [native/Telegram/Web identities](../reports/codex-telegram-acceptance-20261010.md), [versions/deployment/сохранность](../reports/codex-deployment-20261010.md), [отдельная native crash/queue/missing-session матрица](../reports/mac-native-acceptance-20261010.md). Scoped fixtures явно отделены от model/Telegram evidence. Старые FAIL не удалены.

Secretary сам создаёт обычные Codex Workers на omarchy и Mac; direct steering, `/q` FIFO, idle/restart context, readable activity и длинный Markdown/code проходят. Тестовый MCP/profile убраны, новые обычные Workers241/242 реально выполнили pwd. Исторические Worker/Node/auth/config bindings и topics сохранены; собственные acceptance Tasks закрыты штатным API с сохранением истории.

## Comments

### Историческое состояние до изменения scope и deployment

Живая приёмка проведена на собственных Mac server/Node runtime. [Отчёт и матрица](../reports/mac-native-acceptance-20261010.md) разделяют реальные PASS, исходные FAIL и ещё не выполненные сценарии. Codex steering во время наблюдаемого sleep, active `/q` FIFO/replay, native MCP/profile/context, idle restart, active crash без auto execution, missing session, Web renderer/observer и восстановление persisted outbox подтверждены на указанных сборках. Prepared queue restart FAIL исправлен readiness guard; новый реальный сценарий на `89e3135` проходит FIFO после reconnect и сохраняет session/context. Scoped 03 resolved. Проверенный immutable Linux candidate с manifest/SHA256 размещён на omarchy; активное развёртывание не выполнялось.

05 остаётся blocked: 01/02/04 не resolved, CC same-Attempt steering не подтверждён, свежий native CC probe на финальной сборке возвращает quota403, omarchy Codex auth/readiness и настоящая remote/Telegram матрица не пройдены. Mac PASS, reviews и fixtures не объявляются готовым MVP или активным production deployment. Собственные acceptance Workers закрыты штатным owner API, история и исходные FAIL сохранены; временный 8092 оставлен idle для просмотра, 8091 остановлен и его Node отозван.



Разблокировать после 01–04, затем claimed. Одобрение пользователя распространяется на работу до настоящего Minimal MVP; повторно согласовывать уже одобренный breakdown/seams не нужно.

2026-10-10: текущий Codex gate закрыт после всех live критериев и cleanup; старое требование CC отложено по явному решению пользователя.
