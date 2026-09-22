# 00 Pi viewer contract

Type: task
Status: ready-for-agent
Blocked by: none

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
