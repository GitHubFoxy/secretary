# 33 Автоматически создавать отдельное persistent native state OpenCode

Type: task
Status: claimed

## Work

Пользователь подтвердил архитектуру `Secretary/Worker → adapter → любой поддерживаемый harness`. Для новых пользователей setup должен создавать новую отдельную OpenCode DB автоматически, а не подключать обычную пользовательскую DB. Требование внесено в `docs/configuration.md` и `docs/node-deployment.md`.

В ticket31 native fixture проходил с чистым data store, но существующая OpenCode DB на omarchy не публиковала managed mode. Смена cache/config-content и private directory с symlink на ту же DB не помогли. Owner явно разрешил удалить старую DB; после закрытого SQLite backup/reset тот же synthetic MCP round-trip проходит в прежнем каталоге, включая повторные запуски. Причина внутри native DB state пока не установлена; не объявлять DB повреждённой без доказательств. После нового login authenticated Secretary MCP тоже прошёл. При этом live Worker read/Resume выявил missing custom managed mode на восстановлении, а unpinned provider-default — authentication error. Создание новой DB само по себе не устраняет эти runtime blockers; ticket31 остаётся claimed.

## Требование

- First setup автоматически выделяет отдельное native data directory и создаёт DB для установки Secretary/Execution Node. Не переписывать обычную `~/.local/share/opencode` пользователя.
- Server domain state остаётся source of truth; native history/session IDs принадлежат runtime adapter. Не делать harness-specific DB частью Worker envelope.
- Native state сохраняется при restart, reconnect, upgrade, новой Attempt и Follow-up. Не пересоздавать DB на каждый процесс, prompt или Attempt.
- Отдельные Nodes/установки не разделяют native state. Определить стабильные директории для Secretary runtime и Worker runtime на Node; старые fx bindings остаются прежними.
- Runtime, probes, setup/doctor и provider login используют согласованный выбранный data directory. Нельзя проверять auth в одной DB, а запускать агента в другой.
- Provider login предлагается явно в выбранном native store; никакого silent copying/import credentials из личной DB. Credentials не пишутся в prompt, config, CLI args, activity или diagnostic export.
- Защита filesystem: directories 0700, DB/auth files 0600. Учитывать SQLite WAL/SHM, процессы с открытым DB и безопасный backup/restore.
- Existing managed native sessions не переносить в пустую DB молча. Migration требует backup, согласованного плана и сохранения mapping/runtime session IDs либо видимой ошибки unavailable.

## Acceptance

- Чистая установка на машине с уже используемым OpenCode создаёт собственную DB; пользовательская DB/config/history не меняются.
- Повторный setup и restart не создают новую DB и не сбрасывают auth/history.
- Secretary managed mode, выбранная model/reasoning и узкий server-owned MCP проходят native round-trip в фактическом target store.
- Worker read/shell и terminal Result проверены; resume/Follow-up сохраняют session ID и history после рестарта.
- Missing auth или missing managed mode даёт видимую ошибку, без fallback на build/plan или другой harness/model.
- Regression покрывает existing user OpenCode DB, shared-state contamination, точный process environment и повторное открытие managed store.
- Docs точно отделяют implemented behaviour от требований. Ticket31 не закрывается только на основании synthetic clean-store проверки.

## Related

Ticket31: default OpenCode и текущий rollout. Tickets05b/13b/23: probes, setup/deployment и clean-machine E2E.

## Comments

### Актуализация перед реализацией ticket33

Текст выше сохранён как историческая запись состояния на момент диагностики; его не следует трактовать как текущий вывод. В текущей реализации OpenCode v2.0.22 ticket31 уже устраняет гонку регистрации managed mode ограниченным повтором точного выбора после публикации native catalog. Private Resume использует исходный session ID и проходит без сброса DB. Первоначальное наблюдение после owner-approved reset не доказывает повреждение базы.

Остаётся отдельный scope ticket33: startup/runtime и реальный inventory ещё не выбирают собственный persistent data home установки/Node. Нельзя снова переключать старые managed sessions на пустой store; до owner-approved migration требуется явно сохранять legacy store либо показывать блокирующее migration requirement. Provider auth не переносится из личного OpenCode store.

### Local implementation и acceptance update

Реализовано на `phase4-implementation`, без commit/review/deploy и без изменения служб, production config/state, bindings или auth:

- Secretary OpenCode store: `$HOME/.local/share/secretary/opencode-native`; local Worker Node: `$HOME/.local/share/secretary/node/data/opencode-native`; каждый remote Node использует свой `<NODE_DATA>/opencode-native`. `secretaryd` создаёт отдельные runtime/LocalNode instances для Secretary и Worker Node.
- Setup на isolated store только инициализирует native DB через `opencode auth list`; legacy store не открывается setup/Doctor/login. Explicit owner flows — `sex opencode login` и `sex node opencode login`. Runtime/probes/title generator/model inventory используют соответствующий selected store; runtime сохраняет exact legacy path для старых managed sessions до approval. Отказ от unselected store — fail-closed.
- Новые data dirs `0700`, native files создаются под `umask 077`; symlinked store/install root отклоняется. Repeat setup regression сохраняет DB/auth canary. OpenCode runtime, probes, title subprocess и explicit shell login очищают ambient provider credentials и user config/XDG env; тесты проверяют OpenAI/AWS/token sentinels.
- Старая Secretary `secretary.db` и legacy Node state без selection manifest закрепляют прежний `XDG_DATA_HOME` (или прежний `$HOME/.local/share`); старая server DB также переводит local Worker Node в legacy, если у неё ещё нет selection. Migration requirement виден и блокирует Doctor; setup не открывает legacy DB, login не перенаправляется в новый store, runtime сохраняет прежний путь/IDs. Нет DB/auth copy/reset или изменения native session ID.
- Обновлены `docs/configuration.md`, `docs/node-deployment.md`, `docs/quickstart.md`, `docs/phase4-release-gate.md` и phase4 map.

Проверки:

- `PASS`: `go test -count=1 ./internal/node ./internal/secretary ./internal/telegram ./cmd/secretaryd ./cmd/secretary-node`.
- `PASS`: `./scripts/sex-cli-test.sh` и `./scripts/node-deployment-test.sh`; повторный setup сохраняет DB/auth и личный-store canary.
- `PASS`: Linux test binary запускался по SSH на omarchy с OpenCode v2.0.22, все native store/Workspace файлы были в приватных `t.TempDir` под `/tmp`: `TestOpenCodeNativeProfilePersistence` (Worker + Secretary; Start/Follow-up/fresh-process Resume с прежним ID/history), `TestOpenCodeSecretaryMCPNativeHTTPFixture`, `TestOpenCodeNativeInventoryMissingAuthDoesNotFallback`. HTTP provider unpaid; последняя проверка подтвердила видимый unauthenticated status и неизменность personal-store canary.
- Полный `go test -p 1 ./...`, race, vet/build отложены для общего ticket batch по заданному порядку.

**Не закрыто:** owner login в выделенных Secretary и целевом Node store; authenticated native inventory/Worker live gate; production setup/restart; migration старых managed Node sessions. Никакие provider credentials не копировались и login от имени owner не выполнялся.
