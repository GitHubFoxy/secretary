# Читаемая Worker activity

Worktree: `/Users/beruseruko/projects/secretary-p5-worker-activity`, ветка `p5/worker-activity-readable`, base `43cd444`.

Presentation commit `7adc71e` группирует соседние assistant text deltas только по совпадающим Worker/Attempt/turn/channel/kind. Tool/status/input и скрытые thinking события остаются границами. Raw whitespace соединяется без trim. SSR regression воспроизвёл сотни карточек вместо цельного текста (490 → ожидаемые 7 первоначального fixture); финальный расширенный fixture даёт 11 отдельных читаемых блоков с полными словами, пробелами, заголовком, bold и code fence, отдельными channel/Attempt/input boundaries и без raw reasoning. Replay identity не дублирует text. 19 Web tests и production build PASS; generated `web/app.js` включён.

Второй дефект pipeline воспроизведён публично: Node normalization отбрасывал `\n\n`, а core Activity.Validate отклонял whitespace text. Минимальная правка сохраняет любой непустой assistant_text_delta. ThinkingSummary/status, empty string, metadata и capability checks остаются прежними.

Новый public test передаёт реальные chunks `# Heading`, `\n\n`, `word`, ` `, `next`, открытый JS fence, `console.log("ok")`, закрытый fence через NormalizeRuntimeActivity → ValidateFor → LocalStore outbox → authenticated WebSocket/ACK → core durable event ingest. Проверяются количество и точная конкатенация. Отдельный public validation test сохраняет отрицательные gates. Оба теста RED до исправления, GREEN после него.

Production/root/spec/auth/browser не менялись. Независимое ревью, integration merge и live browser verification выполняются отдельно; complete Codex MVP этим отчётом не объявляется.

Whitespace commit: `6bf4380219c39bf2622a86b78e0e6597053d2523` поверх `7adc71e`. `go test -race ./internal/core` PASS (27.7 сек), Node PASS (28.2 сек). Первый Node запуск встретил только untrusted own-worktree mise config при subprocess build; повтор с command-scoped `MISE_TRUSTED_CONFIG_PATHS` прошёл без глобальных изменений. Worktree чистый, diff check PASS.
