# Проверка кавычек в Telegram code

Замечаний: 0; правка не требуется.

Проверен production путь `result.accepted` → `telegramEvent` JSON decode → `stringField(summary)` → terminal `safeText` → fenced `html.EscapeString` → Bot API URL form. Он сохраняет обычные кавычки и не добавляет обратные слеши в Markdown code.

Отдельный тест через существующий adapter + настоящий HTTP encoding Bot API fixture выполнен посредством временного Go overlay, без изменений репозитория. Проверены оба назначения, topic и General: `console.log("ok")` остаётся без слешей; намеренно переданный код с literal backslashes сохраняет их. Проверка PASS.

Root подтвердил непосредственными DOM booleans, что General Result bubbles 7601/7604 не содержат backslash, code text length 17 и код равен корректному `console.log("ok")`; во всех проверенных code elements backslash count 0. Видимые `\"` были экранировкой JSON сериализации AXI snapshot, а не Telegram DOM.

Это проверка конкретного подозрения, не полный Codex MVP gate. Production/runtime/auth/browser этой диагностикой не изменялись.
