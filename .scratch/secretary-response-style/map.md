# Карта: стиль ответа и делегирования

## Notes

[Тикет 01](issues/01-brief-ack-and-worker-prompts.md) реализован и закрыт. [Спецификация](spec.md), [проверки и production evidence](report.md).

## Decisions so far

- Общие требования к результату находятся во внешнем Worker profile, который задаётся через `profiles.worker`. Project `AGENTS.md` для этого не меняется.
- Production использует custom profiles: `profiles/secretary-codex-phase5.md` и `profiles/worker-template-luna6-low-9e3-v1.md`. При обновлении их нужно заменить явно.
- Раннее подтверждение не занимает единственный слот финального addressed reply: ошибку делегирования после подтверждения пользователь должен увидеть отдельно.
- Claude Code остаётся вне текущей живой приёмки.
- Runtime `6b8f826` развёрнут на обоих Nodes, внешний Secretary profile из main `4cba003` установлен явно. Native Web/Telegram и replay restart прошли; два независимых review без замечаний.

## Fog

- Нет открытых блокировок. Telegram доставка асинхронна: canonical acknowledgement сохраняется до рабочего вызова, сетевой receipt не является синхронным барьером для inventory tools.
