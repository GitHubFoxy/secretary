# Issue 01: Сохранить и показать исходную причину ошибки Worker

Type: task
Status: ready-for-agent
Blocked by: None

## Краткое описание

При прямом взаимодействии с Worker (как через idle follow-up, так и через active steering) реальная причина ошибки провайдера (исчерпание квоты, сбой аутентификации, таймаут) теряется и заменяется неинформативным сообщением:
```text
acp rpc -32603: Internal error
```

Задача состоит в том, чтобы:
1. Сохранить полезную нагрузку ошибки (`error.data`) на границе протокола ACP (`internal/acp/jsonl.go`).
2. Классифицировать сбой в рантайме Worker (`internal/node/acp_runtime.go`), извлекая категорию ошибки, объяснение провайдера и технические параметры.
3. Обеспечить сквозную доставку метаданных сбоя через сетевой протокол нод:
   - Для **Path A (Idle follow-up terminal)**: сформировать понятный `Result.Summary` и заполненные поля `FailureCode`, `ErrorCode`, `Diagnostics` в outcome и Result read model. `ErrorCode`/`Diagnostics` принадлежат AttemptOutcome; не добавлять вымышленные существующие колонки в `phase4_results`.
   - Для **Path B (Active steering transport error)**: сохранить типизированные поля в `CommandOutcome`, предотвратить их стирание функцией `ServerManager.SendCommandAndWait`, вернуть структурированный JSON через `writeClientMutationError`, не сбрасывая черновик ввода пользователя в Web UI и отправляя сервисное сообщение в Telegram топик.
4. Применить политику санитайзинга (Redaction): безопасный allowlist, лимит длины сообщения не более 1000 рун, маскирование токенов и секретов.
5. Обеспечить надёжный fallback: использовать текст активности ассистента только при наличии верифицированного признака сбоя провайдера (не подменять произвольный текст модели на ошибку).

---

## Исследование и первопричина (Root Cause Analysis)

### 1. Верифицированный инцидент в Production
11 октября 2026 г. в 16:24:33 +07 (09:24:33 UTC) на хосте `omarchy` произошёл сбой выполнения Worker:
- **Worker reference:** `wrk_cc7c4e4ec94db7859822326ba7690a0e` (ID: `wrk_e711cf078eee3a22499ced1c402e4300`).
- **Turn ID:** `trn_a731f628454db7433188f37d89747805`.
- **Attempt ID:** `att_26931171d9894a7dd7fc81060ff05622` (Turn 2, Attempt 1).
- **Result ID:** `res_cee355ff10419cbf0263bff2a9ea6997`.
- **Хост исполнения:** `omarchy` (co-located Node).
- **Нативная сессия Codex:** `/home/coder/.codex/sessions/2026/10/11/rollout-2026-10-11T15-18-46-01a12a0b-023a-7251-aa36-4cf6452b61c4.jsonl`.
- **Событие `task_complete` (строка 25):**
  ```json
  {
    "type": "task_complete",
    "root_turn_id": "01a12a47-2945-7053-be25-f599b729f549",
    "turn_id": "01a12a47-2945-7053-be25-f599b729f549",
    "error": {
      "message": "You’ve hit your usage limit. Upgrade to Pro (https://chatgpt.com/explore/pro), visit https://chatgpt.com/settings/usage to purchase more credits or try again at 4:37 PM.",
      "codex_error_info": "usage_limit_exceeded"
    }
  }
  ```

### 2. Цепочка потери информации (Point of Failure)
1. **Адаптер `@agentclientprotocol/codex-acp@1.12.0`**:
   При получении ошибки `usage_limit_exceeded` формирует JSON-RPC 2.0 ответ:
   ```json
   {
     "jsonrpc": "2.0",
     "id": 4,
     "error": {
       "code": -32603,
       "message": "Internal error",
       "data": {
         "message": "You’ve hit your usage limit. Upgrade to Pro (https://chatgpt.com/explore/pro), visit https://chatgpt.com/settings/usage to purchase more credits or try again at 4:37 PM.",
         "codexErrorInfo": "usage_limit_exceeded"
       }
     }
   }
   ```
