# 34 Переключить OpenCode на Codex и проверить полный пользовательский путь

Type: task
Status: claimed
Blocked by: 31, 21, 22, 29

## Work

Пользователь попросил отдельную проверку полного пути с Codex после стабилизации OpenCode. Это отложенный сравнительный прогон, не замена текущей работы ticket31 и не автоматическая постоянная смена default. Если OpenCode после ticket31 работает корректно, пользователь готов остаться на нём.

Сначала завершаются ticket31, затем tickets21/22/29 и human review. Этот тикет не запускать в параллель с ними.

## Scope

- Согласовать временное переключение Secretary и default новых Workers с OpenCode на Codex для проверки полного пути; конкретный rollout/rollback выполнить только после согласования с owner.
- Установить реальные binary/version/auth/runtime protocol Codex. Не предполагать, что официальный Codex CLI предоставляет ACP: проверить native app-server либо используемый ACP bridge и его возможности по документации и исходникам.
- Сохранять выбранные модели Secretary `gpt-6.1-sol` / `xhigh` и новых Workers `gpt-6-luna` / `xhigh`, если exact model/effort действительно поддержаны. При отсутствии — видимая ошибка и новое решение owner, без silent substitution.
- Existing fx/OpenCode Workers сохраняют свои immutable binding и history. Переключение default относится только к новым Workers, не мигрирует старые сессии.
- Title generation остаётся независимой конфигурацией, даже если использует OpenCode.
- Secretary имеет только server-owned lifecycle MCP tools; не добавлять execute/worker:write и не открывать builtin shell/read/edit.

## Acceptance

- Реальный Secretary принимает запрос из General, вызывает scoped MCP и создаёт нового Codex Worker на выбранном Node/Project.
- Worker Topic содержит исходное поручение и настоящие tool identities с безопасным preview; без guesses по progress titles, raw ACP, секретов и внутренних инструкций.
- Текст/Markdown/ссылки и длинные сообщения сохраняют принятое в tickets21/22 поведение.
- Terminal Result отдельно от progress, ровно одна canonical доставка в General и предусмотренная копия в Topic; Secretary не пересказывает его.
- Новые независимые сообщения в General принимаются во время работы Worker; Topic Follow-up адресуется тому же Worker.
- Read/tool/input/approval/cancel и terminal Result проходят сквозную проверку на настоящем runtime.
- Restart/reconnect/resume сохраняют тот же native session и контекст либо дают явную unavailable ошибку; нет silent replacement session.
- Проверены model/reasoning selection, реальный observed Node inventory, authentication и fail-closed отказ неизвестных pins.
- Записаны результаты сравнения с OpenCode, ограничения, backup/rollback и вывод owner: остаться на OpenCode либо согласованно перейти на Codex. Один успешный model call не считается full-path acceptance.

## Related

Ticket31 — текущий OpenCode default rollout. Tickets21/22/29 — оформление и доставка. Ticket20 — genuine tool identity. Ticket33 — отдельное persistent native state OpenCode, не часть переключения Codex.

## Comments

### Безопасная подготовка, 6 октября 2026

Ticket остаётся **blocked by 31, 21, 22, 29**. Все четыре issues сейчас `claimed`, не `resolved`. Acceptance этого тикета не запускалась: default/harness/service не менялись, bridge не устанавливался, авторизация и session state не читались/копировались, production не трогался; paid calls и Telegram messages не отправлялись. Title generation и fx/OpenCode bindings/history не затронуты.

**Codex runtime — проверено по установленному бинарнику и upstream source:**

