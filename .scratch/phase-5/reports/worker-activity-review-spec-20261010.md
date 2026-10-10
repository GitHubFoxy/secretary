# Worker activity: независимый Spec review

Дата: 2026-10-10. Диапазон: `43cd444...7adc71e33fff6821e937a3cb5e850b8bfbcf44de`. Проверен commit `7adc71e` в отдельном worktree `secretary-p5-worker-activity`; реализация не изменялась.

Основание: ticket [04](../issues/04-readable-unified-chat.md), критерий «Worker text activity отображается текстом, tool/status — компактно и понятно, generic payload не доминирует как сырой JSON», пункт 7 Phase 5 и текущий Codex-only scope integration checkout. CC отложен и здесь не оценивается.

## Findings

**0 P1/P2.** Объединение применяется в действительном Worker observer через общий `WorkerActivity`, а не только в тесте. Adjacent text chunks склеиваются без trim; сохранены границы Worker, Attempt, turn, channel и kind. Tool/status/input и скрытые reasoning events прерывают объединение. Replay dedup использует существующие server identities и seq; одинаковый текст разных identities не удаляется. Markdown использует существующий безопасный renderer, аргументы инструментов и raw reasoning не добавляются в видимый текст. Новая клиентская очередь или lifecycle не появились.

## Проверки

- `npm test --prefix web`: **19/19 PASS**, включая SSR production component. Hundreds-of-fragments fixture даёт единый читаемый Markdown block с heading, bold и JavaScript code; проверяет полную длину текста, tool/status ordering, Attempt/channel/input/hidden-reasoning boundaries и duplicate replay.
- Дополнительная read-only Node assertion проверила точное сохранение пробела и двух переносов, отдельные rows для разных Worker/Attempt/turn/channel, replay ordering/dedup и отсутствие thinking/reasoning/channel-analysis текста: **PASS**.
- Просмотрен actual observer flow и source diff; compiled bundle обновлён в commit. Live acceptance этой версии ещё не проверялась данным reviewer.

Ограничение: SSR fixture доказывает renderer, не весь native transport. Существующий до этого diff `normalizeRuntimeActivity(ActivityText)` отбрасывает whitespace-only chunks. Если ACP пришлёт отдельно пробел/перенос, renderer не сможет восстановить уже потерянный текст. Это не finding введённого изменения; сообщено координатору для реальной проверки читаемости native Markdown после deploy. Непроверенный live PASS не заявляется.