2. **Клиент ACP в Secretary (`internal/acp/jsonl.go:43`)**:
   Структура `RPCError` объявлена как:
   ```go
   type RPCError struct {
       Code    int    `json:"code"`
       Message string `json:"message"`
   }
   ```
   Поле `data` игнорируется при десериализации. Вся полезная нагрузка провайдера отбрасывается.
3. **Рантайм ноды (`internal/node/acp_runtime.go:1443`)**:
   В методе `promptTurn` при `err != nil`:
   ```go
   s.result <- Result{Status: "failed", Summary: err.Error()}
   ```
   В `Summary` записывается сухой текст `acp rpc -32603: Internal error`. Накопленный в стриминге активности текст ассистента (`turnAnswer` / `legacyTurnText`), содержащий пояснение от провайдера, сбрасывается без использования.
4. **Сетевой транспорт между нодой и сервером**:
   - В `internal/node/execution_node.go:683`: в `AttemptOutcomeEnvelope` переносится `Summary: result.Summary`, а `ErrorCode: result.Status` (т.е. строка `"failed"` вместо кода ошибки). Поле `FailureCode` остаётся пустым.
   - В `internal/node/execution_node.go:378`: при ошибке `Steer` создаётся `CommandOutcome{State: CommandFailed, ErrorCode: code, ErrorMessage: err.Error()}`. По сети WebSocket через `SendCommandOutcome` поля приходят на сервер, но в `internal/node/server_manager.go:334`:
     ```go
     return fmt.Errorf("node command %s: %s", outcome.ErrorCode, outcome.ErrorMessage)
     ```
     Функция схлопывает структурированные поля в плоскую строку `error`, стирая типизацию. Go-оператор `%w` не передаёт типизированные структуры между хостами.
5. **Интерфейсы Web API и Telegram**:
   - В `internal/webapi/observer.go:247`: обработчик `steer` при получении ошибки вызывает `http.Error(w, "steer worker", http.StatusBadGateway)`.
   - В `web/src/App.svelte`: при ошибке запроса очищается черновик в поле ввода `steerText`.
   - В Telegram: пользователь видит сухой сбой или остаётся без ответа.

---

## Архитектурные решения и контракты

### 1. Сохранение `Data` в `acp.RPCError`
В `internal/acp/jsonl.go`:
```go
type RPCError struct {
    Code    int             `json:"code"`
    Message string          `json:"message"`
    Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) HasData() bool {
    return len(e.Data) > 0 && string(e.Data) != "null"
}
```

### 2. Типизация сбоев (`WorkerFailure`)
В `internal/core/phase4_types.go`:
```go
type FailureCategory string

const (
    FailureCategoryUsageLimit     FailureCategory = "usage_limit"
    FailureCategoryAuthRequired   FailureCategory = "auth_required"
    FailureCategoryBillingCredits FailureCategory = "billing_credits"
    FailureCategoryRateLimit      FailureCategory = "rate_limit"
    FailureCategoryInternalError  FailureCategory = "internal_error"
)

type WorkerFailure struct {
    Category     FailureCategory `json:"category"`
    ProviderCode string          `json:"provider_code,omitempty"`
    Message      string          `json:"message"`
    Technical    string          `json:"technical,omitempty"`
}
```

### 3. Классификация ошибок провайдера
В `internal/node/acp_runtime.go` функция `classifyACPError(rpcErr *acp.RPCError, collectedText string) WorkerFailure`:
- Проверяет `rpcErr.Data`:
  - Если присутствует `codexErrorInfo == "usage_limit_exceeded"` или реально полученное wire-поле `codexErrorInfo == "usageLimitExceeded"`:
    - Категория: `usage_limit`.
    - Сообщение: извлечённый текст `message` провайдера, очищенный от служебных ссылок и ограниченный 1000 рун.
    - `ProviderCode = "usage_limit_exceeded"`.
  - Если присутствует признак аутентификации (`auth_required`, `invalid_api_key`, `token_expired`):
    - Категория: `auth_required`.
  - Если присутствует признак нехватки кредитов без временного сброса (`insufficient_quota`, `purchase more credits` без даты сброса):
    - Категория: `billing_credits`.
