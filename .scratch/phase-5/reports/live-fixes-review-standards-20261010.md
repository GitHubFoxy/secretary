## Проверка стандартов

Проверен фиксированный snapshot `372bee322f3d9d63dcedfbe2d8bac6e4d7aae787`, diff `d76c372...372bee3`, commits `b694f1e`, `c401bfd`, `6b09b9b`, `5f8cb84`, `72637fb`, `372bee3`. Readonly review; файлы repo и пользовательские изменения `spec/THE spec.md`, `spec/memory.md` не изменены.

**Новых findings нет:** hard violations документированных правил и обоснованных Fowler smells (judgement calls) не обнаружено. Проверены AGENTS, docs/agents, GLOSSARY, runtime-contracts и code-comment-policy; новых code comments в diff нет. `docs/adr/` отсутствует.

Все пять прежних findings закрыты непосредственно кодом:

- **Claude descendant Close:** `claude_runtime.go` создаёт собственную process group, однократно завершает её до `Cmd.Wait`, закрывает stdin/stdout, ограничивает stderr drain через `WaitDelay=250ms`, ждёт `done` максимум пять секунд. Lifetime cancellation использует тот же teardown; watcher превращает ошибку Close в failed Outcome. Владение PID сохраняется до Wait; repeated Close не повторяет сигнал.
- **Production owner routing:** `rootHandler` отправляет HTTP Node routes в webapi, сохраняя отдельный `/v1/nodes/connect`. `nodeDispatch` проверяет owner/client scopes, а admin/enrollment operations остаются у ServerManager. Owner cookie не даёт admin authority.
- **ACP MCP wire:** единый `acpMCPServers` применяется в Start и Resume; outer list, Args и Env кодируются массивами, входные slices копируются.
- **Durable failed receipts:** `CompleteCommand` сохраняет failed Dispatch/Resume и sequenced receipt одной записью LocalStore; Daemon flush/reconnect использует прежние identities, ACK отправляется после authenticated production sink. Pending dedup и completed command предотвращают повтор native Start/Resume; duplicate command возвращает прежний receipt.
- **Late activity:** watcher заканчивается после terminal Outcome. Server проверяет payload и точные Node/HarnessInstance/Worker/Turn/Attempt внутри lifecycle transaction, затем ACK/drop terminal replay. Пустой Event подавляет Approval handoff; неверные bindings/unknown Attempt отклоняются. Result и lifecycle не меняются, execution не повторяется.

Изучены public regressions для descendant shutdown/context cancellation, production owner/admin routes, ACP wire, Dispatch/Resume reconnect/ACK/dedup и terminal replay/permission/identity rejection. Полный Go test/build PASS и узкие race PASS предоставлены integration/implementer; повторный запуск без нерешённого подозрения не потребовался.

Это Standards review исправлений, а не закрытие live gate. Claude auth, steering, remote и Telegram acceptance blockers остаются открытыми.
