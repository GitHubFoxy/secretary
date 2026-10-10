# Карта: стиль ответа и делегирования

## Notes

Активный [тикет 01](issues/01-brief-ack-and-worker-prompts.md) реализует [спецификацию](spec.md).

## Decisions so far

- Общие требования к результату находятся во внешнем Worker profile, который задаётся через `profiles.worker`. Project `AGENTS.md` для этого не меняется.
- Production использует custom profiles: `profiles/secretary-codex-phase5.md` и `profiles/worker-template-luna6-low-9e3-v1.md`. При обновлении их нужно заменить явно.
- Раннее подтверждение не занимает единственный слот финального addressed reply: ошибку делегирования после подтверждения пользователь должен увидеть отдельно.
- Claude Code остаётся вне текущей живой приёмки.

## Fog

- Требуется доказать порядок раннего сообщения и вызова инструментов на реальном native Codex после обновления production.