- Если в `Data` структурированные поля отсутствуют, но в `collectedText` (накопленном тексте стриминга активности) зафиксировано верифицированное системное сообщение провайдера (например, префикс `"You’ve hit your usage limit"`):
  - Текст используется только для объяснения пользователю. Префикс в assistantText сам по себе не доказывает происхождение от провайдера: без typed provider signal категория остаётся `internal_error`, планировщик не запускается.
- Произвольный текст ассистента без маркеров ошибок провайдера **не** трактуется как сбой.
- Если признаков нет: категория `internal_error`, техническое сообщение `acp rpc %d: %s`.

### 4. Политика очистки (Redaction & Length Bounds)
В `internal/node/tool_sanitizer.go`:
- Функция `SanitizeFailureMessage(msg string) string`:
  - Вырезает токены, API-ключи по регулярным выражениям (`sk-[a-zA-Z0-9]+`, `bearer [a-zA-Z0-9_\-\.]+`, `xoxb-[a-zA-Z0-9]+`).
  - Ограничивает длину сообщения максимум 1000 рунами (с добавлением многоточия `...` при усечении).

### 5. Сквозная передача метаданных steering (Path B)
1. В `internal/node/protocol_types.go`:
   Расширить `CommandOutcome`:
   ```go
   type CommandOutcome struct {
       CommandID       string          `json:"command_id"`
       Kind            CommandKind     `json:"kind"`
       State           CommandState    `json:"state"`
       TurnID          string          `json:"turn_id,omitempty"`
       AttemptID       string          `json:"attempt_id,omitempty"`
       ErrorCode       string          `json:"error_code,omitempty"`
       ErrorMessage    string          `json:"error_message,omitempty"`
       FailureCategory FailureCategory `json:"failure_category,omitempty"`
       ProviderCode    string          `json:"provider_code,omitempty"`
   }
   ```
2. В `internal/node/server_manager.go`:
   Создать типизированную ошибку (в псевдокоде `FailureCategory` квалифицировать как `core.FailureCategory`):
   ```go
   type NodeCommandError struct {
       Outcome CommandOutcome
   }

   func (e *NodeCommandError) Error() string {
       if e.Outcome.ErrorMessage != "" {
           return fmt.Sprintf("node command %s: %s", e.Outcome.ErrorCode, e.Outcome.ErrorMessage)
       }
       return fmt.Sprintf("node command %s", e.Outcome.State)
   }
   ```
   В `SendCommandAndWait`: при `outcome.State != CommandAccepted` возвращать `&NodeCommandError{Outcome: outcome}` вместо потери структуры в `fmt.Errorf`.
3. В `internal/webapi/idempotency.go`:
   Обновить `writeClientMutationError(w http.ResponseWriter, err error)`:
   - Проверять наличие `*NodeCommandError`.
   - При наличии: выставлять `Content-Type: application/json` и сохранить прежний HTTP status mapping и возвращать 400 для provider failure с телом:
     ```json
     {
       "error": "Лимит использования исчерпан при отправке указания Worker",
       "category": "usage_limit",
       "provider_code": "usage_limit_exceeded",
       "technical": "acp rpc -32603: Internal error"
     }
     ```
4. В `web/src/App.svelte`:
   - При ошибке steering извлекать JSON ошибки, отображать пользователю понятное уведомление с категорией и сообщением.
   - **Не очищать** поле ввода `steerText`, сохраняя набранный пользователем текст для повторной отправки.
5. В `internal/telegram/adapter.go`:
   - При ошибке steering в топике отправлять сервисное уведомление:
     `"⚠️ Не удалось передать указание Worker: Лимит использования исчерпан (usage_limit_exceeded). Повторите попытку позже."`

---

## Обязательные уточнения сквозного контракта

