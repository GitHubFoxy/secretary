# 21 Keep Worker progress out of the final Result and preserve Telegram formatting

Type: task
Status: claimed

## Work

The Worker output shown in Telegram currently mixes progress narration into the final Result and loses its formatting. The confirmed causes are:

1. The ACP adapter appends all Worker text for a turn to one `turnText`, so intermediate updates such as "Попробую получить…" are stored with the final answer.
2. Telegram `safeText()` normalizes whitespace with `strings.Join(strings.Fields(text), " ")`, removing paragraph breaks, headings and list layout. The saved Result still has its newlines; they disappear during Telegram delivery.
3. Telegram sends ordinary text without `parse_mode`, so Markdown links such as `[36:21](...)` are shown literally.

Keep progress/activity separate from the terminal Result. Preserve meaningful newlines and render supported Markdown safely, including clickable links. If Telegram parsing fails or content cannot be represented safely, fall back to readable plain text rather than dropping the Result. Continue applying Telegram's content-safety redaction.

## Acceptance

- The terminal Result contains the Worker's final answer, not intermediate progress narration accumulated during the turn.
- Progress remains available through the appropriate Worker activity/status path and is not silently mistaken for final output.
- Paragraphs, headings and lists retain their line breaks in Telegram.
- Supported Markdown links render as clickable links; unsafe or invalid markup falls back to readable plain text.
- Tests prove that sanitization preserves newlines while still removing forbidden internal data.
- Tests cover mixed progress/final ACP output, Telegram formatting, link parsing failures and duplicate delivery.
- Для короткого запроса пользователь получает короткий финальный ответ с источником/временем данных, а не журнал попыток.
- General не показывает технический prefix `wrk_...` вместо понятной адресации. Сохранять связь с исходным запросом через reply/доступную пользовательскую identity, без internal IDs.

## Comments

3 октября 2026 после deploy `459709c` пользователь предоставил screenshot `clipboard-2026-10-03-154839-116E8F48.png`. Worker Result начинается с "Проверю актуальную погоду..." и содержит "Попробую получить текущие данные Open-Meteo..." перед финальным выводом. Paragraph breaks потеряны, Markdown link `[wttr.in](https://wttr.in/Barnaul)` показан буквально, в General есть громоздкий `wrk_...` prefix.

Контрольный screenshot `clipboard-2026-10-03-154937-87A78503.png` показывает желаемый вид: один короткий ответ о погоде, отдельная строка с временем данных и clickable source. Числа из этого примера не являются тестовым ожиданием и не подставляются в реальный Result.

Следующий отдельный Secretary пересказ заведён в ticket29; web_fetch failure исследуется отдельно в ticket30. Не смешивать content assembly, transport formatting и фактическую доступность weather sources в одну предполагаемую причину.

### Первая локальная версия для parent review — superseded по compatibility21

**Важно:** parent review выявил blocking regression ниже: отсутствие summary у non-opt-in ACP нельзя превращать в новый universal failure. Прежняя трактовка этого поведения как «безопасного compatibility blocker» отклонена; текущий утверждённый scope и исправление записаны в последнем комментарии «Compatibility fix после blocking parent review». Evidence первой версии сохранено как история, не как разрешение на rollout.

**Установленные contracts:** native source OpenCode v2.0.22 `packages/cli/src/acp/translate.ts:session.text.delta` публикует explicit `messageId=assistantMessageID`; `response()` возвращает `stopReason/usage/_meta`, НЕ `summary`. Last assistant step с `end_turn` может быть собран без progress предыдущих messages. `max_tokens/refusal/cancelled` не объявляются complete final answer. Child messages помечены `_meta["opencode/child-session"]` и не входят в parent Result. Это verified version-specific mapping, не универсальная догадка ACP.

