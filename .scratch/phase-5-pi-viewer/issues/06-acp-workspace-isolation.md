# 06 ACP cwd/workspace isolation

Type: task
Status: resolved
Blocked by: none
Contract: `docs/pi-viewer.md`
Blocks: 01 (real Worker acceptance)

## Goal

ACP-harness должен стартовать строго в mapped project workspace. Worker с project mapping не должен писать вне declared workspace.

## Background

Real Worker flow из тикета 01 признан FAIL: worker с mapping `omarchy -> .../acceptance-ws` создал файл с верным содержимым в `/home/coder/ACCEPTANCE.md`, workspace остался пустым.

Причина подтверждена кодом: `internal/acp.StartWithLogEnv()` (`internal/acp/jsonl.go`) создаёт `exec.CommandContext`, но не получает и не ставит `process.Dir`. `ACPRuntime.connect()` (`internal/node/acp_runtime.go`) не передаёт `request.Workspace` в subprocess. fx ACP берёт primary workspace из process cwd (`fx acp` надо стартовать из каталога проекта, per fx docs), per-session `cwd` в `session/new` его не меняет. Для сравнения: `claude_runtime.go:181` выставляет `cmd.Dir = request.Workspace`.

## Work

Минимальный scope:

```text
StartRequest.Workspace
→ ACP subprocess cmd.Dir
→ fx acp / Codex ACP / любой ACP harness стартует именно в mapped workspace
```

- Прокинуть workspace из `ACPRuntime.connect()` в `acp.StartWithLogEnv()` (или эквивалент) как `process.Dir`.
- Не менять approval DTO, scope validation и limit-параметры (тикеты 02-03).
- Не менять контракт `docs/pi-viewer.md`.

## Acceptance

```text
Worker пишет sentinel только в mapped workspace.
$HOME не содержит stray file.
Неявный temporary workspace не подменяет Project mapping.
Claude и ACP runtimes имеют одинаковый workspace contract.
Tests проверяют cmd.Dir без real provider.
Real fx run подтверждает фактический cwd.
```

Порядок: сначала фикс, затем повтор real Worker flow из тикета 01, backup/restore-check, и только тогда закрывать тикет 01.

## Answer

Закрыт 2026-09-22, проверено на реальном железе (omarchy).

- Фикс: `StartWithLogEnvDir` (`internal/acp/jsonl.go`) + `connect` принимает workspace (`internal/node/acp_runtime.go`), коммит `f336024`. Unit-тесты проверяют фактический cwd дочернего процесса без real provider.
- Деплой: `git pull`, `sex setup` (пересборка), restart обоих юнитов, health ok.
- Повторный real fx flow: один Worker на `omarchy/fx`, один terminal succeeded Result. Sentinel с точным содержимым создан строго в mapped workspace, `$HOME` чист (stray files отсутствуют).
- Свежий backup + restore-check: `integrity_check ok`, `workers|2`, `phase4_results|2` (оба `succeeded`).
- Все пункты acceptance тикета выполнены. Тикеты 06 и 01 закрыты.