- Расширение `node.Result` в `internal/node/node.go` и command outcomes — планируемое, сейчас этих typed fields нет. Terminal metadata проходит `ExecutionNode.watchSession` → Node event/outbox serialization/validation → `RecordNodeAttemptOutcome` → `FinishAttemptInput`/`AttemptOutcome` → persisted Result/read model → `internal/webapi/public_worker.go`. Явно добавить миграцию и encode/decode нового bounded failure object (nullable для старых Result); migration/reopen/public DTO regression входит в первый ticket. Существующее `FailureCode` можно переиспользовать, `Diagnostics` остаётся bounded string в outcome.
- Для remote steering failure заполнить metadata не только в ExecutionNode, но и в daemon command failure receipt, local durable outbox, command receipt persistence, ServerManager и `errors.As`. Иначе после restart/reconnect останется плоская строка. Authenticated immutable command binding и прежний idempotency status mapping сохраняются. Local Node path возвращает тот же typed failure контракт. Старые peers без optional metadata продолжают показывать текст ошибки.
- Provider-code allowlist является основанием классификации; minified имя переменной `error51` не является wire field. Неподтверждённый prefix в assistant text может дать очищенное объяснение, но не запускает retry. Историческая Claude proxy403 «quota or missing billing» неоднозначна; не считать usage limit/auth/billing без отдельного сигнала.
- Текст пользовательской ошибки и `technical` проходят одинаковый sanitizer: JSON raw Data в публичный ответ не копируется. Существующий raw log redactor не ослаблять. Ограничить Data 64 KiB перед secondary decode, nested shape — allowlisted `message/provider code` без arbitrary traversal; malformed/oversize даёт generic fallback. 1000 Unicode runes после очищения; auth header values, token-like строки и чувствительные query values удалять, включая case-insensitive Bearer и URL userinfo. Публичные фиксированные provider help URLs допустимы, неизвестные URLs не копировать в details. Не утверждать, что regex гарантированно очищает любой секрет: allowlist — первичная граница.
- Первый ticket улучшает объяснение и переносит verified category/provider code; фактическое извлечение абсолютного reset из native Codex JSONL — второй ticket. Если первый получил validated structured timestamp непосредственно в RPC data, его можно показать, но текст 4:37PM нельзя объявлять подтверждённым absolute reset.
- Web уже сохраняет `steerText` при ошибке: это гарантия, которую нужно сохранить и проверить, а не существующий дефект. Telegram delivery must remain durable/replay-safe в прежнем Worker topic; новые диагностические сообщения не создают второй Result. Path A не запускает retries до реализации второго ticket.

## Пошаговый план реализации

### Шаг 1: `internal/acp/jsonl.go`
- Добавить поле `Data json.RawMessage` в структуру `RPCError`.
- Проверять наличие `Data` непосредственно, отдельный helper для одного вызова не требуется.
- Добавить модульные тесты на сохранение `Data` при парсинге JSON-RPC в `internal/acp/jsonl_test.go`.

### Шаг 2: `internal/core/phase4_types.go` и `harness_contract.go`
- Объявить `FailureCategory` и структуру `WorkerFailure`.
- Добавить в `AttemptOutcomeEnvelope` поля:
  - `FailureCategory FailureCategory`
  - `FailureCode string`
- Добавить валидацию и сериализацию этих полей.

### Шаг 3: `internal/node/acp_runtime.go` и `tool_sanitizer.go`
- Реализовать функцию `classifyACPError(rpcErr *acp.RPCError, collectedText string) WorkerFailure`.
- Реализовать функцию `SanitizeFailureMessage(msg string) string` с безопасным allowlist и ограничением в 1000 рун.
- В методе `promptTurn` при `err != nil`:
  - Классифицировать ошибку через `classifyACPError`.
  - Заполнять `Result{Status: "failed", Summary: failure.Message, FailureCode: failure.ProviderCode, FailureCategory: failure.Category, Technical: failure.Technical}`.
- В методе `Steer` при ошибке `_session/steering`:
  - Извлекать классификацию и обогащать возвращаемую ошибку.