Проверен public pinned source `https://github.com/vercel-labs/fx/archive/refs/tags/v0.0.8.tar.gz` (только `/private/tmp/secretary-ticket21-source/`), локальный `fx --version` —0.0.8. `src/acp/types.zig:writePromptResponse{WithUsage}` также возвращает stopReason/usage без summary. `prompt.zig:pushText` сбрасывает message identity на assistant_started и отправляет operational text отдельными messageId, но в wire frame нет authoritative assistant-vs-operational phase. Поэтому **реальный fx response.summary contract не подтверждён: summary существует в наших fakes/extension, не в этой native версии**. Для других ACP summary также не предполагается стандартным полем; explicit extension сохранён и покрыт тестом.

**Изменено:** `acp_answer.go` хранит messages только текущего prompt. Native OpenCode включается явно через `TerminalMessageGrouping`; последний полный message после tool boundary становится Result, предыдущие/late старые messages остаются Activity. Нет regex/классификации по словам: тест намеренно использует «Final answer» в progress и «Попробую получить» в финальном тексте. Parent FIFO `RequestDrainingEvents` и replay separation сохранены: перед чтением native final answer обработаны все preceding deltas; без sleeps. User echoes, thought, child-session text, session/load replay и другие sessionId не входят в Result. Отсутствующая/неполная metadata даёт visible failed terminal report, не ложный `completed` и не guessed finality.

**Важный совместимый scope/gate:** existing fx binding/model/history/config не меняются. Но native fx0.0.8 без explicit terminal summary/phase теперь получает явное `Terminal answer unavailable`, а не объединённый журнал. Полный чистый Result для fx/прочих ACP без проверенного extension остаётся **compatibility blocker** до отдельного authoritative phase/terminal-message contract. Не выдавать fake summary test за native fx acceptance и не выкатывать этот change как готовую cross-harness совместимость. OpenCode native gate ниже проверен.

Telegram: `safeText` больше не схлопывает whitespace; headings/lists/paragraphs сохраняют переносы. Formatter каждого **существующего** size-only chunk генерирует allowlisted HTML (`b`, `code`, `pre`, `a`) с HTML escaping; clickable Markdown links допускают только absolute http/https без credentials/control whitespace. Unsafe links теряют URI, неполная/неподдержанная разметка остаётся читаемым escaped text. Content redaction сохранена и повторяется после сборки Secretary deltas. Подтверждённый Bot API400 «can't/cannot parse entities» включает durable readable plain fallback. 429/403/503/прочие400 и uncertain transport errors НЕ вызывают немедленную вторую отправку. Outbox checkpoints считаются в исходных bytes, не в HTML bytes, fallback policy durable; destination/event identity dedupe сохранена.

General показывает сохранённое пользовательское Topic title (либо нейтральное `Worker`), не `wrk_...`. Bridge больше не подменяет title terminal summary. Worker Topic/source routing и внутренние delivery identities сохранены; нового reply mapping/attachment/preview не добавлено. Размер/границы/порядок текущих chunks не менялись; исследование22 записано ДО formatter changes.

**Файлы21:** `internal/node/acp_answer{,_test}.go`, `internal/node/acp_runtime.go`, `internal/node/opencode_runtime.go`, минимальная metadata-поправка native fresh-answer fixture `opencode_configuration_test.go`, новый progress/final case в `opencode_runtime_e2e_test.go`; `internal/telegram/{adapter.go,adapter_test.go,delivery_test.go,http.go,format.go,format_test.go}`, `cmd/secretaryd/{telegram.go,telegram_test.go}`. Новый research fixture22 отдельно: `internal/telegram/long_message_research_test.go`.

**Точные проверки:**

