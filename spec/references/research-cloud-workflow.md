# Облачная диктовка и Telegram: продуктовый контекст

Дата проверки: 2026-10-11. Только исследование; API, модели, код и remote не менялись.

## Willow и вероятный Wispr Flow

Под «Whisper Voice (or something like that)» предположительно имеется в виду **Wispr Flow**. Это предположение о названии, не установленная идентичность продукта пользователя. Whisper как ASR-семейство не является тем же продуктом.

[Willow Privacy Policy](https://willowvoice.com/privacy-policy) описывает cloud processing аудио для функций диктовки и controls хранения. Private Mode предусматривает transient обработку без сохранения audio/text; правила отличаются от opt-in улучшения моделей. Это условия готового Willow приложения, не гарантия для произвольного выбранного нами API-провайдера.

[Wispr Security overview](https://docs.wisprflow.ai/articles/3467817258-security-and-compliance-faq) подтверждает cloud transcription и отдельные controls хранения/использования данных. [What is Flow](https://docs.wisprflow.ai/articles/2772472373-what-is-flow) описывает диктовку, cleanup и вставку текста после остановки; desktop не показывает live word-by-word текст. Эти документы не доказывают конкретную chunking-архитектуру или точные ASR/LLM модели внутри.

[Willow Developers](https://willowvoice.com/developers) и [Wispr Developers](https://wisprflow.ai/developers) — страницы про использование dictation приложений разработчиками. В прочитанных страницах не найден контракт публичного ASR+cleanup API с ценой для интеграции Secretary. Это ограниченный результат поиска, а не доказательство отсутствия всех enterprise/API предложений. Не использовать private client endpoints вместо документированного API.

## Почему Telegram — другой путь

Telegram Bot API доставляет voice в Message после отправки пользователем. В обычном bot workflow нет доступа к микрофону пользователя и к ещё не отправленной записи. Voice — не обязательный MP3: Telegram voice обычно OGG/Opus; надо проверять metadata и реальный контейнер, а не только расширение. [Telegram Voice](https://core.telegram.org/bots/api#voice), [описание OGG/Opus voice](https://core.telegram.org/bots/api#inlinequeryresultvoice), [getFile](https://core.telegram.org/bots/api#getfile).

Для обычного облачного Bot API файл скачивается по file_id через getFile; лимит скачивания 20 MB. Link с bot token нельзя передавать стороннему ASR как публичный URL или логировать. Скачивать своими credentials, затем передавать байты провайдеру либо размещать временно только по отдельно выбранной политике. Прямая поддержка OGG/Opus у выбранного ASR избавляет от ffmpeg; где нужен PCM, конвертация остаётся отдельным этапом.

Streaming API может ускорить обработку готового файла, если позволяет отправить файл быстрее real-time и корректно финализировать. Сам термин streaming не означает, что Telegram даст аудио во время диктовки или что полная запись быстрее batch endpoint. Нужны отдельные проверки uploader/backpressure/flush и latency готового результата.

## Что измерять и как давать выбор

Общий путь: Telegram availability → download → optional decode → upload → ASR final → formatter final → canonical input → Secretary/Worker. Для UX важен p50/p95 всего пути после Send и отдельно каждого этапа. TTFT formatter не означает готовый usable transcript; до final и проверки сохранения смысла команду исполнять нельзя.

Дать выбор по двум понятным осям: где распознаётся аудио и где форматируется текст. Полностью локальный режим сохраняет обе стадии на host; полностью облачный снимает требования к inference hardware; hybrid local ASR + cloud formatting передаёт текст, но не аудио. «Локальный» не обещает, что оригинальный Telegram файл не проходил через Telegram.

Для первого продукта достаточно local/cloud профилей и optional cleanup; список провайдеров и установщик не реализованы и не выбраны. Облачный режим можно исследовать как BYOK/pay-as-you-go с usage cap; managed subscription требует отдельно учесть платежи, инфраструктуру, retries, поддержку, налоги и margin. $1 себестоимости модели не равно $1 устойчивого тарифа unlimited.

Сравнивать bundle из двух моделей и single audio model для transcribe+format в одном запросе. Последний убирает сетевой hop, но труднее отделить ошибки распознавания от переписывания; word-level accuracy и names/negation preservation требуют отдельной оценки. Punctuation/ITN в ASR не равны cleanup самокоррекций и абзацев.

Подробные цены/shortlist: [cloud ASR](research-cloud-asr.md), [cloud formatter](research-cloud-formatting.md). Это исследование не содержит измерений фактической скорости API.
