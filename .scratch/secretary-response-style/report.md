# Проверка раннего ответа Secretary

Status: in progress

## Исходная причина

Legacy runtime сохранял обычный ответ Secretary только после terminal. Telegram дополнительно удерживал такой ответ до завершения хода. Старый профиль явно требовал после Dispatch перечислить Worker и Node. Адаптер Telegram сам добавлял заголовок «Задача от Secretary:» к prompt.

## Решение и границы

Отдельное раннее подтверждение сохраняется через canonical conversation с identity текущего input. Оно не является финальным addressed reply и не удовлетворяет terminal completion guard. После успешного делегирования точное повторение подтверждения не должно создавать второе сообщение. Отличающийся финальный ответ, в том числе ошибка делегирования, остаётся видимым.

Подтверждение сохраняется до рабочего tool. Telegram доставляет его через упорядоченный outbox, не ожидая terminal. Момент фактической доставки зависит от Telegram и сети; синхронное подтверждение получения Telegram до запуска backend tool не является контрактом.

Общие правила находятся в external Worker profile: минимальный полезный результат и необходимый источник, краткая блокировка, запрет выдумывать факты. Task prompt сохраняет суть запроса и существенные ограничения пользователя.

## Проверки

Первый код `c2be91f`: полный `go test -p 1 ./...` прошёл, race для core/MCP/secretaryd прошёл. Immutable build всех пяти binaries для Linux amd64 и Darwin arm64 прошёл; Web 19/19 tests и оба bundles прошли.

Первый независимый Spec review: 0 findings по реализации, живая модель ещё ожидает проверки. Standards review: два findings, которые исправляются до deployment:

- P1: crash после удаления отправленного acknowledgement из outbox, но до сохранения event cursor мог вызвать повтор отправки. Нужна durable отметка доставки по send identity.
- P2: проверка наличия acknowledgement перед stream delta была вне транзакции сохранения delta. Нужна атомарная проверка и запись, чтобы поздний delta не повторял сообщение.

Production этим первым candidate не обновлялся. Исправления находятся в `6b8f826`: оба новых regression подтвердили RED на `c2be91f` и GREEN после исправления. Полный повтор `go test -p 1 ./...` прошёл, focused race с тремя повторениями прошёл. Один прогон поймал неизменённый OpenCode fixture timeout (1s); отдельный повтор и последующий полный прогон прошли, посторонний timeout не менялся.

Повторные независимые reviews полного диапазона `cff9725...6b8f826`: Standards 0 findings, Spec 0 findings. P1 и P2 закрыты. Живой native Codex на финальном build ещё проверяется.

## Production

До обновления: source `af1290a`, main `cff9725`; оба Nodes online, активных Secretary turns/Worker Attempts нет, outbox пуст, SQLite `quick_check=ok`, 39 Telegram topics. Погодный Worker пользователя сохранён.

Custom profiles, которые необходимо обновить явно:

- `profiles/secretary-codex-phase5.md`
- `profiles/worker-template-luna6-low-9e3-v1.md`

Mac Node получает compiled Worker profile от сервера.
