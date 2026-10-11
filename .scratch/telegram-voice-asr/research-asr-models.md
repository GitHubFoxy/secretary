# ASR для Telegram: русские и английские модели

Дата доступа к источникам: **11 октября 2026**. Исследование без установки моделей, запуска распознавания и обращения к платным API. Данные omarchy получены от параллельного исследования, здесь оценивается их соответствие опубликованным моделям.

## Рекомендация для тикета

Существующий Hermes ASR на omarchy — **Parakeet TDT 0.6B v3, Q8_0 GGUF, nemo-speech на CPU** — использовать как исходную точку будущего сравнения. Для русского главный конкурент — **GigaAM-v3 RNNT**; для общего RU/EN сценария — **Qwen3-ASR-0.6B**, а 1.7B оставить кандидатом качества при достаточной памяти. Не выбирать победителя по одному общему WER: русские рейтинги дают разный порядок, а актуальная английская таблица содержит несопоставимые агрегаты.

Это рекомендация к проверке, а не решение об изменении работающей системы. Скорость и точность на i5-1235U, 8 GiB RAM, около 4 GiB свободной памяти **не измерялись**. Файл GGUF 681 MiB не означает такой же размер рабочего процесса.

## Русский: три первичных источника

### 1. ESpeech: широкий рейтинг по доменам

