# Аутентификация durable outbox после reopen

P1 finding из live restart/replay: `/private/tmp/p5-mac-live/late-wire-diagnostic-safe.txt` показал successful handshake и закрытие persisted event sequence 1364 с `StatusPolicyViolation: node protocol: authentication failed`. Проверка production sink на безопасной копии приняла тот же event; блокер оказался на transport HMAC boundary.

## Публичное воспроизведение

`TestAuthenticatedProtocolReplaysOutboxAfterStoreReopen` создаёт terminal event через LocalStore.QueueOutcome, закрывает и повторно открывает store, проверяет наличие persisted pretty JSON в payload, проходит authenticated handshake и отправляет PendingEvent через публичный ProtocolConnection. После ACK ещё один reopen должен сохранять пустую outbox и прежний ACK sequence. Native execution в этом сценарии вообще не вызывается.

На прежнем signer публичный overlay `/private/tmp/p5-canonical-auth-red-overlay.json` (source `/private/tmp/p5-pre-canonical-protocol_types.go`) дал RED в обоих случаях summary — string whitespace и HTML/U2028/U2029: handshake проходит, event закрывает socket с authentication failed. Команда: `go test -overlay /private/tmp/p5-canonical-auth-red-overlay.json ./internal/node -run '^TestAuthenticatedProtocolReplaysOutboxAfterStoreReopen$' -count=1`.

Отдельный публичный Envelope Sign/Marshal/Decode/Verify regression подтвердил несовместимость formatted JSON и raw `<>&`/Unicode separators с прежней подписью. `LocalStore.MarshalIndent` форматирует RawMessage, тогда как `Envelope.json.Marshal` компактизирует и HTML-escapes его. HMAC от исходных RawMessage bytes не совпадал с HMAC от decoded wire payload.

## Исправление и совместимость

General signing caller — NewEnvelope; все transport/daemon inbound paths вызывают Authenticator.Verify. Sign теперь канонизирует payload через стандартный `json.Marshal(RawMessage)`, тот же serializer, что используется envelope wire. Простого json.Compact недостаточно: он не воспроизводит HTML/U2028 escaping. JSON не декодируется в generic object: порядок ключей, lexical number representation и пробелы внутри string values не нормализуются.

Verify сравнивает canonical HMAC. Чтобы не отвергать прежде валидные manually formatted signed frames, оставлен второй raw-byte HMAC comparison. Он доступен только после Validate, включающей json.Valid, и проверки nonempty per-Node secret/signature. Подпись остаётся обязательной; secret, Node identity, envelope metadata и nonce authentication не менялись. Invalid JSON не может пройти legacy fallback. Оба сравнения используют hmac.Equal.

Независимые static golden signatures получены Python standard-library HMAC для старого documented frame input: compact, pretty и escaped payloads. Тест подтверждает unchanged normal compact signature, принятие старых valid signatures, отказ изменённых пробелов внутри строки, payload tamper, Node identity tamper и корректно legacy-signed invalid JSON. Реальные credentials не использовались и не выводились.

Исправление не редактирует pending outbox, не меняет lifecycle payload/identities и не повторяет execution. При штатном replay после restart исправленная transport подпись позволяет событию дойти до production sink и ACK path.

## Проверки

- Public reopen/wire-format/golden regressions — RED до исправления и PASS после.
- `go test -race ./internal/node -run 'Protocol|Authenticated|Daemon|Receipt|LateNative|StopsActivity|LocalStore' -count=1` — PASS.
- `go test -race ./cmd/secretaryd -run 'TestNodesConnect|TestRootHandlerOwnerNodeAccess' -count=1` — PASS.
- `go build ./...` — PASS.
- `git diff --check` — PASS.

Это fixture evidence исправленного restart/replay boundary. Полная native restart acceptance требует свежего integrated deploy; старый live FAIL не объявляется PASS по unit/fixture результату. Claude quota403 и остальные открытые gate constraints сохраняются.
