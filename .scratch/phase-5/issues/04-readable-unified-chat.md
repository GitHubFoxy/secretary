# 04: читаемый единый чат и Telegram topics без дублей

Type: task
Status: ready-for-agent
Blocked by: None

## What to build

Пользователь читает одну сохраняемую переписку в Web и Telegram: ответы Secretary и Worker Results содержат нормальный текст, Markdown и code blocks. Web observer показывает Worker text/tool/status; Telegram группа с ботом-администратором маршрутизирует General к Secretary, topic к конкретному Worker и сохраняет topic mappings.

## Acceptance criteria

- [ ] В Web Conversation, Secretary stream и Result читаются Markdown/code/link с безопасным escaping; unsafe HTML/URLs не исполняются. Worker text activity отображается текстом, tool/status — компактно и понятно, generic payload не доминирует как сырой JSON.
- [ ] Native assistant final и MCP addressed reply дают ровно одну canonical Secretary entry; stream/replay/terminal не создают вторые bubbles или Telegram sends. Если canonical reply не появился либо turn failed, пользователь видит ошибку без скрытого retry.
- [ ] Worker Result идёт один раз в Personal Conversation без automatic Secretary turn. Replay/reconnect не удваивает reply/Result; проверки различают identity и текст.
- [ ] Telegram General и Web показывают одну Personal Conversation; Worker creation создаёт topic с Task prompt, известный topic адресует точный Worker, неизвестный topic не создаёт случайную работу.
- [ ] Создание topic не зависит от OpenCode-only title generator или дополнительного model turn: для MVP используется сохранённое Worker title/Task title с детерминированным безопасным truncation. Явные исторические данные не удаляются.
- [ ] Topic mapping и delivery cursors сохраняются после restart; Result один раз в topic и один раз в General, private/live Worker activity не ошибочно пересказывается в General.
- [ ] Существующий Telegram Markdown→HTML formatter сохраняет safe links, escaping, fenced code, UTF-16 limit/splitting и persisted parse-error fallback. Конкретный дефект сначала воспроизведён; formatter не переписывается по предположению.
- [ ] Реальное отсутствие bot admin/forum permissions даёт понятную ошибку создания topics. В реальной тестовой группе подтверждены права и правильная маршрутизация.
- [ ] Обычный input и `/q` имеют понятное представление по server state; renderer не реализует собственный lifecycle или очередь. Финальная проверка новых queued состояний выполняется после 03 в gate 05, не блокируя основной renderer/reply slice.

## Проверка

Public seams: Web rendering, canonical server events и Bot API channel output. Использовать существующие Web, Telegram formatter/topic/delivery и canonical addressed-reply bridge tests. Новый regression проверять на наблюдаемом bubble/text/send, не на внутренних helper вызовах. Выполнить browser visual check для text/code и настоящую Telegram отправку; live evidence отдельно. Не приписывать нынешнюю ошибку историческому пропуску conversation.entry без воспроизведения.

## Answer

Пока отсутствует.

## Comments

Canonical delivery исправлялась раньше; baseline исследования не доказывают текущий Telegram formatting defect. Scope ограничен читаемостью и доставкой согласованного MVP.
