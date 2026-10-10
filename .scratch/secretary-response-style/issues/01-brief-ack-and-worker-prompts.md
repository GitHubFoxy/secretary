# 01: короткое подтверждение до инструментов и простой Worker prompt

Type: task
Status: resolved
Blocked by: None

## What to build

Реализовать [spec](../spec.md): acknowledgement до инструментов, короткий стиль без внутренних IDs/Node, минимальные task prompts и общие требования в external Worker profile, убрать Telegram префикс.

## Acceptance criteria

- [x] Короткий acknowledgement виден до tool/Dispatch, без позднего повторного «передал Worker…».
- [x] Worker prompt содержит суть запроса и важные ограничения, без повторяемого процедурного boilerplate.
- [x] Общий Worker profile требует минимальный отчёт либо сообщение о блокировке; значения при недоступных источниках не выдумываются.
- [x] Telegram topic показывает исходный prompt без «Задача от Secretary:», safe formatting/splitting/replay сохранены.
- [x] Canonical identity/ordering/errors/completion guards и exactly-once Results сохранены.
- [x] Scoped tests, два независимых review и живая production проверка проходят.
- [x] Изменения отправлены в main; production binaries/profiles обновлены; история и пользовательские файлы сохранены.

## Answer

Исправлено через субагента: runtime commits `c2be91f` и `6b8f826`, финальный Secretary profile в main `4cba003`. Подтверждение сохраняется до рабочих инструментов и доставляется без ожидания terminal; точный final echo не создаёт второго сообщения, ошибки сохраняются. Worker получает основную задачу и реальные ограничения, общие требования находятся во внешнем profile. Telegram заголовок-обёртка удалён.

Полный Go run и race прошли, Web 19/19 tests и immutable build прошли. Два независимых review после исправлений: 0 findings. Native Codex в Web/Telegram подтвердил поведение; controlled restart не добавил подтверждений, Results или Attempts. Production binaries и оба custom profiles обновлены. Детали и границы асинхронной доставки: [отчёт](../report.md).

## Comments

Точный пользовательский пример: вместо «Передал проверку … Codex Worker wrk_… на Node omarchy. Ответ … придёт автоматически» требуется короткое «Сейчас проверю» до вызова инструментов. Prompt Worker должен быть примерно «Какая погода в Барнауле?». Worker profile хранит общий стиль, а не project AGENTS.md.
