# 00 Pi viewer contract

Type: task
Status: resolved
Blocked by: none
Contract: `docs/pi-viewer.md`

## Goal

Зафиксировать первый usable Secretary client: Pi на MacBook Air только читает состояние always-on Laptop server.

## Topology

```text
Always-on Laptop: Secretary server, SQLite, Node daemon, Codex/fx, Tailscale
MacBook Air: Pi viewer, Tailscale
Phone/Telegram: out of scope
```

## Work

- Описать read-only Pi viewer contract в production documentation.
- Зафиксировать, что Pi не является Node, Worker harness или вторым source of truth.
- Зафиксировать initial snapshot, live updates, reconnect и resync semantics.
- Зафиксировать безопасные public fields для Conversation, Secretary state, Worker, Activity, Result и Approval summary.
- Зафиксировать не-цели первого релиза: сообщения, Worker commands, approvals, Node control, Telegram и Web UI changes.

## Acceptance

- Один документ однозначно описывает topology, credential model, screens, reconnect и privacy boundary.
- Все следующие tickets ссылаются на этот contract.
- Contract не требует нового domain state или нового transport.

## Answer

Контракт написан в `docs/pi-viewer.md`, документы 01-05 получили строку `Contract: docs/pi-viewer.md`, долги переданы в тикеты 01-03. Нового domain state и транспорта не добавлено: limit/cursor параметры и public DTO не меняют ни то, ни другое.

Первое ревью вернуло тикет в работу, второе подтвердило исправления и закрытие. Что подтвердилось по коду:

- `approvalList` отдаёт `[]core.Approval` целиком без sanitizer: `request_id`, `worker_id`, `turn_id`, `attempt_id`, `node_id`, `project_id`, `response`, `resolved_by`, `audit_event_id`.
- `Store.Approvals` читает `phase4_approvals` без фильтра по Person или Conversation.
- `EntriesAfter`, `GET /v1/workers` и `GET /v1/approvals` не имеют limit.
- Пустой `scopes` при pairing даёт полный default набор из 13 scopes, а `SecretaryPairOptions.scopes` в Pi optional и `client.ts` не отправляет поле, если оно не задано.
- `secretaryd` слушает `127.0.0.1:8081` (`-listen`), `sex` хардкодит тот же адрес; `-listen 0.0.0.0` ничем не запрещён. Control Room и `/v1/control/*` отдаются только при `-debug`.
- Sanitizer не трогает `node_id`, `project_id`, `harness_instance_id`, `workspace`, `policy_snapshot`.

Исправлено в контракте:

- Topology переписана как конкретная модель: loopback-only listener, Tailscale Serve HTTPS proxy, ACL по owner devices, запрет прямого bind, Control Room вне публикуемого surface.
- Serve surface зафиксирован вариантом B: полный `/v1` proxy поверх одного loopback server, с перечислением защищённых routes и требованием negative tests вместо path allowlist.
- Раздел "Требуемая работа по surface" фиксирует bounded snapshot, public approval DTO и фильтр approvals как долг, а не как существующую гарантию.
- Privacy boundary переформулирован: каждый Pi endpoint возвращает отдельный allowlisted public DTO, generic sanitizer остаётся defence-in-depth.
- Состав snapshot задан явно: status, latest Result summary, последняя безопасная activity summary, pending approvals, без WebSocket в `--once`.
- Credential model: pairing без явного scope list объявлен требованием контракта, отмечено, что server-side проверки пока нет.

Осталось закрыть тикетами, не документом:

- 01: loopback + Tailscale Serve + ACL, запрет `-listen 0.0.0.0`, Serve surface с negative tests против Node/internal/control routes, Control Room debug-only.
- 02: server отклоняет pairing без явного scope list (существующие вызовы парятся без scopes, нужна миграция), read-only scope matrix, revoke для HTTP и WebSocket streams.
- 03: server-side limit/cursor для snapshot, allowlisted DTO включая approval summary, фильтр approvals по Conversation.

Вне scope тикета: в `CONTEXT.md` нет термина `Pi viewer`, у `phase-5-pi-viewer` нет `map.md`, поэтому запись в `Decisions so far` не делалась. Изменения не закоммичены.
