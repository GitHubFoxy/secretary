# Issue tracker: GitHub

Задачи и спецификации этого репозитория ведутся в [GitHub Issues](https://github.com/GitHubFoxy/secretary/issues). Для работы с ними используй `gh` из корня репозитория. `.scratch/` оставлен для исследований, спецификаций и отчётов, но не для новых тикетов.

## Основные операции

- Создать задачу: `gh issue create --title "..." --body-file <file>`; для короткого текста можно использовать `--body`.
- Прочитать задачу вместе с комментариями: `gh issue view <number> --comments`.
- Найти открытые задачи: `gh issue list --state open --json number,title,body,labels,comments`.
- Добавить комментарий: `gh issue comment <number> --body "..."`.
- Назначить или снять метку: `gh issue edit <number> --add-label <label>` / `--remove-label <label>`.
- Закрыть задачу: `gh issue close <number> --comment "..."`.

Определи репозиторий по `git remote -v`; `gh` обычно выбирает его автоматически в этом клоне. Статусы triage отражаются метками из `triage-labels.md`. Для перенесённых задач со статусом `claimed` используется метка `in-progress`; завершённые задачи закрываются.

## Wayfinding

Карты `/wayfinder` хранятся в GitHub как issues с меткой `wayfinder:map`. Дочерние задачи перечисляются в карте как список GitHub issues; в теле дочерней задачи указывается `Part of #<map>`. Если GitHub sub-issues доступны, их также можно связать через API.

- Блокирующие задачи указываются в теле как `Blocked by: #<number>` и, если API поддерживает зависимости, связываются нативной зависимостью GitHub. Задача разблокирована, когда все блокеры закрыты.
- Frontier — открытая дочерняя задача без открытых блокеров и без исполнителя; порядок берётся из списка в карте.
- Claim: назначить задачу текущему пользователю через `gh issue edit <number> --add-assignee @me`.
- Resolve: оставить итоговый комментарий, закрыть issue и обновить Decisions so far в issue-карте.

Для существующих исследований и отчётов можно хранить Markdown в `.scratch/`; ссылки на задачи оформляй как `#<number>` или полными GitHub URL.
