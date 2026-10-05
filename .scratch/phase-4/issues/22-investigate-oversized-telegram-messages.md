# 22 Decide how to deliver oversized Worker messages in Telegram

Type: research
Status: claimed

## Work

A single long Worker message appears broken into pieces in Telegram. Establish exactly what the user sees and whether the problem is caused by Telegram's message-size limit, our chunking boundary, formatting loss, or another part of delivery. The current adapter chunks messages at Telegram's 4096 UTF-16-unit limit and can split at a valid UTF-8 boundary without preserving paragraph, sentence or Markdown structure.

Reproduce with the supplied long-result example and inspect each delivered chunk, including links, lists, code fences, emoji and retry/replay behavior. Recommend a clear product behavior before implementing a fix. Options to assess include splitting at semantic/formatting boundaries, sending the full text as a file, or showing a short preview with access to the complete Result. Do not assume that Telegram or the model is at fault.

## Acceptance

- The report identifies where and why the message appears broken, with a reproducible case and the relevant Telegram size accounting.
- A recommendation states how long content should appear in Worker Topic and General, including the trade-off between multiple messages and a file/preview.
- The recommendation covers Markdown boundaries, Unicode, message ordering, delivery retry and deduplication.
- The decision and follow-up implementation scope are recorded in this ticket before changing delivery behavior.

## Comments

### Локальное исследование implementer — до изменений formatter21

Исходный long-result payload/screenshot в доступных материалах не найден; восстановление его содержания по памяти не выполнялось. Воспроизводимый equivalent: `internal/telegram/long_message_research_test.go`, `TestLongMessageResearchBoundaries`. Это synthetic fixture, не Telegram acceptance.

Команда `go test ./internal/telegram -run '^(TestLongMessageResearchBoundaries|TestLongWorkerResultResumesBothDestinationsAfterRestart)$' -count=1 -v` — PASS (0.552s). Проверено текущее поведение до правок: `telegramMessageChunk` режет исходный текст по 4096 UTF-16 units, а не bytes/runes; astral emoji занимает 2 units. Суррогатная пара/UTF-8 code point не разрезаются. Grapheme cluster может разрезаться: combining accent и ZWJ sequence остаются в следующей части. Шесть fixtures дают два lossless UTF-8 chunks: astral4095+3, Markdown link4096+28, list item4096+1, fence4096+22, combining4096+1, ZWJ4096+3. Link разрезан внутри label, fence — после opening fence. Paragraph/sentence/list/Markdown boundaries splitter не учитывает. `safeText` дополнительно удаляет newlines — отдельная причина21; Telegram пока получает literal Markdown без parse mode.

Доставка идёт последовательно: все chunks Topic, затем все chunks General; General сейчас добавляет Worker reference. Outbox сохраняет `DeliveredBytes` после каждого подтверждённого chunk. Regression с ошибкой на Topic chunk2 и General chunk1, restart и event retry подтверждает отсутствие потери/повторения уже подтверждённых chunks и отдельную dedup каждой destination. Неустранимая текущим Bot API граница: если Telegram принял sendMessage, но acknowledgement потерян либо процесс упал до durable checkpoint, retry может повторить chunk (у sendMessage нет server-side idempotency key). Это не обещание exactly-once Telegram.

Bot API limit относится к тексту после entity parsing, не к bytes HTML request. После formatter21 текущий splitter продолжает консервативно считать UTF-16 units исходного Markdown/plain chunk (включая syntax/URI); generated HTML может быть длиннее4096 на проводе. `TestTelegramHTMLExpansionKeepsParsedSizeWithinLimit`:4096 символов `&` дают20480 HTML chars `&amp;`, но ровно4096 parsed UTF-16 units и lossless readable fallback. Нельзя подменять эту проверку ограничением4096 на длину escaped HTML.

Сравнение вариантов:

