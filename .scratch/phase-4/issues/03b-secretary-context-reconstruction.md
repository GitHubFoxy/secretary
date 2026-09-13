# 03b Secretary context reconstruction

Type: task
Status: resolved
Blocked by: 03a, 05b, 07, 08

## Work

Собрать canonical context для каждого нового Secretary turn после появления всех server-owned sources.

- Реконструировать context из Secretary identity, актуальной revision `user.md`, durable Conversation summary, recent entries, unseen Worker Results, open Workers, Projects, Nodes, HarnessInstances, active Approvals и policy/Profile snapshot.
- Использовать native Secretary runtime session только как cache/optimization, а не как source of truth.
- Применять новый `user.md` к следующему Secretary turn без изменения уже созданных Worker policy snapshots.
- Принимать terminal Worker Result напрямую в Personal Conversation без дополнительного Secretary model turn.
- Передавать следующий Secretary turn unseen Result из server-owned context.
- При restart/reload проверять сохранённый Worker/Turn state и не продолжать неизвестную работу молча.

## Acceptance

- Изменение `user.md` через durable revision видно в preference/context следующего Secretary turn.
- Terminal Result появляется в Conversation сразу после финального Turn и не создаёт дополнительный Secretary turn.
- Следующий Secretary turn получает этот Result как unseen Worker Result.
- Context reconstruction переживает server restart и пересоздание native runtime.
- Open Workers, Projects, Nodes, HarnessInstances и Approvals попадают в context с server-owned state.
- Unknown или stale native session не подменяет canonical context.