- `go test ./internal/node -run '^(TestACPTurnAnswerUsesIdentityNotWords|TestACPAuthoritativeAnswerFIFOAndReplay|TestOpenCodeResumeDrainsHistoryBeforeFreshResult)$' -count=10` — PASS0.890s: mixed progress/final,80 FIFO progress chunks, final deltas, explicit summary, missing identity/unverified adapter failure, two attempts/Resume replay, thought/user/child exclusion.
- `go test ./internal/telegram -run '^(TestTelegram|TestLongMessage|TestLongWorker)' -count=1 -v` — PASS0.874s: layouts/links/HTML escaping/unsafe schemes, parse rejection → fallback503 → restart/retry/dedup обеих destinations, no immediate non-format fallback, balanced current chunk tags, Unicode boundaries.
- `go test ./...` — PASS; отдельно `go test ./... -count=1` — PASS без cache (node16.552s, telegram4.057s, secretary3.930s, secretaryd4.554s).
- `go test -race -p 1 ./...` — PASS (node27.982s из focused race, secretary7.053s, secretaryd7.490s, webapi36.444s); `go vet ./...`, `go build ./...`, `git diff --check` — PASS.
- `zsh scripts/sex-cli-test.sh` — все isolated HOME cases PASS; `zsh scripts/node-deployment-test.sh` — PASS. Это тестовые service commands в isolated HOME, не production mutations.

Первый промежуточный run выявил compile issues formatter (исправлены), backpressure regression при generic burst с explicit summary (native FIFO оставлен, старый generic non-draining contract восстановлен), а полный suite — старые raw-apostrophe/internal-label assertions (уточнены под HTML/user-facing title). В одном раннем combined run legacy31 `missing-mode/resume=false` не прочитал safe counts; fixture делает in-place WriteFile у startup deadline, поэтому возможна interrupted metadata write. Native configuration policy/timeouts31 не менялись. Следующие focused suites, full uncached и full race PASS; этот ранний transient не скрывается и не считается доказанно устранённым unrelated fixture race.

**Native unpaid acceptance на omarchy, actual shared store:**

```sh
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -c ./internal/node -o /private/tmp/secretary212229-node.test
scp -q /private/tmp/secretary212229-node.test omarchy:/tmp/secretary212229-node.test
ssh -o BatchMode=yes omarchy 'PATH="$HOME/.local/bin:$PATH" SECRETARY_OPENCODE_ACP_E2E=1 TEST_OPENCODE_SHARED_DATA_HOME="$HOME/.local/share" /tmp/secretary212229-node.test -test.run="^TestOpenCode(ACPProgressFinalNativeHTTPFixture|ACPNativeHTTPFixture|SecretaryMCPNativeHTTPFixture|NativeProfilePersistence|NativeInventory)$" -test.count=2 -test.v'
```

Artifact SHA256 `9d74f0b40c23b02c4460e777bafbb91978442e44f8416b996d23205372f8a17f`. Remote chosen binary `$HOME/.local/bin/opencode` —v2.0.22 (system `/usr/bin/opencode` —1.18.29, не использован). Новый native progress-before-real-read/final-after-real-tool regression PASS1.50s/1.51s: progress наблюдён в Activity, Result exact `fixture ACP completed` без progress, несколько final deltas непосредственно перед terminal. Никаких sleep в regression; parent native FIFO fence используется. Native original HTTP fixture PASS1.45s/1.48s, Secretary scoped MCP PASS1.26s/1.29s, обе roles persistence phases0/1/2 PASS4.31s/4.42s, inventory PASS1.74s/1.42s (28 models,7 reasoning levels, обе выбранные модели с observed xhigh). Нет paid calls/auth copying/reset/DB reset/production Worker или Conversation mutations.

**Оставшиеся gates:** fixture HTTP — НЕ real Telegram acceptance; полный General→Topic→tools→Result→Follow-up и release/production gate31 не выполнены здесь. Parent strict authenticated marker/history65/98-byte equality tests и definitions-before-format сохранены без изменений; новых paid calls не было. Default/model/policy/runtime31 и другие dirty tickets не перезаписывались.

**Native named tool cards НЕ исправлены:** в `translate.ts` native `session.tool.input.started` знает `event.data.name`; `tool.ts:pendingToolCall/runningToolUpdate` принимает `input.toolName`, но ACP frame публикует только id/title/kind/rawInput/locations/status. `completedToolUpdate` также не публикует имя. Возможный путь — согласованное vendor `_meta` с explicit toolName на той же ACP-связи, извлечённое upstream из этого authoritative field; сейчас оно не отправляется. Требуется отдельное approval/подтверждение, никакого нового transport или upstream patch не добавлено. Named-tool product gate остаётся blocker; name из title/kind/arguments не угадывается.

