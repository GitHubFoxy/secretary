# Domain docs

Правила чтения domain documentation этого repo.

## Перед исследованием codebase

- Прочитать `CONTEXT.md` в корне, либо `CONTEXT-MAP.md`, если он существует. `CONTEXT-MAP.md` указывает на `CONTEXT.md` отдельных contexts.
- Прочитать ADR из `docs/adr/`, относящиеся к текущей области.
- Если этих файлов ещё нет, молча продолжить. Не создавать их заранее. `/domain-modeling`, `/grill-with-docs` и `/improve-codebase-architecture` добавят их, когда появятся устойчивые термины или решения.

## Layout

Это single-context repo:

```text
/
├── CONTEXT.md
├── docs/adr/
└── src/
```

## Vocabulary

В issue titles, предложениях рефакторинга, гипотезах и именах тестов использовать термины из `CONTEXT.md`. Если нужного термина там нет, не вводить его молча: это либо ненужный синоним, либо пробел, который надо обработать через `/domain-modeling`.

## ADR conflicts

Если новое предложение противоречит существующему ADR, указать это явно, а не переопределять решение молча.
