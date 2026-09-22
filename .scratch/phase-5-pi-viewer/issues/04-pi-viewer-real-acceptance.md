# 04 Pi viewer real acceptance

Type: task
Status: ready-for-agent
Blocked by: 01, 02, 03

## Goal

Подтвердить Pi viewer на настоящих always-on Laptop и MacBook Air, а не только doubles.

## Work

- Запустить настоящий Secretary server и Node daemon на always-on Laptop.
- Подключить MacBook Air через Tailscale с отдельным read-only Pi credential.
- Проверить initial snapshot, live Secretary response, active Worker, Activity и terminal Result.
- Проверить Pi restart, network interruption, server restart, Node offline/reconnect и credential revoke.
- Создать redacted evidence ledger с command, time, commit и PASS/FAIL/BLOCKED classification.

## Acceptance

- Реальный `pi --experimental secretary --once` получает server snapshot через Tailscale.
- Реальный interactive Pi видит Worker, который выполняется на always-on Laptop через fx или Codex.
- После controlled disconnect Pi корректно replay/resync-ится без duplicate entries.
- После server restart Pi восстанавливает viewer state.
- После revoke viewer не видит новые события и не reconnect-ится.
- Evidence не содержит credentials, tokens, prompts, native IDs или raw ACP frames.
