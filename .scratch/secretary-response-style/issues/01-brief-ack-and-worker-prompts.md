# 01: короткое подтверждение до инструментов и простой Worker prompt

Type: task
Status: claimed
Blocked by: None

## What to build

Реализовать [spec](../spec.md): acknowledgement до инструментов, короткий стиль без внутренних IDs/Node, минимальные task prompts и общие требования в external Worker profile, убрать Telegram префикс.

## Acceptance criteria

- [ ] Короткий acknowledgement виден до tool/Dispatch, без позднего повторного «передал Worker…».
- [ ] Worker prompt содержит суть запроса и важные ограничения, без повторяемого процедурного boilerplate.
- [ ] Общий Worker profile требует минимальный отчёт либо сообщение о блокировке; значения при недоступных источниках не выдумываются.
- [ ] Telegram topic показывает исходный prompt без «Задача от Secretary:», safe formatting/splitting/replay сохранены.
- [ ] Canonical identity/ordering/errors/completion guards и exactly-once Results сохранены.
- [ ] Scoped tests, два независимых review и живая production проверка проходят.
- [ ] Изменения отправлены в main; production binaries/profiles обновлены; история и пользовательские файлы сохранены.

## Answer

В работе.

## Comments

Точный пользовательский пример: вместо «Передал проверку … Codex Worker wrk_… на Node omarchy. Ответ … придёт автоматически» требуется короткое «Сейчас проверю» до вызова инструментов. Prompt Worker должен быть примерно «Какая погода в Барнауле?». Worker profile хранит общий стиль, а не project AGENTS.md.
