# Проверка стандартов: читаемая Worker activity

Проверен точный диапазон `43cd444..7adc71e33fff6821e937a3cb5e850b8bfbcf44de` в изолированном implementation worktree `secretary-p5-worker-activity`.

**Обязательные нарушения: 0. Замечания Fowler: 0. P1/P2: 0.**

Источники: `AGENTS.md`, `docs/agents/domain.md`, `GLOSSARY.md`, `docs/architecture/runtime-contracts.md`, `.scratch/code-comment-policy/spec.md` и полный эвристический baseline с приоритетом правил репозитория.

`WorkerActivity.svelte` отделяет отображение Worker activity от большого observer template и использует существующий `Markdown`. `workerActivityRows` находится в принятом модуле преобразования UI-данных; `activityPayload` сохраняет общую логику нормализации вместо её копирования. Нового источника durable state, runtime lifecycle или зависимостей нет.

Группа соседних text delta включает Worker, Attempt, Turn, channel и kind; tool/status и скрытые reasoning events прерывают группу. Исходные events не изменяются: текст объединяется только в новых presentation rows. Это соответствует различению Worker activity и canonical Result в runtime contracts. Названия отражают назначение функций, без спекулятивных интерфейсов или лишнего посредника.

Новых поясняющих комментариев нет. Регрессионный тест компилирует реальные Svelte-компоненты и проверяет итоговый HTML: полноценный Markdown, непотерянный длинный текст, порядок tools, границы Attempts/channels и отсутствие private reasoning/arguments. Это проверка поведения, а не копия алгоритма группировки.

Сборочный `web/app.js` рассмотрен как сгенерированный артефакт. Тесты не запускались повторно. Заключение относится только к Standards данного изменения; оно не заменяет Spec review или живую проверку опубликованного observer.
