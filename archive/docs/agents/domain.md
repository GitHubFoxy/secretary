# Domain docs

Правила чтения domain documentation этого repo.

## Перед исследованием codebase

- Прочитать `GLOSSARY.md` в корне либо `GLOSSARY-MAP.md`, если он существует. `GLOSSARY-MAP.md` указывает на `GLOSSARY.md` отдельных contexts.
- Если задача затрагивает runtime, identity, доставку сообщений, Execution node или Worker lifecycle, прочитать `docs/architecture/runtime-contracts.md`.
- Прочитать ADR из `docs/adr/`, относящиеся к текущей области.
- Если этих файлов ещё нет, молча продолжить. Не создавать их заранее. `/domain-modeling`, `/grill-with-docs` и `/improve-codebase-architecture` добавят их, когда появятся устойчивые термины или решения.

## Layout

Это single-context repo:

```text
/
├── GLOSSARY.md
├── docs/
│   ├── architecture/runtime-contracts.md
│   └── adr/
└── src/
```

## Vocabulary

В issue titles, предложениях рефакторинга, гипотезах и именах тестов использовать термины из `GLOSSARY.md`. Если нужного термина там нет, не вводить его молча: это либо ненужный синоним, либо пробел, который надо обработать через `/domain-modeling`.

## ADR conflicts

Если новое предложение противоречит существующему ADR, указать это явно, а не переопределять решение молча.