### Шаг 4: `internal/node/execution_node.go` и `server_manager.go`
- В `execution_node.go`:
  - В `watchSession`: при `result.Status == "failed"` заполнять `outcome.FailureCode = result.FailureCode`, `outcome.FailureCategory = result.FailureCategory`, `outcome.Summary = result.Summary`.
  - В обработчике `steer`: переносить категорию и код ошибки в возвращаемый `CommandOutcome`.
- В `server_manager.go`:
  - Объявить `NodeCommandError`.
  - В `SendCommandAndWait`: при `outcome.State != CommandAccepted` возвращать `&NodeCommandError{Outcome: outcome}`.

### Шаг 5: `internal/ctl/service.go` и `internal/ctl/worker_lifecycle.go`
- Обеспечить проброс `NodeCommandError` через вызовы `WorkerService.SteerWorker`.
- При получении сбоя в жизненном цикле логировать структурированную категорию ошибки.

### Шаг 6: `internal/webapi/idempotency.go` и `observer.go`
- Обновить `writeClientMutationError`: при `errors.As(err, &cmdErr)` сериализовать структурированный JSON с категорией и описанием ошибки.
- В `observer.go`: для endpoint steering вызывать `writeClientMutationError` вместо плоского `http.StatusBadGateway`.
- В `workerDiagnostics`: отдавать типизированный сбой `WorkerFailure` в ответе наблюдателя.

### Шаг 7: `web/src/App.svelte`
- Обновить логику отправки steering в `workerAction`:
  - Парсить JSON-ответ ошибки.
  - Сохранять значение `steerText` в поле ввода при сбое.
  - Выводить информативный бейдж ошибки с категорией и возможностью раскрыть технические детали.

### Шаг 8: `internal/telegram/adapter.go`
- При обработке сообщений для топика: при ошибке steering перехватывать `NodeCommandError` и отправлять понятное сервисное сообщение в топик без падения поллинга.

---

## Критерии приёмки (Acceptance Checklist)

### Исследовательские критерии (Research Acceptance)
- [x] Факт инцидента от 11 октября 2026 г. подтверждён первичными записями `phase4_results` (`res_cee355ff...`) и нативного транскрипта сессии Codex (`01a12a0b...` строка 25).
- [x] Выявлена точная цепочка потери данных: потеря `error.data` в `acp.RPCError`, сброс `assistantText` в `acp_runtime.go:1443`, схлопывание `CommandOutcome` в плоскую строку ошибки в `server_manager.go:334`, текстовый fallback в Web API.
- [x] Проверены границы безопасности: запрет утечки секретов и токенов, ограничение длины сообщений 1000 рунами, запрет трактовки произвольного текста ассистента как ошибки провайдера.

### Критерии реализации (Implementation Checklist — для исполнителя)
- [ ] `acp.RPCError` сохраняет `Data json.RawMessage` при десериализации JSON-RPC 2.0 ответов.
- [ ] В `acp_runtime.go` при ошибке `session/prompt` извлекаются `codexErrorInfo` и `message` из `data` либо безопасный fallback из стриминга активности.
- [ ] В `phase4_results` для инцидентов с лимитом записывается понятный `Summary`, `FailureCategory = "usage_limit"` и `FailureCode = "usage_limit_exceeded"`.
- [ ] `CommandOutcome` сохраняет `FailureCategory` и `ProviderCode` при ошибке steering, а `ServerManager.SendCommandAndWait` возвращает типизированный `NodeCommandError`.
- [ ] Ошибки active steering возвращают структурированный JSON в Web API (`writeClientMutationError`), а черновик ввода в Web UI не стирается.
- [ ] Произвольные ошибки `-32603`, HTTP 403/429 без явных typed признаков не доказывают auth/usage_limit: сохранять объяснение и консервативную категорию. `auth_required`/`rate_limit` требуют подтверждённых provider codes.
- [ ] Все сообщения об ошибках маскируют секреты согласно политике sanitizing и ограничены 1000 рунами.
- [ ] Web observer и Telegram topic отображают причину сбоя и время восстановления (если предоставлено провайдером).
- [ ] Все запланированные тесты регрессии успешно выполняются.

---

## План тестирования и запланированные тесты регрессии

