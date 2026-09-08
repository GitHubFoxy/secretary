# Issue tracker: Local Markdown

Задачи и спецификации этого repo хранятся как Markdown-файлы в `.scratch/`.

## Conventions

- Один feature на директорию: `.scratch/<feature-slug>/`.
- Спецификация: `.scratch/<feature-slug>/spec.md`.
- Implementation issues: по одному файлу на ticket в `.scratch/<feature-slug>/issues/<NN>-<slug>.md`, нумерация от `01`. Не создавать один общий файл tickets.
- Triage state записывается строкой `Status:` вверху issue. Список ролей находится в `triage-labels.md`.
- Комментарии и история обсуждения добавляются в конец файла под заголовком `## Comments`.

## Когда skill говорит "publish to the issue tracker"

Создать файл в `.scratch/<feature-slug>/`, при необходимости создав директорию.

## Когда skill говорит "fetch the relevant ticket"

Прочитать файл по переданному пути или номеру issue.

## Wayfinding operations

Используются `/wayfinder`.

- Map: `.scratch/<effort>/map.md`. В нём находятся Notes, Decisions so far и Fog.
- Child ticket: `.scratch/<effort>/issues/NN-<slug>.md`. Вверху указываются `Type:` (`research`, `prototype`, `grilling` или `task`) и `Status:` (`claimed` или `resolved`).
- Blocking: строка `Blocked by: NN, NN`. Ticket разблокирован, когда все указанные tickets имеют статус `resolved`.
- Frontier: открытые, не заблокированные и не claimed файлы в `.scratch/<effort>/issues/`. Приоритет у меньшего номера.
- Claim: до начала работы установить `Status: claimed` и сохранить файл.
- Resolve: добавить ответ под `## Answer`, поставить `Status: resolved`, затем добавить краткую ссылку на решение в `Decisions so far` файла `map.md`.
