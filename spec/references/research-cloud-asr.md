# Облачный ASR для готовых Telegram voice

Проверено **11.10.2026** по первичным источникам. Только документация: модели не запускались, аудио в облако не отправлялось, ключи и omarchy не менялись. Продолжение [локального ASR research](../.scratch/telegram-voice-asr/research-asr-models.md). Silero TE исключён по решению пользователя.

## Рекомендация

Для первого сравнения качества RU/EN взять **ElevenLabs `scribe_v2`** и **OpenAI `gpt-transcribe`**. Экономичный кандидат — **Groq `whisper-large-v3-turbo`**, дополнительный баланс качества/цены — **Soniox `stt-async-v5`**. Это shortlist для проверки, а не установленный победитель: сопоставимого независимого теста всех актуальных моделей на Telegram voice не найдено.

Отдельно проверить два готовых ASR+cleanup решения: **Scribe v2 + `transcript_edit`** и **AssemblyAI Dictation API**. Один внешний запрос может убрать второй сетевой обмен, но обработка форматтером всё равно выполняется после ASR. Поэтому один API не доказывает меньшую полную задержку.

## Цены и пригодность

Все ставки — публичные API, без налогов, подписок, бесплатных промокредитов и индивидуальных скидок. ASR тут означает обработку законченного файла, а не ночной discounted Batch API и не живую диктовку.

| Провайдер и модель | Ставка | RU/EN и форматирование | Практическое ограничение |
|---|---:|---|---|
| OpenAI `gpt-transcribe` | $0.0045/min | Текущий рекомендованный старт для file transcription; context/keywords/language hints | Отдельный cleanup нужен для гарантированного режима редактирования |
| OpenAI `gpt-4o-transcribe` / `gpt-4o-mini-transcribe` | ориентир $0.006 / $0.003/min | Предшественники, заявлено улучшение WER относительно Whisper | Token-based, поминутные значения **оценочные**, не фиксированный тариф |
| ElevenLabs `scribe_v2` | $0.22/h | RU/EN; `no_verbatim` убирает disfluencies; `transcript_edit` задаёт стиль | Edit экспериментальный, +$0.07/h по pricing |
| Groq `whisper-large-v3-turbo` / `whisper-large-v3` | $0.04 / $0.111/h | Multilingual Whisper; punctuation и context prompt | Минимум **10 секунд оплаты на запрос**; prompt не chat instruction |
| Soniox `stt-async-v5` | около $0.10/h | RU/EN, mixed speech, structured-data formatting | Реально token-based; async job добавляет lifecycle |
| Deepgram `nova-3`, `language=multi` | $0.0052/min prerecorded PAYG | RU/EN code-switching; smart formatting | Smart formatting не произвольный редактор диктовки |
| AssemblyAI `universal-3-5-pro` async / `universal-2` | $0.21 / $0.15/h | Pro async опубликован для 18 языков **без RU**; RU через Universal-2 | Не переносить новые streaming языки на async Pro |
| AssemblyAI Dictation API | $0.62/h all-in | Языковая документация явно содержит RU/EN; cleanup + instruction | WAV/PCM, **до 120 секунд**, отдельный endpoint |
| Yandex SpeechKit `general` async | $0.004967214/min | RU/EN, normalization | Минимум 15 секунд, далее округление до секунды; mixed-language ограничения |

