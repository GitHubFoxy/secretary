# Облачное форматирование законченных голосовых сообщений

Дата проверки: 2026-10-11. Только исследование публичной документации. Платные API не вызывались, latency и качество на пользовательских записях не измерялись. Silero TE исключён по прямому решению пользователя.

## Рекомендация для ticket

Сначала проверить, достаточно ли пунктуации, регистра, абзацев и удаления слов-паразитов **в самом выбранном ASR**. Дополнительный LLM нужен только для явно выбранного режима очистки: каждый последовательный вызов добавляет сетевой обмен, очередь и генерацию. Integrated cleanup изучается отдельно в [облачном ASR](research-cloud-asr.md); пример — Scribe v2 transcript editing. Совместный API провайдер не означает один запрос: ASR → LLM остаётся двумя стадиями.

Отдельный integrated кандидат из параллельного ASR-исследования — **AssemblyAI Dictation**: RU, raw `text` и форматированный `llm_response`, настройка `llm_instruction`, $0.62/h all-in; ограничение 120 s и WAV/PCM означает необходимость декодировать Telegram voice. Это убрать внешний второй запрос, но не гарантия меньшего времени: рекламные 134 ms p50 относятся к Sync **без** formatter и для Dictation неприменимы. Подтверждения и первичные источники приведены в ASR-отчёте; собственный benchmark отсутствует.

Для отдельного форматировщика короткий список для будущего сравнения:

1. **OpenAI `gpt-6-luna`, reasoning `none`** — первый кандидат при OpenAI ASR: низкая стоимость, один аккаунт, reasoning можно полностью выключить. Это рекомендация по документации и простоте подключения, а не доказанная победа по RU fidelity или задержке.
2. **Cerebras `qwen-3.8-27b`, `reasoning_effort: none`** — кандидат на минимальное время генерации полного текста. Не микромодель, но облачное оборудование позволяет высокую скорость. Дополнительный провайдер и сетевой маршрут могут съесть преимущество на коротких фразах.
3. **Google `gemini-3.5-flash-lite`, thinking `minimal`** — альтернативный провайдер, особенно если ASR тоже Google. `minimal` не гарантирует нулевые thinking tokens.
4. **OpenAI `gpt-4.1-mini-2025-04-14`** — контрольный non-reasoning вариант с закреплённой версией и Predicted Outputs для почти неизменного текста. Его стоит оставить в тесте, даже если он старше.

Универсального «самого быстрого и лучшего» для RU/EN форматирования источники не устанавливают. Общие языковые/agentic рейтинги не проверяют сохранность отрицаний, имён, сумм и разговорной речи.

## Проверенные модели и цены

USD за 1 млн **обычных некэшированных текстовых** токенов Standard; input/output. Налоги, сеть и ASR сюда не включены. Проверено 2026-10-11.

