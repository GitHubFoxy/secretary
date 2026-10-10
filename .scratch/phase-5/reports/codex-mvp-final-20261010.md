# Codex Minimal MVP — 10 октября 2026

Текущий статус: **Codex MVP PASS**. Пользователь явно отложил Claude Code; этот gate относится только к Codex.

Рабочий сервер: [Secretary](https://omarchy.tail089ef.ts.net/), приватный HTTPS в Tailscale. Secretary и co-located Node работают на omarchy; remote Node `macbook-codex` подключается с Mac по исходящему HTTPS. Оба используют собственную локальную авторизацию Codex.

| Сценарий | Результат | Свидетельство |
| --- | --- | --- |
| Native Secretary из Telegram General | PASS | General203: один canonical reply в Core, Web и Telegram |
| Native Secretary из Web | PASS | Web233: один paragraph и один Telegram bubble7638 |
| Secretary сам создаёт local/remote Codex Workers | PASS | Native MCP list_nodes + два spawn_worker223; точные Task/Attempt/Result bindings |
| Profile и Node-local MCP | PASS | Workers221/222 вернули разные фактические Node markers и общий developer profile213 |
| Direct topic input и same-Attempt steering | PASS | Omarchy225 и Mac229: observed own sleep, injected input, изменённый Result текущей Attempt |
| Durable `/q`, FIFO, отсутствие ранней delivery | PASS | Omarchy226/227, Mac230/231: pending до terminal, последовательные отдельные Follow-ups с прежней session |
| Idle/restart resume на обеих машинах | PASS | Restart43 без autoinput; explicit236/237 вернули прежний context/native identity |
| Mac reboot и outbound auto-start | PASS | LaunchAgent сохранил Node identity, native216/220 продолжил прежнюю session |
| Queue restart и active crash без autoretry | PASS | Отдельная настоящая Mac матрица89e3135; старые FAIL и исправления сохранены |
| Topic mappings и Results без дублей | PASS |36 mappings сохранены; direct Worker inputs не запускали Secretary автоматически |
| Длинный Markdown/code | PASS | Native234:75 строк, по2 chunks в topic/General, весь fenced JS без повреждения |
| Понятный failed/interrupted terminal | PASS | General201/202: видимая ошибка обоих каналов, исходный input не повторён |
| Offline/auth/unsupported/empty/missing session | PASS с границами | Public/scoped negative tests и actual missing-session/crash матрица; invalid auth не выдумывается как удачный native turn |
| Readable Web Worker activity | PASS | Native Web Message238:40 deltas, включая whitespace-only; concat точно равен Result156bytes, один DOM block с h1/strong/pre/code |
| Обычные defaults после удаления тестовой конфигурации | PASS | Модельный General240 создал новых Workers241/242: native pwd на обеих машинах, обычный profile222bytes и пустые Node MCP lists |

Подробности и identities: [развёртывание](codex-deployment-20261010.md), [живой Web/Telegram](codex-telegram-acceptance-20261010.md), [Mac native и recovery](mac-native-acceptance-20261010.md). Fixtures и reviews не выдаются за вызовы настоящей модели или отправку Telegram.

Исторические Workers, bindings, topics, данные и native auth сохраняются. Пользовательские изменения `spec/THE spec.md` и `spec/memory.md` не входят в commits агента.

Claude Code ticket02 остаётся отложенным и не считается пройденным. Старые требования обоих harness не объявляются выполненными текущим Codex gate.

Финальная исполняемая сборка: `af1290a15bae1b345c20654e26143fc0dab859ec`; source SHA256 `8eb3e657f6fb9e03134c2c6f044979cbfec1f4ec3708577d12a924bd99a117b5`. Manifest: `/Users/beruseruko/.local/share/secretary/phase5-builds/release-af1290a-giyoajv3/manifest.json`. Full Go `-p1`, Web19 tests и production build — PASS. Исходный параллельный Go run имел3s timeout существующего CC test под нагрузкой; повтор выполнен на том же immutable source, без изменений CC.

Итог cleanup: шесть собственных acceptance Workers закрыты штатным API; их Results/topics сохранены. Всего41 Worker, активных Attempts/Secretary turns0. Все35 исторических Workers и32 исторические topics неизменны; теперь38 mappings. SQLite quick_checkok, оба Node outbox0, оба Nodes online, services enabled, обычные Codex defaults сохранены. Career браузер оставлен для ручной проверки.
