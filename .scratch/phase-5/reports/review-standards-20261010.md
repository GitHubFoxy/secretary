## Standards

Проверено `git diff 43b9a8a...HEAD`, итоговый HEAD `acdba0caa299cb1633ab066f5b1134747435a163`. Код не изменён.

- **P1 — гонка Close с handoff отменённого Queued message.** `internal/ctl/worker_queue.go:109`, `internal/ctl/worker_lifecycle.go:843`. Pump может подготовить Follow-up и проверить AttemptStarting, затем остановиться перед handoff. Close отменяет delivering row, но сохраняет pending command intent; попытка Cancel новой, ещё не переданной Attempt завершается `runtime_session_unavailable`. После этого pump продолжает Dispatch по старому snapshot: claim не проверяет отмену queue, а `CompleteQueuedWorkerMessage` на уже canceled row возвращает nil (`internal/core/worker_queue.go:135`). Пользователь видит Canceled, хотя сообщение запускается. Это correctness finding и нарушение контракта `docs/architecture/runtime-contracts.md`, раздел Conversations: «Close отменяет накопленные сообщения до остановки активной Attempt». Нужна согласованная отмена delivery intent/claim с Close; перечитать row вне атомарного claim недостаточно.

- **P2 — добавлены запрещённые объясняющие комментарии.** `internal/core/worker_queue.go:112,122,180`, `internal/ctl/worker_queue.go:10,80,105`, `internal/node/codex_runtime.go:11`, `internal/node/codex_probe.go:18,65`, `web/src/Markdown.svelte:3`, `scripts/phase5-setup-test.sh:2`; аналогичные новые комментарии есть в runtime/tests. Hard violation `.scratch/code-comment-policy/spec.md`: «Do not add explanatory, descriptive, or rationale comments to code files». Пример: «CancelQueuedWorkerMessages commits closure intent …» утверждает защиту от гонки, которой нет. Удалить новые explanatory comments, необходимое объяснение перенести в Markdown; shebang/toolchain directives сохранять.

Других обоснованных Fowler smells не выявлено; автоматические style/tooling checks не дублировались.

Live evidence: deployment gate Codex и Claude quota blocker явно отмечены в документации; отсутствующие live acceptance claims не считаю отдельным нарушением качества реализации. Native локальные Codex проверки и executable fixtures не доказывают production Web/Telegram/remote round-trip.
