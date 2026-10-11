# Cron Jobs и Heartbeat Watchdog в Secretary v2

## Цель

Реализовать в `secretaryd` подсистему регламентных задач по расписанию (**Cron Jobs**) и периодического автономного контроля состояния воркеров и очередей (**Heartbeat Watchdog**), сохраняя устойчивость к сбоям, детерминированность проверок и защиту от неконтролируемого расхода LLM-токенов.

---

## Архитектурные компоненты

### 1. Модель данных (SQLite миграции в `internal/migration`)

1. **`scheduled_jobs`** (определение расписания):
   - `id` (TEXT PRIMARY KEY) — UUID задания.
   - `title` (TEXT NOT NULL) — человекочитаемое название.
   - `description` (TEXT) — описание назначения.
   - `job_type` (TEXT NOT NULL) — `cron` (календарное правило) или `interval` (относительный период).
   - `schedule_expr` (TEXT NOT NULL) — cron-выражение (например, `0 9 * * 1-5`) или строка интервала (`every 30m`).
   - `timezone` (TEXT NOT NULL DEFAULT 'UTC') — IANA timezone (например, `Europe/Moscow`, `Asia/Krasnoyarsk`).
   - `action_type` (TEXT NOT NULL) — `task_prompt` (создание `Task` с промптом для воркера) или `action` (детерминированная системная проверка / уведомление без LLM).
   - `target_config_json` (TEXT NOT NULL) — параметры целевого исполнения: воркер-профиль, project/workspace, model override, routing (канал доставки, telegram topic id).
   - `missed_run_policy` (TEXT NOT NULL DEFAULT 'run_once_now') — `skip`, `run_once_now`, `catch_up_all`.
   - `overlap_policy` (TEXT NOT NULL DEFAULT 'skip') — поведение при еще активном предыдущем запуске: `skip`, `queue`, `cancel_previous`.
   - `enabled` (INTEGER NOT NULL DEFAULT 1) — флаг активности.
   - `next_run_at` (INTEGER) — вычисленный следующий запуск в миллисекундах UTC.
   - `last_run_at` (INTEGER) — время предыдущего запуска в миллисекундах UTC.
   - `created_at`, `updated_at` (INTEGER NOT NULL).

2. **`scheduled_job_runs`** (журнал срабатываний и идемпотентность):
   - `id` (TEXT PRIMARY KEY) — UUID запуска.
   - `job_id` (TEXT NOT NULL REFERENCES `scheduled_jobs(id)`).
   - `scheduled_for` (INTEGER NOT NULL) — плановое время срабатывания UTC.
   - `started_at` (INTEGER), `finished_at` (INTEGER).
   - `status` (TEXT NOT NULL) — `pending`, `running`, `succeeded`, `failed`, `skipped`.
   - `task_id` (TEXT) — ссылка на созданный `Task` (если применимо).
   - `error_summary` (TEXT) — детали ошибки при сбое.
   - *Уникальный индекс:* `UNIQUE(job_id, scheduled_for)` для предотвращения двойных запусков при рестартах.

3. **`heartbeat_ticks`** (аудит тиков системного watchdog):
   - `id` (TEXT PRIMARY KEY), `tick_at` (INTEGER NOT NULL).
   - `duration_ms` (INTEGER NOT NULL).
   - `checked_attempts_count` (INTEGER NOT NULL).
   - `stalled_detected_count` (INTEGER NOT NULL).
   - `anomalies_json` (TEXT) — обнаруженные аномалии (зависшие задачи, недоступные ноды, застрявшие очереди).
   - *Ротация:* автоматическая очистка записей старше 7 дней.

4. **`scheduler_leases`** (координация инстансов):
   - `lease_key` (TEXT PRIMARY KEY) — например, `scheduler_master`.
   - `holder_id` (TEXT NOT NULL), `acquired_at` (INTEGER NOT NULL), `expires_at` (INTEGER NOT NULL).

---

## Семантика исполнения

### Планировщик в `secretaryd` (`internal/ctl`)
- **Единый Event-Loop:** планировщик просыпается по минимальному времени среди активных `next_run_at` и периода Heartbeat.
- **Транзакционный захват:** выборка и резервирование запуска происходят в короткой SQLite-транзакции с обновлением `next_run_at` и созданием `scheduled_job_runs`.
- **Асинхронный запуск:** генерация `Task` и вызов `WorkerService.Dispatch` производятся вне блокировок базы данных.

### Heartbeat Watchdog
- Каждые $N$ секунд (настраивается в конфиге, default: 30–60с) выполняется детерминированная проверка:
  1. **Свежесть Node Heartbeats:** проверка времени последнего ping от подключенных `secretary-node`.
  2. **Мониторинг активных Attempts:** поиск попыток без новых событий дольше допустимого порога (`inactivity_timeout`). Перевод в `suspected_stalled` и опрос узла.
  3. **Контроль очередей сообщений:** выявление застрявших сообщений в `WorkerQueue`.
  4. **Штатный режим:** завершается без вызова LLM и без расхода токенов.

---

## Архитектурные развилки (для детальной проработки)

1. **Типы джоб (Q1):** поддержка как LLM `task_prompt`, так и детерминированных действий `action` (статические алерты, системные проверки).
2. **Роутинг плановой работы (Q2):** создание изолированного `Task` по умолчанию против опционального `Follow-up` в активный контекст проекта.
3. **Политика пропущенных запусков (Q3):** дифференцированная обработка (календарные задачи `run_once_now`, частые проверки `skip`).
4. **Семантика Heartbeat (Q4):** чисто детерминированный мониторинг Go-кодом без прямого обращения к LLM.
5. **Реакция на зависший Attempt (Q5):** алерт оператору и безопасная отмена `Cancel`, без автоматического retry при статусе `unknown`.
6. **Интерфейс управления (Q6):** CLI-команды `secretaryctl` и конфигурация `config.yaml` в MVP, расширение до Control Room UI и команд ассистента.
7. **Таймзоны (Q7):** хранение планового времени в UTC с явным указанием IANA-зоны в описании джобы.
8. **Масштабирование (Q8):** поддержка одного ведущего планировщика с защитой через SQLite lease.

---

## Критерии приёмки (Acceptance Criteria)

- [ ] Создана миграция SQLite для таблиц `scheduled_jobs`, `scheduled_job_runs`, `heartbeat_ticks`, `scheduler_leases`.
- [ ] Реализован фоновый планировщик в `secretaryd` с парсингом cron-выражений и интервалов, расчетом `next_run_at` с учетом IANA timezone.
- [ ] Обеспечена идемпотентность запусков: уникальный индекс `(job_id, scheduled_for)` исключает дублирование при перезапуске сервера.
- [ ] Реализована обработка простоя: после выключения и рестарта сервер корректно обрабатывает пропущенные запуски согласно `missed_run_policy`.
- [ ] Heartbeat Watchdog выполняет регулярную детерминированную проверку доступности нод и активных `Attempt` без вызова LLM.
- [ ] При обнаружении зависшего `Attempt` формируется диагностическое событие и уведомление; отсутствуют слепые повторные запуски при недоступности ноды.
- [ ] Результаты выполнения регламентных задач доставляются в указанный канал (Telegram с заданным topic_id, Web UI) через стандартный контракт доставки.
- [ ] Все тесты пакетов `internal/ctl`, `internal/migration`, `cmd/secretaryd` проходят успешно (`go test ./...`).