Финальный дополнительный `TestTelegramHTMLExpansionKeepsParsedSizeWithinLimit` PASS:4096 parsed UTF-16 units могут занимать20480 HTML wire chars; readable fallback lossless. После него заново выполнены `go test ./...`, `go test -race -p 1 ./...`, vet/build/diff-check — PASS (telegram2.613s/4.071s; остальные unchanged packages cached). SHA256-проверка49 исходных unrelated dirty files подтверждает `unchanged=true`, включая31 authenticated acceptance tests с exact65/98 bytes, defaults, FIFO transport и другие tickets. В четырёх намеренно затронутых native runtime/fixture файлах31 сохранены исходные managed-mode/model/replay assertions; новые changes ограничены21.

Статус `claimed` сохранён. Локальные21 changes и report22 готовы к независимому review, не к rollout; originating echo29 остаётся конкретным response-contract blocker (см. его comments).

### Follow-up: убрать cross-channel ordering flake из ACP drain regression

Независимый Spec review сообщил, что прежний `TestACPRuntimeAddressedReplyDrainsOutputBeforeResult` флапал: в combined/`-count=20` потребитель видел `progress=69`/`66`, потому что FIFO fence подтверждает обработку notification-ов ACP watcher-ом, но `Activity` и `Result` — отдельные буферизованные публичные каналы; Go `select` не обещает порядок чтения между ними. Это не доказательство потери Activity и не основание вводить runtime guarantee «все Activity потреблены до Result».

- В `internal/node/acp_answer_test.go` test переименован в `TestACPRuntimeDrainKeepsProgressAvailableAndTurnScoped`. Production sync и `internal/acp` transport не менялись. Тест использует только `Session.Activity()`, `Session.Result()`, `Session.Prompt()` и `StartRequest.DrainOutputBeforeResult`; не читает private queue len, не спит и не повторяет до случайного PASS.
- Fixture выдаёт в двух разных turns по 80 progress events, tool boundary и по две final deltas. Первый Result — exact explicit summary без progress; второй — exact сгруппированные final deltas без данных первого turn. Тест принимает оба межканальных порядка и подтверждает, что все progress/final Activity остаются доступны через публичный stream. Существующие replay tests не менялись.
- `go test ./internal/node -run '^TestACPRuntimeDrainKeepsProgressAvailableAndTurnScoped$' -count=20` — PASS (0.400s). `go test -race -p 1 ./internal/node ./internal/acp ./internal/core ./internal/ctl ./internal/webapi -count=2` — PASS (59.647s/1.361s/41.682s/13.741s/36.245s).
- Один финальный `./scripts/phase4-release-gate.sh` после patches завершился **FAIL**, run `p4-ticket15-domain-drain-459709c-20261005T105601Z`. На Go test stage `TestTwoSecretaryNodeProcessesPairInventoryAndReconnect` timeout-нулся через 30.03s на ожидании authenticated reconnect; все остальные перечисленные пакеты Go stage прошли. Отдельный точный запуск этого process test прошёл один раз за 21.38s, но это не отменяет gate failure и не гарантирует устранение reconnect timing failure. Gate не запускался повторно; после раннего failure остальные gate stages не выполнились.
- Manual Telegram General → Topic → tools → Result → Follow-up по-прежнему не принят; Ticket 21 остаётся `claimed`. Production/auth/paid calls не выполнялись.

### Compatibility fix после blocking parent review

**Принято родителем:** strict message grouping и missing-native-metadata failure включаются только explicit `TerminalMessageGrouping` OpenCode. Non-opt-in fx/default/прочие ACP сохраняют прежний успешный legacy terminal report без нового обязательного summary/phase contract. Legacy assembly не называется authoritative final answer. Parent прямо отклонил universal fail-closed из первой версии как regression существующих fx Workers; это **исправление бага**, а не принятие нового product tradeoff.

**Изменено только в этом follow-up:**

