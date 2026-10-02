# 18 Configure the Worker Topic title model

Type: task
Status: resolved

## Work

Add separate configuration for the model and reasoning level used to generate Worker Topic titles. Proposed keys are `title_model` and `title_model_reasoning`. The intended defaults are `gpt-6-luna` and no or minimal reasoning, subject to the provider's supported values.

This setting applies only to Topic-title generation. It must not silently change the Secretary runtime or Worker harness configuration. Document the keys, defaults and accepted reasoning values in the config reference and example config.

## Acceptance

- Configuration can select the title model and its reasoning level independently.
- Defaults use `gpt-6-luna` with no or minimal reasoning.
- Invalid or unsupported values produce a clear configuration error or a documented safe fallback.
- Existing configuration files without these keys continue to load.
- Tests cover defaults, explicit values, validation and backwards compatibility.

## Answer

Добавлена независимая секция `[telegram]` с ключами `title_model` и `title_model_reasoning`. Defaults: `gpt-6-luna` и `minimal`. Ключи входят в `config.Config.Telegram`, JSON snapshot и `config.Diff` под ключом `telegram`.

Загрузчик применяет defaults только к отсутствующим ключам. Явно пустые, пробельные и невалидные model IDs отклоняются с `config.ErrInvalid` и названием ключа. Допустимые reasoning значения: `none`, `minimal`, `low`, `medium`, `high`, `xhigh`; неизвестные значения отклоняются. Невалидный reload не заменяет активный snapshot.

Настройки не меняют Secretary/Worker policy, legacy runtime/model aliases и compiled Profile hashes. Реальный вызов модели не добавлялся: генерация, provider-specific проверка model/effort, timeout и детерминированный fallback остаются в тикете 17. Доступность `gpt-6-luna` у реального provider не проверялась.

Изменены `internal/config/config.go` и поставляемый `internal/config/defaults/config.toml`. Добавлены `internal/config/topic_title_test.go`, справочник `docs/configuration.md` и пример в `docs/quickstart.md`.

Tests покрывают defaults, partial/explicit settings, legacy config без новой секции, accepted/invalid reasoning, invalid model IDs, JSON round-trip, независимость profiles, version/diff, запись корректного reload и сохранение snapshot после ошибки.

Проверки:

- `mise exec -- go test ./internal/config -run TopicTitle -count=1`: PASS.
- `mise exec -- go test ./internal/config -count=1`: PASS.
- `mise exec -- go test ./internal/config -run TopicTitle -count=5`: PASS.
- `mise exec -- go test ./...`: PASS.
- `mise exec -- go test -race ./...`: PASS.
- `mise exec -- go vet ./...`: PASS.
- `mise exec -- go build ./cmd/...`: PASS.
- `git diff --check`: PASS.

Существующие configs, credentials и Worker records не изменялись. `omarchy` не обновлялся и не перезапускался. Конфигурация публикуется вместе с генератором из тикета 17.