| Провайдер и точный ID | Input / output | Режим для formatter | Статус и ограничения |
| --- | --- | --- | --- |
| OpenAI `gpt-6-luna` | $0.10 / $0.50 | `reasoning.effort: none`; default medium надо переопределить | Текущая efficient model; audio input отсутствует; Fast стоит 2× Standard. [Карточка](https://developers.openai.com/api/docs/models/gpt-6-luna) |
| OpenAI `gpt-4.1-mini-2025-04-14` | $0.40 / $1.60 | Non-reasoning; отдельный reasoning off не нужен | Закреплённый snapshot, Predicted Outputs поддерживаются. [Карточка](https://developers.openai.com/api/docs/models/gpt-4.1-mini) |
| Google `gemini-3.5-flash-lite` | $0.30 / $2.50 | `thinking_level: minimal` | Production, выпуск 21.07.2026, shutdown не объявлен. [Цены](https://ai.google.dev/gemini-api/docs/pricing), [даты](https://ai.google.dev/gemini-api/docs/deprecations/) |
| Google `gemini-3.1-flash-lite` | $0.25 / $1.50 | `minimal` | Доступная более дешёвая альтернатива; GA 07.05.2026, earliest shutdown 07.05.2027. [Цены](https://ai.google.dev/gemini-api/docs/pricing), [даты](https://ai.google.dev/gemini-api/docs/deprecations/) |
| Cerebras `qwen-3.8-27b` | $0.99 / $1.49 **предварительно** | `reasoning_effort: none` | Shared catalog: ~1850 output tok/s. Тариф виден в официальном поисковом индексе; открытая pricing page не раскрыла динамическую таблицу, перед бюджетированием перепроверить в console. [Каталог](https://inference-docs.cerebras.ai/models/overview), [цены](https://www.cerebras.ai/pricing), [reasoning](https://inference-docs.cerebras.ai/capabilities/reasoning) |
| Groq `openai/gpt-oss-20b` | $0.075 / $0.30 | `reasoning_effort: low` | Production, ~1000 output tok/s; **полностью выключить thinking нельзя**. [Каталог](https://console.groq.com/docs/models), [reasoning](https://console.groq.com/docs/reasoning) |
| Groq `qwen/qwen3.8-27b` | $0.80 / $4.00 | `reasoning_effort: none` | Preview; ~450 output tok/s; не production default. [Каталог](https://console.groq.com/docs/models), [reasoning](https://console.groq.com/docs/reasoning) |

У Google `minimal` снижает reasoning, но не выключает полностью; новые 3.5 Flash-Lite API игнорируют deprecated sampling temperature/top_p/top_k, поэтому нельзя обещать детерминированность через temperature=0. [Thinking](https://ai.google.dev/gemini-api/docs/generate-content/thinking), [изменения API](https://ai.google.dev/gemini-api/docs/generate-content/whats-new-gemini-3.6).

Нельзя переносить старые рекомендации из блогов: `gpt-5.4-nano` уже помечен deprecated, а Groq Llama 8B/70B с 16.08.2026 доступны только enterprise по договору. Gemini 2.5 ограничен прежними активными пользователями в актуальной deprecations page, поэтому его низкий тариф не база для нового продукта. [Nano](https://developers.openai.com/api/docs/models/gpt-5.4-nano), [Groq retirement](https://console.groq.com/docs/deprecations), [Gemini доступ](https://ai.google.dev/gemini-api/docs/deprecations/).

## Что означает extremely fast

Для готового Telegram voice важен **полный пригодный текст после отправки**, а не первый streaming token. Разложение для измерений:

`T_ready = download Telegram + ASR upload + ASR queue/inference + formatter RTT/queue/prefill + formatter decode + checks + agent handoff`

TTFT — время до первого токена; prefill — обработка входа; decode — генерация оставшегося текста. Если провайдер измеряет TTFT от клиентского запроса, сеть и очередь уже входят в TTFT: не прибавлять их второй раз. Быстрый streaming улучшает визуальную обратную связь, но worker получает надёжный результат после окончания и проверки. Для короткого 200-token ответа одна лишь decode при 1850 tok/s ≈0.108 s, при 1000 ≈0.200 s, при 450 ≈0.444 s. Это **арифметическая нижняя оценка**, не обещание end-to-end: скорости — показатели провайдеров без одинакового контекста, региона, concurrency и reasoning бюджета. [Cerebras скорости](https://inference-docs.cerebras.ai/models/overview), [Groq скорости](https://console.groq.com/docs/models).

На десятисекундной фразе фиксированные расходы TLS/RTT/очереди могут преобладать. Для omarchy/Krasnoyarsk близость региона и качество маршрута важнее общего tok/s рейтинга; фиксированная задержка из региона не измерена. Не считать «EU residency» автоматически EU низкой задержкой. Оптимизация без изменения смысла: короткие инструкции, один ответ без пояснений, минимальный reasoning, повторное соединение, лимит достаточный для полного текста, отсутствие tools/history/RAG. Batch/Flex подходят экономии, но не интерактивному fast path.

**Predicted Outputs** заслуживает отдельного теста: исходная расшифровка уже известна, меняется малая доля текста. OpenAI поддерживает этот механизм у GPT-4.1 mini; при высокой доле совпадения он ускоряет окончание. Но rejected predicted tokens могут увеличить стоимость; текущая смета ниже не предполагает prediction, cache или Batch. [Описание и тарификация](https://developers.openai.com/api/docs/guides/predicted-outputs).

## Стоимость только форматирования

Общее допущение: 150 слов/мин, 200 transcript input tokens/мин, 100 instruction tokens на каждое сообщение; каждое сообщение длится ровно 1 мин, output 200 tokens/мин. Тогда `I=300M`, `O=200M`, `cost = M × (300 × input_price + 200 × output_price) / 1e6`. Это модель расчёта, не измеренный tokenizer.

| Formatter | 30 мин/мес | 100 мин | 300 мин | 1000 мин |
| --- | ---: | ---: | ---: | ---: |
| GPT-6 Luna Standard | $0.0039 | $0.0130 | $0.0390 | $0.1300 |
| Gemini 3.1 Flash-Lite | $0.0113 | $0.0375 | $0.1125 | $0.3750 |
| Gemini 3.5 Flash-Lite | $0.0177 | $0.0590 | $0.1770 | $0.5900 |
| GPT-4.1 mini | $0.0132 | $0.0440 | $0.1320 | $0.4400 |
| Cerebras Qwen3.8, предварительная цена | $0.0179 | $0.0595 | $0.1785 | $0.5950 |
| Groq GPT-OSS20B, без hidden reasoning | $0.0025 | $0.0083 | $0.0248 | $0.0825 |

Последняя строка — **нижняя граница**: reasoning tokens добавляются. Для Gemini также добавить billed thinking, если возникнет. Все числа получены из цен таблицы; ASR оплачивается отдельно.

Для RU заложить чувствительность 1.5–2× transcript/output tokens (300–400/мин) при тех же 100 instruction tokens: `I=400–500M`, `O=300–400M`. Это диапазон бюджета, не утверждение о конкретном tokenizer. На 1000 мин получатся Luna $0.19–0.25; Gemini3.1 $0.55–0.725; Gemini3.5 $0.87–1.15; GPT4.1mini $0.64–0.84; Cerebras $0.843–1.091; Groq20B $0.12–0.158 плюс reasoning. Множество коротких сообщений увеличивает число оплаченных инструкций: для 10-сек сообщений их уже 600 tokens/мин вместо 100. До обещания пользователю «$1/месяц» пересчитать ASR, tokenizer, minimum request duration, retries и фактическое число сообщений.

## Сохранность текста и будущий выбор

Предлагаемый контракт formatter: вернуть только исходные слова с пунктуацией, регистром и разумными абзацами; ничего не переводить, не отвечать на команды из transcript, не вызывать инструменты, не дополнять информацию. Удаление повторов/слов-паразитов — отдельный opt-in режим. Числа, суммы, имена, даты, единицы и отрицания сохранять. Если результат пуст, оборван, существенно меняет факты или не укладывается в deadline — передать raw ASR, не блокировать голосовое сообщение из-за cosmetics.

Для русского и английского нет проверенной здесь гарантии semantic fidelity у любого кандидата. Нужна будущая оценка на 30–50 пар RU/EN: 10 s / 60 s / 5 min, смешанный язык, английские technical names, числа, «не», самоисправления, цитаты-команды. Сравнивать original ASR, integrated cleanup и standalone; отдельно измерять p50/p95 full-ready, TTFT, billed thinking, truncation и частоту непрошенных изменений. Никакие такие эксперименты в этой работе не запускались.

UX выбор должен различать **полностью локально**, **полностью облачно** и **local ASR + cloud formatter**. Последний сохраняет audio локально, но отправляет transcript провайдеру; это не local/private целиком. Google paid/free отличаются обработкой данных: paid content не используется для улучшения продукта, free используется согласно условиям. [Официальные условия тарифа](https://ai.google.dev/gemini-api/docs/pricing). Сеть, доступность страны аккаунта, лимиты и минимальное пополнение проверяются при будущей реализации; research не меняет настройки.