Источники ставок: [OpenAI pricing](https://developers.openai.com/api/docs/pricing), [GPT-Transcribe card](https://developers.openai.com/api/docs/models/gpt-transcribe), [ElevenAPI pricing](https://elevenlabs.io/pricing/api), [Groq STT](https://console.groq.com/docs/speech-to-text), [Soniox pricing](https://soniox.com/pricing), [Deepgram pricing](https://deepgram.com/pricing), [AssemblyAI pricing](https://www.assemblyai.com/pricing), [Yandex USD pricing](https://aistudio.yandex.ru/en/docs/speechkit/pricing). Языки AssemblyAI: [async Pro release](https://www.assemblyai.com/blog/universal-3-5-pro-code-switching-contextual-prompting), [Universal-2 languages](https://www.assemblyai.com/docs/pre-recorded-audio/supported-languages), [Dictation languages](https://www.assemblyai.com/docs/dictation/language-selection).

Soniox **v5**, а не v4: [каталог](https://soniox.com/docs/stt/models) датирует async-v5 11.06.2026; старый v4 маршрутизируется на v5. Soniox оценка из pricing: 30,000 audio input tokens/h × $1.50/M + 15,000 output text tokens/h × $3.50/M = **$0.0975/h**, плюс context tokens. В таблице расходов используется округлённый ориентир $0.10/h; это не точный счёт.

Yandex ставка: $0.0012418035 за 15 секунд × 4. USD применим к определённым международным контрагентам, российский договор тарифицируется в RUB. Конвертацию валют здесь не делаем. Deferred-general дешевле, но откладываемый режим не следует выбирать ради интерактивного UX без проверки сроков. [Правила тарификации](https://aistudio.yandex.ru/en/docs/speechkit/pricing).

### Расходы на месяц

Формула: `minutes × price_per_minute` либо `minutes / 60 × hourly_rate`. Допущение — одна звуковая дорожка, без retries, короткие записи не попадают под минимумы; иначе счёт выше. Форматтер отдельно, кроме строк с явно включённым cleanup.

| Вариант | 30 min | 100 min | 300 min | 1000 min |
|---|---:|---:|---:|---:|
| Groq turbo | $0.020 | $0.067 | $0.200 | $0.667 |
| Groq full v3 | $0.056 | $0.185 | $0.555 | $1.850 |
| Soniox, оценка | ~$0.050 | ~$0.167 | ~$0.500 | ~$1.667 |
| OpenAI mini, оценка | ~$0.090 | ~$0.300 | ~$0.900 | ~$3.000 |
| OpenAI gpt-transcribe | $0.135 | $0.450 | $1.350 | $4.500 |
| OpenAI 4o full, оценка | ~$0.180 | ~$0.600 | ~$1.800 | ~$6.000 |
| Scribe v2 | $0.110 | $0.367 | $1.100 | $3.667 |
| Scribe v2 + edit ($0.29/h) | $0.145 | $0.483 | $1.450 | $4.833 |
| Deepgram multilingual | $0.156 | $0.520 | $1.560 | $5.200 |
| AssemblyAI Universal-2 | $0.075 | $0.250 | $0.750 | $2.500 |
| AssemblyAI Pro async (EN) | $0.105 | $0.350 | $1.050 | $3.500 |
| AssemblyAI Dictation all-in | $0.310 | $1.033 | $3.100 | $10.333 |
| Yandex async USD | $0.149 | $0.497 | $1.490 | $4.967 |

У Groq фактическая формула `sum(max(duration_seconds,10))/3600 × hourly_rate`: например, 300 записей по 3 секунды дают 15 минут аудио, но оплачиваются как 50 минут. У Yandex async — `sum(max(ceil(seconds),15))`, каналы округляются до пары. Минимум edit ElevenLabs также 10 секунд. [Groq billing](https://console.groq.com/docs/speech-to-text), [Yandex rounding](https://aistudio.yandex.ru/en/docs/speechkit/pricing), [edit billing](https://elevenlabs.io/docs/eleven-api/guides/how-to/speech-to-text/batch/transcript-editing).

Расход и платёж различаются. **ElevenLabs PAYG минимум пополнения $5**, деньги действуют 12 месяцев; подписка не обязательна. [PAYG](https://elevenlabs.io/docs/overview/administration/pay-as-you-go). AssemblyAI заявляет отсутствие commitments/upfront fees в pricing, но [account docs](https://www.assemblyai.com/docs/faq/how-to-get-your-api-key) описывают deposit/credits: порядок оплаты аккаунта требует проверки. Точный минимум стартового платежа OpenAI/Groq/Soniox в просмотренных страницах не установлен. Нельзя обещать покупателю буквально платёж $1 каждый месяц только из таблицы расхода.

## Файл Telegram и задержка

Завершённый voice — подходящая задача для file ASR. Для OGG/Opus документация допускает прямой вход у [ElevenLabs](https://elevenlabs.io/docs/overview/capabilities/speech-to-text/), [Deepgram](https://developers.deepgram.com/docs/supported-audio-formats), [Groq](https://console.groq.com/docs/speech-to-text), [Yandex](https://aistudio.yandex.ru/en/docs/speechkit/formats). OpenAI [API reference](https://developers.openai.com/api/reference/resources/audio/subresources/transcriptions/methods/create) включает OGG/FLAC, тогда как список в [guide](https://developers.openai.com/api/docs/guides/speech-to-text) их пропускает; это расхождение документации, direct OGG нужно отдельно подтвердить небольшой проверкой. Guide указывает лимит 25 MB. ElevenLabs — 3 GB и до 10 часов standard; Groq direct attachment 25 MB, более крупный файл через URL. У Soniox точный перечень контейнеров в этом проходе не подтверждён; безопасно планировать проверку прямого OGG либо decode, не заявлять отсутствие transcoding.

**AssemblyAI Dictation требует WAV/raw 16-bit PCM**, до 120 секунд. Это означает decode OGG и рост upload размера. Записи длиннее требуют другого пути, а разрезание может потерять контекст самоисправления. [Audio requirements](https://www.assemblyai.com/docs/dictation/audio-requirements). Docs quickstart содержит 32 языка, pricing — 19; RU подтверждён отдельно языковой страницей, но модель/rollout конкретного аккаунта нужно проверять.

Нет общего доказательства «самый быстрый». Groq публикует RTFx 216/189 — throughput относительно продолжительности записи, не p95 ответа с сетью. ElevenLabs realtime ~150 ms относится к инкрементальной речи, не готовому file. AssemblyAI Sync публикует ~134 ms p50 с pre-warmed connection и US/EU — отдельное измерение без cleanup и без download Telegram. [Sync technical walkthrough](https://www.assemblyai.com/blog/sync-speech-to-text-api-technical-walkthrough). Pricing Sync сейчас $0.30/h, [сравнительный blog](https://www.assemblyai.com/blog/sync-api-vs-dictation-api) ещё $0.45/h; использовать pricing, не старый blog. «Under a second» Dictation — vendor statement, не воспроизведённый Telegram E2E результат.

Будущий критерий UX: p50/p95 от получения Telegram update до пригодного текста; отдельно Telegram download, decode, cloud upload/queue/ASR, formatting, agent dispatch. Измерить 5/15/60/120/300 секунд, RU/EN/mixed, concurrency 1/5, warm/cold connections. Не смешивать этот показатель с генерацией первого токена и streaming endpointing.

## Качество: что подтверждено

Scribe v2 — сильный quality candidate, а не доказанный универсальный лидер. [Релиз](https://elevenlabs.io/blog/introducing-scribe-v2) показывает FLEURS benchmark и улучшение стабильности long-form; [capability docs](https://elevenlabs.io/docs/overview/capabilities/speech-to-text) группируют RU/EN в ≤5% WER. Это измерения поставщика; read speech и агрегация языков не заменяют noisy Telegram voice, имена и команды. Независимый [commercial code-switching paper](https://arxiv.org/abs/2605.19069) ставит Scribe v2 первым на Arabic/Persian/German + EN, но **RU там нет**, и это не тест новых gpt-transcribe/Soniox-v5.

[AssemblyAI benchmarks](https://www.assemblyai.com/benchmarks) публикует FLEURS/Common Voice WER и отдельные realtime таблицы; автор — конкурент, набор поддерживаемых языков у систем отличается. Нельзя сравнивать average по семи языкам с average по восемнадцати, или переносить realtime U3.6 RU на async U3.5. У нового [GPT-Transcribe](https://developers.openai.com/api/docs/models/gpt-transcribe) заявлены hints/code-switching, но на просмотренной card нет общего воспроизводимого RU/EN сравнения с этой четвёркой. Deepgram [Nova-3 release](https://deepgram.com/learn/introducing-nova-3-speech-to-text-api) тоже vendor benchmark; Groq WER 10.3/12% на docs не сопровождается Telegram RU split. Поэтому окончательный quality winner требует собственных аудио и единой normalization.

[Yandex model docs](https://aistudio.yandex.ru/en/docs/speechkit/stt/models) подтверждают `general`, `ru-RU`, `en-US` и auto; одновременно предупреждают, что внутри смешанной фразы слова могут оказаться транскрибированы в одном языке. Это существенное ограничение для русской речи с английскими названиями. Актуального независимого сопоставимого leaderboard всех cloud кандидатов для RU не найдено.

## Cleanup в одном запросе

**Scribe:** `no_verbatim` удаляет fillers/false starts; `transcript_edit` применяет instruction после распознавания, возвращая отдельный `edited_transcript`. Original timestamps относятся к raw text. Edit добавляет задержку пропорционально длине; не совместим с entity detection/redaction и multichannel. Docs дают surcharge 30%, pricing округлённый +$0.070/h: расчёт выше использует опубликованный ценник. [Edit contract](https://elevenlabs.io/docs/eleven-api/guides/how-to/speech-to-text/batch/transcript-editing).

**AssemblyAI Dictation:** default cleanup и `llm_instruction`; возвращаются raw `text` и cleaned `llm_response`. Сервис имеет внутренний 5-second rewrite deadline: timeout может вернуть HTTP 200 с raw transcript и `llm_error`, а не успешное formatting. [Quickstart](https://www.assemblyai.com/docs/dictation), [failure semantics](https://www.assemblyai.com/blog/sync-api-vs-dictation-api). Сам ASR — Universal-3.5 Pro, сервис форматирования отдельный даже при одном HTTP call.

Мой вывод для выбора: Scribe+edit первым проверить как качественный integrated кандидат с прямым OGG; Dictation — challenger для коротких голосовых и latency, учитывая PCM upload и лимит 120s. Обычная punctuation/ITN у Deepgram/Soniox/Yandex не обещает удаления повторов, правильного разрешения оговорок и списка задач. Смысловую безопасность нельзя гарантировать одной рекламной функцией: хранить raw+formatted, проверять числа/имена/отрицания, при timeout использовать raw. Эти пункты — предложения, не реализованные изменения.

## Данные пользователей

- **OpenAI:** API data не идёт на training без opt-in; для `/v1/audio/transcriptions` таблица явно указывает **None** для abuse monitoring и application state retention, ZDR eligible. Для text formatting через Chat/Responses действуют другие правила (обычно abuse logs 30 дней); EU residency требует одобрённых retention controls. Не считать отдельный downstream formatting endpoint автоматически равным ASR endpoint по retention. [Data controls](https://developers.openai.com/api/docs/guides/your-data).
- **ElevenLabs:** training opt-out в Data use; Zero Retention Mode — Enterprise, не стандартный дешёвый PAYG. [Training setting](https://elevenlabs.io/docs/help-center/legal/is-my-data-used-to-improve-eleven-labs-ai-models), [ZRM](https://elevenlabs.io/docs/eleven-api/resources/zero-retention-mode). Точный обычный срок хранения Scribe в просмотренных docs не установлен.
- **Deepgram:** по умолчанию retained/model improvement; `mip_opt_out=true` исключает content retention, US/EU endpoint задаёт регион. Async callback хранится до доставки; metadata отдельно 90 дней. Дополнительную стоимость opt-out надо подтвердить для аккаунта, опубликованный ASR расход её не включает. [Your Data](https://developers.deepgram.com/trust-security/your-data).
- **Soniox:** no training; **Async хранит** аудио/результаты до 30 дней либо удаления API. Не переносить no-retention realtime на async. [Privacy](https://soniox.com/docs/security-and-privacy).
- **Groq:** inference обычно не хранится; debugging/abuse исключения до 30 дней, all customers могут включить ZDR; retained data в US. [Your Data](https://console.groq.com/docs/your-data).
- **AssemblyAI Dictation:** US/EU endpoints documented; гарантии retention конкретно этого нового сервиса и training opt-out нужно закрепить перед production. [Data zones](https://www.assemblyai.com/docs/dictation/endpoints-and-data-zones). Для Yandex precise retention/processing region этого файла в данном проходе не установлены.

Пользовательский выбор стоит описывать как «локально на вашем сервере» / «облако с оплатой использования». Обещать «лучшее качество за $1» пока рано: около 100 минут Scribe+edit стоит ~$0.48 переменного расхода, Dictation ~$1.03, но это без налога, retries, минимального пополнения и инфраструктуры. По нашей арифметике $1 теоретически покрывает ~207 минут Scribe+edit или ~97 минут Dictation. Реальный тариф продукта и выбранный cloud default требуют проверки UX и финансовой модели.
