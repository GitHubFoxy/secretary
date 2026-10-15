# Исследование для #48: язык ответов Secretary и Worker

## Вывод

У Secretary в bundled profile уже есть правило отвечать владельцу по-русски, а у Worker действует противоположное правило: отвечать на языке пользователя. Поэтому самый вероятный основной шаг — согласовать оба profiles и проверить, что Secretary сохраняет явный запрос другого языка при делегировании. Глобальный перевод входного сообщения или изменение Channel adapters не требуется по найденному пути: модели получают инструкции через Profiles, а Worker получает отдельный профиль вместе с Task.

## Текущий путь

- Начальные Profiles копируются из `internal/config/defaults/secretary.md` и `worker.md` в `<data-dir>/profiles/` только при первом setup; уже существующие файлы не перезаписываются: [`internal/config/manager.go:71-95`](../../internal/config/manager.go#L71). Пути объявлены в [`internal/config/defaults/config.toml:3-6`](../../internal/config/defaults/config.toml#L3).
- Config читает тексты этих файлов и собирает `Profile.Content`: [`internal/config/config.go:203-250`](../../internal/config/config.go#L203). Входной текст отдельно не переводится/не определяется по языку этим слоем.
- Secretary получает Profile как ведущие инструкции перед canonical context и исходным user turn: [`internal/core/secretary_context.go:357-365`](../../internal/core/secretary_context.go#L357), вызов из runtime — [`internal/secretary/runtime.go:920-946`](../../internal/secretary/runtime.go#L920).
- Новый Worker получает managed Worker Profile, фиксируемый в binding snapshot: [`internal/ctl/worker_profile.go:19-41`](../../internal/ctl/worker_profile.go#L19), [`internal/ctl/worker_profile.go:53-73`](../../internal/ctl/worker_profile.go#L53). Node доставляет его нативному runtime либо как `AGENTS.md`: [`internal/node/node.go:193-209`](../../internal/node/node.go#L193), [`internal/node/profile.go:107-128`](../../internal/node/profile.go#L107).
- Secretary default передаёт Worker суть запроса и отдельно запрещает добавлять собственные требования о response language в Task: [`internal/config/defaults/secretary.md:1-3`](../../internal/config/defaults/secretary.md#L1-L3). Значит явное пожелание пользователя о языке ответа может потеряться при преобразовании запроса для Worker; это нужно покрыть тестом и решить, как сохранить такое пожелание, не копируя лишние инструкции Secretary.

## Конфликт правил

- Secretary profile уже говорит: “Always answer the owner in Russian … Switch languages only when the owner explicitly asks”: [`internal/config/defaults/secretary.md:5-9`](../../internal/config/defaults/secretary.md#L5-L9).
- Worker profile велит возвращать результат “in the user's language”: [`internal/config/defaults/worker.md:1`](../../internal/config/defaults/worker.md#L1). Это расходится с #48: русский должен быть базовым независимо от языка ввода, а другой язык — только по явному запросу.
- Внешние profiles важны: production может иметь собственные файлы, и обновление embedded defaults само по себе их не меняет. Репозиторный precedent прямо указывает на custom paths и необходимость обновлять их отдельно: [`.scratch/secretary-response-style/report.md`](../secretary-response-style/report.md), разделы «Общие правила» и «Production».

## План исследования/реализации для исполнителя

1. Проследить, как Secretary строит `spawn_worker` и `message_worker` аргументы, и уточнить, доступны ли модели исходный ввод/контекст пользователя при follow-up. Проверить, как выражать явное пожелание языка ответа так, чтобы оно относилось только к соответствующему ответу/Task.
2. Изменить bundled Secretary и Worker profiles в согласии с контрактом #48. Сохранить Secretary правило русского по умолчанию; заменить у Worker правило «язык пользователя» на русский по умолчанию. Явный запрос на другой язык должен переходить к Worker при Dispatch и Follow-up, если он применим к порученной работе.
3. Проверить setup/profile loading, Secretary prompt composition и Worker profile snapshot/delivery. Поскольку профили и snapshot могут быть durable, определить и документировать границу: новые Profiles применяются к новым bindings; уже сохранённые Worker snapshots автоматически не переписываются.
4. Добавить проверки содержимого обоих default profiles и поведения передачи language request: русский при английском/ином вводе; переключение только при явном запросе; отсутствие переключения из-за языка исходного Task; сохранение explicit языка через Secretary delegation/follow-up. Использовать существующие config/profile/context tests как ближайшие seams.
5. Просмотреть связанные документы и operational instructions: как минимум `docs/configuration.md` и response-style research note. Если deployment включён в scope, перечислить фактические внешние `profiles/secretary.md` и `profiles/worker.md` и проверить их синхронизацию вручную; установка новых defaults не обновит уже имеющиеся файлы.

## Ближайшие существующие проверки

- `internal/core/secretary_context_test.go:15-48` проверяет, что Markdown Profile стоит первым в Secretary prompt.
- `internal/config/table_prompt_test.go` — пример теста обязательных формулировок default profiles; расширить/создать аналогичную проверку language contract.
- `internal/config/config_test.go` проверяет загрузку Profiles; `internal/config/manager_test.go` проверяет начальное копирование defaults.
- `internal/ctl/worker_profile_dispatch_test.go` и `internal/node/profile_test.go` проверяют передачу/доставку managed Worker Profile. Тестировать нужно не только текст defaults, но и сохранность разрешённого language preference в реальном dispatch input.

## Решение и предположения

Issue задаёт русский как fallback для Secretary и всех Workers, независимо от языка сообщения; исключение срабатывает только при явном запросе пользователя. Явная просьба «переведи/ответь по-английски» относится к требуемому ответу, а сам английский Task без такой просьбы не меняет язык результата. Поведение для Worker, которому пользователь адресовал новый follow-up на другом языке, требует явного теста: контекст #48 указывает, что язык ввода не является разрешением переключиться.

`GLOSSARY.md` и `docs/architecture/runtime-contracts.md` прочитаны. Каталога `docs/adr/` нет, так что найденного ADR, ограничивающего изменение, нет.
