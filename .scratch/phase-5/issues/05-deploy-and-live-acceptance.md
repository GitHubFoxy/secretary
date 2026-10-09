# 05: развёртывание и реальная приёмка Minimal MVP

Type: task
Status: blocked
Blocked by: 01, 02, 03, 04

## What to build

Развернуть проверенную интегрированную сборку на согласованных hosts и дать пользователю работающий Secretary: единый Web/Telegram chat, Codex/CC Workers на local/remote Nodes, прямой steering, durable `/q`, resume и читаемые Results в Worker topics и General.

## Acceptance criteria

- [ ] 01–04 resolved, существующие scoped checks и сборка проходят на одной integration branch. Ни один unmet CC steering criterion не исключён из gate.
- [ ] Развёрнутые binary/version/config совпадают с проверяемой сборкой; services реально active, health отвечает. Credentials доступны service environment без вывода значений, actual runtime readiness успешна.
- [ ] На используемых Nodes установлен совместимый adapter выбранного native runtime; MVP default/предлагаемый выбор — Codex и CC. Historical FX/OpenCode/Pi bindings/data и global harness settings не удалены.
- [ ] Настоящий Secretary model turn в Web и Telegram General даёт один canonical reply в обоих каналах; второй turn тоже отвечает, пустой/error terminal видим и не маскируется успешным статусом.
- [ ] Через Secretary MCP выполнены реальные Workers обоих harness и обе топологии local/remote: Task/Worker binding, native profile marker, доступный MCP, выполнение и Result. Минимальный setup — Secretary+Codex Worker на omarchy и CC Worker на paired Mac с существующей авторизацией. Не требуется каждый harness на каждой машине; subscription credentials не копируются.
- [ ] Для каждого из двух harness на авторизованном Node обычный direct input во время безопасной долгой работы изменяет текущую Attempt на safe boundary до natural terminal. Сохранены события принятия и наблюдаемого применения, отсутствие подмены следующим turn/interrupt.
- [ ] Для каждого из двух harness на авторизованном Node `/q` виден сразу, не доставлен до terminal, запускает FIFO Follow-up после Result; replay не повторяет delivery, restart сохраняет queue.
- [ ] Idle Follow-up и restart/resume сохраняют прежнюю session identity и context marker. Active Attempt при restart не запускается снова автоматически; отсутствующая session fails closed.
- [ ] Настоящий Telegram bot admin/forum group создаёт topics; General→Secretary и topic→Worker работают, Result один раз в topic и General; длинный Markdown/code читается после splitting. Web показывает ту же durable переписку и читаемую activity.
- [ ] Offline node, invalid/missing auth, unsupported operation и empty terminal дают понятные ошибки без hidden retry. Временные acceptance tasks завершены аккуратно, историческая пользовательская работа сохранена.
- [ ] `## Answer` содержит PASS/FAIL каждого сценария, дату, build/runtime versions, hosts, identities, наблюдаемые события и ссылки на evidence. Scoped fixtures перечислены отдельно и не выдаются за native/Telegram evidence. Phase 5 PASS только когда все требования реально пройдены.

## Проверка

Public seams: server HTTP/MCP, native Session, Web rendering и Bot API. Проверять текущую service environment, не делать вывод об auth по случайной интерактивной SSH shell. На безопасных коротких model tasks подтвердить и transport acceptance, и применение требования. Результаты хранить отдельно от deterministic fixtures, без credentials/private prompts/reasoning. Если environment временно мешает одному сценарию, точно назвать blocker, продолжить независимые проверки и оставить gate открытым.

## Answer

Пока отсутствует. Развёртывание и live acceptance ещё не проводились в Phase 5.

## Comments

Разблокировать после 01–04, затем claimed. Одобрение пользователя распространяется на работу до настоящего Minimal MVP; повторно согласовывать уже одобренный breakdown/seams не нужно.
