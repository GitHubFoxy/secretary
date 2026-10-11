# Карта: steering активного Secretary

## Notes

Подготовка к реализации: [тикет 01](issues/01-durable-secretary-steering.md), [спецификация](spec.md), [исследование первичных источников](research.md), [инцидент и воспроизведение](incident.md).

## Decisions so far

- Исправить общий server/runtime ingress, сохранив прямую адресацию Worker topic.
- Обычный активный ввод — steering; `/q` — явная очередь. Native safe boundary не означает Cancel инструмента.
- Сохранить identity каждого input и исходный immutable turn snapshot; не заменять `secretary_turns.input_id` новым значением.
- Unknown receipt/restart не допускают silent retry или очередь. Native `startedNewTurn` не считается steering.
- Native acceptance отличает принятие от применения; fake тест нужен для регрессии Store/runtime, но не заменяет live proof.
- [Исследование](research.md) завершено; тикет 01 имеет статус `ready-for-agent`. Обновление membership/observer/guard обязательно, простой вызов `Steer(text)` не закрывает задачу.

## Fog

Нет открытых вопросов продукта, блокирующих начало реализации. Native terminal во время steering RPC может вызвать ACP fallback `startedNewTurn`; это известный риск с обязательной видимой reconciliation и тестом. Гарантия отсутствия side effects такого fallback потребовала бы отдельного изменения native extension с expected-turn/no-fallback и не обещается этим тикетом.