- **Semantic splitting полного Result**: сохраняет текущий inline UX и весь текст в обоих contexts; можно выбирать paragraph/list/fence/link и grapheme boundaries, повторно открывать markup. Цена — несколько уведомлений, длинный General; oversized одиночный блок всё равно требует fallback. Рекомендую первым вариантом после owner approval.
- **Full text file**: один attachment, весь исходный текст, удобнее для огромных отчётов. Цена — новый document UX/API/scopes и необходимость открыть/скачать файл; inline links/navigation хуже. Не выбран пользователем.
- **Preview + full Result**: уменьшает шум General, но вводит новую навигацию/сокращённый inline ответ и выбор места полного текста. Не выбран пользователем; нельзя молча сокращать Result.

**Предложение, не approved decision:** полный inline Result сохранять и в General, и в Worker Topic; добавить semantic/grapheme-aware splitting с безопасным markup в отдельном follow-up, без file/preview. Owner должен решить допустимость множества сообщений/уведомлений и нужны ли разные представления General/Topic для очень больших Results. Research можно считать подготовленным к review, не выдавать pending delivery implementation за fix.

**Уже разрешённый scope21:** сохранить newlines и безопасно форматировать существующие size-only chunks; неполная/неподдержанная Markdown-разметка остаётся читаемым escaped/plain text. Границы и порядок chunks не менять, нового attachment/preview UX не вводить. Это не semantic splitting implementation и не approval owner research22. Статус остаётся `claimed`.

### Проверки после локальной работы21/29

`TestLongMessageResearchBoundaries` продолжает PASS с теми же 4096-unit/Unicode boundaries; текущие raw chunk boundaries не изменились. `TestTelegramCurrentChunkFormattingHasBalancedTags` подтверждает, что HTML не разрезается после генерации: неполные Markdown links/fences в исходном chunk показываются escaped/plain, не превращаются в invalid HTML. Это безопасный formatter21, а не semantic splitting22; link, пересекающий исходную границу, пока не становится clickable в двух частях. `TestLongWorkerResultResumesBothDestinationsAfterRestart` и production-HTTP fixture `TestTelegramParseRejectionFallbackRetryAndDedup` PASS. Подтверждённые chunks не повторяются при event retry/restart; неизвестный исход sendMessage остаётся at-least-once limitation.

Полные `go test ./...` и `go test ./... -count=1`, `go test -race -p 1 ./...`, vet/build/diff-check, CLI/deployment isolated tests — PASS (точные команды/evidence в comments21). Real Telegram acceptance здесь не проводился. Исследование завершено рекомендацией для review; owner approval ни semantic splitting, ни file/preview не дан. Pending delivery implementation не объявляется выполненной.

## Answer

> Историческая owner-ready recommendation ниже была записана до ответа владельца. Owner впоследствии явно подтвердил её; актуальный approval и implementation evidence добавлены в конце этого ответа.

### Owner-ready recommendation — approval pending (история)

**Рекомендую оставить полный Result inline в обоих уже предусмотренных местах: сначала целиком в Worker Topic, затем полным зеркалом в General под сохранённым пользовательским названием Worker.** Для длинного Result заменить только size-only boundary на семантическую разбивку с сохранением разметки и extended grapheme clusters; файл и preview не вводить. Это сохраняет полный читаемый ответ и текущую прямую доставку, но сознательно оставляет несколько Telegram-сообщений и дублирование уведомлений между Topic и General. Это рекомендация, не утверждённый owner выбор.

Если owner подтвердит её, точный follow-up implementation scope:

1. До Telegram HTML formatting выбирать границы сначала между абзацами/блоками, затем между законченными пунктами списка или предложениями; не разрезать Markdown link, inline markup, code span или fenced block, если целая единица помещается в лимит. Для многочастного fenced block закрывать и повторно открывать fence с тем же language marker в каждой части.
2. Ограничивать результат после entity parsing лимитом Bot API (1–4096 characters); учитывать UTF-16 units для Unicode-safe границ и не разрезать grapheme cluster. Если отдельная единица сама длиннее лимита, сохранить её полностью, отступив к grapheme-safe plain-text split; не отбрасывать остаток и не обещать clickable link при разбиении чрезмерно длинного URL.
3. Сохранять текущую последовательность: все части Topic по порядку, затем все части General по порядку; durable checkpoint после подтверждённой отправки каждой части и независимую dedup по destination. Не заявлять exactly-once: неизвестный исход HTTP send или crash до checkpoint может повторить уже принятую Telegram часть, поскольку `sendMessage` не принимает idempotency key.
4. Не менять Result assembly, текст/summary, Worker identity, General/Topic routing, approvals или retries Attempt; не добавлять `sendDocument`, attachment, preview, navigation, новые credentials/scopes либо миграцию. Добавить focused coverage на production `BotAPITransport` HTTP fixture и экспортированный `Adapter.HandleDurableEvent`/`Transport` seam. Реальное Telegram acceptance выполнить отдельно до утверждения rollout.