- `internal/node/acp_runtime.go`: отдельный legacy collector складывает assistant/operational text в wire order, не классифицирует messageId и не выбирает «последний final» для fx. При отсутствующем explicit summary берётся legacy text, при пустом тексте сохранён прежний `completed`; status не меняется из-за отсутствия metadata. `cancelled/canceled` остаются `canceled`, RPC failure — `failed`; explicit summary extension остаётся приоритетным. User echoes/raw thought и session/load replay не добавляются. Native strict OpenCode collector, child exclusion и missing metadata → `failed` сохранены.
- `internal/acp/jsonl.go`: добавлен local-only `RequestWithDeferredEventDrain` поверх **того же** FIFO fence; reader/eventBarrier logic не заменены, нового wire RPC/transport нет. Legacy без summary ждёт fence до snapshot текста, чтобы не потерять preceding final deltas. Explicit summary сохраняет ранний Result при заполненном Activity buffer, а deferred drain завершается до `finishTurn`/queued prompt, чтобы старые preceding notifications не попали в следующий collector. Native `RequestDrainingEvents`/Resume replay продолжают ждать fence до возврата response.
- `internal/node/acp_answer_test.go`: `unverified-adapter` теперь ожидает утверждённую successful legacy assembly, не native fx acceptance и не guessed finality. Native/idless strict и explicit-summary cases сохранены.
- Новые `internal/node/acp_legacy_answer_test.go`, `internal/acp/deferred_barrier_test.go`, `internal/node/fx_legacy_native_test.go`.
- Только comments этого issue;22/29/31, formatter, defaults, fx profiles/bindings/history и другие dirty changes не редактировались.

**Граница доказанного:** separation progress/final21 доказано только для OpenCode v2.0.22 с explicit flag. Legacy fx terminal presentation/separation **не объявляется исправленной**: report по-прежнему может содержать progress/operational журнал. Он не регрессирует в `Terminal answer unavailable`/failed; для чистого fx final нужен отдельно approved adapter-specific phase contract. Общий Telegram safe formatter прежнего этапа не менялся. Arbitrary unscoped notifications, пришедшие уже после terminal response и после admission следующего prompt, нельзя надёжно приписать Attempt без нового native contract; тесты не выдают такую корреляцию за имеющуюся. Проверенные late deltas — notifications, preceding terminal response, но отстающие в consumer/Activity queue.

**Focused regressions:**

```sh
go test ./internal/node ./internal/acp -run '^(TestACPLegacy|TestACPAuthoritativeAnswerFIFOAndReplay|TestACPTurnAnswerUsesIdentityNotWords|TestACPRuntimeDeliversEveryActivityInBurst|TestFXRuntimeInterruptAndContinue|TestOpenCodeResumeDrainsHistoryBeforeFreshResult|TestRequest)' -count=10
go test -race -p 1 ./internal/node ./internal/acp -run '^(TestACPLegacy|TestACPAuthoritativeAnswerFIFOAndReplay|TestACPTurnAnswerUsesIdentityNotWords|TestACPRuntimeDeliversEveryActivityInBurst|TestFXRuntimeInterruptAndContinue|TestOpenCodeResumeDrainsHistoryBeforeFreshResult|TestRequest)' -count=3
```

PASS: plain node7.658s/acp0.351s; race node8.376s/acp1.396s. `TestACPLegacyFXWireShapeQueuedAndSecondAttempts`:8 combinations default ACP/FX wrapper × direct/queued second Attempt × first without summary/with explicit summary. Pinned fx0.0.8 source `types.zig` имеет32 lower-hex messageId, `prompt.zig:pushText` assistant/operational chunks с разными IDs, terminal `stopReason=end_turn` без summary. Fixture воспроизводит именно этот wire shape, включая late delta прежнего messageId; report exact успешен, все assistant/operational chunks сохранены, старый текст не попадает во вторую Attempt. Это **synthetic wire-shape regression**, не native acceptance. Reverse-request handshake удерживает первый prompt active до Queue без sleeps.128 preceding Activity events проверяют ранний explicit Result и ожидание queue boundary.14 terminal cases default/FX покрывают cancelled/canceled, empty report, explicit summary, summary+cancellation и RPC failed. Native strict missing-ID test по-прежнему ожидает failed. Старый `TestACPRuntimeDeliversEveryActivityInBurst` не ослаблялся.

