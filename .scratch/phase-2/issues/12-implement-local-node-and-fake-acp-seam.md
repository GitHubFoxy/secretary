# Implement local Node and fake ACP seam

Type: task
Status: resolved
Triage: ready-for-agent
Blocked by: 10

Spec: ../spec.md

## Work

Implement the Node application contract and its embedded local implementation. Node must create Workspace, accept Dispatch only after runtime readiness, forward live activity to observer subscribers, deliver Steering with idle fallback, Cancel active Attempt and report idempotent Result.

Build a deterministic fake Codex ACP process for automated tests. It must model readiness, steerable and non-steerable turns, activity, interrupt and terminal Result. Do not depend on a real model in automated tests.

## Answer

Local Node реализует Workspace creation, runtime readiness, persistent Worker session, Steering, optional idle-only Queueer, Cancel и activity stream. `ACPRuntime` работает поверх stdio JSONL, выполняет `initialize` и `session/new`, преобразует ACP updates, поддерживает повторные prompt turns и cancelled stop reason.

Добавлен отдельный `cmd/fake-codex-acp` и process-level helper в `internal/node/acp_runtime_test.go`. Tests покрывают readiness, activity, steerable/non-steerable behavior, queued prompt, Cancel и terminal Result без реальной модели.