Проверенное текущее поведение: Bot API `sendMessage` ограничивает текст после разбора entities, а не байтовую длину HTTP-поля. Реальная отправка передаёт `text` и `parse_mode=HTML`; текущий splitter до formatting режет исходную строку по 4096 UTF-16 units, включая Markdown syntax и URL, поэтому для обычного текста это консервативно, но не сохраняет структуру. Fixture с 4096 `&` даёт HTML `&amp;` длиной 20480 символов при 4096 parsed units. Официальная ссылка: [Telegram Bot API: sendMessage](https://core.telegram.org/bots/api#sendmessage) и [Formatting options / entity offsets](https://core.telegram.org/bots/api#formatting-options). Этот расчёт подтверждён локальной fixture, не живой Telegram-посылкой.

Проверка через доступные seams: `TestLongMessageResearchBoundaries` воспроизводит 4096-unit cuts внутри link label и list item, а также после opening fence; отдельные Unicode cases разрезают combining mark и ZWJ emoji. Все строки остаются UTF-8-valid и lossless. После formatter21 пересекающие границы link/fence остаются безопасным читаемым plain text, но link не становится clickable в двух частях. `constrainedTransport` в существующих тестах проверяет длину outgoing HTML до parsing, поэтому это намеренно synthetic/более строгая граница, не точная эмуляция Bot API для entity expansion; `TestTelegramHTMLExpansionKeepsParsedSizeWithinLimit` отдельно проверяет 4096 parsed units при 20480 HTML chars. `TestLongResultTopicThenGeneralCheckpointOrder` фиксирует порядок `Topic chunk 1 → Topic chunk 2 (ошибка) → Topic chunk 2 (retry) → General chunks 1–2`; restart/replay не повторяет checkpointed chunk и не отправляет завершённый Result повторно. Это synthetic Transport и локальный HTTP fixture, не acceptance Telegram. Потерянный ACK напрямую не симулирован; at-least-once ограничение следует из отсутствия Telegram idempotency key и durable checkpoint только на нашей стороне.

Исходный long-result payload/screenshot всё ещё отсутствует в доступных repo/history материалах, поэтому exact user incident не воспроизводился; использован synthetic equivalent. В сохранённой истории issue до текущей работы было только `ready-for-agent`, а `map.md`, approved phase4 spec и текущие issue comments не содержат owner-selected inline/file/preview behavior. Перечень `/implement` ticket22 не трактовался как такой выбор. Статус оставлен `claimed`, поскольку продуктовый выбор и implementation approval ещё не получены.

**Вопрос owner до ответа:** подтверждаешь ли полный Result inline и целиком в обоих местах (Topic первым, затем General) с семантическим/grapheme-aware splitting как описано выше, без file/preview? До ответа delivery boundaries и product UX не менять.

**Статус на момент этого handoff:** исследовательская рекомендация готова, implementation и owner approval pending. Этот исторический статус superseded обновлением ниже.

### Owner approval и локальная реализация — approval получен

Владелец ответил **YES** именно на полный inline Result в обоих destinations, Topic первым, затем General, semantic/grapheme-aware splitting, без file/preview. Scope и public acceptance seams `Adapter.HandleDurableEvent` / `Transport` / `BotAPITransport` HTTP подтверждены. Это разрешило local implementation; production rollout не разрешён.

Реализовано только для ticket22:

- `telegramMessageChunk` предпочитает paragraph, list-item и UAX #29 sentence boundaries, затем безопасные grapheme/Markdown границы. Complete links, inline code/emphasis и помещающиеся fenced blocks сохраняются целиком.
- Для длинного fenced block каждая исходная code-body часть выдаётся отдельно как balanced `<pre>…</pre>`; открывающий/закрывающий fence остаётся presentation syntax и не сдвигает source cursor.
- `DeliveredBytes` увеличивается на consumed UTF-8 bytes из исходного `OutgoingMessage.Text`; synthetic HTML и generated fences в offset не входят. Legacy partial outbox с astral Unicode возобновляется по старому source-byte checkpoint без повторения подтверждённого фрагмента.
- Oversized single grapheme — невозможный для Telegram атом — прогрессирует по UTF-8 code-point boundary только как fallback; обычные combining/ZWJ clusters не делятся. Не помещающиеся Markdown atoms отправляются lossless escaped text без malformed HTML.
- `github.com/rivo/uniseg v0.4.7` реализует UAX #29 grapheme/sentence segmentation (Unicode 15.0), без самодельного emoji-only алгоритма.
- HTTP fixture проверяет 4096 parsed UTF-16 units: 4096 `&` дают 20480 HTML wire chars и остаются одним parsed-size допустимым сообщением. Retry, topic-before-General order, per-destination dedup, parse-error-only fallback и отсутствие немедленного retry при других ошибках сохранены.

**RED → GREEN evidence:**

- `TestLongResultSplitsAtParagraphBoundaryThroughBotAPI`: до реализации fail — chunk разрезал абзац; после semantic splitter PASS.
- `TestLongResultKeepsListItemBoundaryThroughBotAPI`: до реализации fail — chunk отрезал list marker/item; после list atom PASS.
- `TestLongResultKeepsSentenceBoundaryThroughBotAPI`: до реализации fail — sentence boundary терялась; после UAX #29 sentence selection PASS.
- `TestLongResultKeepsFittingMarkdownLinkWholeThroughBotAPI`: hard-size split без paragraph boundary сначала разрезал link; после atom scanner PASS. `TestLongResultKeepsFittingInlineCodeWholeThroughBotAPI` PASS.
- `TestLongResultReopensLongFencedCodeInEveryBotAPIChunk` до реализации fail — fence fragments уходили plain/unbalanced; после generated balanced pre fragments PASS.
- `TestLegacyPartialFencedOutboxResumesAtOriginalSourceByteOffset` PASS: pre-existing offset 8186 UTF-8 bytes (4096 old UTF-16 source units), retry/restart продолжает точную raw-byte позицию, не HTML/fence offset.
- `TestResultKeepsExtendedGraphemeClustersWholeThroughBotAPI`, `TestSingleOversizedGraphemeUsesLosslessCodePointFallbackThroughBotAPI`, `TestOversizedMarkdownAtomsRemainLosslessEscapedTextThroughBotAPI`, `TestBotAPIChunkLimitCountsParsedUTF16NotEscapedHTMLBytes` и `TestLongResultTopicThenGeneralCheckpointOrder` PASS.

Команды после изменений: `go test ./internal/telegram -count=1` PASS; `go test -race -p 1 ./internal/telegram -run '^(TestLong.*|TestLegacyPartialFencedOutboxResumesAtOriginalSourceByteOffset|TestOversizedMarkdownAtomsRemainLosslessEscapedTextThroughBotAPI|TestSingleOversizedGraphemeUsesLosslessCodePointFallbackThroughBotAPI|TestResultKeepsExtendedGraphemeClustersWholeThroughBotAPI|TestBotAPIChunkLimitCountsParsedUTF16NotEscapedHTMLBytes|TestTelegramParseRejectionFallbackRetryAndDedup)$' -count=1` PASS; `go vet ./internal/telegram`, `go mod verify` и scoped `git diff --check` PASS. Все HTTP acceptance tests используют synthetic local Bot API fixture. Реальная Telegram acceptance не проводилась; исходный пользовательский payload/screenshot всё ещё недоступен. Acceptance по live Telegram и exact incident остаётся pending, поэтому ticket остаётся `claimed`; это не finished rollout/fix acceptance.