> **Примечание о статусе тестирования:**  
> В рамках исследовательского раунда live turns и модификации кода не запускались. Ниже перечислены существующие проверенные швы и конкретные тесты, которые исполнитель обязан реализовать в рамках задачи.

### 1. Запланированные модульные и интеграционные тесты для реализации:
1. **Тест сохранения `data` в `acp.RPCError`** (`internal/acp/jsonl_test.go`):
   - `TestRPCErrorPreservesData`: проверка, что при парсинге JSON-RPC ответа с полем `data` (объект или массив) оно не зануляется и доступно через `e.Data`.
2. **Тест классификации ошибок ACP** (`internal/node/acp_runtime_test.go`):
   - `TestClassifyACPError_UsageLimit`: проверка извлечения категории `usage_limit` и `codexErrorInfo` из `RPCError.Data`.
   - `TestClassifyACPError_FallbackFromActivity`: проверка извлечения причины из стриминга активности при пустом `Data`, если в стриминге есть верифицированный маркер провайдера.
   - `TestClassifyACPError_NoFalsePositive`: проверка, что обычный текст ответа ассистента без маркеров ошибки провайдера классифицируется как `internal_error`, а не `usage_limit`.
3. **Тест санитизации и ограничения длины** (`internal/node/tool_sanitizer_test.go`):
   - `TestSanitizeFailureMessage_Redaction`: проверка вырезания токенов `sk-`, `bearer` и ограничения длины 1000 рунами.
4. **Тест сохранения структуры ошибки steering через сетевой шлюз** (`internal/node/server_manager_test.go`):
   - `TestSendCommandAndWait_PreservesNodeCommandError`: проверка, что при отказе ноды `SendCommandAndWait` возвращает `*NodeCommandError` с заполненными полями категории и провайдера.
5. **Тест структурированного ответа Web API** (`internal/webapi/observer_test.go`):
   - `TestWorkerSteerError_ReturnsStructuredJSON`: проверка, что при сбое steering клиенту отдаётся HTTP 400/422 с JSON-телом, содержащим поля `error`, `category`, `provider_code`.
6. **Тест сохранения черновика в интерфейсе** (`web/src/App.test.ts` или E2E):
   - Проверка, что при получении ошибки мутации поле `steerText` не обнуляется.

### 2. Команды запуска тестов для исполнителя:
```sh
# Проверка ACP клиента
go test ./internal/acp -run 'TestRPCError' -v -count=1

# Проверка классификации ошибок и рантайма ноды
go test ./internal/node -run 'Test(ClassifyACPError|SanitizeFailureMessage|SendCommandAndWait)' -v -count=1

# Проверка Web API мутаций
go test ./internal/webapi -run 'TestWorkerSteerError' -v -count=1

# Сквозной прогон всех затронутых пакетов
go test ./internal/acp ./internal/node ./internal/ctl ./internal/webapi -count=1
```

---

## Comments

- **2026-10-11 (Maintainer triage)**: Пользователь сообщил `acp rpc -32603: Internal error` при прямом сообщении Worker. Зафиксирована потеря диагностических данных.
- **2026-10-11 (Gemini Verification)**: Проведено расследование инцидента на хосте `omarchy`. Подтверждён сбой `usage_limit_exceeded` в сессии `01a12a0b-023a-7251-aa36-4cf6452b61c4` строка 25. Установлена цепочка подавления ошибки в `internal/acp/jsonl.go` и `internal/node/acp_runtime.go`.
- **2026-10-11 (Round 2 Triage Closure)**: Устранены замечания родительского агента:
  1. Специфицирован сквозной путь метаданных для Path B (Active steering): устранена потеря типизации в `ServerManager.SendCommandAndWait`, добавлен `NodeCommandError` и структурированный JSON в `writeClientMutationError`.
  2. Зафиксирована политика sanitizing: безопасный allowlist, лимит 1000 рун, fallback на стриминг только при наличии верифицированных признаков провайдера.
  3. Чётко разграничены существующие проверенные швы и список новых запланированных тестов.
  4. Задача переведена в статус `ready-for-agent`.