- Read-only SSH в `omarchy`: `~/.local/bin/codex --version` → `codex-cli 0.150.1`. Совпадающий upstream tag: [`rust-v0.150.1`](https://github.com/openai/codex/releases/tag/rust-v0.150.1). В этом бинарнике `codex app-server --help` называет app-server experimental и предлагает собственный JSON-RPC stdio transport. `codex app-server generate-json-schema --out "$dir" --experimental` создал private schema в `/tmp`; schema этого же CLI содержит native `thread/start`, `thread/resume`, `turn/start`, `turn/steer`, `turn/interrupt`, tool/activity и approval/user-input notifications/requests. Upstream [`app-server README` этого tag](https://github.com/openai/codex/blob/rust-v0.150.1/codex-rs/app-server/README.md) подтверждает, что это version-specific native protocol, а не ACP. Сервер не запускался.
- В Secretary Codex сейчас проходит через ACP: `internal/node/runtime_router.go` направляет `HarnessCodex` в `ACPRuntime`, а `cmd/secretary-node/main.go` задаёт `codex-acp`. `sex` устанавливает upstream `@agentclientprotocol/codex-acp@1.10.0`. Источник [`v1.10.0`](https://github.com/agentclientprotocol/codex-acp/tree/v1.10.0) реализует ACP поверх Codex app-server; ранее прочитанный ticket `.scratch/phase-2/issues/05-research-codex-acp-steering.md` фиксирует его `session/cancel` и negotiated `_session/steering` mapping на native interrupt/steer. Подменить ожидаемый ACP executable на `codex app-server` нельзя: это другой wire protocol и потребовало бы отдельного Node runtime/adapter.
- На текущем интерактивном `omarchy` PATH `codex-acp` не найден, global npm package отсутствует. Это не доказывает отсутствие отдельной service PATH/configuration: их не инспектировали. До дальнейших действий нужен точный bridge path/version именно того Node, который будет выбран для private acceptance.
- Текущая setup pin `codex-acp@1.10.0` старше upstream `v1.12.0`, релиз которого отдельно добавил tool names в ACP tool-call events ([release](https://github.com/agentclientprotocol/codex-acp/releases/tag/v1.12.0), [source change](https://github.com/agentclientprotocol/codex-acp/commit/e46df48fe7e54f2a4073cb11f9e24f1a223fc9e6)). Поэтому named-tool acceptance нельзя выводить из старого pinned bridge. Upgrade не выполнялся; потребуется version-pinned private verification и отдельное разрешение, если понадобится менять setup pin.

**Model/effort evidence — только фактически выданный каталог, без подмен:**

На `omarchy` `codex debug models` показал `gpt-5.6-luna` с `xhigh`, но не показал ни точный `gpt-6.1-sol`, ни `gpt-6-luna`. Это означает, что оба выбранных pins сейчас не подтверждены на целевом Codex Node; `gpt-5.6-luna` и любые похожие ID не являются заменой. Локальная машина отдельно имеет Codex `0.155.1` и показывает `gpt-6-sol`/`gpt-6-luna`, но это другая машина/версия, а `gpt-6-sol` не равен `gpt-6.1-sol`; эти данные не разрешают dispatch на `omarchy`.

Безопасные повторные preflight-команды (не делают login и не вызывают модель):

```sh
ssh -o BatchMode=yes omarchy 'PATH="$HOME/.local/bin:$PATH" codex --version'
ssh -o BatchMode=yes omarchy 'PATH="$HOME/.local/bin:$PATH" codex app-server --help'
ssh -o BatchMode=yes omarchy 'PATH="$HOME/.local/bin:$PATH" codex debug models' \
  | jq -c '[.models[] | select(.slug=="gpt-6.1-sol" or .slug=="gpt-6-luna") | {slug, supported_reasoning_levels}]'
ssh -o BatchMode=yes omarchy 'bash -lc "command -v codex-acp || true"'
ssh -o BatchMode=yes omarchy 'bash -lc "npm ls -g --depth=0 @agentclientprotocol/codex-acp 2>/dev/null || true"'
```

Пустой список exact models — стоп и вопрос владельцу, не попытка с другим pin. До будущего запуска нужно решить: ждать target Codex catalog, который наблюдаемо содержит обе выбранные модели и `xhigh`, либо явно утвердить отдельные Codex-specific model pins. Также нужно утвердить bridge version/tool-name contract, owner-provisioned login в отдельном store и оплату минимального live acceptance; credential copy/import не допускается.

**План после снятия blockers:**

1. Повторить preflight выше на выбранном Node; сохранить только version, bridge version, observed model IDs/efforts и safe capability names. При необходимости сгенерировать app-server schema в `mktemp -d` с `chmod 700`; не читать auth/session data.
2. На отдельном private Secretary/Node state с owner-provisioned auth и отдельном временном Project workspace выполнить одинаковые OpenCode/Codex сценарии через General → lifecycle MCP → новый Worker → Topic → terminal Result. Fixture: private read-only файл со случайным marker, которого нет в исходном поручении; ожидаются exact marker, explicit tool identity, исходный Worker intent и один canonical Result без echo. Затем проверить Follow-up того же Worker, новое независимое General message, Cancel/Approval/input, restart/reconnect и unknown model fail-closed. Сравнение допустимо только при exact observed model/effort pins. Telegram full-path требует owner-provisioned private bot/chat; API fixtures сами по себе Telegram PASS не дают.
3. В repo сейчас нет одного opt-in private runner, который выполняет весь этот Secretary/General/Topic сценарий на настоящем Codex. Существующие `go test` seams проверяют routing/inventory/ACP совместимость, а не live General-to-Topic acceptance. Добавлять или запускать такой runner и оплачиваемые calls до разрешения blockers не стал.

Безопасный откат для будущего сравнения: запускать только в отдельном test data-dir/config/workspace с сохранённым private snapshot; при сбое остановить только эти тестовые процессы и восстановить только snapshot. Не менять global defaults/services, не перепривязывать Workers, не удалять native session IDs и не чистить production state. Если отдельно будет одобрен production rollout, потребуется отдельный backup/rollback approval; этот prep его не выполнял.

Локальные focused проверки существующих public seams — PASS (синтетические tests, не full path):

```sh
go test ./internal/node -run '^(TestSharedHarnessCompatibilitySuite|TestRuntimeRouterUsesImmutableHarnessBindingAndPins|TestRuntimeRouterUsesProfileHarness|TestRuntimeRouterFailsExplicitlyWhenHarnessUnavailable|TestCodexSuccessfulEmptyAuthStatusIsAuthenticated|TestCodexAuthMethodIsObserved)$' -count=1
go test ./internal/core -run '^(TestObservedInventoryRejectsMissingModelAndReasoningPins|TestWorkerDefaultModelMustBeObservedWithoutFallback|TestDispatchResolverHonorsExplicitClaudeInstanceAndObservedPins)$' -count=1
```

Результат: `internal/node` PASS 0.469s, `internal/core` PASS 0.284s. Полный batch suite не запускался. **Не объявлять Codex full-path PASS или dependencies resolved.**