[ESpeech Russian ASR Leaderboard](https://huggingface.co/spaces/ESpeech/russian_asr_leaderboard), [встроенные данные и методика](https://huggingface.co/spaces/ESpeech/russian_asr_leaderboard/blob/main/index.html). На странице указаны дата теста **07.05.2026**, автор Николай Шмырев и средний WER по 11 наборам. В текущем содержимом также присутствуют более новые модели; дата заголовка не доказывает дату оценки каждой строки. Наборы: аудиокниги АЦ, Ru LibriSpeech, Common Voice 12, Golos Crowd/Farfield, Sova, телевещание, медицина, команды Яндекса, звонки заказов/поддержки. Шум и редкие слова вынесены отдельно и не входят в основное среднее.

| Модель | Опубликованный средний WER, % |
|---|---:|
| GigaAM2 CTC + LM | **7.13** |
| GigaAM3 RNNT | **7.19** |
| GigaAM2 RNNT | 7.42 |
| GigaAM3 RNNT E2E | 10.05 |
| Whisper V3 Podlodka Turbo | 13.36 |
| Parakeet TDT V3 | 14.85 |
| Whisper Large V3 Transformers | 15.44 |
| Qwen3-1.7B | 16.55 |

Лидер опубликованного рейтинга — GigaAM2 CTC + LM; отдельная LM означает дополнительный компонент. Рейтинг полезен как ориентир, но статическая страница не предоставляет полного воспроизводимого протокола декодирования, нормализации и точных revisions для каждой модели. Это не достаточное доказательство превосходства на Telegram voice.

### 2. Vikhr: другой набор меняет победителя

[Первичная таблица Vikhr](https://huggingface.co/datasets/Vikhrmodels/russian-asr-leaderboard), [определение наборов](https://huggingface.co/spaces/Vikhrmodels/Russian_ASR_Leaderboard/blob/main/constants.py). Dataset revision `e66f837931370190888273330e85232d41702cef`, последнее изменение данных **31.08.2025**, восемь моделей. Средний WER по шести наборам: Russian LibriSpeech, Common Voice 22, Tone Webinars/Books/Speak и Sova RuDevices; Speak включает синтетическую речь.

Лидер этого отдельного рейтинга — Whisper large-v3, **10.16%**; Podlodka Turbo **10.36%**, Whisper Turbo **11.07%**, GigaAM-v2 RNNT **18.21%**. У последнего Webinars WER около **80%**, поэтому среднее особенно чувствительно к этому домену. Здесь отсутствуют GigaAM-v3 и Parakeet-v3. Сравнивать 10.16% Vikhr с 7.19% ESpeech как результаты одного испытания нельзя. Полного evaluation runner в просмотренном Space не установлено; неизвестные настройки и аномальный домен требуют осторожности.

### 3. GigaAM: подробная оценка разработчика

[Официальная evaluation table](https://github.com/salute-developers/GigaAM/blob/main/evaluation.md) отдельно сообщает WER по десяти русским доменам. GigaAM-v3 RNNT: среднее **8.3%**, Natural Speech **6.9%**, Callcenter **9.5%**, OpenSTT Phone Calls **17.4%**. На той же таблице Whisper: среднее **21.0%**. Для некоторых моделей автор применяет удаление пунктуации/регистра и нормализацию чисел; нельзя переносить результаты на другие правила подсчёта. Часть наборов внутренняя, поэтому это первичный результат разработчика, а не независимый полностью воспроизводимый рейтинг.

## Английский: HF Open ASR и проблема `avg`

[Код и методика HF](https://github.com/huggingface/open_asr_leaderboard), [официальные результаты CSV](https://huggingface.co/datasets/hf-audio/open-asr-leaderboard-results/blob/main/english_short_latest.csv). Revision данных `c7826fcb10421e50500dc71f7b04c704d245604d`, последнее изменение **09.10.2026**. HF использует WER и RTFx; текущие short-form задания выполняются в Docker на H200 141 GB. Пропускная способность H200 не прогнозирует время ответа ноутбука при одной записи. Также существуют отдельные long-form и multilingual tracks; их результаты нельзя смешивать.

**Проверка CSV выявила несопоставимый `avg`.** Например, Parakeet-v3 содержит девять WER, но `avg=6.5143` совпадает со средним семи колонок без LS Clean/Other. `resonant-1-flash` имеет `avg=4.02375`, отсутствующий URGENT2024 и другое покрытие. Сортировка по `avg` показывает преимущество строк с другой формулой/пропусками. Поэтому «лучший в HF» без указания покрытия сейчас вводит в заблуждение.

Для сравнения ниже **наш расчёт по опубликованным ячейкам**, не официальный новый leaderboard: арифметическое среднее строго девяти колонок `AMI-Cleaned`, `Earnings22-Cleaned-AA-chunked`, `Gigaspeech-Cleaned`, `LS Clean`, `LS Other`, `SPGISpeech`, `URGENT2024`, `Voice Arena Monsoon`, `Voxpopuli-AA-Cleaned`; включены только строки, где заполнены все девять. Округление после усреднения.

| Модель | Режим доступности | WER по одинаковым 9 наборам, % |
|---|---|---:|
| zoom/scribe_v2_pro | API, закрытая | **4.081** |
| microsoft/azure-speech-07-2026 | API, закрытая | 4.488 |
| modulate/multilingual | API, закрытая | 4.530 |
| elevenlabs/scribe_v2 | API, закрытая | 4.550 |
| Qwen/Qwen3-ASR-1.7B-hf | открытые веса | **4.936** |
| CohereLabs/cohere-transcribe-03-2026 | открытые веса | 5.207 |
| HojoAI/Hojo-ASR-V1 | открытые веса | 5.222 |
| Parakeet TDT v3, стандартный runtime | открытые веса | 5.583 |
| Qwen/Qwen3-ASR-0.6B-hf | открытые веса | 5.770 |
| Whisper large-v3 | открытые веса | 6.663 |

Таким образом, на этом фиксированном покрытии лучший доступный открытый результат — Qwen3-ASR-1.7B-hf, закрытый — Zoom Scribe v2 Pro. Строка `-hf` и оригинальная Qwen модель — разные конкретные evaluation artifacts; не переносить результат автоматически на иной runtime/quant. API-строки полезны для ориентира, но не являются вариантами локальной установки.

## Практичные локальные кандидаты

| Кандидат | Языки и лицензия весов | Размер и runtime | Роль / ограничение |
|---|---|---|---|
| Уже установленный Parakeet TDT 0.6B v3 | RU/EN, CC-BY-4.0 | 600M; NeMo / NeMo-Speech.cpp GGUF CPU, Q8_0 | Первый baseline без миграции; качество конкретного quant не подтверждено рейтингом |
| GigaAM-v3 RNNT | прежде всего RU, MIT | 220–240M; PyTorch либо официальный ONNX export, CPU/GPU | Основной RU конкурент; обычный RNNT выдаёт текст для отдельного форматирования |
| GigaAM-v3 E2E RNNT | RU, MIT | тот же класс, PyTorch/ONNX | Уже делает пунктуацию/нормализацию; исследовать как альтернативу двум моделям |
| Qwen3-ASR-0.6B / 1.7B | RU/EN и другие, Apache-2.0 | qwen-asr Transformers/vLLM | Современный bilingual конкурент; официальные быстрые примеры ориентированы на CUDA, CPU память/скорость не обещаны |
| Moonshine Streaming Small/Medium | только EN, MIT | 123M / 245M; Moonshine Voice и Transformers CPU/GPU | Отдельная экономная ветка для EN; не решает RU |

Источники: [Parakeet model card](https://huggingface.co/nvidia/parakeet-tdt-0.6b-v3), [GigaAM card](https://huggingface.co/ai-sage/GigaAM-v3), [Qwen card](https://huggingface.co/Qwen/Qwen3-ASR-0.6B), [Moonshine card](https://huggingface.co/moonshine-ai/moonshine-streaming-medium).

Для ориентира, только весовые тензоры при 16 битах требуют примерно `parameters × 2 bytes`: 0.24B ≈ 0.48 GB, 0.6B ≈ 1.2 GB, 1.7B ≈ 3.4 GB. Это расчёт нижнего ориентировочного объёма, **не оценка полной RAM**; frontend, encoder tensors, decoder cache и runtime добавляются. При 4 GiB свободных Qwen 1.7B в float формате вместе с форматтером может не поместиться: это риск, не измеренный факт.

GigaAM требует Python ≥3.10 и ffmpeg; `transcribe` рассчитан на аудио до 25 секунд, longform использует дополнительные зависимости и gated pyannote модель. Для Telegram длинные сообщения должны войти в отдельную проверку сегментации. Подробности: [GigaAM README](https://github.com/salute-developers/GigaAM). Встроенное E2E форматирование не равно очистке повторов, исправлению оговорок и превращению диктовки в список.

Canary-v2, Omnilingual-v2, Voxtral, Granite и новые Nemotron также просмотрены через официальные результаты и model/runtime cards. Canary-v2 поддерживает RU, но в доступной таблице Vikhr не лидер. Большие speech-LLM и 7B/24B модели не первые кандидаты для 8 GiB ноутбука; лидер общей оценки не обязательно пригоден для этого железа.

## Конкретный Hermes ASR и аудио Telegram

Официальная [документация NeMo-Speech.cpp](https://github.com/NVIDIA/NeMo-Speech.cpp) поддерживает именно Parakeet TDT v3. Runtime Apache-2.0, веса Parakeet CC-BY-4.0. [Описание моделей](https://github.com/NVIDIA/NeMo-Speech.cpp/blob/main/docs/asr/models.md) указывает: Parakeet-v3 — multilingual offline transducer, сам расставляет пунктуацию; cache-aware streaming не поддерживается. Для voice, уже записанного целиком, это нормальное ограничение. Q8_0 квантирует линейные/pointwise-convolution веса, часть тензоров остаётся floating point.

Model card Parakeet требует mono 16 kHz `.wav`/`.flac`. Найденный wrapper с ffmpeg → PCM16 mono 16kHz соответствует этому входу. Нельзя передать OGG/Opus напрямую модели, которая принимает PCM, и считать формат решённым только расширением файла. Новый runtime может принимать другие PCM sample rates, но это не доказательство того же поведения установленной версии.

[Официальный runtime benchmark](https://github.com/NVIDIA/NeMo-Speech.cpp/blob/main/BENCHMARK.md) измеряет другой ASR — **Nemotron Speech Streaming EN 0.6B**, CPU i7-11700K, 8 потоков, подвыборку LibriSpeech. Он не подтверждает скорость Parakeet на i5-1235U. Сторонний [parakeet.cpp](https://github.com/mudler/parakeet.cpp) публикует проверки parity/quant, но принадлежит другому runtime: их нельзя выдавать за тест установленного nemo-speech. Отдельно измерить WER после Q8_0 и peak RSS.

Если позже рассматривается облако, **не путать Whisper v3 и `whisper-1`**: [официальная OpenAI API reference](https://developers.openai.com/api/reference/resources/audio/subresources/transcriptions/methods/create) связывает `whisper-1` с Whisper V2 и документирует `gpt-transcribe`, OGG/WAV и другие контейнеры. Никакие OpenAI API в этом исследовании не запускались; выбор облака здесь не предлагается.

## План проверки Telegram voice, без запуска

1. Подготовить вручную проверенные реальные RU/EN голосовые: по 30–50 в каждой группе плюс отдельная группа RU+EN code-switching. Разделить подбор параметров и отложенную оценку; не считать read speech или синтетическую диктовку представительной заменой.
2. Включить записи 5–15 секунд, 30–90 секунд и 2–5 минут; шум, тишину, обрывки, повторы/самоисправления, акценты, даты/суммы, имена, английские названия в русской речи, команды и отрицания. Сохранить исходный Telegram OGG/Opus и точные параметры конвертации.
3. Сравнить текущий GGUF Parakeet, GigaAM-v3 RNNT, GigaAM-v3 E2E и один Qwen 0.6B. При ресурсе добавить Qwen 1.7B и EN Moonshine. Зафиксировать checkpoint/revision/hash, quant, decoder, VAD, число потоков. Модели не менять между двумя языковыми группами без явного указания маршрутизации.
4. Считать нормализованный WER `(S+D+I)/N`, CER для RU, ошибки имён/чисел/отрицаний, галлюцинации на тишине и пропуски хвоста; отдельно читать ненормализованный текст. Не смешивать raw ASR WER с оценкой уже переписанного форматтером текста.
5. Для omarchy измерить холодный и тёплый p50/p95 общего времени, отдельно запуск процесса, ffmpeg, ASR и форматтер; peak RSS и swap, 1 запрос и очередь. RTF = время обработки / длительность аудио; RTFx обратный. Не переносить throughput GPU leaderboard на UX.
6. Проверить пригодность текста как команды: сохранены намерение, числа, имена, отрицания; списки и абзацы читаются в Telegram. При сравнении форматтеров хранить raw transcript и formatted transcript отдельно. «Красивый» текст с изменённым смыслом — проигрыш независимо от WER.

Нерешённое: полная методика русского ESpeech, причины аномального Tone Webinars у GigaAM-v2, качество bilingual/code-switching конкретного GGUF и предел памяти с одновременным форматтером. Эти вопросы требуют последующей оценки; исследование не даёт готового победителя по Telegram voice.