**Дополнительная реальная unpaid native fx0.0.8 проверка:**

```sh
SECRETARY_FX_LEGACY_E2E=1 go test ./internal/node -run '^TestFXLegacyNativeHTTPFixture$' -count=1 -v
SECRETARY_FX_LEGACY_E2E=1 go test -race -p 1 ./internal/node -run '^TestFXLegacyNativeHTTPFixture$' -count=3 -v
```

PASS: первичный run0.21s (package0.888s); race repeats0.10s/0.09s/0.10s (package1.933s). В каждом lifecycle реальный локальный `fx --version=0.0.8`, FXRuntime без opt-in, two Attempts: `status=succeeded`, exact expected report53 bytes,2 local synthetic provider calls. Test использует exact environment, private HOME/config/cache/data/workspace, dummy fixture key и только localhost gateway HTTP; production auth/state не копировались и не использовались. No paid calls. Это native legacy delivery smoke, не proof fx separation/operational phase или real Telegram.

**Полные проверки, последовательно и без cache:**

```sh
go test -p 1 ./... -count=1
go test -race -p 1 ./... -count=1
go vet ./...
go build ./...
git diff --check
zsh scripts/sex-cli-test.sh
zsh scripts/node-deployment-test.sh
```

Все PASS. Plain: node19.471s/acp0.398s/secretary1.782s/telegram2.806s; race: node34.916s/acp1.441s/core63.695s/secretary8.000s/telegram4.202s. CLI — все isolated HOME cases PASS, Node deployment script PASS. Никаких recoverable failures в этих final serial runs; скрипты не управляли production services.

**Native OpenCode repeat после fix, actual shared store omarchy:**

```sh
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go test -c ./internal/node -o /private/tmp/secretary21-compat-review/node.test
scp -q /private/tmp/secretary21-compat-review/node.test omarchy:/tmp/secretary21-compat-node.test
ssh -o BatchMode=yes omarchy 'PATH="$HOME/.local/bin:$PATH" SECRETARY_OPENCODE_ACP_E2E=1 TEST_OPENCODE_SHARED_DATA_HOME="$HOME/.local/share" /tmp/secretary21-compat-node.test -test.run="^TestOpenCode(ACPProgressFinalNativeHTTPFixture|ACPNativeHTTPFixture|SecretaryMCPNativeHTTPFixture|NativeProfilePersistence|NativeInventory)$" -test.count=2 -test.v'
```

Artifact SHA256 `2373f146be032555d76c8ca2dee9a480cadf2727e8b67ae9a408c28dfce9ca47`. Все native cases PASS дважды: progress/final1.49s/1.50s (Activity progress сохранён, Result exact final после real tool), original HTTP1.51s/1.45s, Secretary scoped MCP1.28s/1.29s, persistence обеих roles phases0/1/2 same_session/history/managed/permissions4.42s/4.37s, inventory1.39s/1.37s. FIFO/native replay не регрессировали. Новых paid authenticated calls не было; strict65/98-byte marker/history acceptance files parent сохранены без изменений.

SHA256 сравнение baseline этого follow-up:65 остальных ранее dirty файлов неизменны, включая formatter,22/29/31, profile/defaults, existing FX runtime adapter и authenticated tests. Единственный затронутый ранее dirty transport file — описанный local deferred FIFO helper21; никаких reset/checkout/stash/commit/push/deploy/install/service changes.

**Pending gates:** parent review полного dirty delta ещё идёт. Native tool identities/named cards остаются limitation; из title/kind/arguments имена не угадываются. Full real Telegram и release/production gate31 не пройдены здесь.29 originating reply-contract blocker принят родителем как product decision, `reply_to_user`/global drop не реализованы.22 semantic splitting/file/preview остаются recommendation без owner approval.21/22/29 остаются `claimed`; этот fix не даёт permission на rollout.
