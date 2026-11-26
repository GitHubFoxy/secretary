---
name: day-summary
description: Use when the owner asks what was done today / за день / вчера.
---

Определи дату в часовом поясе Host (`date +%F`, для вчера `date -d yesterday +%F`). Подставь дату YYYY-MM-DD в команду. Базу открывай только для чтения. Запрос ограничивает текст 1000 символами, вывод — 100 строками; последний ответ каждой Session имеет приоритет. Если вывод обрезан, скажи об этом владельцу.

```sh
sqlite3 -readonly -json "$HOME/.secretary/secretary.sqlite" <<'SQL'
.parameter init
.parameter set @day "'YYYY-MM-DD'"
WITH activity AS (
  SELECT e.rowid AS rid, e.session_id, e.time, e.json,
    json_extract(e.json, '$.type') AS type,
    json_extract(e.json, '$.message.content') AS message,
    CASE WHEN json_extract(e.json, '$.type') = 'result' AND NOT coalesce(json_extract(e.json, '$.is_error'), 0)
      THEN json_extract(e.json, '$.result')
      WHEN json_extract(e.json, '$.method') = 'item/completed' AND json_extract(e.json, '$.params.item.type') = 'agentMessage'
      THEN json_extract(e.json, '$.params.item.text') END AS answer
  FROM events e
  WHERE e.time >= strftime('%Y-%m-%dT%H:%M:%fZ', @day, 'utc')
    AND e.time < strftime('%Y-%m-%dT%H:%M:%fZ', @day, '+1 day', 'utc')
    AND json_valid(e.json)
), selected AS (
  SELECT *, row_number() OVER (PARTITION BY session_id ORDER BY CASE WHEN answer IS NOT NULL THEN 0 ELSE 1 END, rid DESC) AS rank
  FROM activity WHERE (type = 'user' AND typeof(message) = 'text' AND message != '') OR (answer IS NOT NULL AND answer != '' AND answer NOT LIKE 'NO_REPLY%')
)
SELECT s.id, s.name, s.folder, s.parent_id,
  (SELECT count(*) FROM activity a WHERE a.session_id = s.id AND a.type = 'user' AND a.message NOT LIKE '[report %') AS turns,
  x.time, CASE WHEN x.answer IS NOT NULL THEN 'answer' WHEN x.message LIKE '[report %' THEN 'report' ELSE 'user' END AS kind,
  substr(coalesce(x.answer, x.message), 1, 1000) AS text
FROM selected x JOIN sessions s ON s.id = x.session_id
WHERE x.answer IS NULL OR x.rid = (SELECT max(a.rid) FROM activity a WHERE a.session_id = x.session_id AND a.answer IS NOT NULL AND a.answer != '' AND a.answer NOT LIKE 'NO_REPLY%')
ORDER BY CASE WHEN x.answer IS NOT NULL THEN 0 ELSE 1 END, x.time DESC LIMIT 100;
SQL
```

Коротко перескажи подтверждённые итоги по проектам и отдельно укажи, что ждёт владельца. Не выдавай задачи или промежуточные сообщения за выполненную работу.
