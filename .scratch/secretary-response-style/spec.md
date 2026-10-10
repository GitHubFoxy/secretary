# Короткий ответ до делегирования и простой prompt Worker

Status: resolved

Пользователь поручил создать ticket, исправить через субагента, проверить, commit/push main и обновить production.

## Цель

На обычный запрос вроде «Привет, какая погода в Барнауле?» Secretary сначала показывает короткое естественное «Сейчас проверю» (аналог One moment), затем вызывает инструменты и делегирует. Не сообщает Worker ref, Node, harness или устройство доставки в обычном ответе. Итог приходит от Worker без повторного пересказа.

Worker получает суть задачи, например «Какая сейчас погода в Барнауле?», а не длинную повторяемую процедуру с датой, инструментами, запретами и инструкцией доставки. Общие правила находятся в Worker profile: минимальный полезный результат; если блокировка мешает завершить работу, кратко сообщить её и не выдумывать результат. Сохранить важные пользовательские ограничения и факты в конкретной задаче.

В Telegram убрать добавляемый адаптером префикс «Задача от Secretary:». Исходный prompt сохраняется и показывается без заголовка-обёртки. Не переписывать существующие snapshots/history.

## Границы

Проверить причину задержки на полном пути profile → native runtime → canonical reply → Web/Telegram delivery. Одной просьбы модели сказать acknowledgement недостаточно, если channel откладывает его до terminal. Использовать существующие canonical/reply механизмы, сохранив ordering, identity, идемпотентность и видимые errors. Не ослаблять empty/error completion guard, не создавать hidden retry или новый agent loop. Краткий acknowledgement не считается успешным Result выполнения задачи. При неудаче Dispatch ошибка остаётся видимой.

Worker profile — external Markdown через profiles.worker; это не AGENTS.md проекта. Не добавлять стилевые указания в пользовательские workspace AGENTS.md и не изменять глобальную native config/auth. Production profile paths могут быть custom, поэтому обновление defaults в repo само по себе не обновляет production.

## Проверка

Public regression подтверждает: короткий user-visible ответ записан/доставлен до первого рабочего tool/Dispatch, один acknowledgement без повторения после terminal, сохранённый Worker Result один раз, replay/restart не удваивают delivery; failed dispatch показывает ошибку. Telegram Task prompt соответствует prompt без префикса, включая длинные сообщения. Profile/prompts проверяют минимальный стиль и сохранение пользовательских ограничений. Живой native Secretary в Web/Telegram подтверждает порядок и простой Worker prompt на deploy; данные погоды не засчитываются без актуального источника, если выбран weather probe.

Пользовательские dirty spec/THE spec.md и spec/memory.md сохраняются. Claude Code остаётся отложенным.
